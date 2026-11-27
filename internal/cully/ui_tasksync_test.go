package cully

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type uiToolCallLog struct{ tool, args string }

func uiCaptureCullyCalls(t *testing.T, connected bool) *[]uiToolCallLog {
	t.Helper()
	var calls []uiToolCallLog
	savedCall, savedConn := uiCullyToolCall, uiCullyMCPConnected
	uiCullyToolCall = func(_, tool, args string) error { calls = append(calls, uiToolCallLog{tool, args}); return nil }
	uiCullyMCPConnected = func(*uiManager) bool { return connected }
	t.Cleanup(func() { uiCullyToolCall, uiCullyMCPConnected = savedCall, savedConn })
	return &calls
}

func uiSettledTask(id string, sessions ...uiTaskSession) uiTaskRecord {
	return uiTaskRecord{ID: id, Title: "Team intent flow", Repo: "/p/x", Branch: "cully/team-intent", State: uiDone, Created: time.Now().Add(-3 * time.Hour), Updated: time.Now().Add(-time.Hour),
		Prompt: "Add the flow", Last: "Unit tests pass; QA outstanding.", Files: 4, Runs: 2, Sessions: sessions}
}

func TestASettledTaskIsSavedToMemoryWithEverySessionAndOneSummary(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	calls := uiCaptureCullyCalls(t, true)
	rec := uiSettledTask("task1",
		uiTaskSession{ID: "01a1f365-7f91-77a3-b4c5-46d6ac7309e0", Agent: "cursor", Role: "origin"},
		uiTaskSession{ID: "c2016c6b-76c9-4297-9a2b-50b5f3154641", Agent: "claude", Role: "run"})
	uiDB().put("task", rec.ID, rec)
	if n := m.syncTasks(); n != 1 {
		t.Fatalf("saved %d", n)
	}
	var sessions, logs int
	ref := regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}-[0-9a-f]{16}$`)
	for _, c := range *calls {
		var a map[string]any
		_ = json.Unmarshal([]byte(c.args), &a)
		switch c.tool {
		case "cully_session":
			sessions++
			if !ref.MatchString(a["session_ref"].(string)) || a["task"] != "Team intent flow" || a["agent_session_id"] == "" {
				t.Fatalf("session call = %s", c.args)
			}
		case "cully_log":
			logs++
			sum := a["summary"].(string)
			if !strings.Contains(sum, "2 sessions (Claude, Cursor)") || !strings.Contains(sum, "4 files changed") || a["entry_type"] != "work" || !ref.MatchString(a["session_ref"].(string)) {
				t.Fatalf("log call = %s", c.args)
			}
		}
	}
	if sessions != 2 || logs != 1 {
		t.Fatalf("calls: %d sessions, %d summaries", sessions, logs)
	}
	var got uiTaskRecord
	uiDB().get("task", "task1", &got)
	if got.Entries != 1 || got.SyncedAt.IsZero() || got.Synced == "" {
		t.Fatalf("record = %+v", got)
	}
	// Unchanged, so not saved again; changed, so saved again.
	*calls = nil
	if n := m.syncTasks(); n != 0 || len(*calls) != 0 {
		t.Fatalf("an unchanged task was saved again: %d %v", n, *calls)
	}
	got.Files = 9
	uiDB().put("task", "task1", got)
	if n := m.syncTasks(); n != 1 {
		t.Fatalf("a changed task was not saved: %d", n)
	}
}

func TestOnlySettledTasksAreSavedAndNothingIsSentWithoutTheMCP(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	calls := uiCaptureCullyCalls(t, true)
	working := uiSettledTask("w1")
	working.State, working.Updated = uiWorking, time.Now()
	fresh := uiSettledTask("f1")
	fresh.Updated = time.Now() // finished a moment ago: wait for it to settle
	waiting := uiSettledTask("a1")
	waiting.State, waiting.Updated = uiWaiting, time.Now().Add(-10*time.Minute) // waiting on you, but not for long
	for _, r := range []uiTaskRecord{working, fresh, waiting} {
		uiDB().put("task", r.ID, r)
	}
	if n := m.syncTasks(); n != 0 || len(*calls) != 0 {
		t.Fatalf("an unsettled task was saved: %d %v", n, *calls)
	}
	idle := uiSettledTask("i1")
	idle.State, idle.Updated = uiWaiting, time.Now().Add(-2*time.Hour)
	uiDB().put("task", idle.ID, idle)
	uiCullyMCPConnected = func(*uiManager) bool { return false }
	if n := m.syncTasks(); n != 0 || len(*calls) != 0 {
		t.Fatalf("something was sent with no MCP: %d", n)
	}
	uiCullyMCPConnected = func(*uiManager) bool { return true }
	if n := m.syncTasks(); n != 1 {
		t.Fatalf("a task idle for two hours should be saved: %d", n)
	}
}

func TestTheTaskSummaryHoldsNoSecretsPathsOrPromptsBeyondTheAsk(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	rec := uiSettledTask("task2", uiTaskSession{ID: "c2016c6b-76c9-4297-9a2b-50b5f3154641", Agent: "claude", Role: "run"})
	rec.Repo, rec.Prompt, rec.Last = repo, "Fix it with tok-aaa1 and password=hunter2hunter2", "Done at "+repo
	sum, outcome, _ := m.taskSummary(rec)
	for _, bad := range []string{"tok-aaa1", "hunter2hunter2"} {
		if strings.Contains(sum+outcome, bad) {
			t.Errorf("the summary contains %q", bad)
		}
	}
	if !strings.Contains(sum, "Task: Team intent flow") {
		t.Fatalf("summary = %q", sum)
	}
}

func TestClosingATaskStopsItKeepsItAndContinuingReopensIt(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "tidy the notes"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	if err := m.taskClose("nope", true); err == nil {
		t.Fatal("closed a task that does not exist")
	}
	if err := m.taskClose(a.id, true); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the run to stop", func() bool { return a.snapshot().State == "stopped" })
	var rec uiTaskRecord
	uiDB().get("task", a.id, &rec)
	if !rec.Closed || rec.ClosedAt.IsZero() || len(m.taskList("all")) != 1 {
		t.Fatalf("closed task = %+v", rec)
	}
	b, err := m.taskContinue(a.id, "one more thing")
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, b, "waiting")
	uiWaitFor(t, "the task to reopen", func() bool {
		var r uiTaskRecord
		uiDB().get("task", a.id, &r)
		return !r.Closed
	})
}

func TestAnalysedSessionsOutsideTasksDripIntoMemoryOnceEach(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	calls := uiCaptureCullyCalls(t, true)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	for _, id := range []string{uiTestSessA, uiTestSessB, uiTestSessC} {
		uiWriteRichTranscript(t, dir, id, repo)
	}
	m.cfg.Advisor = func(_ string, _ string, _ bool, _ string) (string, error) {
		return "TITLE: Fix the login bug\nSUMMARY: Fixed login and logout.\nDIFFERENTLY: Write a regression test first\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: Fix login\nCHECKS: login works", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	for i := 0; i < 3; i++ {
		m.analyzeNext()
	}
	setUpdated := func(id string, at time.Time) {
		m.idxMu.Lock()
		m.loadIndexLocked()
		m.index.Sessions[id].Updated = at
		m.saveIndexLocked()
		m.idxMu.Unlock()
	}
	// A session still in use is left alone until it has been quiet a while.
	setUpdated(uiTestSessA, time.Now())
	if n := m.syncSessions(); n != 2 {
		t.Fatalf("the open session should wait, the other two be saved: %d", n)
	}
	setUpdated(uiTestSessA, time.Now().Add(-time.Hour))
	if n := m.syncSessions(); n != 1 {
		t.Fatalf("the session that went quiet should now be saved: %d", n)
	}
	for _, c := range *calls {
		var a map[string]any
		_ = json.Unmarshal([]byte(c.args), &a)
		if c.tool != "cully_log" || a["entry_type"] != "work" || a["agent_session_id"] == nil || !strings.Contains(a["summary"].(string), "Fix the login bug") || strings.Contains(c.args, "SECRET") || strings.Contains(c.args, repo) {
			t.Fatalf("call = %s", c.args)
		}
	}
	if len(*calls) != 3 {
		t.Fatalf("calls = %d", len(*calls))
	}
	*calls = nil
	if n := m.syncSessions(); n != 0 || len(*calls) != 0 {
		t.Fatalf("a session was saved twice: %d", n)
	}
}

func TestEveryOutboundFieldIsCleanedAndASecretBlocksTheCall(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	calls := uiCaptureCullyCalls(t, true)
	rec := uiSettledTask("task9", uiTaskSession{ID: "c2016c6b-76c9-4297-9a2b-50b5f3154641", Agent: "claude", Role: "run"})
	rec.Title, rec.Branch = "Fix login with tok-aaa1", "cully/use-password=hunter2hunter2-now"
	uiDB().put("task", rec.ID, rec)
	if n := m.syncTasks(); n != 1 {
		t.Fatalf("saved %d", n)
	}
	for _, c := range *calls {
		for _, bad := range []string{"tok-aaa1", "hunter2hunter2"} {
			if strings.Contains(c.args, bad) {
				t.Errorf("%s call holds %q: %s", c.tool, bad, c.args)
			}
		}
	}
	// The last guard refuses a call that still holds a secret, whatever put it there.
	if m.outboundOK(`{"summary":"x tok-aaa1 y"}`) || m.outboundOK(`{"summary":"password=abcdefgh12345"}`) || !m.outboundOK(`{"summary":"fine"}`) {
		t.Fatal("the outbound guard is wrong")
	}
}
