package cully

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type improveFake struct {
	planned, applied, dropped []string
	planErr, applyErr         error
	plan                      applyPlan
}

func (f *improveFake) actions() improveActions {
	return improveActions{
		Home: "/home/u",
		Plan: func(p improvement) (applyPlan, []string, error) {
			f.planned = append(f.planned, p.ID)
			return f.plan, []string{"shell commands"}, f.planErr
		},
		Apply: func(p improvement, _ applyPlan) error { f.applied = append(f.applied, p.ID); return f.applyErr },
		Drop:  func(p improvement) error { f.dropped = append(f.dropped, p.ID); return nil },
	}
}

func improveItems() []improvement {
	now := time.Now()
	return []improvement{
		{ID: "setup-1", Suite: "setup", Status: "pending", Created: now, Updated: now, Evidence: []evalFailure{{Check: "skill linked", Detail: "missing"}, {Check: "hooks installed"}}},
		{ID: "harness-2", Suite: "harness", Status: "pending", Created: now, Updated: now, Evidence: []evalFailure{{Check: "Skills missing"}}},
	}
}

func rune_(r rune) replayKey { return replayKey{Kind: "rune", R: r} }

func newTestImprove(f *improveFake) *improveModel {
	f.plan = applyPlan{Summary: "Guard links", ClaudeMDSection: "Check links."}
	return newImproveModel(improveItems(), f.actions(), 80, 24, false)
}

func TestImproveSelectionNeverDropsAnything(t *testing.T) {
	f := &improveFake{}
	m := newTestImprove(f)
	m.Update(replayKey{Kind: "down"})
	m.Update(replayKey{Kind: "enter"}) // details
	if m.Mode != improveDetail || m.Cursor != 1 {
		t.Fatalf("mode=%s cursor=%d", m.Mode, m.Cursor)
	}
	m.Update(replayKey{Kind: "esc"}) // back
	m.Update(replayKey{Kind: "esc"}) // quit
	if !m.Quit || len(m.Items) != 2 || len(f.dropped)+len(f.applied)+len(f.planned) != 0 {
		t.Fatalf("looking changed something: %+v %+v", m.Items, f)
	}
}

func TestImproveDetailOnTitleClickAndRender(t *testing.T) {
	f := &improveFake{}
	m := newTestImprove(f)
	m.Render()
	m.Update(replayKey{Kind: "click", X: 5, Y: improveListTop}) // the first title
	if m.Mode != improveDetail {
		t.Fatalf("click on a title should open details: %s", m.Mode)
	}
	text := strings.Join(m.Render(), "\n")
	for _, want := range []string{"Setup: skill linked (+1 more)", "skill linked — missing", "hooks installed", "What Apply does", "[ Apply ]", "[ Drop ]"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail lacks %q:\n%s", want, text)
		}
	}
}

func TestImproveApplyShowsPlanThenWritesOnlyOnYes(t *testing.T) {
	f := &improveFake{}
	m := newTestImprove(f)
	m.Update(rune_('a'))
	if m.Busy != "plan" || len(f.applied) != 0 {
		t.Fatal("apply did not start planning")
	}
	m.Update(rune_('y')) // ignored while planning
	m.Finish()
	if m.Mode != improveReview || len(f.planned) != 1 {
		t.Fatalf("mode=%s planned=%v", m.Mode, f.planned)
	}
	if text := strings.Join(m.Render(), "\n"); !strings.Contains(text, "Guard links") || !strings.Contains(text, "Ignored") || !strings.Contains(text, "[ Yes, apply ]") {
		t.Fatalf("plan not shown:\n%s", text)
	}
	// Declining keeps the suggestion.
	m.Update(rune_('n'))
	if m.Mode != improveDetail || len(f.applied) != 0 || len(m.Items) != 2 || !strings.Contains(m.Message, "still pending") {
		t.Fatalf("cancel changed state: %s %v %d %q", m.Mode, f.applied, len(m.Items), m.Message)
	}
	m.Update(rune_('a'))
	m.Finish()
	m.Update(rune_('y'))
	if len(f.applied) != 1 || f.applied[0] != "setup-1" || len(m.Items) != 1 || m.Mode != improveList {
		t.Fatalf("approval did not apply: %v %d %s", f.applied, len(m.Items), m.Mode)
	}
}

