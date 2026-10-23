//go:build !windows

package cully

import (
	"bytes"
	"fmt"
	"testing"
)

func TestPanelInputLegacyMouseSplitAndPaste(t *testing.T) {
	for _, button := range []int{0, 3, 35, 64, 65, 80, 81} {
		t.Run(fmt.Sprint(button), func(t *testing.T) {
			data := []byte{'\x1b', '[', 'M', byte(button + 32), 232, 72}
			for split := 1; split < len(data); split++ {
				var input panelInput
				if got := input.feed(data[:split], false); len(got) != 0 {
					t.Fatalf("split %d prematurely decoded %v", split, got)
				}
				got := input.feed(append(bytes.Clone(data[split:]), 'x'), false)
				if len(got) != 2 || got[0].Mouse == nil || string(got[1].Data) != "x" {
					t.Fatalf("split %d: %v", split, got)
				}
				mouse := got[0].Mouse
				if mouse.Button != button || mouse.X != 200 || mouse.Y != 40 || mouse.Release != (button == 3) {
					t.Fatalf("split %d: %+v", split, mouse)
				}
			}
			var input panelInput
			paste := append([]byte("\x1b[200~"), data...)
			paste = append(paste, []byte("\x1b[201~")...)
			var forwarded []byte
			for _, key := range input.feed(paste, false) {
				if key.Mouse != nil || key.Action != "" {
					t.Fatal("pasted mouse bytes became controls")
				}
				forwarded = append(forwarded, key.Data...)
			}
			if !bytes.Equal(forwarded, paste) {
				t.Fatalf("paste changed: %q", forwarded)
			}
		})
	}
}

func TestPanelMouseWheelModifiers(t *testing.T) {
	for _, modifiers := range []int{0, 4, 8, 16, 28} {
		for _, direction := range []int{0, 1} {
			mouse := panelMouse{Button: 64 | direction | modifiers}
			up, ok := mouse.verticalWheel()
			if !ok || up != (direction == 0) {
				t.Fatalf("wheel %+v: up=%t ok=%t", mouse, up, ok)
			}
			mouse.Release = true
			if _, ok := mouse.verticalWheel(); ok {
				t.Fatal("release became wheel")
			}
		}
	}
	for _, button := range []int{0, 32, 66, 67, 96, 128, 192} {
		mouse := panelMouse{Button: button}
		if _, ok := mouse.verticalWheel(); ok {
			t.Fatalf("button %d became vertical wheel", button)
		}
	}
}

func TestEscBeforeWheelIsNotLost(t *testing.T) {
	for _, wheel := range [][]byte{[]byte("\x1b[<64;5;1M"), {'\x1b', '[', 'M', 96, 37, 33}} {
		var input panelInput
		if keys := input.feed([]byte{0x1b}, false); len(keys) != 0 {
			t.Fatal("standalone Esc must await disambiguation")
		}
		keys := input.feed(wheel, false)
		if len(keys) != 2 || keys[0].Action != "escape" || keys[1].Mouse == nil {
			t.Fatalf("Esc followed by wheel lost its action: %+v", keys)
		}
		if up, ok := keys[1].Mouse.verticalWheel(); !ok || !up {
			t.Fatal("wheel gesture changed after Esc")
		}
	}
}
