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
	for _, want := range []string{"Setup 5/7 ready", "Gaps Guardrails, Plugins", "Waste 2.3k tok/session · Bash 1.5k", "10 Claude sessions"} {
		if !strings.Contains(got, want) {
			t.Errorf("rows lack %q:\n%s", want, got)
		}
	}
	if pending := ansi.Strip(strings.Join(harnessPanelRows(harnessPanelData{Ready: 7, Total: 7}), "\n")); !strings.Contains(pending, "measuring") || strings.Contains(pending, "Gaps") {
		t.Errorf("unmeasured, complete setup: %s", pending)
	}
	if none := ansi.Strip(strings.Join(harnessPanelRows(harnessPanelData{Ready: 7, Total: 7, Measured: true}), "\n")); !strings.Contains(none, "no recent Claude sessions") {
		t.Errorf("no sessions: %s", none)
	}
}

func TestPanelDropsCountersAndUnavailablePlaceholders(t *testing.T) {
	isolateWorkflow(t)
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

func TestHarnessPanelAppliesDelegationEvidenceAndRejectsStaleWorkers(t *testing.T) {
	items := []harnessItem{{Component: "Instructions", Found: []string{"AGENTS.md"}}, {Component: "Subagents"}}
	c := harnessPanelState{generation: 3, measuring: true, items: items}
	c.finishMeasurement(2, harnessEvidence{Sessions: 1, Delegations: 99})
	if !c.measuring || c.data.Measured {
		t.Fatal("a stale worker changed the active measurement")
	}
	c.finishMeasurement(3, harnessEvidence{Sessions: 1, Delegations: 2})
	if c.measuring || !c.data.Measured || c.data.Ready != 2 || len(c.data.Missing) != 0 || c.data.Evidence.Delegations != 2 {
		t.Fatalf("panel disagrees with evidence-based CLI audit: %+v", c.data)
	}
}

func TestCompactPanelOmitsOnlyMissingPartialInstruments(t *testing.T) {
	isolateWorkflow(t)
	for _, view := range []sessionView{
		{FiveHour: "5h 82% left", Fast: "Fast off"},
		{Weekly: "Weekly 91% left", ChangesKnown: true, LinesAdded: 2},
	} {
		rows, _ := compactInstrumentRows(149, toolStats{}, view)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		for _, missing := range []string{"5h unavailable", "Week unavailable", "Fast unavailable", "Git unavailable"} {
			if strings.Contains(plain, missing) {
				t.Errorf("invented placeholder %q: %s", missing, plain)
			}
		}
		if view.Fast != "" && !strings.Contains(plain, "Fast off") || view.ChangesKnown && !strings.Contains(plain, "Git +2/-0") {
			t.Fatalf("observed value was dropped: %s", plain)
		}
	}
}