func TestImproveApplyFailuresAndEmptyPlansKeepTheSuggestion(t *testing.T) {
	for name, f := range map[string]*improveFake{
		"plan error":   {planErr: errors.New("model offline")},
		"empty plan":   {},
		"apply failed": {applyErr: errors.New("disk full")},
	} {
		t.Run(name, func(t *testing.T) {
			m := newTestImprove(f)
			if name == "empty plan" {
				f.plan = applyPlan{Notes: "needs a code change"}
			}
			m.Update(rune_('a'))
			m.Finish()
			if m.Mode == improveReview {
				m.Update(rune_('y'))
			}
			if len(m.Items) != 2 || m.Mode != improveDetail || !strings.Contains(m.Message, "still pending") {
				t.Fatalf("suggestion lost or wrong state: %d %s %q", len(m.Items), m.Mode, m.Message)
			}
		})
	}
}

func TestImproveDropNeedsConfirmation(t *testing.T) {
	f := &improveFake{}
	m := newTestImprove(f)
	m.Update(rune_('d'))
	if m.Mode != improveDrop || len(f.dropped) != 0 {
		t.Fatal("drop acted before confirming")
	}
	m.Update(replayKey{Kind: "esc"})
	if len(f.dropped) != 0 || len(m.Items) != 2 || m.Mode != improveDetail {
		t.Fatalf("esc dropped: %v %d %s", f.dropped, len(m.Items), m.Mode)
	}
	m.Update(rune_('d'))
	m.Update(rune_('n'))
	if len(f.dropped) != 0 || len(m.Items) != 2 {
		t.Fatal("n dropped")
	}
	m.Update(rune_('d'))
	m.Update(rune_('y'))
	if len(f.dropped) != 1 || f.dropped[0] != "setup-1" || len(m.Items) != 1 || m.Mode != improveList {
		t.Fatalf("confirmed drop failed: %v %d %s", f.dropped, len(m.Items), m.Mode)
	}
}

func TestImproveButtonsAreClickable(t *testing.T) {
	f := &improveFake{}
	m := newTestImprove(f)
	m.Update(replayKey{Kind: "enter"})
	m.Render()
	var drop improveButton
	for _, b := range m.buttons {
		if b.Label == "Drop" {
			drop = b
		}
	}
	m.Update(replayKey{Kind: "click", X: drop.X0 + 1, Y: m.Height})
	if m.Mode != improveDrop {
		t.Fatalf("Drop button ignored: %s", m.Mode)
	}
	m.Render()
	for _, b := range m.buttons {
		if b.Label == "No, keep it" {
			m.Update(replayKey{Kind: "click", X: b.X0, Y: m.Height})
		}
	}
	if m.Mode != improveDetail || len(f.dropped) != 0 {
		t.Fatalf("keep button failed: %s %v", m.Mode, f.dropped)
	}
}

func TestImproveRenderFitsTheTerminal(t *testing.T) {
	f := &improveFake{}
	for _, size := range [][2]int{{40, 10}, {80, 24}, {120, 40}} {
		m := newTestImprove(f)
		m.Width, m.Height = size[0], size[1]
		for _, mode := range []string{improveList, improveDetail, improveReview, improveDrop} {
			m.Mode = mode
			lines := m.Render()
			if len(lines) != m.Height {
				t.Fatalf("%dx%d %s: %d lines", size[0], size[1], mode, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.Width {
					t.Fatalf("%dx%d %s: line wider than the terminal: %q", size[0], size[1], mode, line)
				}
			}
		}
	}
}
