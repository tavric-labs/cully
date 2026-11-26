package cully

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

type uiAdviceAdvisor struct {
	mu      sync.Mutex
	prompts []string
	cwds    []string
	out     string
}

func (f *uiAdviceAdvisor) run(agent, cwd string, research bool, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	f.cwds = append(f.cwds, cwd)
	return f.out, nil
}

func uiWaitAdvice(t *testing.T, a *uiAgent, n int) []uiAdvice {
	t.Helper()
	var got []uiAdvice
	uiWaitFor(t, "advice", func() bool { got = a.adviceList(); return len(got) >= n })
	return got
}

func TestAdviseProducesLeveledAdviceForTheAgent(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.NoConflictAlerts = true
	adv := &uiAdviceAdvisor{out: "WARN|Tests keep failing, so run them before editing more.\nCAUT|You edited gateway.go twice without checking it.\nADV|Summarize what changed when you finish.\nADV|A fourth line is dropped."}
	m.cfg.Advisor = adv.run
	a, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix login", Prompt: "make login stricter"})
	uiWaitState(t, a, "waiting")
	if err := m.advise(a.id); err != nil {
		t.Fatal(err)
	}
	got := uiWaitAdvice(t, a, 3)
	if len(got) != 3 {
		t.Fatalf("advice count = %d, want at most 3: %+v", len(got), got)
	}
	if got[0].Level != "warn" || got[1].Level != "caution" || got[2].Level != "advice" {
		t.Fatalf("levels = %q %q %q", got[0].Level, got[1].Level, got[2].Level)
	}
	if strings.Contains(got[0].Text, "WARN|") || !strings.HasPrefix(got[0].Text, "Tests keep failing") {
		t.Fatalf("text = %q", got[0].Text)
	}
	if got[0].ID == "" || got[0].ID == got[1].ID {
		t.Fatal("advice ids are missing or not unique")
	}
	adv.mu.Lock()
	defer adv.mu.Unlock()
	if len(adv.prompts) != 1 || !strings.Contains(adv.prompts[0], "make login stricter") || !strings.Contains(adv.prompts[0], "Do not use any tools") {
		t.Fatalf("advisor prompt = %.200q", adv.prompts)
	}
	if adv.cwds[0] != a.snapshot().Worktree {
		t.Fatalf("advisor ran in %q, want the agent's worktree", adv.cwds[0])
	}
}

func TestAdviseIsRateLimited(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	adv := &uiAdviceAdvisor{out: "ADV|One thing."}
	m.cfg.Advisor = adv.run
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	if err := m.advise(a.id); err != nil {
		t.Fatal(err)
	}
	uiWaitAdvice(t, a, 1)
	err := m.advise(a.id)
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "accounting") {
		t.Fatalf("second request err = %v, want a friendly wait message", err)
	}
	if err := m.advise("nope"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestApplyAdviceSendsItToThatAgent(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.NoConflictAlerts = true
	m.cfg.Advisor = (&uiAdviceAdvisor{out: "WARN|Run the tests before editing more."}).run
	a, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix login", Prompt: "go"})
	uiWaitState(t, a, "waiting")
	m.advise(a.id)
	adv := uiWaitAdvice(t, a, 1)
	if err := m.applyAdvice(a.id, adv[0].ID); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "advice on the agent's stdin", func() bool {
		for _, l := range uiStdinLines(t, out) {
			if strings.Contains(l, "Advice from Cully's advisor") && strings.Contains(l, "Run the tests before editing more.") {
				return true
			}
		}
		return false
	})
	if !a.adviceList()[0].Applied {
		t.Fatal("advice not marked applied")
	}
	if uiNotes(a, "Advisor advice") < 1 {
		t.Fatal("feed has no note about the applied advice")
	}
	if err := m.applyAdvice(a.id, "nope"); err == nil {
		t.Fatal("unknown advice accepted")
	}
	a.stop()
	uiWaitState(t, a, "stopped")
	if err := m.applyAdvice(a.id, adv[0].ID); err == nil {
		t.Fatal("advice applied to a finished agent")
	}
}

func TestDismissAdvice(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.Advisor = (&uiAdviceAdvisor{out: "ADV|Something."}).run
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	m.advise(a.id)
	adv := uiWaitAdvice(t, a, 1)
	if err := m.dismissAdvice(a.id, adv[0].ID); err != nil {
		t.Fatal(err)
	}
	if !a.adviceList()[0].Dismissed {
		t.Fatal("advice not marked dismissed")
	}
}

func TestAdviceEndpoints(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	m.cfg.NoConflictAlerts = true
	m.cfg.Advisor = (&uiAdviceAdvisor{out: "CAUT|Check the error handling."}).run
	r := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "title": "Fix login", "prompt": "go"}, uiAuthed)
	var v uiAgentView
	json.NewDecoder(r.Body).Decode(&v)
	a, _ := m.get(v.ID)
	uiWaitState(t, a, "waiting")
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+v.ID+"/advise", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agents/nope/advise", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+v.ID+"/advise", nil, uiAuthed); r.StatusCode != 202 {
		t.Fatalf("advise: %d", r.StatusCode)
	}
	adv := uiWaitAdvice(t, a, 1)
	var all []uiAdviceFor
	rr := uiDo(t, "GET", srv.URL+"/api/advice", nil, uiAuthed)
	json.NewDecoder(rr.Body).Decode(&all)
	if len(all) != 1 || all[0].AgentID != v.ID || all[0].AgentTitle != "Fix login" || len(all[0].Advice) != 1 {
		t.Fatalf("advice list = %+v", all)
	}
	apply := srv.URL + "/api/agents/" + v.ID + "/advice/" + adv[0].ID
	if r := uiDo(t, "POST", apply+"/apply", nil, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("apply: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+v.ID+"/advice/nope/apply", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("apply unknown advice: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", apply+"/dismiss", nil, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("dismiss: %d", r.StatusCode)
	}
}

