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

func TestPaneScrollbarThumbAndGestures(t *testing.T) {
	var s paneScroll
	if start, size := s.thumb(90, 10); start != 9 || size != 1 {
		t.Fatalf("live thumb=%d,%d", start, size)
	}
	s.Offset = 90
	if start, size := s.thumb(90, 10); start != 0 || size != 1 {
		t.Fatalf("oldest thumb=%d,%d", start, size)
	}
	if !s.scrollbar(&panelMouse{Button: 0, X: 39, Y: 1}, 39, 10, 90) || !s.Dragging {
		t.Fatal("thumb press did not start drag")
	}
	// Dragging outside the gutter still owns the pointer and reaches live output.
	if !s.scrollbar(&panelMouse{Button: 32, X: 2, Y: 50}, 39, 10, 90) || s.Offset != 0 {
		t.Fatalf("drag to bottom: %+v", s)
	}
	s.scrollbar(&panelMouse{Button: 0, Release: true, X: 2, Y: 1}, 39, 10, 90)
	if s.Dragging || s.Offset != 90 {
		t.Fatalf("release at top: %+v", s)
	}
	// Track clicks jump to that position, keeping the thumb under the pointer.
	s.scrollbar(&panelMouse{Button: 0, X: 39, Y: 6}, 39, 10, 90)
	if !s.Dragging || s.Offset != 40 {
		t.Fatalf("track click: %+v", s)
	}
	s.reset()
	if s.Dragging || s.Offset != 0 {
		t.Fatal("reset left a drag active")
	}
	for _, mouse := range []*panelMouse{{Button: 64, X: 39, Y: 1}, {Button: 0, X: 38, Y: 1}, {Button: 0, X: 39, Y: 11}, {Button: 0, Release: true, X: 39, Y: 1}} {
		if s.scrollbar(mouse, 39, 10, 90) {
			t.Fatalf("unowned input consumed: %+v", mouse)
		}
	}
	if !s.scrollbar(&panelMouse{Button: 0, X: 39, Y: 1}, 39, 10, 0) || s.Dragging {
		t.Fatal("empty history must consume gutter click without dragging")
	}
	if start, size := s.thumb(0, 10); start != 0 || size != 10 {
		t.Fatalf("empty thumb=%d,%d", start, size)
	}
	if start, size := s.thumb(90, 0); start != 0 || size != 0 {
		t.Fatalf("zero-height thumb=%d,%d", start, size)
	}
}

func TestPaneScrollbarReservesTextAndKeepsCursorAndTheme(t *testing.T) {
	agent := vt.NewEmulator(38, 6)
	defer agent.Close()
	_, _ = agent.Write([]byte(strings.Repeat("x", 37) + "Z"))
	var s paneScroll
	frame := renderTerminalPaneView(agent, 0, 40, 8, []string{"status"}, false, "", gripIdle) + renderPaneScrollbar(&s, 90, 6, 39, false)
	host := vt.NewEmulator(40, 8)
	defer host.Close()
	_, _ = host.Write([]byte(frame))
	if cell := host.CellAt(37, 0); cell == nil || cell.Content != "Z" {
		t.Fatal("scrollbar overwrote final text column")
	}
	if cell := host.CellAt(38, 5); cell == nil || cell.Content != "┃" {
		t.Fatalf("live thumb missing: %+v", cell)
	}
	if host.CursorPosition() != agent.CursorPosition() {
		t.Fatal("scrollbar moved the agent cursor")
	}
	if strings.Contains(frame, "\x1b[48;") || strings.Contains(frame, "\x1b]10;") || strings.Contains(frame, "\x1b]11;") {
		t.Fatal("scrollbar changed terminal appearance")
	}
	if got := renderPaneScrollbar(&s, 90, 6, 39, true); strings.Contains(got, "┃") {
		t.Fatal("alternate-screen private history has a fictitious thumb")
	}
}
