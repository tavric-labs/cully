//go:build !windows

package cully

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type advisorDrawer struct {
	Open, Details    bool
	Items            []string
	Selected, Scroll int
	Message          string
	HitRows          map[int]int
	AcceptRow        int
}

func (d *advisorDrawer) open(advice []string) {
	d.Open = true
	d.Details = false
	d.Selected = 0
	d.Scroll = 0
	d.Message = ""
	d.Items = append([]string(nil), advice...)
}

func (d *advisorDrawer) close() { d.Open = false }

func (d *advisorDrawer) move(delta int) {
	if d.Details {
		d.Scroll = max(0, d.Scroll+delta)
		return
	}
	d.Selected = min(max(0, d.Selected+delta), max(0, len(d.Items)-1))
	d.Message = ""
}

func (d *advisorDrawer) escape() {
	if d.Details {
		d.Details = false
		d.Scroll = 0
		d.Message = ""
		return
	}
	d.close()
}

// The full advisor is an overlay: opening it never resizes the child PTY or
// reflows the conversation. Mouse targets correspond to visible screen rows.
func (d *advisorDrawer) render(cols, rows int, stats toolStats, view sessionView) string {
	width := max(1, cols-5)
	var lines []string
	add := func(text string) {
		for _, line := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
			lines = append(lines, "  "+line+rst)
		}
	}
	add(cyan + bold + "✦ Cully Advisor" + rst + dim + "  ·  " + view.Project + rst)
	add(dim + "↑/↓ select · Enter add to prompt · Tab instruments · Esc close" + rst)
	add(strings.Repeat("─", width))
	d.HitRows = make(map[int]int)
	d.AcceptRow = 0
	budget := max(1, rows-4-len(lines))
	if d.Details {
		info := sessionStatusContent(cols, nil, stats, view).status
		info = append(info, "")
		info = append(info, cullyToolRows(stats.Cully, max(1, cols-5))...)
		d.Scroll = min(d.Scroll, max(0, len(info)-budget))
		lines = append(lines, info[d.Scroll:min(len(info), d.Scroll+budget)]...)
	} else {
		// Display complete selected advice, with surrounding items when space permits.
		first := min(d.Scroll, d.Selected)
		if d.Selected >= first+max(1, budget/3) {
			first = d.Selected - max(0, budget/3-1)
		}
		d.Scroll = max(0, first)
		for i := d.Scroll; i < len(d.Items); i++ {
			prefix := "  "
			colour, badge := adviceBadge(d.Items[i])
			if i == d.Selected {
				prefix = "▶ "
				colour += bold
			}
			text := badge + " · " + terminalSafeText(stripSeverityPrefix(d.Items[i]))
			wrapped := strings.Split(ansi.Wrap(text, max(1, width-4), ""), "\n")
			// A large recommendation cannot hide selection or the Apply control.
			if len(wrapped) > budget {
				wrapped = wrapped[:budget]
				wrapped[len(wrapped)-1] += "…"
			}
			if len(lines)+len(wrapped) > rows-4 {
				break
			}
			for j, line := range wrapped {
				d.HitRows[len(lines)+1] = i
				marker := "  "
				if j == 0 {
					marker = prefix
				}
				lines = append(lines, "  "+colour+marker+line+rst)
			}
			if len(lines) < rows-4 {
				lines = append(lines, "")
			}
		}
	}
	for len(lines) < max(0, rows-3) {
		lines = append(lines, "")
	}
	if d.Details {
		lines = append(lines, "  "+dim+"Tab returns to suggestions · ↑/↓ scroll instruments"+rst)
	} else {
		label := "Add to prompt"
		if d.Selected < len(d.Items) && strings.HasPrefix(d.Items[d.Selected], "MEMO|") {
			label = "Informational tip"
		}
		d.AcceptRow = len(lines) + 1
		lines = append(lines, "  "+green+bold+"[ Enter · "+label+" ]"+rst+dim+"   you review it before it sends"+rst)
	}
	lines = append(lines, "  "+terminalSafeText(d.Message))
	lines = append(lines, "  "+dim+fmt.Sprintf("%d suggestions · Ctrl+] / F6 returns to coding", len(d.Items))+rst)
	var out strings.Builder
	out.WriteString("\x1b[?25l")
	for y := 0; y < rows; y++ {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[0m\x1b[2K", y+1)
		if y < len(lines) {
			out.WriteString(ansi.Truncate(lines[y], max(0, cols-1), "…"))
		}
	}
	out.WriteString(rst)
	return out.String()
}

func terminalSafeText(text string) string {
	text = ansi.Strip(text)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 {
			return r
		}
		return -1
	}, text)
}
