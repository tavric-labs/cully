//go:build !windows

package cully

import (
	"bytes"
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
)

type panelKey struct {
	Data   []byte
	Action string
	Mouse  *panelMouse
}
type panelMouse struct {
	Button, X, Y int
	Release      bool
}
type panelInput struct {
	pending []byte
	paste   bool
}

// Decode only explicit advisor controls. Preserve every other byte, including
// bracketed paste and terminal replies. Escape sequences can span reads.
func (p *panelInput) feed(data []byte, flush bool) []panelKey {
	p.pending = append(p.pending, data...)
	var keys []panelKey
	forward := func(n int) {
		keys = append(keys, panelKey{Data: bytes.Clone(p.pending[:n])})
		p.pending = p.pending[n:]
	}
	sequences := []struct{ value, action string }{
		{"\x1ba", "focus"}, {"\x1bA", "focus"},
		{"\x1b[17~", "focus"}, // F6, independent of Option/Meta settings
		{"\x1b[A", "up"}, {"\x1bOA", "up"}, {"\x1b[B", "down"}, {"\x1bOB", "down"},
		{"\x1b[5~", "pageup"}, {"\x1b[6~", "pagedown"},
		{"\x1b[H", "home"}, {"\x1bOH", "home"}, {"\x1b[1~", "home"},
		{"\x1b[F", "end"}, {"\x1bOF", "end"}, {"\x1b[4~", "end"},
		{"\x1b[200~", "paste"},
	}
	for len(p.pending) > 0 {
		if p.paste {
			end := []byte("\x1b[201~")
			if index := bytes.Index(p.pending, end); index >= 0 {
				forward(index + len(end))
				p.paste = false
				continue
			}
			n := len(p.pending)
			for keep := 1; keep < len(end) && keep <= len(p.pending); keep++ {
				if bytes.Equal(p.pending[len(p.pending)-keep:], end[:keep]) {
					n = len(p.pending) - keep
				}
			}
			if n > 0 {
				forward(n)
			}
			break
		}
		if p.pending[0] == 0x1c { // Ctrl+\ toggles divider dragging in Warp
			keys = append(keys, panelKey{Data: []byte{0x1c}, Action: "resize"})
			p.pending = p.pending[1:]
			continue
		}
		if p.pending[0] == 0x1d { // Ctrl+], a single unambiguous terminal byte
			keys = append(keys, panelKey{Data: []byte{0x1d}, Action: "focus"})
			p.pending = p.pending[1:]
			continue
		}
		if p.pending[0] == '\r' || p.pending[0] == '\n' || p.pending[0] == '\t' {
			action := "enter"
			if p.pending[0] == '\t' {
				action = "details"
			}
			keys = append(keys, panelKey{Data: bytes.Clone(p.pending[:1]), Action: action})
			p.pending = p.pending[1:]
			continue
		}
		if p.pending[0] != 0x1b {
			n := bytes.IndexAny(p.pending, "\x1b\x1c\x1d\r\n\t")
			if n < 0 {
				n = len(p.pending)
			}
			forward(n)
			continue
		}
		if bytes.HasPrefix(p.pending, []byte("\x1b[<")) {
			end := bytes.IndexAny(p.pending[3:], "Mm")
			if end < 0 && len(p.pending) <= 40 && !flush {
				break
			}
			if end >= 0 {
				n := end + 4
				mouse := &panelMouse{Release: p.pending[n-1] == 'm'}
				if _, err := fmt.Sscanf(string(p.pending[3:n-1]), "%d;%d;%d", &mouse.Button, &mouse.X, &mouse.Y); err == nil && mouse.X > 0 && mouse.Y > 0 {
					keys = append(keys, panelKey{Data: bytes.Clone(p.pending[:n]), Action: "mouse", Mouse: mouse})
					p.pending = p.pending[n:]
					continue
				}
			}
		}
		// Terminals that ignore SGR mode still send the original three-byte
		// mouse encoding. Decode it here instead of leaking it into the prompt.
		if bytes.HasPrefix(p.pending, []byte("\x1b[M")) {
			if len(p.pending) < 6 && !flush {
				break
			}
			if len(p.pending) >= 6 && p.pending[3] >= 32 && p.pending[4] > 32 && p.pending[5] > 32 {
				button := int(p.pending[3]) - 32
				mouse := &panelMouse{Button: button, X: int(p.pending[4]) - 32, Y: int(p.pending[5]) - 32, Release: button&3 == 3 && button&64 == 0 && button&32 == 0}
				keys = append(keys, panelKey{Data: bytes.Clone(p.pending[:6]), Action: "mouse", Mouse: mouse})
				p.pending = p.pending[6:]
				continue
			}
		}
		matched, partial := false, false
		if bytes.HasPrefix([]byte("\x1b[<"), p.pending) || bytes.HasPrefix([]byte("\x1b[M"), p.pending) {
			partial = true
		}
		for _, seq := range sequences {
			value := []byte(seq.value)
			if bytes.HasPrefix(p.pending, value) {
				if seq.action == "paste" {
					forward(len(value))
					p.paste = true
				} else {
					keys = append(keys, panelKey{Data: bytes.Clone(value), Action: seq.action})
					p.pending = p.pending[len(value):]
				}
				matched = true
				break
			}
			if bytes.HasPrefix(value, p.pending) {
				partial = true
			}
		}
		if matched {
			continue
		}
		if partial && !flush {
			break
		}
		if len(p.pending) == 1 {
			keys = append(keys, panelKey{Data: []byte{0x1b}, Action: "escape"})
			p.pending = nil
		} else {
			forward(1)
		}
	}
	return keys
}

// verticalWheel ignores modifier bits when identifying the wheel direction.
// Horizontal wheels, pointer motion and button releases stay separate.
func (m *panelMouse) verticalWheel() (up, ok bool) {
	button := m.Button &^ (4 | 8 | 16)
	return button == 64, !m.Release && (button == 64 || button == 65)
}

func (m *panelMouse) event() uv.MouseEvent {
	mouse := uv.Mouse{X: m.X - 1, Y: m.Y - 1}
	button := m.Button & 3
	switch {
	case m.Button&128 != 0:
		mouse.Button = uv.MouseBackward + uv.MouseButton(button)
	case m.Button&64 != 0:
		mouse.Button = uv.MouseWheelUp + uv.MouseButton(button)
	case button == 3:
		mouse.Button = uv.MouseNone
	default:
		mouse.Button = uv.MouseLeft + uv.MouseButton(button)
	}
	if m.Button&4 != 0 {
		mouse.Mod |= uv.ModShift
	}
	if m.Button&8 != 0 {
		mouse.Mod |= uv.ModAlt
	}
	if m.Button&16 != 0 {
		mouse.Mod |= uv.ModCtrl
	}
	if m.Release {
		return uv.MouseReleaseEvent(mouse)
	}
	if m.Button&32 != 0 {
		return uv.MouseMotionEvent(mouse)
	}
	if m.Button&64 != 0 {
		return uv.MouseWheelEvent(mouse)
	}
	return uv.MouseClickEvent(mouse)
}
