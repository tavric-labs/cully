package cully

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHarnessPanelRowsShowSetupGapsAndMeasuredWaste(t *testing.T) {
	d := harnessPanelData{Ready: 5, Total: 7, Missing: []string{"Guardrails", "Plugins"}, Measured: true,
		Evidence: harnessEvidence{Sessions: 10, BigTokens: 23000, BashTokens: 15000}}
	got := ansi.Strip(strings.Join(harnessPanelRows(d), "\n"))
	for _, want := range []string{"Setup 5/7 ready", "Gaps Guardrails, Plugins", "Waste 2.3k tok/session · Bash 1.5k", "10 sessions"} {
		if !strings.Contains(got, want) {
			t.Errorf("rows lack %q:\n%s", want, got)
		}
	}
	if pending := ansi.Strip(strings.Join(harnessPanelRows(harnessPanelData{Ready: 7, Total: 7}), "\n")); !strings.Contains(pending, "measuring") || strings.Contains(pending, "Gaps") {
		t.Errorf("unmeasured, complete setup: %s", pending)
	}
	if none := ansi.Strip(strings.Join(harnessPanelRows(harnessPanelData{Ready: 7, Total: 7, Measured: true}), "\n")); !strings.Contains(none, "no recent sessions") {
		t.Errorf("no sessions: %s", none)
	}
}

func TestPanelDropsCountersAndUnavailablePlaceholders(t *testing.T) {
	rows, _ := compactInstrumentRows(100, toolStats{Tools: 14, Searches: 3, Edits: 2, Checks: 1, Commits: 1}, sessionView{Project: "/work/cully"})
	plain := ansi.Strip(strings.Join(rows, "\n"))
	for _, ban := range []string{"Tools 14", "Searches", "Commits", "Web 0", "Fast / Git unavailable", "Git unavailable", "Weekly unavailable"} {
		if strings.Contains(plain, ban) {
			t.Errorf("panel still shows %q", ban)
		}
	}
	if !strings.Contains(plain, "Harness") || !strings.Contains(plain, "Setup ") {
		t.Errorf("panel lacks the harness column:\n%s", plain)
	}
}
