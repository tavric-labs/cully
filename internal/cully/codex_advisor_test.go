//go:build !windows

package cully

import (
	"strings"
	"testing"
	"time"
)

func TestCodexAdviceContextPressure(t *testing.T) {
	for _, tc := range []struct {
		left   int
		prefix string
	}{
		{0, "WARN|"}, {10, "WARN|"}, {11, "CAUT|"}, {25, "CAUT|"}, {26, "MEMO|"},
	} {
		got := statsAdviceWithStatus(toolStats{Tools: 1}, sessionView{ContextKnown: true, ContextLeft: tc.left})
		if len(got) != 1 || !strings.HasPrefix(got[0], tc.prefix) {
			t.Fatalf("context %d%% produced %v", tc.left, got)
		}
	}
}

func TestAdvisorSignalsPreserveHostAndEvidenceLimits(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "cursor", "custom-agent"} {
		view := sessionView{Agent: agent, JournalCoverage: "partial: older journal history was discarded"}
		sig := advisorSignals(toolStats{Tools: 3, UnknownChecks: 1, Partial: true}, view)
		if advisorAgent(sig) != agent {
			t.Fatalf("routed %s as %s", agent, advisorAgent(sig))
		}
		for _, want := range []string{"unknown_check_outcomes=1", "signal_history_partial=true", "journal_coverage=partial:", "signal_coverage="} {
			if !strings.Contains(sig, want) {
				t.Fatalf("%s lost %q: %s", agent, want, sig)
			}
		}
		if got := panelVerification(toolStats{Tools: 3}, view); strings.Contains(got, "no pending edits") {
			t.Fatalf("journal gap claimed clean: %s", got)
		}
	}
	for _, view := range []sessionView{{Agent: "cursor"}, {Agent: "custom-agent"}} {
		if got := panelVerification(toolStats{Tools: 3}, view); strings.Contains(got, "no pending edits") {
			t.Fatal(got)
		}
	}
}

func TestCodexCombinedAdviceKeepsWarningsAndDeepFindings(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	session := "combined"
	if err := writeReportLines(session, "/project", []string{"ADV|🔌 Audit a relevant integration."}); err != nil {
		t.Fatal(err)
	}
	writeSnapshot(session, cullySnapshot{AdvisorOK: true, AdvisorAgent: "codex", AdvisorAt: time.Now().UTC().Format(time.RFC3339), AdvisorMemory: "checked", AdvisorResearch: "skipped"})
	got := combinedAdvice(session, toolStats{Tools: 5, EditsSinceCheck: 2}, sessionView{ContextKnown: true, ContextLeft: 8})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"nearly full", "focused check", "Audit a relevant integration", "memory checked"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lost %q in %v", want, got)
		}
	}
	if after := readSuggestions(session); len(after) != 1 || after[0] != "ADV|🔌 Audit a relevant integration." {
		t.Fatal("panel rewrote worker report", after)
	}
}

func TestCodexAdvicePreservesWorkflowWarnings(t *testing.T) {
	got := statsAdviceWithStatus(toolStats{Tools: 20, Errors: 3, Searches: 11, EditsSinceCheck: 2}, sessionView{ContextKnown: true, ContextLeft: 8})
	if len(got) != 4 || !strings.HasPrefix(got[0], "WARN|") || !strings.Contains(got[1], "failed") || !strings.Contains(got[2], "searches") || !strings.Contains(got[3], "check") {
		t.Fatalf("missing source-specific warnings: %v", got)
	}
}

func TestCodexAdviceReportsUnavailableSignals(t *testing.T) {
	got := statsAdviceWithStatus(toolStats{}, sessionView{ContextKnown: true, ContextLeft: 80})
	if len(got) != 1 || !strings.Contains(got[0], "Awaiting Codex tool signals") || strings.Contains(got[0], "steady") {
		t.Fatalf("no events must not imply healthy workflow: %v", got)
	}
	got = statsAdviceWithStatus(toolStats{Tools: 1}, sessionView{})
	if len(got) != 1 || !strings.Contains(got[0], "Context pressure is unavailable") {
		t.Fatalf("missing context should be explicit: %v", got)
	}
	got = statsAdviceWithStatus(toolStats{}, sessionView{ContextKnown: true, ContextLeft: 8})
	if len(got) != 2 || !strings.HasPrefix(got[0], "WARN|") || !strings.Contains(got[1], "Awaiting") {
		t.Fatalf("known pressure must survive missing workflow signals: %v", got)
	}
}
