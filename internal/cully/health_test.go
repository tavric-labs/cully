package cully

import (
	"strings"
	"testing"
	"time"
)

func healthIn(files, ctx int) healthInputs {
	return healthInputs{Branch: "main", GitFiles: files, ContextUsed: ctx, Now: rescueNow}
}

func TestRenderSessionHealthNoJournal(t *testing.T) {
	got := renderSessionHealth(nil, healthIn(0, -1))
	if !strings.Contains(got, "cully run AGENT") || strings.Contains(got, "Risk") {
		t.Fatalf("got %q", got)
	}
}

func TestRenderSessionHealthRunning(t *testing.T) {
	events := []journalEvent{
		{Time: rescueNow.Add(-90 * time.Minute), Agent: "codex", Class: journalStart},
		{Time: rescueNow.Add(-80 * time.Minute), Class: journalEdit},
		{Time: rescueNow.Add(-70 * time.Minute), Class: journalCheck, Failed: true, Sig: "a"},
		{Time: rescueNow.Add(-60 * time.Minute), Class: journalSearch},
		{Time: rescueNow.Add(-50 * time.Minute), Class: journalMemory, Tool: "cully_context"},
		{Time: rescueNow.Add(-45 * time.Minute), Class: journalMemory, Tool: "cully_log"},
		{Time: rescueNow.Add(-40 * time.Minute), Class: journalNote, Note: noteLoop},
	}
	got := renderSessionHealth(events, healthIn(4, 71))
	for _, want := range []string{
		"CULLY", "Agent          Codex", "Branch         main", "Session        1h30m (running)",
		"Context        ███████░░░ 71%", "Tests          0 ✓  1 ✕", "Verification   failed",
		"Loops          1", "Uncommitted    4 files", "Memory         1 recall · 1 save",
		"Risk           HIGH · the last check failed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	for _, banned := range []string{"Progress", "Task"} {
		if strings.Contains(got, banned) {
			t.Fatalf("status must not show unmeasured %q:\n%s", banned, got)
		}
	}
}

func TestSessionHealthVerification(t *testing.T) {
	edit := journalEvent{Class: journalEdit}
	pass := journalEvent{Class: journalCheck}
	fail := journalEvent{Class: journalCheck, Failed: true}
	cases := map[string][]journalEvent{
		"no edits":               {pass},
		"none since last edit":   {pass, edit},
		"passed since last edit": {edit, fail, pass},
		"failed":                 {edit, pass, edit, fail},
	}
	for want, events := range cases {
		events = append([]journalEvent{{Class: journalStart}}, events...)
		if got := measureHealth(events).Verification; got != want {
			t.Fatalf("%v: got %q want %q", events, got, want)
		}
	}
}

func TestRenderSessionHealthEndedHidesUnknowns(t *testing.T) {
	events := []journalEvent{
		{Time: rescueNow.Add(-time.Hour), Class: journalStart},
		{Time: rescueNow.Add(-30 * time.Minute), Class: journalEnd},
	}
	got := renderSessionHealth(events, healthInputs{GitFiles: -1, ContextUsed: -1, Now: rescueNow})
	if !strings.Contains(got, "30m (ended)") || strings.Contains(got, "Uncommitted") ||
		strings.Contains(got, "Context") || strings.Contains(got, "Branch") {
		t.Fatalf("unknown values must be left out:\n%s", got)
	}
	if !strings.Contains(got, "Memory         none this session") {
		t.Fatalf("got\n%s", got)
	}
}

func TestHealthRiskRules(t *testing.T) {
	cases := []struct {
		name  string
		h     sessionHealth
		in    healthInputs
		level string
	}{
		{"loop", sessionHealth{Looping: true, Verification: "passed since last edit"}, healthIn(0, -1), "HIGH"},
		{"resolved loop", sessionHealth{Loops: 1, Verification: "passed since last edit"}, healthIn(2, 30), "LOW"},
		{"failed check", sessionHealth{Verification: "failed"}, healthIn(0, -1), "HIGH"},
		{"context full", sessionHealth{Verification: "no edits"}, healthIn(0, 92), "HIGH"},
		{"unchecked edits", sessionHealth{Verification: "none since last edit"}, healthIn(0, -1), "MEDIUM"},
		{"context filling", sessionHealth{Verification: "no edits"}, healthIn(0, 80), "MEDIUM"},
		{"many files", sessionHealth{Verification: "no edits"}, healthIn(20, -1), "MEDIUM"},
		{"clean", sessionHealth{Verification: "passed since last edit"}, healthIn(2, 30), "LOW"},
	}
	for _, c := range cases {
		if level, reason := healthRisk(c.h, c.in); level != c.level || reason == "" {
			t.Fatalf("%s: got %s %q, want %s", c.name, level, reason, c.level)
		}
	}
}

func TestHealthGauge(t *testing.T) {
	if got := healthGauge(71); got != "███████░░░ 71%" {
		t.Fatalf("gauge = %q", got)
	}
	if healthGauge(-5) != "░░░░░░░░░░ 0%" || healthGauge(140) != "██████████ 100%" {
		t.Fatal("gauge must clamp")
	}
}
