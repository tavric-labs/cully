package cully

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// Keep native input negotiation and host integrations outside the screen
// emulator. The emulator owns cursor/clear operations that protect Cully's UI.
type nativeTerminal struct {
	Visible bool
	mouse   map[ansi.Mode]bool
	modes   map[ansi.Mode]bool
	output  io.Writer
	gate    *gatedWriter
	// pushes are the keyboard-protocol pushes this agent still has in effect,
	// kept verbatim so a session switched back in can restore them.
	pushes []string
}

// gatedWriter drops host-terminal integrations from sessions that are not the
// one currently shown.
type gatedWriter struct {
	w  io.Writer
	on bool
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	if !g.on {
		return len(p), nil
	}
	return g.w.Write(p)
}

func attachNativeTerminal(emulator *vt.Emulator, output io.Writer) *nativeTerminal {
	gate := &gatedWriter{w: output, on: true}
	n := &nativeTerminal{Visible: true, mouse: map[ansi.Mode]bool{}, modes: map[ansi.Mode]bool{}, output: gate, gate: gate}
	output = gate
	mode := func(value ansi.Mode, on bool) {
		switch value {
		case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
			n.mouse[value] = on
		case ansi.ModeBracketedPaste, ansi.ModeFocusEvent, ansi.ModeCursorKeys:
			n.modes[value] = on
			operation := 'l'
			if on {
				operation = 'h'
			}
			fmt.Fprintf(output, "\x1b[?%d%c", value, operation)
		}
	}
	emulator.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { n.Visible = visible },
		EnableMode:       func(value ansi.Mode) { mode(value, true) }, DisableMode: func(value ansi.Mode) { mode(value, false) },
		Bell:  func() { io.WriteString(output, "\a") },
		Title: func(title string) { fmt.Fprintf(output, "\x1b]2;%s\x1b\\", terminalSafeText(title)) },
	})
	for _, command := range []int{9, 10, 11, 12, 22, 52, 110, 111, 112, 777} {
		emulator.RegisterOscHandler(command, func(data []byte) bool {
			fmt.Fprintf(output, "\x1b]%s\x1b\\", data)
			// Let color changes update the virtual screen too; queries must
			// receive the physical terminal's colors rather than fake defaults.
			return command != 10 && command != 11 && command != 12 || bytes.HasSuffix(data, []byte(";?"))
		})
	}
	build := func(prefix, final byte, params ansi.Params) string {
		var seq strings.Builder
		seq.WriteString("\x1b[")
		if prefix != 0 {
			seq.WriteByte(prefix)
		}
		for i, param := range params {
			if value := param.Param(-1); value >= 0 {
				fmt.Fprint(&seq, value)
			}
			if i+1 < len(params) {
				if param.HasMore() {
					seq.WriteByte(':')
				} else {
					seq.WriteByte(';')
				}
			}
		}
		seq.WriteByte(final)
		return seq.String()
	}
	relay := func(prefix, final byte, params ansi.Params) {
		io.WriteString(output, build(prefix, final, params))
	}
	for _, prefix := range []byte{'?', '>', '<', '='} {
		emulator.RegisterCsiHandler(ansi.Command(prefix, 0, 'u'), func(params ansi.Params) bool {
			if prefix == '>' {
				n.pushes = append(n.pushes, build(prefix, 'u', params))
			}
			if prefix == '<' {
				count := 1
				if len(params) > 0 {
					count = max(1, params[0].Param(1))
				}
				n.pushes = n.pushes[:max(0, len(n.pushes)-count)]
			}
			relay(prefix, 'u', params)
			return true
		})
	}
	emulator.RegisterCsiHandler(ansi.Command('>', 0, 'm'), func(params ansi.Params) bool {
		if len(params) == 0 || params[0].Param(0) != 4 {
			return false
		}
		relay('>', 'm', params)
		return true
	})
	emulator.RegisterCsiHandler(ansi.Command(0, ' ', 'q'), func(params ansi.Params) bool {
		value := 0
		if len(params) > 0 {
			value = params[0].Param(0)
		}
		fmt.Fprintf(output, "\x1b[%d q", value)
		return false
	})
	return n
}

func (n *nativeTerminal) mouseRequested() bool {
	for _, on := range n.mouse {
		if on {
			return true
		}
	}
	return false
}
func (n *nativeTerminal) close() {
	for value, on := range n.modes {
		if on {
			fmt.Fprintf(n.output, "\x1b[?%dl", value)
		}
	}
	if len(n.pushes) > 0 {
		fmt.Fprintf(n.output, "\x1b[<%du", len(n.pushes))
	}
	io.WriteString(n.output, "\x1b[0 q")
}

// deactivate releases the host terminal for another session. The agent's own
// requests stay recorded and are applied again by activate.
func (n *nativeTerminal) deactivate() {
	n.close()
	n.gate.on = false
}

func (n *nativeTerminal) activate() {
	n.gate.on = true
	for value, on := range n.modes {
		if on {
			fmt.Fprintf(n.output, "\x1b[?%dh", value)
		}
	}
	for _, push := range n.pushes {
		io.WriteString(n.output, push)
	}
}

func nativeTerminalReply(data []byte) bool {
	return bytes.HasPrefix(data, []byte("\x1b]")) || bytes.HasPrefix(data, []byte("\x1bP")) || bytes.HasPrefix(data, []byte("\x1b[?")) || bytes.Equal(data, []byte("\x1b[I")) || bytes.Equal(data, []byte("\x1b[O"))
}
