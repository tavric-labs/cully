package cully

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffShowsCommittedAndUncommittedChanges(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	wt := a.snapshot().Worktree
	os.WriteFile(filepath.Join(wt, "f.txt"), []byte("committed line\n"), 0o644)
	for _, args := range [][]string{{"add", "f.txt"}, {"commit", "-q", "-m", "add f"}} {
		if out, err := exec.Command("git", append([]string{"-C", wt}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(wt, "f.txt"), []byte("committed line\nlater edit\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "g.txt"), []byte("untracked\n"), 0o644)
	got := a.diff()
	for _, want := range []string{"f.txt", "+committed line", "+later edit", "Untracked files", "g.txt", "+untracked"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff lacks %q:\n%s", want, got)
		}
	}
}

func TestDiffIsBounded(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	wt := a.snapshot().Worktree
	os.WriteFile(filepath.Join(wt, "big.txt"), []byte(strings.Repeat("x\n", uiDiffMax)), 0o644)
	exec.Command("git", "-C", wt, "add", "big.txt").Run()
	got := a.diff()
	if len(got) > uiDiffMax+200 || !strings.Contains(got, "truncated") {
		t.Fatalf("diff length %d, truncated marker missing", len(got))
	}
}

func TestPermissionModePassedToClaude(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	a1, _ := m.start(uiStartRequest{Repo: repo, Prompt: "default"})
	a2, _ := m.start(uiStartRequest{Repo: repo, Prompt: "plan", Mode: "plan"})
	uiWaitState(t, a1, "waiting")
	uiWaitState(t, a2, "waiting")
	b, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(b), "--permission-mode acceptEdits") || !strings.Contains(string(b), "--permission-mode plan") {
		t.Fatalf("args.log = %s", b)
	}
}

func TestInvalidModeRejected(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for _, bad := range []string{"bypassPermissions", "full", "plan --dangerously-skip-permissions"} {
		if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "x", Mode: bad}); err == nil {
			t.Fatalf("mode %q accepted", bad)
		}
	}
}

func TestDiffSkipsLargeUntrackedFileContent(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	wt := a.snapshot().Worktree
	os.WriteFile(filepath.Join(wt, "huge.bin"), []byte(strings.Repeat("y", uiUntrackedMax+10)), 0o644)
	got := a.diff()
	if !strings.Contains(got, "huge.bin") || strings.Contains(got, strings.Repeat("y", 100)) {
		t.Fatalf("large untracked file handling wrong:\n%.300s", got)
	}
	if !strings.Contains(got, "too large to show") {
		t.Fatalf("no size note:\n%.300s", got)
	}
}

func TestAgentsLaunchWithOutboundCommandsDenied(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	b, _ := os.ReadFile(filepath.Join(out, "args.log"))
	args := string(b)
	i := strings.Index(args, "--disallowedTools ")
	if i < 0 {
		t.Fatalf("no --disallowedTools in %q", args)
	}
	for _, want := range []string{"Bash(git push:*)", "Bash(curl:*)", "Bash(wget:*)", "Bash(ssh:*)"} {
		if !strings.Contains(args[i:], want) {
			t.Fatalf("%s not denied: %q", want, args)
		}
	}
}