// ---- scout ----

const fakeScoutClaude = `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"s1","name":"WebSearch","input":{"query":"flaky go tests best practice"}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"s1","content":"results"}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"s2","name":"WebFetch","input":{"prompt":"summarize","url":"https://go.dev/doc/articles/race_detector"}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"s2","content":"page"}]}}'
printf '{"type":"result","subtype":"success","is_error":false,"result":"Use the race detector: https://go.dev/doc/articles/race_detector %s"}\n' "$(head -c 3000 /dev/zero | tr '\0' 'x')"
`

func TestStreamScoutReportsProgressAndTheFullFindings(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	bin := uiWriteScript(t, fakeScoutClaude)
	old := uiScoutBinary
	uiScoutBinary = func() string { return bin }
	t.Cleanup(func() { uiScoutBinary = old })
	var progress []string
	text, err := uiStreamScout(context.Background(), t.TempDir(), "find advice", func(s string) { progress = append(progress, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 2 || progress[0] != "Searching: flaky go tests best practice" || progress[1] != "Reading: https://go.dev/doc/articles/race_detector" {
		t.Fatalf("progress = %q", progress)
	}
	if !strings.HasPrefix(text, "Use the race detector") || len(text) < 3000 {
		t.Fatalf("findings were clipped or lost: %d bytes", len(text))
	}
}

func TestScoutStageKeepsFindingsAndLiveProgress(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.Advisor = (&uiFakeAdvisor{}).run
	m.cfg.ScoutStream = func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
		progress("Searching: gateway timeouts")
		progress("Reading: https://example.com/a")
		return "Raise the client timeout. Source: https://example.com/a", nil
	}
	a, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix gateway", Prompt: "fix the gateway timeout", Recall: true, Scout: true})
	uiWaitState(t, a, "waiting")
	var view uiScoutView
	for _, v := range m.scouts() {
		if v.AgentID == a.id {
			view = v
		}
	}
	if view.Scout.Status != "ok" || len(view.Scout.Log) != 2 || view.Scout.Log[0] != "Searching: gateway timeouts" {
		t.Fatalf("scout = %+v", view.Scout)
	}
	if !strings.Contains(view.Scout.Output, "Raise the client timeout") {
		t.Fatalf("findings not kept: %q", view.Scout.Output)
	}
	if view.Recall.Status != "ok" || view.Recall.Output != "recall-notes" || view.Task != "fix the gateway timeout" {
		t.Fatalf("recall/task = %+v / %q", view.Recall, view.Task)
	}
}

func TestScoutEndpointAndSkippedStages(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	m.cfg.Advisor = (&uiFakeAdvisor{}).run
	r := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": "go"}, uiAuthed)
	var v uiAgentView
	json.NewDecoder(r.Body).Decode(&v)
	a, _ := m.get(v.ID)
	uiWaitState(t, a, "waiting")
	if r := uiDo(t, "GET", srv.URL+"/api/scout", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var got []uiScoutView
	rr := uiDo(t, "GET", srv.URL+"/api/scout", nil, uiAuthed)
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].Scout.Status != "skipped" || got[0].Recall.Status != "skipped" {
		t.Fatalf("scouts = %+v", got)
	}
}

func TestToolSummaryPrefersTheQueryOverOtherFields(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"WebFetch","input":{"prompt":"summarize","url":"https://a.example/x"}}]}}`))
	if len(p.Events) != 1 || p.Events[0].Text != "https://a.example/x" {
		t.Fatalf("summary = %+v", p.Events)
	}
	r := parseStreamLine([]byte(`{"type":"result","is_error":false,"result":"` + strings.Repeat("y", 5000) + `"}`))
	if len(r.Final) != 5000 || len([]rune(r.Events[0].Text)) > uiEventTextMax+1 {
		t.Fatalf("final=%d shown=%d", len(r.Final), len([]rune(r.Events[0].Text)))
	}
}

func TestParseAdviceIgnoresMachineOutputAndFences(t *testing.T) {
	out := "{\"type\":\"system\",\"subtype\":\"init\"}\n```\n[1,2,3]\n---\nWARN|Run the tests again.\n```json\nADV|Summarize at the end."
	got := uiParseAdvice(out, nil)
	if len(got) != 2 || got[0].Text != "Run the tests again." || got[1].Text != "Summarize at the end." {
		t.Fatalf("advice = %+v", got)
	}
	if len(uiParseAdvice("{\"a\":1}\n[\"b\"]", nil)) != 0 {
		t.Fatal("JSON-only output produced advice")
	}
}
