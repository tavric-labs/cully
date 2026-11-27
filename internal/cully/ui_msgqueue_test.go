package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A turn whose text contains "hold" does not finish until the test creates $FAKE_OUT/release.
const fakeClaudeHold = `#!/bin/bash
echo '{"type":"system","subtype":"init"}'
while IFS= read -r line; do
  echo "$line" >> "$FAKE_OUT/stdin.log"
  case "$line" in
    *hold*) while [ ! -f "$FAKE_OUT/release" ]; do sleep 0.05; done; rm -f "$FAKE_OUT/release" ;;
  esac
  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"turn done"}'
done
`

func uiHeldTask(t *testing.T) (*uiAgent, string) {
	t.Helper()
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaudeHold), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "hold this turn"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "task working", func() bool { return a.snapshot().State == uiWorking && a.stdinReady() })
	return a, out
}

func (a *uiAgent) stdinReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stdin != nil
}

func uiRelease(t *testing.T, out string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(out, "release"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func uiUserTexts(a *uiAgent) []string {
	var got []string
	for _, e := range a.allEvents() {
		if e.Kind == "user" {
			got = append(got, e.Text)
		}
	}
	return got
}

func TestFollowUpIsDeliveredOnlyAfterTheTurnEnds(t *testing.T) {
	a, out := uiHeldTask(t)
	res, err := a.submit("then do this", uiModeFollowup)
	if err != nil || res.Status != "queued" {
		t.Fatalf("submit = %+v, %v", res, err)
	}
	if q := a.snapshot().Queue; len(q) != 1 || q[0].Mode != uiModeFollowup || q[0].Text != "then do this" {
		t.Fatalf("queue = %+v", q)
	}
	for _, x := range uiUserTexts(a) {
		if x == "then do this" {
			t.Fatal("a follow-up was sent while the turn was running")
		}
	}
	uiRelease(t, out)
	uiWaitFor(t, "follow-up delivered", func() bool {
		return len(a.snapshot().Queue) == 0 && a.snapshot().State == uiWaiting && a.doneTurnsNow() == 2
	})
	if got := uiUserTexts(a); len(got) == 0 || got[len(got)-1] != "then do this" {
		t.Fatalf("user events = %v", got)
	}
}

func (a *uiAgent) doneTurnsNow() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.doneTurns
}

func TestSteerIsDeliveredImmediately(t *testing.T) {
	a, out := uiHeldTask(t)
	res, err := a.submit("change course", "")
	if err != nil || res.Status != "sent_midturn" {
		t.Fatalf("submit = %+v, %v", res, err)
	}
	if got := uiUserTexts(a); got[len(got)-1] != "change course" {
		t.Fatalf("a steer was not written at once: %v", got)
	}
	q := a.snapshot().Queue
	if len(q) != 1 || !q[0].Sent || q[0].Mode != uiModeSteer {
		t.Fatalf("queue = %+v", q)
	}
	if err := a.cancelQueued(q[0].ID); err == nil {
		t.Fatal("a sent steer cannot be cancelled")
	}
	uiRelease(t, out)
	uiWaitFor(t, "both turns finished", func() bool { return a.doneTurnsNow() == 2 && len(a.snapshot().Queue) == 0 })
}

func TestQueuedMessagesKeepTheirOrderAndGoOnePerTurn(t *testing.T) {
	a, out := uiHeldTask(t)
	for _, x := range []string{"first", "second", "third"} {
		if _, err := a.submit(x, uiModeFollowup); err != nil {
			t.Fatal(err)
		}
	}
	uiRelease(t, out)
	uiWaitFor(t, "all delivered", func() bool { return a.doneTurnsNow() == 4 && a.snapshot().State == uiWaiting })
	b, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 4 || !strings.Contains(lines[1], "first") || !strings.Contains(lines[2], "second") || !strings.Contains(lines[3], "third") {
		t.Fatalf("stdin order = %v", lines)
	}
}

func TestCancelAndPromote(t *testing.T) {
	a, out := uiHeldTask(t)
	r1, _ := a.submit("drop me", uiModeFollowup)
	r2, _ := a.submit("send me now", uiModeFollowup)
	if err := a.cancelQueued(r1.Item.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.cancelQueued(r1.Item.ID); err != errNoQueued {
		t.Fatalf("second cancel = %v", err)
	}
	if err := a.promoteQueued(r2.Item.ID); err != nil {
		t.Fatal(err)
	}
	if got := uiUserTexts(a); got[len(got)-1] != "send me now" {
		t.Fatalf("promoted message not sent: %v", got)
	}
	if q := a.snapshot().Queue; len(q) != 1 || !q[0].Sent || q[0].Mode != uiModeSteer {
		t.Fatalf("queue = %+v", q)
	}
	uiRelease(t, out)
	uiWaitFor(t, "finished", func() bool { return a.doneTurnsNow() == 2 && a.snapshot().State == uiWaiting })
	for _, x := range uiUserTexts(a) {
		if x == "drop me" {
			t.Fatal("a cancelled message was sent")
		}
	}
}

func TestStoppedTaskDropsItsQueue(t *testing.T) {
	a, _ := uiHeldTask(t)
	if _, err := a.submit("never sent", uiModeFollowup); err != nil {
		t.Fatal(err)
	}
	a.stop()
	if q := a.snapshot().Queue; len(q) != 0 {
		t.Fatalf("queue after stop = %+v", q)
	}
	<-a.done
	for _, x := range uiUserTexts(a) {
		if x == "never sent" {
			t.Fatal("a dropped message was sent")
		}
	}
}

func TestQueueChangesAreQuietEvents(t *testing.T) {
	a, _ := uiHeldTask(t)
	if _, err := a.submit("later", uiModeFollowup); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, e := range a.allEvents() {
		if e.Kind == "queue_update" && strings.Contains(e.Text, "later") {
			saw = true
		}
	}
	if !saw || strings.Contains(a.snapshot().Last, "later") {
		t.Fatalf("saw=%v last=%q", saw, a.snapshot().Last)
	}
}
