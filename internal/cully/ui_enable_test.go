package cully

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEnableSessionTrackingFromThePage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, _, _ := uiTestServer(t, false)
	claudeTracking := func() bool {
		for _, a := range uiSetup().Agents {
			if a.Agent == "claude" {
				return a.Items[0].OK
			}
		}
		return false
	}
	if claudeTracking() {
		t.Fatal("tracking already on in a fresh config")
	}
	if r := uiDo(t, "POST", srv.URL+"/api/setup/claude/enable", nil, nil); r.StatusCode != 401 {
		t.Fatalf("without a token: %d", r.StatusCode)
	}
	noHeader := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+uiTestToken) }
	if r := uiDo(t, "POST", srv.URL+"/api/setup/claude/enable", nil, noHeader); r.StatusCode != 403 {
		t.Fatalf("without X-Cully-UI: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/setup/emacs/enable", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	r := uiDo(t, "POST", srv.URL+"/api/setup/claude/enable", nil, uiAuthed)
	if r.StatusCode != 200 {
		t.Fatalf("enable: %d", r.StatusCode)
	}
	var out struct{ Message string }
	json.NewDecoder(r.Body).Decode(&out)
	if !strings.Contains(out.Message, "Claude") {
		t.Fatalf("message = %q", out.Message)
	}
	if !claudeTracking() {
		t.Fatal("session tracking is still off after enabling it")
	}
}

func TestStartTheAdvisorFromThePage(t *testing.T) {
	calls := 0
	old := uiStartAdvisor
	uiStartAdvisor = func() error { calls++; return nil }
	t.Cleanup(func() { uiStartAdvisor = old })
	srv, _, _ := uiTestServer(t, false)
	if r := uiDo(t, "POST", srv.URL+"/api/setup/advisor/start", nil, nil); r.StatusCode != 401 {
		t.Fatalf("without a token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/setup/advisor/start", nil, uiAuthed); r.StatusCode != 200 || calls != 1 {
		t.Fatalf("start: %d calls=%d", r.StatusCode, calls)
	}
	uiStartAdvisor = func() error { return http.ErrAbortHandler }
	if r := uiDo(t, "POST", srv.URL+"/api/setup/advisor/start", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("a failed start should say so: %d", r.StatusCode)
	}
}

func TestConnectTheCullyMCPFromThePage(t *testing.T) {
	var gotAgent, gotURL string
	old := uiAddMCP
	uiAddMCP = func(agent, url string) error { gotAgent, gotURL = agent, url; return nil }
	t.Cleanup(func() { uiAddMCP = old })
	srv, _, _ := uiTestServer(t, false)
	body := map[string]string{"url": "https://mcp.example.org/cully/mcp"}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/connect", body, nil); r.StatusCode != 401 {
		t.Fatalf("without a token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/emacs/connect", body, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/connect", map[string]string{"url": "not a url"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("an invalid address: %d (the real validator runs only through AddMCP)", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/connect", body, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("connect: %d", r.StatusCode)
	}
	if gotAgent != "claude" || gotURL != "https://mcp.example.org/cully/mcp" {
		t.Fatalf("AddMCP got %q %q", gotAgent, gotURL)
	}
}

func TestAppOffersNotificationsAndEnableButtons(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"Notification", "isSecureContext", "/api/setup/", "/enable", "advisor/start", "/connect", "cully-notify"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js lacks %q", want)
		}
	}
}

func TestStartAScoutFromThePage(t *testing.T) {
	release := make(chan struct{})
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.ScoutStream = func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
		progress("Searching: " + "go flaky tests")
		<-release
		return "Use the race detector. https://go.dev/doc/articles/race_detector", nil
	}
	v, err := m.startScout("go flaky tests in CI")
	if err != nil || v.ID == "" || v.Status != "running" {
		t.Fatalf("start = %+v, %v", v, err)
	}
	uiWaitFor(t, "live progress", func() bool { r := m.scoutRuns(); return len(r) == 1 && len(r[0].Log) == 1 })
	if r := m.scoutRuns()[0]; r.Query != "go flaky tests in CI" || r.Status != "running" || r.Log[0] != "Searching: go flaky tests" {
		t.Fatalf("run while working = %+v", r)
	}
	close(release)
	uiWaitFor(t, "findings", func() bool { return m.scoutRuns()[0].Status == "ok" })
	if r := m.scoutRuns()[0]; !strings.Contains(r.Output, "race detector") {
		t.Fatalf("findings = %q", r.Output)
	}
}

