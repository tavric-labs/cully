package cully

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeClaude is a stand-in for `claude -p --input-format stream-json`: it
// records what it receives, answers each stdin line with one tool call and a
// result, and leaves a background child so stop() can prove it kills the group.
const fakeClaude = `#!/bin/bash
echo "$@" >> "$FAKE_OUT/args.log"
echo -n "${CLAUDE_CODE_OAUTH_TOKEN: -1}" >> "$FAKE_OUT/token.log"
echo >> "$FAKE_OUT/token.log"
sleep 300 &
echo $! > "$FAKE_OUT/child.pid"
echo '{"type":"system","subtype":"init"}'
while IFS= read -r line; do
  echo "$line" >> "$FAKE_OUT/stdin.log"
  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"working"},{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"'"$PWD"'/a.txt"}}]}}'
  echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"turn done"}'
done
`

const failingClaude = `#!/bin/bash
echo "claude exploded" >&2
exit 3
`

func uiWriteScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func uiNewTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func uiTestManager(t *testing.T, bin string, tokens ...string) (*uiManager, string, string) {
	t.Helper()
	repo := uiNewTestRepo(t)
	out := t.TempDir()
	t.Setenv("FAKE_OUT", out)
	body := ""
	for i, tok := range tokens {
		body += "CLAUDE_CODE_OAUTH_TOKEN_" + strconv.Itoa(i+1) + "=" + tok + "\n"
	}
	pool, err := loadUITokens(writeTokenFile(t, body))
	if err != nil {
		t.Fatal(err)
	}
	m := newUIManager(uiConfig{Repos: []string{repo}, Tokens: pool, ClaudeBin: bin, MaxAgents: 2})
	t.Cleanup(m.stopAll)
	return m, repo, out
}

func uiWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func uiWaitState(t *testing.T, a *uiAgent, state string) {
	t.Helper()
	uiWaitFor(t, "state "+state+" (now "+a.snapshot().State+")", func() bool { return a.snapshot().State == state })
}

func TestStartCreatesWorktreeAndBranch(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Title: "Fix Flaky Test!", Prompt: "do x"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	v := a.snapshot()
	if !strings.HasPrefix(v.Branch, "cully/fix-flaky-test-") {
		t.Fatalf("branch = %q", v.Branch)
	}
	if !strings.HasPrefix(v.Worktree, filepath.Join(ConfigDir(), "ui-worktrees")) {
		t.Fatalf("worktree = %q", v.Worktree)
	}
	if _, err := os.Stat(v.Worktree); err != nil {
		t.Fatal(err)
	}
	got, _ := exec.Command("git", "-C", v.Worktree, "branch", "--show-current").Output()
	if strings.TrimSpace(string(got)) != v.Branch {
		t.Fatalf("worktree on %q, want %q", got, v.Branch)
	}
}

func TestAgentGetsTokenEnvAndRoundRobin(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	a1, _ := m.start(uiStartRequest{Repo: repo, Prompt: "one"})
	a2, _ := m.start(uiStartRequest{Repo: repo, Prompt: "two"})
	uiWaitState(t, a1, "waiting")
	uiWaitState(t, a2, "waiting")
	if a1.snapshot().Token != 1 || a2.snapshot().Token != 2 {
		t.Fatalf("token indexes = %d, %d", a1.snapshot().Token, a2.snapshot().Token)
	}
	log, _ := os.ReadFile(filepath.Join(out, "token.log"))
	if !strings.Contains(string(log), "1") || !strings.Contains(string(log), "2") {
		t.Fatalf("children saw tokens %q", log)
	}
	for _, e := range a1.allEvents() {
		if strings.Contains(e.Text, "tok-aaa1") {
			t.Fatalf("token leaked into event %+v", e)
		}
	}
}

func TestSendWritesStreamJSONUserMessage(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "first"})
	uiWaitState(t, a, "waiting")
	if err := a.send("hello again"); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "second stdin line", func() bool {
		b, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
		return strings.Count(string(b), "\n") >= 2
	})
	b, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &msg); err != nil {
		t.Fatalf("stdin line is not JSON: %q", lines[1])
	}
	if msg.Type != "user" || msg.Message.Role != "user" || msg.Message.Content != "hello again" {
		t.Fatalf("stdin message = %+v", msg)
	}
}

func TestTurnResultMovesToWaiting(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	kinds := map[string]bool{}
	for _, e := range a.allEvents() {
		kinds[e.Kind] = true
	}
	for _, k := range []string{"text", "tool", "tool_result", "result", "user"} {
		if !kinds[k] {
			t.Fatalf("missing %s event in %v", k, kinds)
		}
	}
}

func TestMissingClaudeFailsWithErrorEvent(t *testing.T) {
	m, repo, _ := uiTestManager(t, filepath.Join(t.TempDir(), "no-such-claude"), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "failed")
	found := false
	for _, e := range a.allEvents() {
		found = found || e.Kind == "error"
	}
	if !found {
		t.Fatal("no error event")
	}
}

