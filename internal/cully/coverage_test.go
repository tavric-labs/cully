package cully

import (
	"os"
	"strings"
	"testing"
)

func TestJournalReportsRotationAndReadFailures(t *testing.T) {
	cwd := journalTestEnv(t)
	appendJournal(cwd, "s", journalEvent{Class: journalStart, Agent: "codex"})
	path := journalPath(cwd, "s")
	line := `{"c":"T","t":"2026-10-21T10:00:00Z"}` + "\n"
	for i := 0; i < 2; i++ {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(strings.Repeat(line, journalMaxBytes/len(line)+1))
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		appendJournal(cwd, "s", journalEvent{Class: journalEdit})
		if got := journalCoverage(readJournal(path)); !strings.Contains(got, "discarded") {
			t.Fatalf("rotation %d hidden: %q", i, got)
		}
	}
	if err := os.WriteFile(path, []byte(line+"invalid private payload\n"+line), 0o600); err != nil {
		t.Fatal(err)
	}
	events := readJournal(path)
	if len(events) != 3 || !strings.Contains(journalCoverage(events), "invalid") {
		t.Fatalf("corruption hidden: %+v", events)
	}
	if got := renderSessionHealth(events, healthIn(0, -1)); !strings.Contains(got, "UNKNOWN") || strings.Contains(got, "private payload") {
		t.Fatal(got)
	}
	if got := journalCoverage(readJournal(cwd)); !strings.Contains(got, "invalid") {
		t.Fatalf("directory read failure hidden: %s", got)
	}
	if got := journalCoverage(readJournal(path + "-missing")); !strings.Contains(got, "unavailable") {
		t.Fatal(got)
	}
}

func TestJournalReportsScannerLimit(t *testing.T) {
	cwd := journalTestEnv(t)
	appendJournal(cwd, "s", journalEvent{Agent: "codex", Class: journalStart})
	f, err := os.OpenFile(journalPath(cwd, "s"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(strings.Repeat("x", (1<<20)+1))
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := journalCoverage(readJournal(journalPath(cwd, "s"))); !strings.Contains(got, "invalid") {
		t.Fatal(got)
	}
}

func TestMissingSignalsStayUnknownAcrossReports(t *testing.T) {
	events := []journalEvent{{Agent: "codex", Class: journalStart}}
	got := renderSessionHealth(events, healthIn(0, -1))
	for _, want := range []string{"no tool events received", "UNKNOWN", "unknown: incomplete"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	gap := append(events, journalEvent{Class: journalNote, Note: noteHistoryTrimmed}, journalEvent{Class: journalCheck})
	for name, report := range map[string]string{
		"status":   renderSessionHealth(gap, healthIn(0, -1)),
		"handoff":  buildHandoff(handoffInput{Events: gap}),
		"timeline": renderTimeline(gap, false),
		"replay":   strings.Join(replayPartial(gap), "\n"),
		"rescue":   strings.Join(buildRescueEvidence(gap, "", "", nil, 0, rescueNow).bullets(), "\n"),
	} {
		if !strings.Contains(report, "discarded") {
			t.Fatalf("%s hides journal gap: %s", name, report)
		}
	}
}

func TestMalformedAndTruncatedToolSignalsReportPartialCoverage(t *testing.T) {
	journalTestEnv(t)
	for _, tc := range []struct{ name, data string }{
		{"malformed", "E.\nTz\n??\nS.\n"},
		{"truncated", strings.Repeat("S.\n", 3000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(signalFile(tc.name), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			stats := readToolStats(tc.name)
			if !stats.Partial || strings.Contains(panelVerification(stats, sessionView{Agent: "codex"}), "no pending edits") {
				t.Fatalf("incomplete signals claimed clean: %+v", stats)
			}
		})
	}
}

func TestCursorUnknownCheckCannotVerifyEditsOrResolveLoop(t *testing.T) {
	cwd := journalTestEnv(t)
	t.Setenv("MODEL_HINT_GUARD", "")
	t.Setenv("CULLY_PANE_SESSION", "cursor-session")
	appendJournal(cwd, "cursor-session", journalEvent{Class: journalStart, Agent: "cursor"})
	appendJournal(cwd, "cursor-session", journalEvent{Class: journalEdit, Agent: "cursor"})
	RunPaneSignalHook("cursor", strings.NewReader(`{"hook_event_name":"afterFileEdit","file_path":"a.go","cwd":"`+cwd+`"}`))
	RunPaneSignalHook("cursor", strings.NewReader(`{"hook_event_name":"afterShellExecution","command":"go test ./...","cwd":"`+cwd+`"}`))
	stats := readToolStats("cursor-session")
	if stats.UnknownChecks != 1 || stats.EditsSinceCheck != 1 {
		t.Fatalf("unknown check cleared verification: %+v", stats)
	}
	events := readJournal(journalPath(cwd, "cursor-session"))
	h := measureHealth(events)
	if h.ChecksPassed != 0 || h.ChecksUnknown != 1 || !strings.Contains(h.Verification, "unknown") {
		t.Fatalf("unknown check claimed success: %+v", h)
	}
	loop := append(plantedJournal("FFF"), journalEvent{Class: journalCheck, Sig: "a", Unknown: true})
	if !detectLoops(loop).Active() {
		t.Fatal("unknown check resolved failing loop")
	}
	steps := buildSteps(events)
	if steps[len(steps)-1].Outcome != "unknown" {
		t.Fatalf("replay claimed success: %+v", steps)
	}
}
