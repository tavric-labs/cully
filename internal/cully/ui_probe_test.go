package cully

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProbeComparisonReportsDriftOnlyForMissingFlags(t *testing.T) {
	var all strings.Builder
	for _, n := range uiClaudeNeeds {
		all.WriteString("  " + n.Flag + " <value>  x\n")
	}
	needs := uiProbeNeeds("claude")
	if d, read := uiProbeCompare(needs, map[string]string{"": all.String()}); !read || len(d) != 0 {
		t.Fatalf("complete help: read=%v drift=%+v", read, d)
	}
	help := strings.ReplaceAll(all.String(), "--fork-session", "--fork-sessions")
	d, _ := uiProbeCompare(needs, map[string]string{"": help})
	if len(d) != 1 || d[0].Flag != "--fork-session" || d[0].Cap != "Fork a session" {
		t.Fatalf("a renamed flag must be reported once: %+v", d)
	}
	if d, read := uiProbeCompare(needs, map[string]string{"": ""}); read || len(d) != 0 {
		t.Fatalf("unreadable help is not drift: %v %+v", read, d)
	}
}

func TestProbeComparisonReadsCommandsAndSubcommandHelp(t *testing.T) {
	needs := uiProbeNeeds("codex")
	root := "Commands:\n  exec    Run\n  resume  Resume\n  fork    Fork\nOptions:\n  -s, --sandbox <M>\n"
	exec := "  --sandbox <M>\n  --ephemeral\n"
	d, _ := uiProbeCompare(needs, map[string]string{"": root, "exec": exec})
	if len(d) != 1 || d[0].Flag != "--skip-git-repo-check" {
		t.Fatalf("drift = %+v", d)
	}
	d, _ = uiProbeCompare(needs, map[string]string{"": strings.Replace(root, "  fork    Fork\n", "", 1), "exec": exec + "--skip-git-repo-check\n"})
	if len(d) != 1 || d[0].Flag != "fork" {
		t.Fatalf("drift = %+v", d)
	}
}

func TestProbeRunsInstalledHarnessesAndCachesForADay(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	saved, savedV := uiProbeHelp, uiHarnessVersion
	calls := 0
	uiProbeHelp = func(agent string, sub []string) (string, bool) {
		calls++
		if agent == "cursor" {
			return "", false
		}
		return "  --print\n", true
	}
	uiHarnessVersion = func(string) string { return "1.0" }
	t.Cleanup(func() { uiProbeHelp, uiHarnessVersion = saved, savedV })
	r := m.probeResult()
	if len(r.Harnesses) != 3 || r.Harnesses[2].Installed || !r.Harnesses[0].Installed || len(r.Harnesses[0].Drift) == 0 {
		t.Fatalf("result = %+v", r)
	}
	n := calls
	m.probeResult()
	if calls != n {
		t.Fatal("probed again within a day")
	}
	r.At = time.Now().Add(-25 * time.Hour).Unix()
	uiDB().put("probe", "last", r)
	m.probeResult()
	if calls == n {
		t.Fatal("did not re-probe after a day")
	}
	m.runProbe()
	var back uiProbeResult
	if !uiDB().get("probe", "last", &back) || back.At == r.At {
		t.Fatal("on-demand probe was not stored")
	}
}

func TestProbeEndpoints(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	saved := uiProbeHelp
	uiProbeHelp = func(string, []string) (string, bool) { return "", false }
	t.Cleanup(func() { uiProbeHelp = saved })
	for _, c := range [][2]string{{"GET", "/api/harness/probe"}, {"POST", "/api/harness/probe/refresh"}} {
		res := uiDo(t, c[0], srv.URL+c[1], nil, uiAuthed)
		var r uiProbeResult
		if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&r) != nil || len(r.Harnesses) != 3 {
			t.Fatalf("%v: %d %+v", c, res.StatusCode, r)
		}
	}
}

