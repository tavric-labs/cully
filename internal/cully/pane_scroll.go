//go:build !windows

package cully

// paneScroll lets the mouse wheel read output that has scrolled off the agent's
// screen. Cully draws the agent from its own terminal emulator, so the host
// terminal never receives that output and has no scrollback of it. The
// emulator keeps the lines; this remembers how far back the view sits.
// Offset is the number of lines above the live screen, and 0 is live.
type paneScroll struct {
	Offset int
	seen   int // scrollback length at the last anchor
}

// wheelLines is how many lines one wheel notch moves.
const wheelLines = 3

func (s *paneScroll) active() bool { return s.Offset > 0 }

// wheel moves the view and reports whether it changed.
func (s *paneScroll) wheel(up bool, scrollbackLen int) bool {
	next := s.Offset - wheelLines
	if up {
		next = s.Offset + wheelLines
	}
	next = min(max(next, 0), scrollbackLen)
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
	return changed
}
