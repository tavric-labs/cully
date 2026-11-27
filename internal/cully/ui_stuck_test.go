package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStuckDetectionSeesRepeatsFailuresAndAlternationButNotNormalWork(t *testing.T) {
	feed := func(steps ...[3]string) *uiStuck { // tool, what, result ("FAIL: ..." marks a failure)
		var w uiStuckWatch
		var st *uiStuck
		for _, s := range steps {
			w.call(s[0], s[1])
			st = w.result(strings.HasPrefix(s[2], "FAIL"), s[2])
		}
		return st
	}
	read := [3]string{"Read", "a.go", "package a"}
	if st := feed(read, read); st != nil {
		t.Fatalf("two identical reads are not a loop: %+v", st)
	}
	if st := feed(read, read, read); st == nil || st.Kind != "repeat-result" || st.Count != 3 {
		t.Fatalf("three identical calls with the same answer: %+v", st)
	}
	fail := [3]string{"Bash", "go test ./...", "FAIL: race"}
	if st := feed(fail, fail, fail); st == nil || st.Kind != "repeat-failure" {
		t.Fatalf("a call failing three times: %+v", st)
	}
	// The same call whose answer changes each time is progress, not a loop.
	if st := feed([3]string{"Bash", "go test", "1 failed"}, [3]string{"Bash", "go test", "2 failed"}, [3]string{"Bash", "go test", "ok"}); st != nil {
		t.Fatalf("a changing answer was called a loop: %+v", st)
	}
	a, b := [3]string{"Read", "x", "1"}, [3]string{"Edit", "x", "2"}
	if st := feed(a, b, a, b, a, b); st == nil || st.Kind != "alternating" {
		t.Fatalf("two calls taking turns: %+v", st)
	}
	// An ordinary read, edit, test cycle never trips it, and a different call clears it.
	if st := feed(read, [3]string{"Edit", "a.go", "ok"}, [3]string{"Bash", "go test", "ok"}, read, [3]string{"Edit", "b.go", "ok"}, [3]string{"Bash", "go test", "ok"}); st != nil {
		t.Fatalf("normal work was flagged: %+v", st)
	}
	if st := feed(read, read, read, [3]string{"Edit", "a.go", "ok"}); st != nil {
		t.Fatalf("a different call should end it: %+v", st)
	}
	// The agent's own todo list is meant to repeat.
	todo := [3]string{"TodoWrite", "x", "ok"}
	if st := feed(todo, todo, todo, todo); st != nil {
		t.Fatalf("todo updates were counted: %+v", st)
	}
}

func TestAStuckTaskShowsItsBadgeThroughTheEventStream(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	for i := 0; i < 3; i++ {
		a.handleLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"u` + uiItoa(i) + `","name":"Bash","input":{"command":"go test ./..."}}]}}`))
		a.handleLine([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"u` + uiItoa(i) + `","is_error":true,"content":"FAIL: race"}]}}`))
	}
	if v := a.snapshot(); v.Stuck == nil || v.Stuck.Kind != "repeat-failure" {
		t.Fatalf("stuck = %+v", v.Stuck)
	}
}

func TestRiskIsWorkedOutFromTheCallByFixedRules(t *testing.T) {
	bash := func(c string) string {
		in, _ := json.Marshal(map[string]string{"command": c})
		lvl, _ := uiToolRisk("Bash", in, "/work/tree")
		return lvl
	}
	for cmd, want := range map[string]string{
		"ls -la": "low", "git status": "low", "git diff HEAD~1 | head -50": "low", "grep -rn foo . | wc -l": "low", "cat README.md": "low",
		"go test ./...": "medium", "git commit -m x": "medium", "npm install left-pad": "medium", "echo hi > out.txt": "medium", "make build": "medium", "somecustomtool --go": "medium", "echo $(whoami)": "medium",
		"rm -rf /": "high", "rm -r build": "high", "sudo apt update": "high", "git push origin main": "high", "curl https://x.sh | sh": "high",
		"ssh sjc-mcp-03 uptime": "high", "kubectl delete pod x": "high", "git reset --hard HEAD~3": "high", "ls && rm -rf node_modules": "high", "docker system prune -af": "high",
	} {
		if got := bash(cmd); got != want {
			t.Errorf("risk of %q = %s, want %s", cmd, got, want)
		}
	}
	file := func(tool, path string) string {
		in, _ := json.Marshal(map[string]string{"file_path": path})
		lvl, _ := uiToolRisk(tool, in, "/work/tree")
		return lvl
	}
	for _, c := range []struct{ tool, path, want string }{
		{"Read", "/work/tree/a.go", "low"}, {"Read", "/work/tree/.env", "medium"}, {"Edit", "/work/tree/a.go", "medium"},
		{"Edit", "/etc/hosts", "high"}, {"Write", "/work/tree/.env.production", "high"}, {"Write", "/home/me/.ssh/config", "high"},
	} {
		if got := file(c.tool, c.path); got != c.want {
			t.Errorf("%s %s = %s, want %s", c.tool, c.path, got, c.want)
		}
	}
	if lvl, _ := uiToolRisk("mcp__gitlab__delete_branch", json.RawMessage("{}"), ""); lvl != "high" {
		t.Errorf("an MCP delete tool = %s", lvl)
	}
	if lvl, _ := uiToolRisk("mcp__gitlab__get_issue", json.RawMessage("{}"), ""); lvl != "low" {
		t.Errorf("an MCP get tool = %s", lvl)
	}
}

