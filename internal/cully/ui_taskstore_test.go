package cully

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTasksAreKeptInTheDatabaseAndSurviveARestart(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "please tidy the notes folder. It is a mess"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	uiWaitFor(t, "the task to be saved", func() bool {
		l := m.taskList("today")
		return len(l) == 1 && l[0].State == "waiting"
	})
	l := m.taskList("today")
	if l[0].ID != a.id || l[0].Title != "Tidy the notes folder" || l[0].Runs != 1 || l[0].Repo == "" {
		t.Fatalf("record = %+v", l[0])
	}
	// A new server (a restart) still has it, and it is in this week's list too.
	again := newUIManager(m.cfg)
	if got := again.taskList("today"); len(got) != 1 || got[0].ID != a.id {
		t.Fatalf("after a restart: %+v", got)
	}
	if got := again.taskList("week"); len(got) != 1 {
		t.Fatalf("week = %+v", got)
	}
	// An old task is in neither, only in the long list.
	old := l[0]
	old.ID, old.Updated = "oldtask", time.Now().AddDate(0, 0, -20)
	uiDB().put("task", old.ID, old)
	if len(again.taskList("week")) != 1 || len(again.taskList("all")) != 2 {
		t.Fatalf("periods: week %d all %d", len(again.taskList("week")), len(again.taskList("all")))
	}
	m.cfg.HideSessionsIn = []string{repo}
	if len(m.taskList("all")) != 0 {
		t.Fatal("a task in a hidden folder was listed")
	}
}

func TestContinuingATaskFromAnotherHarnessRunsInTheSameRecordWithItsHandoff(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	rec := uiTaskRecord{ID: "task1", Title: "Team intent flow", Repo: repo, State: "done", Created: time.Now().Add(-time.Hour), Updated: time.Now().Add(-time.Hour), Prompt: "Add the flow", Last: "Unit tests pass; end-to-end QA is outstanding.", Files: 4, Runs: 1,
		Sessions: []uiTaskSession{{ID: "01a1f365-aaaa", Agent: "cursor", Role: "origin"}}}
	uiDB().put("task", rec.ID, rec)
	a, err := m.taskContinue("task1", "run the end-to-end QA")
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	if a.taskID() != "task1" {
		t.Fatalf("the run belongs to %q", a.taskID())
	}
	uiWaitFor(t, "the record to count the second run", func() bool {
		l := m.taskList("today")
		return len(l) == 1 && l[0].Runs == 2 && l[0].ID == "task1"
	})
	got := m.taskList("today")[0]
	if len(got.Sessions) == 0 || got.Sessions[0].Agent != "cursor" || got.Sessions[0].Role != "origin" {
		t.Fatalf("the origin session was lost: %+v", got.Sessions)
	}
	sent := a.req.Prompt
	if !strings.Contains(sent, "An earlier Cursor session worked on it") || !strings.Contains(sent, "end-to-end QA is outstanding") || !strings.Contains(sent, "run the end-to-end QA") {
		t.Fatalf("the handoff was thin: %q", sent)
	}
	_ = out
	if _, err := m.taskContinue("task1", ""); err == nil {
		t.Fatal("a running task was started again")
	}
	if _, err := m.taskContinue("nope", ""); err == nil {
		t.Fatal("an unknown task was continued")
	}
}

func TestTheTaskReportIsSelfContainedEscapedAndHoldsNoSecretsOrPaths(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Title: `Fix <script>alert(1)</script> & more`, Prompt: "Use tok-aaa1 to fix the login <b>bug</b>"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	a.handleLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"p1","name":"TodoWrite","input":{"todos":[{"content":"Find the bug","status":"completed"},{"content":"Fix it","status":"in_progress"}]}}]}}`))
	s := &uiServer{m: m}
	req := httptest.NewRequest("GET", "/api/agents/"+a.id+"/report?download=1", nil)
	req.SetPathValue("id", a.id)
	rec := httptest.NewRecorder()
	s.reportGet(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	for _, bad := range []string{"<script", "tok-aaa1", repo, "<b>bug</b>"} {
		if strings.Contains(body, bad) {
			t.Errorf("the report contains %q", bad)
		}
	}
	for _, want := range []string{"Fix &lt;script&gt;", "Find the bug", "1 of 2 done", filepath.Base(repo), "Made with Cully"} {
		if !strings.Contains(body, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Errorf("csp = %q", csp)
	}
}
