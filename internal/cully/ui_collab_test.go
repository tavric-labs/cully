package cully

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiNotes(a *uiAgent, contains string) int {
	n := 0
	for _, e := range a.allEvents() {
		if e.Kind == "note" && strings.Contains(e.Text, contains) {
			n++
		}
	}
	return n
}

func uiStdinLines(t *testing.T, out string) []string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func uiCommitFile(t *testing.T, worktree, name, content string) {
	t.Helper()
	os.WriteFile(filepath.Join(worktree, name), []byte(content), 0o644)
	for _, args := range [][]string{{"add", name}, {"commit", "-q", "-m", "add " + name}} {
		if o, err := exec.Command("git", append([]string{"-C", worktree}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, o)
		}
	}
}

func TestConflictAlertsReachBothAgentsOnce(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	a1, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix login", Prompt: "one"})
	uiWaitState(t, a1, "waiting")
	a2, _ := m.start(uiStartRequest{Repo: repo, Title: "Add docs", Prompt: "two"})
	uiWaitState(t, a2, "waiting")
	uiWaitFor(t, "alerts on both agents", func() bool {
		return uiNotes(a1, "also edited a.txt") >= 1 && uiNotes(a2, "also edited a.txt") >= 1
	})
	time.Sleep(600 * time.Millisecond) // the alert triggers another turn; it must not re-alert
	if uiNotes(a1, "also edited a.txt") != 1 || uiNotes(a2, "also edited a.txt") != 1 {
		t.Fatalf("alert counts = %d, %d, want 1 each", uiNotes(a1, "also edited a.txt"), uiNotes(a2, "also edited a.txt"))
	}
	if uiNotes(a1, "Add docs") != 1 || uiNotes(a2, "Fix login") != 1 {
		t.Fatal("each alert should name the other agent")
	}
	alerts := 0
	for _, l := range uiStdinLines(t, out) {
		if strings.Contains(l, "Heads up from Cully") {
			alerts++
		}
	}
	if alerts != 2 {
		t.Fatalf("agents received %d alerts on stdin, want 2", alerts)
	}
}

func TestConflictAlertsCanBeDisabled(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	m.cfg.NoConflictAlerts = true
	a1, _ := m.start(uiStartRequest{Repo: repo, Prompt: "one"})
	uiWaitState(t, a1, "waiting")
	a2, _ := m.start(uiStartRequest{Repo: repo, Prompt: "two"})
	uiWaitState(t, a2, "waiting")
	time.Sleep(500 * time.Millisecond)
	if uiNotes(a1, "") != 0 || uiNotes(a2, "") != 0 {
		t.Fatal("alerts sent although disabled")
	}
}

func TestRelayReplyAndDiffToAnotherAgent(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	m.cfg.NoConflictAlerts = true
	a1, _ := m.start(uiStartRequest{Repo: repo, Title: "Author", Prompt: "one"})
	a2, _ := m.start(uiStartRequest{Repo: repo, Title: "Checker", Prompt: "two"})
	uiWaitState(t, a1, "waiting")
	uiWaitState(t, a2, "waiting")
	if err := m.relay(a1.id, a2.id, "reply", "please check this"); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "relay on the target's stdin", func() bool {
		for _, l := range uiStdinLines(t, out) {
			if strings.Contains(l, "Message from another agent") && strings.Contains(l, "Author") && strings.Contains(l, "working") && strings.Contains(l, "please check this") {
				return true
			}
		}
		return false
	})
	if uiNotes(a2, "Author") < 1 || uiNotes(a1, "Sent reply to Checker") != 1 {
		t.Fatalf("feeds missing relay notes: target=%d source=%d", uiNotes(a2, "Author"), uiNotes(a1, "Sent reply to Checker"))
	}
	uiCommitFile(t, a1.snapshot().Worktree, "f.txt", "relayed line\n")
	if err := m.relay(a1.id, a2.id, "diff", ""); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "diff relayed", func() bool {
		for _, l := range uiStdinLines(t, out) {
			if strings.Contains(l, "f.txt") && strings.Contains(l, "Message from another agent") {
				return true
			}
		}
		return false
	})
}

func TestRelayRejectsBadRequests(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	m.cfg.NoConflictAlerts = true
	a1, _ := m.start(uiStartRequest{Repo: repo, Prompt: "one"})
	a2, _ := m.start(uiStartRequest{Repo: repo, Prompt: "two"})
	uiWaitState(t, a1, "waiting")
	uiWaitState(t, a2, "waiting")
	if err := m.relay(a1.id, a1.id, "reply", ""); err == nil {
		t.Fatal("relay to itself accepted")
	}
	if err := m.relay(a1.id, a2.id, "everything", ""); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if err := m.relay("nope", a2.id, "reply", ""); err == nil {
		t.Fatal("unknown source accepted")
	}
	if err := m.relay(a1.id, "nope", "reply", ""); err == nil {
		t.Fatal("unknown target accepted")
	}
	if err := m.relay(a1.id, a2.id, "reply", strings.Repeat("n", uiMaxPromptRunes+1)); err == nil {
		t.Fatal("oversized note accepted")
	}
	a2.stop()
	uiWaitState(t, a2, "stopped")
	if err := m.relay(a1.id, a2.id, "reply", ""); err == nil {
		t.Fatal("relay to a finished agent accepted")
	}
}

func TestReviewAgentIsReadOnlyAndSeesTheDiff(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	m.cfg.NoConflictAlerts = true
	author, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix login", Prompt: "make login stricter"})
	uiWaitState(t, author, "waiting")
	wt := author.snapshot().Worktree
	uiCommitFile(t, wt, "login.go", "committed line\n")
	rev, err := m.review(author.id)
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, rev, "waiting")
	if v := rev.snapshot(); v.Title != "Review: Fix login" {
		t.Fatalf("title = %q", v.Title)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(args), "--permission-mode plan") || !strings.Contains(string(args), "--add-dir "+wt) {
		t.Fatalf("reviewer args = %s", args)
	}
	var prompt string
	for _, l := range uiStdinLines(t, out) {
		if strings.Contains(l, "Review the following change") {
			prompt = l
		}
	}
	for _, want := range []string{"make login stricter", "login.go", "+committed line", "read-only"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("review prompt lacks %q: %.300s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Make the smallest complete change") {
		t.Fatal("a read-only reviewer was told to commit changes")
	}
}

func TestReviewNeedsChanges(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaudeNeedsAuth), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	if _, err := m.review(a.id); err == nil || !strings.Contains(err.Error(), "no changes") {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.review("nope"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestPlanModeAgentsAreLeftOutOfConflictAlerts(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	a1, _ := m.start(uiStartRequest{Repo: repo, Title: "Author", Prompt: "one"})
	uiWaitState(t, a1, "waiting")
	// The fake edits a.txt whatever the mode; a real plan-mode agent cannot.
	a2, _ := m.start(uiStartRequest{Repo: repo, Title: "Planner", Prompt: "two", Mode: "plan"})
	uiWaitState(t, a2, "waiting")
	time.Sleep(500 * time.Millisecond)
	if uiNotes(a1, "") != 0 || uiNotes(a2, "") != 0 {
		t.Fatalf("a read-only agent took part in a conflict alert: %d, %d", uiNotes(a1, ""), uiNotes(a2, ""))
	}
}
