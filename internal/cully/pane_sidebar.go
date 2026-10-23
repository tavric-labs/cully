package cully

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	// sidebarWidth includes the divider column on the right.
	sidebarWidth = 24
	// The sidebar hides itself when the window cannot also fit a usable agent.
	sidebarMinCols = 60
	sidebarMaxName = 20
	// Rows 1 and 2 hold the title and the New/Rename controls.
	sidebarListTop = 3
)

// sidebarState is the window-local state of the named-session list. Sessions
// exist only while the Cully window does; nothing here is persisted.
type sidebarState struct {
	Visible  bool
	Focus    bool
	Selected int
	Renaming bool
	Input    []rune
}

type sidebarHitKind int

const (
	sidebarNoHit sidebarHitKind = iota
	sidebarHitNew
	sidebarHitRename
	sidebarHitSession
)

func padCell(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

// sidebarWindow returns the first listed session so the selection stays visible.
func sidebarWindow(count, selected, rows int) int {
	visible := max(1, rows-sidebarListTop+1)
	if count <= visible {
		return 0
	}
	return min(max(0, selected-visible/2), count-visible)
}

// sidebarLines renders one line per terminal row, each exactly sidebarWidth
// columns wide.
func sidebarLines(names []string, active int, state sidebarState, rows int) []string {
	width := sidebarWidth - 1
	lines := make([]string, rows)
	title := " Sessions"
	if state.Focus {
		title = "\x1b[7m" + padCell(title, width) + "\x1b[0m"
	} else {
		title = bold + padCell(title, width) + rst
	}
	controls := " [+ New]  [Rename]"
	if state.Renaming {
		typed := string(state.Input)
		if tail := []rune(typed); len(tail) > width-4 {
			typed = string(tail[len(tail)-(width-4):])
		}
		controls = " ✎ " + typed + "▏"
	}
	cells := make([]string, rows)
	if rows > 0 {
		cells[0] = title
	}
	if rows > 1 {
		cells[1] = padCell(controls, width)
	}
	first := sidebarWindow(len(names), state.Selected, rows)
	for row := sidebarListTop - 1; row < rows; row++ {
		index := first + row - (sidebarListTop - 1)
		if index >= len(names) {
			break
		}
		marker := " "
		if index == active {
			marker = "▸"
		}
		text := padCell(" "+marker+" "+names[index], width)
		switch {
		case state.Focus && index == state.Selected:
			text = "\x1b[7m" + text + "\x1b[0m"
		case index == active:
			text = bold + text + rst
		}
		cells[row] = text
	}
	for i := range lines {
		cell := cells[i]
		if cell == "" {
			cell = strings.Repeat(" ", width)
		}
		lines[i] = cell + "\x1b[0m" + dim + "│" + rst
	}
	return lines
}

// sidebarHit maps a 1-based pointer position inside the sidebar to a control.
func sidebarHit(x, y, count, selected, rows int) (sidebarHitKind, int) {
	switch {
	case y == 2 && x >= 2 && x <= 8:
		return sidebarHitNew, 0
	case y == 2 && x >= 11 && x <= 18:
		return sidebarHitRename, 0
	case y >= sidebarListTop && y <= rows:
		index := sidebarWindow(count, selected, rows) + y - sidebarListTop
		if index < count {
			return sidebarHitSession, index
		}
	}
	return sidebarNoHit, 0
}

func cleanSessionName(input []rune) string {
	var out []rune
	for _, r := range input {
		if unicode.IsPrint(r) {
			out = append(out, r)
		}
	}
	name := strings.TrimSpace(string(out))
	if runes := []rune(name); len(runes) > sidebarMaxName {
		name = string(runes[:sidebarMaxName])
	}
	return name
}

// renameSessionName cleans a rename candidate and rejects blanks and names
// another session already uses, keeping the numbered sidebar unambiguous.
// Creation already guarantees unique names; renames must not break that.
func renameSessionName(names []string, index int, input []rune) (string, bool) {
	name := cleanSessionName(input)
	if name == "" {
		return "", false
	}
	for i, other := range names {
		if i != index && other == name {
			return "", false
		}
	}
	return name, true
}
