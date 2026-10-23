package cully

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestNativeTerminalProtocolNegotiationSurvivesWrapper(t *testing.T) {
	emulator := vt.NewEmulator(80, 12)
	defer emulator.Close()
	var host bytes.Buffer
	native := attachNativeTerminal(emulator, &host)
	for _, sequence := range []string{
		"\x1b[?2004h", "\x1b[?1004h", "\x1b[?1h", "\x1b[?u", "\x1b[>1u", "\x1b[>4;2m",
		"\x1b]11;?\x1b\\", "\x1b]52;c;dGVzdA==\x1b\\", "\x1b[5 q",
	} {
		host.Reset()
		if _, err := emulator.WriteString(sequence); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(host.String(), sequence) {
			t.Fatalf("native protocol consumed: %q became %q", sequence, host.String())
		}
	}
	_, _ = emulator.WriteString("\x1b[?25l\x1b[?1000h")
	if native.Visible || !native.mouseRequested() {
		t.Fatal("native cursor/mouse state lost")
	}
	host.Reset()
	native.close()
	for _, sequence := range []string{"\x1b[?2004l", "\x1b[?1004l", "\x1b[<1u"} {
		if !strings.Contains(host.String(), sequence) {
			t.Fatalf("native mode left enabled: %q", host.String())
		}
	}
}

func TestNativeFooterAndFinalColumnRemainVisible(t *testing.T) {
	emulator := vt.NewEmulator(40, 8)
	defer emulator.Close()
	_, _ = emulator.WriteString("\x1b[1;40HZ\x1b[8;1HGPT-6 medium · Context 75% left")
	frame := renderTerminalPane(emulator, 40, 12, []string{"Cully"}, true, "")
	if !strings.Contains(frame, "Z") || !strings.Contains(frame, "Context 75% left") {
		t.Fatalf("native UI was removed: %q", frame)
	}
	if !strings.Contains(frame, "\x1b[?7l") || !strings.Contains(frame, "\x1b[?7h") {
		t.Fatal("full-width paint must restore wrapping")
	}
	if strings.Contains(renderTerminalPane(emulator, 40, 12, nil, false, ""), "\x1b[?25h") {
		t.Fatal("wrapper made a native hidden cursor visible")
	}
}
