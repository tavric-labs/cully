package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Claude that names its session and, unlike fakeClaude, can be told never to end its turn.
const switchClaude = `#!/bin/bash
echo "$@" >> "$FAKE_OUT/args.log"
echo '{"type":"system","subtype":"init","session_id":"11111111-2222-3333-4444-555555555555"}'
while IFS= read -r line; do
  echo "$line" >> "$FAKE_OUT/stdin.log"
  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"working"},{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"'"$PWD"'/a.txt"}}]}}'
  echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}'
  if [ -z "$FAKE_NEVER" ]; then
    echo '{"type":"result","subtype":"success","is_error":false,"result":"turn done"}'
  fi
done
`

func uiSwitchFast(t *testing.T) {
	a, b, c := uiSwitchTurnWait, uiSwitchPoll, uiSwitchStopWait
	uiSwitchTurnWait, uiSwitchPoll = 3*time.Second, 10*time.Millisecond
	t.Cleanup(func() { uiSwitchTurnWait, uiSwitchPoll, uiSwitchStopWait = a, b, c })
}

func uiSwitchSetup(t *testing.T) (*uiManager, *uiAgent, string) {
	uiSwitchFast(t)
	m, _, out := uiTestManager(t, uiWriteScript(t, switchClaude), "tok-aaa1")
	m.cfg.CodexBin = uiWriteScript(t, fakeCodex)
	a, err := m.start(uiStartRequest{Prompt: "add a file", Title: "Add a file"})
	if err != nil {
		t.Fatal(err)
	}
	return m, a, out
}

func TestSwitchClaudeToCodex(t *testing.T) {
	m, a, out := uiSwitchSetup(t)
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	oldTree, oldBranch := a.worktree, a.branch
	n, err := m.switchHarness(a.id, "codex", "also add tests")
	if err != nil {
		t.Fatal(err)
	}
	if n.id == a.id || n.worktree != oldTree || n.branch != oldBranch {
		t.Fatalf("the new run must share the worktree %s and branch %s, got %s %s", oldTree, oldBranch, n.worktree, n.branch)
	}
	if n.taskID() != a.taskID() {
		t.Fatal("same task")
	}
	if a.snapshot().State != uiStopped {
		t.Fatalf("old run state %s", a.snapshot().State)
	}
	uiWaitFor(t, "codex waiting", func() bool { return n.snapshot().State == uiWaiting })
	if n.snapshot().Agent != "codex" {
		t.Fatal("not on codex")
	}
	log, _ := os.ReadFile(filepath.Join(out, "codex.log"))
	for _, want := range []string{"You are continuing a task another agent started", "add a file", "also add tests", `"cwd": "` + oldTree + `"`} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("codex was not given %q:\n%s", want, log)
		}
	}
	uiWaitFor(t, "sessions recorded", func() bool {
		var rec uiTaskRecord
		uiDB().get("task", a.taskID(), &rec)
		return len(rec.Sessions) == 2 && rec.Sessions[1].Agent == "codex"
	})
	var rec uiTaskRecord
	uiDB().get("task", a.taskID(), &rec)
	first, second := rec.Sessions[0], rec.Sessions[1]
	if first.Agent != "claude" || first.Run != a.id || first.Reason != "switched to Codex" || first.Ended.IsZero() || uiLive(first.State) {
		t.Fatalf("first session %+v", first)
	}
	if second.Role != "handoff" || second.Run != n.id || second.ID == "" {
		t.Fatalf("second session %+v", second)
	}
	if rec.Runs != 2 || rec.Synced != "" {
		t.Fatalf("record %+v", rec)
	}
	// The overview lists the task once, with both runs and the live one being the new run.
	found := 0
	for _, tr := range m.taskList("all") {
		if tr.ID == a.taskID() {
			found++
			if len(tr.Sessions) != 2 || tr.Live != n.id {
				t.Fatalf("overview %+v live %s", tr.Sessions, tr.Live)
			}
		}
	}
	if found != 1 {
		t.Fatalf("task listed %d times", found)
	}
	// The graph carries the cross-harness reason.
	uiWaitFor(t, "graph edge", func() bool {
		var r uiTaskRecord
		uiDB().get("task", a.taskID(), &r)
		if r.Graph == nil {
			return false
		}
		for _, e := range r.Graph.Nodes {
			if e.Cause == "resumed-on-other-harness" {
				return true
			}
		}
		return false
	})
	// The summary sent to memory mentions both harnesses and the handoff.
	sum, _, _ := m.taskSummary(rec)
	if !strings.Contains(sum, "Claude") || !strings.Contains(sum, "Codex") || !strings.Contains(sum, "Handed off to Codex") {
		t.Fatalf("summary %q", sum)
	}
	// The old run is still there and keeps its history.
	if _, ok := m.get(a.id); !ok || len(a.allEvents()) == 0 {
		t.Fatal("old run must stay visible")
	}
}

func TestSwitchRefusals(t *testing.T) {
	m, a, _ := uiSwitchSetup(t)
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	if _, err := m.switchHarness(a.id, "claude", ""); err == nil || !strings.Contains(err.Error(), "already runs") {
		t.Fatalf("same harness: %v", err)
	}
	if _, err := m.switchHarness(a.id, "nonesuch", ""); err == nil {
		t.Fatal("an unsupported harness must be refused")
	}
	uiMarkExhausted("codex", time.Now().Add(time.Hour), "usage limit")
	if _, err := m.switchHarness(a.id, "codex", ""); err == nil || !strings.Contains(err.Error(), "out of tokens") {
		t.Fatalf("exhausted: %v", err)
	}
	if a.snapshot().State != uiWaiting {
		t.Fatal("a refused switch must leave the run alone")
	}
	uiClearExhausted("codex")
	m.cfg.CodexBin = ""
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, err := m.switchHarness(a.id, "codex", ""); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing: %v", err)
	}
}

func TestSwitchStopsWhenTurnNeverEnds(t *testing.T) {
	uiSwitchFast(t)
	uiSwitchTurnWait = 200 * time.Millisecond
	t.Setenv("FAKE_NEVER", "1")
	m, _, _ := uiTestManager(t, uiWriteScript(t, switchClaude), "tok-aaa1")
	m.cfg.CodexBin = uiWriteScript(t, fakeCodex)
	a, err := m.start(uiStartRequest{Prompt: "long job"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "working", func() bool { return a.snapshot().State == uiWorking && len(a.allEvents()) > 2 })
	begin := time.Now()
	n, err := m.switchHarness(a.id, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(begin) < 200*time.Millisecond {
		t.Fatal("it should have waited for the turn")
	}
	if a.snapshot().State != uiStopped {
		t.Fatalf("old run %s", a.snapshot().State)
	}
	uiWaitFor(t, "codex waiting", func() bool { return n.snapshot().State == uiWaiting })
}

func TestSwitchHandoffText(t *testing.T) {
	text := uiSwitchHandoff(uiSwitchInput{From: "codex", To: "claude", Title: "Fix login", Prompt: "make login work", Last: "tests failing",
		Files: []string{"a.go", "b.go"}, Pack: "## State\n- one", Note: "use the new API",
		Earlier: []uiTaskSession{{ID: "abc", Agent: "codex"}}})
	for _, want := range []string{"You are continuing a task another agent started", "Codex", "Claude", "make login work", "tests failing", "a.go, b.go", "An earlier Codex session worked on it: abc", "## State", "What the user wants next: use the new API"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
}
