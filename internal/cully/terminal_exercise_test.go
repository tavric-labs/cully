//go:build !windows

package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalVisualConfirmationIgnoresPaste(t *testing.T) {
	var input panelInput
	var exercise terminalExercise
	for _, data := range []string{"\x1b[200~", "u", "c", "\x1b[201~"} {
		for _, key := range input.feed([]byte(data), false) {
			exercise.confirmInput(key)
		}
	}
	if exercise.Unicode || exercise.Colors {
		t.Fatal("pasted text became visual confirmation")
	}
	for _, key := range input.feed([]byte("uc"), false) {
		exercise.confirmInput(key)
	}
	if !exercise.Unicode || !exercise.Colors {
		t.Fatal("combined real keystrokes lost visual confirmation")
	}
}

func TestTerminalReportCannotInferVisualSuccessFromFrames(t *testing.T) {
	output, err := os.Create(filepath.Join(t.TempDir(), "report"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	e := terminalExercise{WheelUp: true, WheelDown: true, DividerUp: true, DividerDown: true, AdvisorOpen: true, AdvisorClose: true, Status: true}
	if err := e.report(output); err == nil || !strings.Contains(err.Error(), "Unicode") {
		t.Fatalf("unconfirmed glyphs reported success: %v", err)
	}
	e.Unicode, e.Colors = true, true
	if err := e.report(output); err != nil {
		t.Fatal(err)
	}
}
