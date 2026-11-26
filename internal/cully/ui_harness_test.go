package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const uiFakeHead = `#!/bin/bash
echo "$@" >> "$FAKE_OUT/args.log"
echo '{"type":"system","subtype":"init"}'
n=0
while IFS= read -r line; do
  n=$((n+1))
  echo "$line" >> "$FAKE_OUT/stdin.log"
`

func uiToolLine(id, name, input string) string {
	return `  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}]}}'
  echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"ok"}]}}'
`
}

const uiFakeTail = `  echo '{"type":"result","subtype":"success","is_error":false,"result":"turn done"}'
done
`

func uiHStdin(t *testing.T, out string) []string {
	b, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
	var l []string
	for _, x := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if x != "" {
			l = append(l, x)
		}
	}
	return l
}

func uiHNotes(a *uiAgent) string {
	var b strings.Builder
	for _, e := range a.allEvents() {
		if e.Kind == "system" && strings.HasPrefix(e.Text, "Cully:") {
			b.WriteString(e.Text + "\n")
		}
	}
	return b.String()
}

func TestHarnessAsksToVerifyThenStopsAtTheRoundLimit(t *testing.T) {
	body := uiFakeHead + uiToolLine("e$n", "Edit", `{"file_path":"'"$PWD"'/a.txt"}`) + uiFakeTail
	m, repo, out := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go", Loop: true, MaxRounds: 2})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the harness to stop the loop", func() bool { return strings.Contains(uiHNotes(a), "Stopped after 2 rounds") })
	lines := uiHStdin(t, out)
	if len(lines) != 3 || !strings.Contains(lines[1], "did not run any checks") {
		t.Fatalf("stdin = %v", lines)
	}
	if v := a.snapshot(); v.Harness == nil || v.Harness.Round != 2 || v.Harness.Max != 2 || v.Harness.Status != "stopped" {
		t.Fatalf("harness = %+v", v.Harness)
	}
}

func TestHarnessJudgesAgainstTheChecksAfterTheAgentRanTests(t *testing.T) {
	body := uiFakeHead + uiToolLine("e$n", "Edit", `{"file_path":"'"$PWD"'/a.txt"}`) + uiToolLine("b$n", "Bash", `{"command":"go test ./..."}`) + uiFakeTail
	m, repo, out := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1")
	verdicts := []string{"VERDICT: fail\nREASON: The login case is missing.", "VERDICT: pass\nREASON: Both checks are met."}
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		v := verdicts[0]
		if len(verdicts) > 1 {
			verdicts = verdicts[1:]
		}
		return v, nil
	}
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "fix login", Loop: true, DoneWhen: "login works"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the checks to be met", func() bool { return strings.Contains(uiHNotes(a), "Checks met") })
	lines := uiHStdin(t, out)
	if len(lines) != 2 || !strings.Contains(lines[1], "login case is missing") {
		t.Fatalf("stdin = %v", lines)
	}
	if v := a.snapshot(); v.Harness.Status != "done" || v.Harness.Round != 1 {
		t.Fatalf("harness = %+v", v.Harness)
	}
}

func TestHarnessSteersAnAgentThatRepeatsTheSameCall(t *testing.T) {
	body := uiFakeHead + uiToolLine("r1$n", "Bash", `{"command":"ls"}`) + uiToolLine("r2$n", "Bash", `{"command":"ls"}`) + uiToolLine("r3$n", "Bash", `{"command":"ls"}`) + uiFakeTail
	m, repo, out := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go", Loop: true, MaxRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the agent to be steered", func() bool { return strings.Contains(uiHNotes(a), "repeated the same call") })
	uiWaitFor(t, "the steering message to arrive", func() bool { return len(uiHStdin(t, out)) >= 2 })
	if l := uiHStdin(t, out); !strings.Contains(l[1], "same call") {
		t.Fatalf("stdin = %v", l)
	}
}

func TestPlainAgentsHaveNoHarness(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	if v := a.snapshot(); v.Harness != nil || len(uiHStdin(t, out)) != 1 {
		t.Fatalf("a plain agent was touched by the harness: %+v", v.Harness)
	}
}

func TestHarnessRoundsAreCapped(t *testing.T) {
	h := newUIHarness(uiStartRequest{MaxRounds: 99, DoneWhen: strings.Repeat("x", 5000)})
	if h.max != uiHarnessMaxRounds || len(h.criteria) > 1100 {
		t.Fatalf("harness = %+v", h)
	}
	if newUIHarness(uiStartRequest{}).max != uiHarnessDefaultRounds {
		t.Fatal("default rounds wrong")
	}
}

func TestPageOffersTheHarnessAndTheScoutHistory(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"Keep going until checked", "doneWhen", "Checking: round", "Scout the web every hour", "Scout history", "/api/scout/auto", "/api/research?kind=scout"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}

func TestAdvisorPageCanApplySuggestionsAndShowsWhereSessionsStarted(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"function pushSuggestion", "Start a task for this", "Copy for a session in a terminal", "Where this session was started", "coachCard(again, true)"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
