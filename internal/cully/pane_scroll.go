//go:build !windows

package cully

import (
	"fmt"
	"strings"
)

// paneScroll lets the mouse wheel read output that has scrolled off the agent's
// screen. Cully draws the agent from its own terminal emulator, so the host
// terminal never receives that output and has no scrollback of it. The
// emulator keeps the lines; this remembers how far back the view sits.
// Offset is the number of lines above the live screen, and 0 is live.
type paneScroll struct {
	Offset   int
	seen     int // scrollback length at the last anchor
	Dragging bool
	grab     int // pointer position within the thumb when dragging began
}

// wheelLines is how many lines one wheel notch moves.
const wheelLines = 3

func (s *paneScroll) active() bool { return s.Offset > 0 }

// wheel moves the view and reports whether it changed.
func (s *paneScroll) wheel(up bool, scrollbackLen int) bool {
	lines := -wheelLines
	if up {
		lines = wheelLines
	}
	return s.move(lines, scrollbackLen)
}

func (s *paneScroll) move(lines, scrollbackLen int) bool {
	next := min(max(s.Offset+lines, 0), scrollbackLen)
	changed := next != s.Offset
	s.Offset = next
	return changed
}

// anchor keeps the lines in view where they are while new output arrives, the
// way a terminal does when you have scrolled up. It also follows a cleared
// scrollback.
func (s *paneScroll) anchor(scrollbackLen int) {
	if s.Offset > 0 {
		s.Offset = min(max(s.Offset+scrollbackLen-s.seen, 0), scrollbackLen)
	}
	s.seen = scrollbackLen
}

// reset returns to the live screen and reports whether it moved.
func (s *paneScroll) reset() bool {
	changed := s.Offset != 0
	s.Offset = 0
	s.Dragging = false
	return changed
}

// thumb returns a zero-based track position and a thumb scaled to the visible
// fraction of normal-screen history. Offset zero is the bottom (live output).
func (s *paneScroll) thumb(history, height int) (start, size int) {
	if height <= 0 {
		return 0, 0
	}
	if history <= 0 {
		return 0, height
	}
	size = max(1, height*height/(history+height))
	travel := height - size
	start = (history - min(max(s.Offset, 0), history)) * travel / history
	return start, size
}

// scrollbar owns left-button gestures starting in its reserved column. A drag
// remains owned until release or cancellation, even outside the track.
func (s *paneScroll) scrollbar(mouse *panelMouse, column, height, history int) bool {
	if !s.Dragging && (mouse.Release || mouse.Button != 0 || mouse.X != column || mouse.Y < 1 || mouse.Y > height) {
		return false
	}
	start, size := s.thumb(history, height)
	travel := height - size
	if !s.Dragging {
		if history <= 0 || travel <= 0 {
			return true
		}
		s.Dragging = true
		s.grab = mouse.Y - 1 - start
		if s.grab >= 0 && s.grab < size {
			return true
		}
		s.grab = size / 2 // clicking the track centers the thumb on the pointer
	} else if !mouse.Release && mouse.Button&32 == 0 {
		return true
	}
	if travel > 0 {
		position := min(max(mouse.Y-1-s.grab, 0), travel)
		s.Offset = history - (position*history+travel/2)/travel
	}
	if mouse.Release {
		s.Dragging = false
	}
	return true
}

// Use the terminal's default foreground/background so the gutter follows the
// user's theme. A fullscreen agent's private history has no measurable thumb.
func renderPaneScrollbar(scroll *paneScroll, history, height, column int, alternate bool) string {
	var out strings.Builder
	out.WriteString("\x1b7\x1b[?25l")
	start, size := scroll.thumb(history, height)
	for y := 0; y < height; y++ {
		style, glyph := "\x1b[2m", "│"
		if !alternate && history > 0 && y >= start && y < start+size {
			style, glyph = "\x1b[1m", "┃"
		}
		fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[0m%s%s\x1b[0m", y+1, column, style, glyph)
	}
	out.WriteString("\x1b8")
	if !scroll.active() {
		out.WriteString("\x1b[?25h")
	}
	return out.String()
}
