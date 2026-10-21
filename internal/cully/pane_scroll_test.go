//go:build !windows

package cully

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestPaneScrollWheelAnchorAndReset(t *testing.T) {
	var s paneScroll
	if s.wheel(true, 0) || s.active() {
		t.Fatal("no scrollback to read")
	}
	if !s.wheel(true, 10) || s.Offset != wheelLines {
		t.Fatalf("up: %d", s.Offset)
	}
	for i := 0; i < 10; i++ {
		s.wheel(true, 10)
	}
	if s.Offset != 10 {
		t.Fatalf("scrolled past the oldest line: %d", s.Offset)
	}
	s.wheel(false, 10)
	if s.Offset != 10-wheelLines {
		t.Fatalf("down: %d", s.Offset)
	}
	// New output arrives while reading: the lines in view stay put.
	s.anchor(10)
	before := s.Offset
	s.anchor(15)
	if s.Offset != before+5 {
		t.Fatalf("view drifted: %d, want %d", s.Offset, before+5)
	}
	s.anchor(2) // scrollback cleared
	if s.Offset > 2 {
		t.Fatalf("offset beyond scrollback: %d", s.Offset)
	}
	if !s.reset() || s.active() || s.reset() {
		t.Fatal("reset")
	}
	// Live view follows output without an offset.
	var live paneScroll
	live.anchor(5)
	live.anchor(50)
	if live.Offset != 0 {
		t.Fatalf("live view moved: %d", live.Offset)
	}
}

func TestRenderTerminalPaneViewShowsScrollback(t *testing.T) {
	emulator := vt.NewEmulator(40, 6)
	for i := 1; i <= 30; i++ {
		_, _ = emulator.Write([]byte(fmt.Sprintf("line %02d\r\n", i)))
	}
	if emulator.ScrollbackLen() == 0 {
		t.Fatal("emulator kept no scrollback")
	}
	frame := func(offset int) string {
		return ansi.Strip(renderTerminalPaneView(emulator, offset, 40, 8, []string{"panel"}, false, "", gripIdle))
	}
	live, back := frame(0), frame(emulator.ScrollbackLen())
	if !strings.Contains(live, "line 30") || strings.Contains(live, "line 01") {
		t.Fatalf("live frame wrong:\n%s", live)
	}
	if !strings.Contains(back, "line 01") || strings.Contains(back, "line 30") {
		t.Fatalf("oldest frame wrong:\n%s", back)
	}
	mid := frame(10)
	if strings.Contains(mid, "line 30") || !strings.Contains(mid, "line 1") {
		t.Fatalf("scrolled frame wrong:\n%s", mid)
	}
	if strings.Contains(renderTerminalPaneView(emulator, 5, 40, 8, nil, false, "", gripIdle), "\x1b[?25h") {
		t.Fatal("cursor shown while reading history")
	}
	if !strings.Contains(renderTerminalPaneView(emulator, 0, 40, 8, nil, false, "", gripIdle), "\x1b[?25h") {
		t.Fatal("cursor hidden on the live screen")
	}
}