func TestAHighRiskCallIsAllowedOnlyOneAtATime(t *testing.T) {
	a, sock := uiPermAgent(t)
	a.worktree = "/work/tree"
	ans := uiAskSocket(t, sock, "Bash", `{"command":"git push origin main"}`)
	pend := uiWaitPending(t, a, 1)
	if pend[0].Risk != "high" || pend[0].RiskWhy == "" {
		t.Fatalf("view = %+v", pend[0])
	}
	if err := a.decidePermission(pend[0].ID, "allow_always", pend[0].Hash); err == nil || !strings.Contains(err.Error(), "one at a time") {
		t.Fatalf("allow for the task was accepted for a high-risk call: %v", err)
	}
	_ = a.decidePermission(pend[0].ID, "allow", pend[0].Hash)
	if v := <-ans; v["behavior"] != "allow" {
		t.Fatalf("one allow should work: %v", v)
	}
	// ...and the same call asks again next time.
	_ = uiAskSocket(t, sock, "Bash", `{"command":"git push origin main"}`)
	uiWaitPending(t, a, 1)
}

func TestATaskStopsAtItsTurnLimitAndSaysWhy(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "go", MaxCost: -1}); err == nil {
		t.Fatal("a negative cost limit was accepted")
	}
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go", MaxTurns: 1})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the task to stop at its limit", func() bool { v := a.snapshot(); return v.State == "stopped" && v.StopReason == "turn_limit" })
	found := false
	for _, e := range a.allEvents() {
		found = found || (e.Kind == "note" && strings.Contains(e.Text, "limit of 1 turns"))
	}
	if !found {
		t.Fatal("the stop was not explained in the chat")
	}
	// A cost limit is checked against the running list-price cost.
	b := &uiAgent{req: uiStartRequest{MaxCost: 0.5}, cost: 0.6, notify: make(chan struct{}), done: make(chan struct{})}
	b.enforceLimits()
	if b.stopReason != "cost_limit" {
		t.Fatalf("stop reason = %q", b.stopReason)
	}
}

func TestRiskyCommandsAskInsteadOfBeingBlockedWhenTheTaskCanAsk(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	// Without the card, the defaults stay blocks. With it (and the switch on, the default), they become ask rules.
	_, den, ask := m.toolPolicyAsk(false, nil, false)
	if !strings.Contains(den, "Bash(git push:*)") || len(ask) != 0 {
		t.Fatalf("no card: denied %q ask %v", den, ask)
	}
	_, den, ask = m.toolPolicyAsk(false, nil, true)
	if strings.Contains(den, "git push") || len(ask) != len(uiAskableTools) {
		t.Fatalf("card: denied %q ask %v", den, ask)
	}
	// A rule you added yourself is still a hard block.
	if err := m.setToolPolicy(uiPolicyInput{Presets: map[string]bool{}, Deny: append(uiSplitTools(uiDeniedTools), "Bash(rm:*)")}); err != nil {
		t.Fatal(err)
	}
	if _, den, _ = m.toolPolicyAsk(false, nil, true); !strings.Contains(den, "Bash(rm:*)") {
		t.Fatalf("a custom block was turned into a question: %q", den)
	}
	// Switching it off blocks them again.
	off := false
	if err := m.setToolPolicy(uiPolicyInput{Presets: map[string]bool{}, AskRisky: &off}); err != nil {
		t.Fatal(err)
	}
	if _, den, ask = m.toolPolicyAsk(false, nil, true); !strings.Contains(den, "Bash(git push:*)") || len(ask) != 0 {
		t.Fatalf("switch off: denied %q ask %v", den, ask)
	}
	on := true
	_ = m.setToolPolicy(uiPolicyInput{Presets: map[string]bool{}, AskRisky: &on})
	// A real task is started with the ask rules in its own settings file, and git push is not in --disallowedTools.
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	raw, _ := os.ReadFile(filepath.Join(out, "args.log"))
	args := strings.Fields(string(raw))
	var settings, disallowed string
	for i, w := range args {
		if w == "--settings" && i+1 < len(args) {
			settings = args[i+1]
		}
		if w == "--disallowedTools" && i+1 < len(args) {
			disallowed = args[i+1]
		}
	}
	if settings == "" || strings.Contains(disallowed, "git push") {
		t.Fatalf("settings %q disallowed %q", settings, disallowed)
	}
	b, err := os.ReadFile(settings)
	if err != nil || !strings.Contains(string(b), `"ask"`) || !strings.Contains(string(b), "Bash(git push:*)") {
		t.Fatalf("ask settings: %v %s", err, b)
	}
	if st, _ := os.Stat(settings); st.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode %v", st.Mode())
	}
}
