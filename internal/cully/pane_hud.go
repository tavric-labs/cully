package cully

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// paneHUD is the one-line Agent Health bar that replaces the panel's top rule:
//
//	── Codex │ cully:main │ 27m │ Context 63% │ ⚠ Loop 3x │ ● 2 unchecked ── Ctrl+] advisor ──
//
// It shows only measured values, so a segment with no data is left out. The
// whole panel is clickable, and the bar ends with the advisor hint.
func paneHUD(cols int, stats toolStats, view sessionView, now time.Time) string {
	if cols < 24 {
		return ""
	}
	var parts []string
	if view.Agent != "" {
		parts = append(parts, bold+journalAgentName(view.Agent)+rst)
	}
	if project := filepath.Base(view.Project); project != "" && project != "." && project != string(filepath.Separator) {
		if view.Branch != "" {
			project += ":" + view.Branch
		}
		parts = append(parts, project)
	}
	if !view.Started.IsZero() {
		parts = append(parts, healthDuration(now.Sub(view.Started)))
	}
	if view.ContextKnown {
		used := min(100, max(0, 100-view.ContextLeft))
		color := ""
		switch {
		case used >= 90:
			color = red
		case used >= 75:
			color = yellow
		}
		parts = append(parts, color+"Context "+strconv.Itoa(used)+"%"+rst)
	}
	if view.Loops.Active() {
		parts = append(parts, yellow+"⚠ Loop "+strconv.Itoa(view.Loops.Loops[0].Attempts)+"x"+rst)
	}
	if stats.EditsSinceCheck > 0 {
		parts = append(parts, "● "+strconv.Itoa(stats.EditsSinceCheck)+" unchecked")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, dim+" │ "+rst)
}

// hudRule draws the HUD inside a horizontal rule of the given width.
func hudRule(width int, hud string, hints ...string) string {
	return hudRuleGrip(width, hud, gripIdle, hints...)
}

// hudRuleGrip is hudRule with a drag handle whose weight follows the pointer:
// a heavy tinted rule at rest, a bright one on hover and a solid band while
// dragging. Without a HUD or room for the handle it stays a plain rule.
func hudRuleGrip(width int, hud string, grip gripState, hints ...string) string {
	if hud == "" || width < 24 {
		return dim + strings.Repeat("─", max(0, width)) + rst
	}
	hint := " Ctrl+] advisor "
	if len(hints) > 0 {
		hint = hints[0]
	}
	chip, tone, fillRune := " ⠿ ⇕ ⠿ ", "\x1b[36m", "━"
	switch grip {
	case gripHover:
		chip, tone = " ⠿ ▲▼ ⠿ ", "\x1b[1;93m"
	case gripDrag:
		chip, tone, fillRune = " ▲ ⇕ ▼ ", "\x1b[1;30;103m", " "
	}
	chipStyle := "\x1b[1;30;46m"
	if grip == gripHover {
		chipStyle = "\x1b[1;30;103m"
	} else if grip == gripDrag {
		chipStyle = "\x1b[1;97;41m"
	}
	handle := chipStyle + chip + rst + tone
	if grip != gripIdle || width >= 40 {
		hint = " " + strings.TrimLeft(hint, " ")
	}
	rule := "━"
	if grip == gripDrag {
		rule = " "
	}
	// On a narrow bar the hint goes first, then the HUD text shrinks; the handle stays.
	for _, h := range []string{hint, ""} {
		body := ansi.Truncate(hud, max(1, width-ansi.StringWidth(h)-ansi.StringWidth(chip)-8), "…")
		left := tone + rule + rule + " " + rst + body + tone + " "
		fill := width - ansi.StringWidth(ansi.Strip(left)) - ansi.StringWidth(chip) - ansi.StringWidth(h) - 2
		if fill >= 2 {
			return left + strings.Repeat(fillRune, fill) + handle + h + rule + rule + rst
		}
	}
	return tone + strings.Repeat(rule, max(0, width)) + rst
}
