package cully

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestPaneHUDShowsOnlyMeasuredValues(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	view := sessionView{
		Agent: "codex", Project: "/work/oauth-service", Branch: "main",
		Started: now.Add(-27 * time.Minute), ContextKnown: true, ContextLeft: 37,
		Loops: loopReport{Loops: []loopFinding{{Kind: loopRepeatFailure, Attempts: 3}}},
	}
	got := ansi.Strip(paneHUD(120, toolStats{EditsSinceCheck: 2}, view, now))
	for _, want := range []string{"Codex", "oauth-service:main", "27m", "Context 63%", "⚠ Loop 3x", "● 2 unchecked"} {
		if !strings.Contains(got, want) {
			t.Fatalf("HUD missing %q: %s", want, got)
		}
	}
	bare := ansi.Strip(paneHUD(120, toolStats{}, sessionView{Agent: "claude", Project: "/p/cully"}, now))
	for _, banned := range []string{"Context", "Loop", "unchecked", "Task", "Progress"} {
		if strings.Contains(bare, banned) {
			t.Fatalf("HUD invented %q: %s", banned, bare)
		}
	}
	if !strings.Contains(bare, "Claude Code") {
		t.Fatalf("agent name missing: %s", bare)
	}
	if paneHUD(10, toolStats{}, view, now) != "" {
		t.Fatal("a very narrow terminal gets no HUD")
	}
}

func TestPaneHUDContextColors(t *testing.T) {
	now := time.Now()
	for left, color := range map[int]string{5: red, 20: yellow, 60: ""} {
		got := paneHUD(120, toolStats{}, sessionView{Agent: "codex", ContextKnown: true, ContextLeft: left}, now)
		if color != "" && !strings.Contains(got, color) {
			t.Fatalf("left %d%% should use %q", left, color)
		}
		if color == "" && (strings.Contains(got, red) || strings.Contains(got, yellow)) {
			t.Fatalf("left %d%% should be uncolored", left)
		}
	}
}

func TestHUDRuleFitsWidth(t *testing.T) {
	hud := paneHUD(100, toolStats{}, sessionView{Agent: "cursor", Project: "/p/app", Branch: "feature/long-branch-name", Started: time.Now()}, time.Now())
	for _, width := range []int{24, 40, 80, 100, 200} {
		rule := hudRule(width, hud)
		if got := ansi.StringWidth(ansi.Strip(rule)); got != width {
			t.Fatalf("width %d: rule is %d wide: %q", width, got, ansi.Strip(rule))
		}
	}
	if got := ansi.Strip(hudRule(60, "")); got != strings.Repeat("─", 60) {
		t.Fatalf("no HUD should leave the plain rule, got %q", got)
	}
	if !strings.Contains(ansi.Strip(hudRule(100, hud)), "Ctrl+] advisor") {
		t.Fatal("the advisor hint should be visible when there is room")
	}
}

func TestHUDRuleGripFitsWidthAndGrowsStronger(t *testing.T) {
	hud := paneHUD(100, toolStats{}, sessionView{Agent: "codex", Project: "/p/app", Branch: "main", Started: time.Now()}, time.Now())
	var weights []int
	for _, grip := range []gripState{gripIdle, gripHover, gripDrag} {
		for _, width := range []int{24, 40, 80, 200} {
			rule := hudRuleGrip(width, hud, grip)
			if got := ansi.StringWidth(ansi.Strip(rule)); got != width {
				t.Fatalf("grip %v width %d: %d wide: %q", grip, width, got, ansi.Strip(rule))
			}
			if !strings.Contains(ansi.Strip(rule), "⇕") && !strings.Contains(ansi.Strip(rule), "▲") && !strings.Contains(ansi.Strip(rule), "▼") {
				t.Fatalf("grip %v width %d has no visible handle: %q", grip, width, ansi.Strip(rule))
			}
		}
		weights = append(weights, len(hudRuleGrip(80, hud, grip)))
	}
	if hudRuleGrip(80, hud, gripIdle) == hudRuleGrip(80, hud, gripHover) || hudRuleGrip(80, hud, gripHover) == hudRuleGrip(80, hud, gripDrag) {
		t.Fatalf("grip states must look different: %v", weights)
	}
}