func TestScoutRunsAreValidatedAndLimited(t *testing.T) {
	block := make(chan struct{})
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	// Runs after the test, before its temporary config folder is put back: the
	// blocked scouts must finish while they can still only write there.
	t.Cleanup(func() {
		close(block)
		uiWaitFor(t, "the scouts to finish", func() bool {
			for _, r := range m.scoutRuns() {
				if r.Status == "running" {
					return false
				}
			}
			return true
		})
		uiWaitFor(t, "the scouts to be recorded", func() bool { return len(m.researchList("scout", 10)) >= uiScoutRunsMax })
	})
	m.cfg.ScoutStream = func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
		<-block
		return "x", nil
	}
	for _, bad := range []string{"", "   ", strings.Repeat("q", uiScoutQueryMax+1)} {
		if _, err := m.startScout(bad); err == nil {
			t.Fatalf("query %.20q accepted", bad)
		}
	}
	for i := 0; i < uiScoutRunsMax; i++ {
		if _, err := m.startScout("topic " + itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.startScout("one too many"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
}

func TestScoutEndpoints(t *testing.T) {
	srv, m, _ := uiTestServer(t, false)
	m.cfg.ScoutStream = func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
		return "Findings here.", nil
	}
	body := map[string]string{"query": "rate limiting patterns"}
	if r := uiDo(t, "POST", srv.URL+"/api/scout/run", body, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/scout/run", map[string]string{"query": ""}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("empty query: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/scout/run", body, uiAuthed); r.StatusCode != 202 {
		t.Fatalf("run: %d", r.StatusCode)
	}
	uiWaitFor(t, "run finished", func() bool {
		var runs []uiScoutRunView
		json.NewDecoder(uiDo(t, "GET", srv.URL+"/api/scout/runs", nil, uiAuthed).Body).Decode(&runs)
		return len(runs) == 1 && runs[0].Status == "ok" && runs[0].Output == "Findings here."
	})
}

func TestJobsListedAndRunFromThePage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := os.MkdirAll(filepath.Dir(jobsConfigFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"jobs": [{"name": "nightly-audit", "command": ["python3", "/x/run.py", "--secret-flag"], "after": "22:30"}]}`
	if err := os.WriteFile(jobsConfigFile(), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	jobs := uiJobs()
	if len(jobs) != 1 || jobs[0].Name != "nightly-audit" || jobs[0].After != "22:30" || jobs[0].Program != "python3" {
		t.Fatalf("jobs = %+v", jobs)
	}
	raw, _ := json.Marshal(jobs)
	if strings.Contains(string(raw), "secret-flag") || strings.Contains(string(raw), "run.py") {
		t.Fatalf("job arguments reached the page: %s", raw)
	}
	var started string
	old := uiStartJob
	uiStartJob = func(name string) error { started = name; return nil }
	t.Cleanup(func() { uiStartJob = old })
	srv, _, _ := uiTestServer(t, false)
	if r := uiDo(t, "POST", srv.URL+"/api/jobs/nightly-audit/run", nil, uiAuthed); r.StatusCode != 202 || started != "nightly-audit" {
		t.Fatalf("run: %d started=%q", r.StatusCode, started)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs/unknown/run", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown job: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/jobs", nil, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("list: %d", r.StatusCode)
	}
}

// The page is opened in the context of one coding agent. Nothing about the
// others is sent to it, so it cannot be shown.

func uiAgentNames(raw []byte, key string) []string {
	var list []map[string]any
	json.Unmarshal(raw, &list)
	var out []string
	for _, a := range list {
		if s, ok := a[key].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestSetupOnlyDescribesTheAgentsInUse(t *testing.T) {
	srv, m, _ := uiTestServer(t, false)
	get := func() uiSetupView {
		var v uiSetupView
		json.NewDecoder(uiDo(t, "GET", srv.URL+"/api/setup", nil, uiAuthed).Body).Decode(&v)
		return v
	}
	v := get()
	if len(v.Agents) != 1 || v.Agents[0].Agent != "claude" {
		t.Fatalf("by default only Claude is described: %+v", v.Agents)
	}
	m.mcp.inUse = map[string]struct{}{}
	m.mcp.markInUse("cursor")
	v = get()
	if len(v.Agents) != 1 || v.Agents[0].Agent != "cursor" {
		t.Fatalf("opened through Cursor: %+v", v.Agents)
	}
	raw, _ := json.Marshal(v)
	for _, other := range []string{"Claude", "Codex", "claude", "codex"} {
		if strings.Contains(string(raw), other) {
			t.Fatalf("a Cursor page mentions %q: %s", other, raw)
		}
	}
}

func TestSignInStatusOnlyListsAgentsInUseAndOnlyProbesThem(t *testing.T) {
	oldProbe, oldInstalled := agentMCPProbe, uiAgentInstalled
	t.Cleanup(func() { agentMCPProbe, uiAgentInstalled = oldProbe, oldInstalled })
	uiAgentInstalled = func(string) bool { return true }
	var probed sync.Map
	agentMCPProbe = func(ctx context.Context, agent string) (string, string, bool) {
		probed.Store(agent, true)
		return "unhealthy", "unauthenticated", true
	}
	srv, _, _ := uiTestServer(t, false)
	r := uiDo(t, "GET", srv.URL+"/api/mcp", nil, uiAuthed)
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	names := uiAgentNames(buf.Bytes(), "agent")
	if len(names) != 1 || names[0] != "claude" {
		t.Fatalf("agents = %v", names)
	}
	for _, other := range []string{"codex", "cursor"} {
		if _, ok := probed.Load(other); ok {
			t.Fatalf("%s was probed although nobody uses it", other)
		}
	}
}

func TestSessionsEndpointOnlyListsAgentsInUse(t *testing.T) {
	repo := uiNewBranchRepo(t, "dev")
	now := time.Now()
	uiFakeProcs(t, []uiProc{
		{Interactive: true, PID: 1, PPID: 9, Cmd: []string{"claude"}, Cwd: repo, Started: now},
		{Interactive: true, PID: 2, PPID: 9, Cmd: []string{"codex"}, Cwd: repo, Started: now},
		{Detached: true, PID: 3, PPID: 9, Cmd: []string{"/x/bin/agent", "/x/cursor-agent/index.js"}, Cwd: repo, Started: now},
	})
	srv, _, _ := uiTestServer(t, false)
	var buf bytes.Buffer
	buf.ReadFrom(uiDo(t, "GET", srv.URL+"/api/sessions", nil, uiAuthed).Body)
	names := uiAgentNames(buf.Bytes(), "agent")
	if len(names) != 1 || names[0] != "claude" {
		t.Fatalf("sessions = %v", names)
	}
}

func TestAppDoesNotOfferToRevealOtherAgents(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, leak := range []string{"Show other agents", "Show sessions from other agents", "showOtherSessions", "showOthers"} {
		if strings.Contains(string(js), leak) {
			t.Fatalf("app.js still lets the page reveal other agents: %q", leak)
		}
	}
}
