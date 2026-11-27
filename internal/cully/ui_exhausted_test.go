package cully

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClassifyLimit(t *testing.T) {
	now := time.Date(2026, 11, 27, 14, 0, 0, 0, time.Local)
	cases := []struct {
		msg  string
		hit  bool
		wait time.Duration
	}{
		{"You have hit your usage limit. Resets at 3:40 PM.", true, 100 * time.Minute},
		{"Codex hit its limit; back about 4:10 PM", true, 130 * time.Minute},
		{"request failed: HTTP 429", true, 30 * time.Minute},
		{"Rate limit reached, try again in 25 minutes", true, 25 * time.Minute},
		{"429 Too Many Requests", true, 30 * time.Minute},
		{"You exceeded your current quota, please check your plan", true, 30 * time.Minute},
		{"You are out of credits", true, 30 * time.Minute},
		{"usage limit reached; resets in 2h 5m", true, 125 * time.Minute},
		{"limit reached. Try again at 1:00 PM", true, 23 * time.Hour},
		{"The weekly limit reached, resets in 400 hours", true, 24 * time.Hour},
		{"team has reached its usage limit, resets 12/1/2026", true, time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local).Sub(now)},
		{"codex exited: exit status 1", false, 0},
		{"could not parse the file at line 429 of main.go", false, 0},
		{"permission denied", false, 0},
		{"the build failed with 3 errors", false, 0},
	}
	for _, c := range cases {
		hit, until := uiClassifyLimit(c.msg, now)
		if hit != c.hit {
			t.Errorf("%q: hit %v, want %v", c.msg, hit, c.hit)
			continue
		}
		if hit && until.Sub(now) != c.wait {
			t.Errorf("%q: waits %v, want %v", c.msg, until.Sub(now), c.wait)
		}
	}
}

func TestLimitsExhausted(t *testing.T) {
	now := time.Now()
	full := uiLimits{FiveHour: uiWindow{Used: 100, Known: true, ResetsAt: now.Add(time.Hour).Unix()}}
	later := uiLimits{SevenDay: uiWindow{Used: 120, Known: true, ResetsAt: now.Add(3 * time.Hour).Unix()}}
	room := uiLimits{FiveHour: uiWindow{Used: 40, Known: true, ResetsAt: now.Add(time.Hour).Unix()}}
	guess := uiLimits{FiveHour: uiWindow{Used: 100, Known: false, ResetsAt: now.Add(time.Hour).Unix()}}
	past := uiLimits{FiveHour: uiWindow{Used: 100, Known: true, ResetsAt: now.Add(-time.Hour).Unix()}}
	if _, ok := uiLimitsExhausted(nil, now); ok {
		t.Fatal("nothing heard must not hide the harness")
	}
	if u, ok := uiLimitsExhausted([]uiLimits{full}, now); !ok || u.Unix() != full.FiveHour.ResetsAt {
		t.Fatalf("full: %v %v", u, ok)
	}
	if u, ok := uiLimitsExhausted([]uiLimits{full, later}, now); !ok || u.Unix() != full.FiveHour.ResetsAt {
		t.Fatalf("two full accounts free up at the earlier time: %v %v", u, ok)
	}
	for name, l := range map[string]uiLimits{"room": room, "unknown": guess, "reset": past} {
		if _, ok := uiLimitsExhausted([]uiLimits{l}, now); ok {
			t.Errorf("%s must not be exhausted", name)
		}
	}
	if _, ok := uiLimitsExhausted([]uiLimits{full, room}, now); ok {
		t.Fatal("one account with room keeps the harness available")
	}
}

func TestExhaustedStoreExpiryAndClear(t *testing.T) {
	uiNewTestRepo(t)
	now := time.Now()
	uiMarkExhausted("codex", now.Add(time.Hour), "usage limit")
	if u, ok := uiExhaustedAt("codex", now); !ok || !u.After(now) {
		t.Fatal("should be exhausted")
	}
	if _, ok := uiExhaustedAt("codex", now.Add(2*time.Hour)); ok {
		t.Fatal("must expire")
	}
	if _, ok := uiExhaustedAt("codex", now); ok {
		t.Fatal("an expired entry is dropped")
	}
	uiMarkExhausted("codex", now.Add(time.Hour), "")
	uiClearExhausted("codex")
	if _, ok := uiExhaustedAt("codex", now); ok {
		t.Fatal("clear")
	}
}

func uiTestOutcomeAgent(m *uiManager, harness string) *uiAgent {
	return &uiAgent{id: "x", req: uiStartRequest{Harness: harness}, tokens: m.cfg.Tokens, mgr: m, notify: make(chan struct{})}
}

func TestOutcomeMarksAndClears(t *testing.T) {
	m, _, _ := uiTestManager(t, "")
	a := uiTestOutcomeAgent(m, "codex")
	a.add(uiEvent{Kind: "user", Text: "go"})
	a.add(uiEvent{Kind: "error", Text: "Codex refused the turn: You've hit your usage limit. Try again in 45 minutes."})
	a.add(uiEvent{Kind: "result", Text: "The turn did not start.", Failed: true})
	m.noteHarnessOutcome(a, uiFailed)
	u, ok := m.harnessExhaustion("codex", time.Now())
	if !ok || u.Sub(time.Now()) < 44*time.Minute {
		t.Fatalf("not marked: %v %v", u, ok)
	}
	// An ordinary error does not mark another harness.
	b := uiTestOutcomeAgent(m, "cursor")
	b.add(uiEvent{Kind: "user", Text: "go"})
	b.add(uiEvent{Kind: "error", Text: "could not start: no such file"})
	m.noteHarnessOutcome(b, uiFailed)
	if _, ok := m.harnessExhaustion("cursor", time.Now()); ok {
		t.Fatal("ordinary error hid cursor")
	}
	// A later turn that worked clears it early.
	c := uiTestOutcomeAgent(m, "codex")
	c.add(uiEvent{Kind: "user", Text: "again"})
	c.add(uiEvent{Kind: "result", Text: "done"})
	m.noteHarnessOutcome(c, uiWaiting)
	if _, ok := m.harnessExhaustion("codex", time.Now()); ok {
		t.Fatal("success should clear")
	}
	// Claude is never learned from failures.
	d := uiTestOutcomeAgent(m, "")
	d.add(uiEvent{Kind: "error", Text: "usage limit reached"})
	m.noteHarnessOutcome(d, uiFailed)
	if _, ok := uiExhaustedAt("claude", time.Now()); ok {
		t.Fatal("claude comes from the limits data")
	}
}

func TestHarnessAvailableMarksExhausted(t *testing.T) {
	m, _, _ := uiTestManager(t, "")
	m.setLimits(0, uiLimits{FiveHour: uiWindow{Used: 100, Known: true, ResetsAt: time.Now().Add(time.Hour).Unix()}})
	uiMarkExhausted("codex", time.Now().Add(time.Hour), "quota")
	list := []uiHarnessChoice{{ID: "claude", Available: true}, {ID: "codex", Available: true}, {ID: "cursor"}}
	m.markExhausted(list)
	if !list[0].Exhausted || !list[1].Exhausted || list[2].Exhausted || list[0].Until == 0 {
		t.Fatalf("%+v", list)
	}
	b, _ := json.Marshal(list[1])
	if !strings.Contains(string(b), `"exhausted":true`) || !strings.Contains(string(b), `"until":`) {
		t.Fatalf("json %s", b)
	}
}