func TestMatrixHasSteeringColumnsWithReasons(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	seen := map[string]uiCapability{}
	for _, c := range m.capabilities().List {
		seen[c.ID] = c
	}
	for _, id := range []string{"steering", "interrupt", "resume", "fork", "relay", "queue", "cost", "structured"} {
		c, ok := seen[id]
		if !ok || c.ClaudeWhy == "" || c.CodexWhy == "" || c.CursorWhy == "" {
			t.Fatalf("%s = %+v", id, c)
		}
	}
}

func TestPauseBlocksEveryStarterAndNeverStopsRunningTasks(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	var run []*uiAgent
	for i := 0; i < 2; i++ {
		a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
		if err != nil {
			t.Fatal(err)
		}
		run = append(run, a)
	}
	if _, err := m.enqueue(uiStartRequest{Repo: repo, Title: "later", Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	uiSetPaused(true)
	if !m.paused() {
		t.Fatal("not paused")
	}
	run[0].stop() // frees a slot; finishing a task would normally start the queued one
	uiWaitFor(t, "first task to end", func() bool { return run[0].snapshot().State != "working" && run[0].snapshot().State != "preparing" })
	m.drainQueue()
	time.Sleep(200 * time.Millisecond)
	if len(m.queueList()) != 1 {
		t.Fatal("queued task started while paused")
	}
	if st := run[1].snapshot().State; st == "done" || st == "stopped" || st == "failed" {
		t.Fatalf("a running task was touched: %s", st)
	}
	// Scheduled agents and automatic scouts do not start.
	in := uiIdentityInputFor(repo)
	if _, err := m.identityCreate(in); err != nil {
		t.Fatal(err)
	}
	m.identitiesDue()
	if len(m.identityList()[0].Runs) != 0 {
		t.Fatal("a scheduled agent started while paused")
	}
	if m.scoutIfDue() != 0 || m.analyzeNext() != 0 {
		t.Fatal("an automatic run started while paused")
	}
	uiSetPaused(false)
	m.identitiesDue()
	uiWaitFor(t, "scheduled agent after resume", func() bool { return len(m.identityList()[0].Runs) == 1 })
	run[1].stop()
	uiWaitFor(t, "queued task after resume", func() bool { m.drainQueue(); return len(m.queueList()) == 0 })
}

func TestManualStartWhilePausedAsksFirst(t *testing.T) {
	srv, _, repo := uiTestServer(t, false)
	uiSetPaused(true)
	body := map[string]any{"repo": repo, "prompt": "go"}
	res := uiDo(t, "POST", srv.URL+"/api/agents", body, uiAuthed)
	var e struct {
		Error  string `json:"error"`
		Paused bool   `json:"paused"`
	}
	_ = json.NewDecoder(res.Body).Decode(&e)
	if res.StatusCode != 409 || !e.Paused || e.Error != "New work is paused. Start anyway?" {
		t.Fatalf("%d %+v", res.StatusCode, e)
	}
	if res := uiDo(t, "POST", srv.URL+"/api/agents?anyway=1", body, uiAuthed); res.StatusCode != 201 {
		t.Fatalf("anyway: %d", res.StatusCode)
	}
}

func TestPauseEndpointSetsAndReadsTheSwitch(t *testing.T) {
	srv, m, _ := uiTestServer(t, false)
	res := uiDo(t, "POST", srv.URL+"/api/pause", map[string]bool{"paused": true}, uiAuthed)
	var r struct{ Paused bool }
	_ = json.NewDecoder(res.Body).Decode(&r)
	if res.StatusCode != 200 || !r.Paused || !m.paused() {
		t.Fatalf("%d %+v", res.StatusCode, r)
	}
	res = uiDo(t, "GET", srv.URL+"/api/pause", nil, uiAuthed)
	r.Paused = false
	_ = json.NewDecoder(res.Body).Decode(&r)
	if !r.Paused {
		t.Fatal("GET did not report paused")
	}
}