func TestNonZeroExitFails(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, failingClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "failed")
	joined := ""
	for _, e := range a.allEvents() {
		if e.Kind == "error" {
			joined += e.Text
		}
	}
	if !strings.Contains(joined, "claude exploded") {
		t.Fatalf("error events = %q", joined)
	}
}

func TestStopKillsProcessGroup(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	raw, err := os.ReadFile(filepath.Join(out, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	a.stop()
	uiWaitState(t, a, "stopped")
	uiWaitFor(t, "child to die", func() bool { return syscall.Kill(pid, 0) != nil })
}

func TestMaxAgentsRefused(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for i := 0; i < 2; i++ {
		if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "one too many"}); err == nil {
		t.Fatal("third agent accepted past MaxAgents=2")
	}
}

func TestStartRejectsRepoOutsideAllowList(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for _, bad := range []string{t.TempDir(), repo + "/../", "/etc"} {
		if _, err := m.start(uiStartRequest{Repo: bad, Prompt: "go"}); err == nil {
			t.Fatalf("repo %q accepted", bad)
		}
	}
	if _, err := m.start(uiStartRequest{Prompt: ""}); err == nil {
		t.Fatal("empty prompt accepted")
	}
}

func TestToolEventsAppendedToJournal(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	v := a.snapshot()
	events := readJournal(journalPath(v.Worktree, "ui-"+v.ID))
	var edit *journalEvent
	for i := range events {
		if events[i].Class == journalEdit {
			edit = &events[i]
		}
	}
	if edit == nil {
		t.Fatalf("no edit event in %+v", events)
	}
	if edit.Path != "a.txt" || strings.HasPrefix(edit.Path, "/") {
		t.Fatalf("journal path = %q, want relative a.txt", edit.Path)
	}
}

func TestSnapshotReportsLastActivity(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	v := a.snapshot()
	if v.Last == "" || len(v.Last) > uiLastMax+3 {
		t.Fatalf("last = %q", v.Last)
	}
	if time.Since(v.Updated) > time.Minute || v.Updated.IsZero() {
		t.Fatalf("updated = %v", v.Updated)
	}
	if strings.Contains(v.Last, "\n") {
		t.Fatalf("last has a newline: %q", v.Last)
	}
}

func TestLastActivityIgnoresStageNoise(t *testing.T) {
	a := &uiAgent{tokens: &uiTokenPool{}, notify: make(chan struct{})}
	a.add(uiEvent{Kind: "text", Text: "real work\nsecond line"})
	a.add(uiEvent{Kind: "stage", Text: "scout: ok"})
	if got := a.snapshot().Last; got != "real work second line" {
		t.Fatalf("last = %q", got)
	}
	a.add(uiEvent{Kind: "tool", Tool: "Edit", Text: "/w/a.go"})
	if got := a.snapshot().Last; got != "Edit /w/a.go" {
		t.Fatalf("last = %q", got)
	}
}

const fakeClaudeNeedsAuth = `#!/bin/bash
echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"cully","status":"needs-auth"}]}'
while IFS= read -r line; do
  echo '{"type":"result","subtype":"success","is_error":false,"result":"done"}'
done
`

func TestAgentReportsMCPNeedingAuth(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaudeNeedsAuth), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	v := a.snapshot()
	if len(v.MCP) != 1 || v.MCP[0].Name != "cully" || !v.MCP[0].NeedsAuth() {
		t.Fatalf("view mcp = %+v", v.MCP)
	}
}

func TestToolEventPathsAreRelativeToWorktree(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	wt := a.snapshot().Worktree
	var sawTool bool
	for _, e := range a.allEvents() {
		if e.Kind == "tool" {
			sawTool = true
			if strings.Contains(e.Text, wt) || e.Text != "a.txt" {
				t.Fatalf("tool text = %q, want relative a.txt", e.Text)
			}
		}
	}
	if !sawTool {
		t.Fatal("no tool event")
	}
}

func TestAgentsMayUseCullyMemoryToolsButNotDeleteThem(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	b, _ := os.ReadFile(filepath.Join(out, "args.log"))
	for _, want := range []string{"mcp__cully__cully_context", "mcp__cully__cully_log", "mcp__cully__cully_recall"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("headless agent is not allowed %s: %s", want, b)
		}
	}
	for _, bad := range []string{"cully_delete", "cully_update"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("agent may call %s", bad)
		}
	}
	p, _ := m.start(uiStartRequest{Repo: repo, Prompt: "plan it", Mode: "plan"})
	uiWaitState(t, p, "waiting")
	b, _ = os.ReadFile(filepath.Join(out, "args.log"))
	last := strings.Split(strings.TrimSpace(string(b)), "\n")
	if line := last[len(last)-1]; strings.Contains(line, "cully_log") {
		t.Errorf("plan-only agent may write memory: %s", line)
	}
}
