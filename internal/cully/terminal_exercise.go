//go:build !windows

package cully

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

type terminalExercise struct {
	WheelUp, WheelDown, DividerUp, DividerDown                 bool
	AdvisorOpen, AdvisorClose, Status, Resize, Unicode, Colors bool
	wasOpen                                                    bool
}

func (e *terminalExercise) confirmInput(key panelKey) {
	if key.Pasted || key.Action != "" {
		return
	}
	e.Unicode = e.Unicode || bytes.IndexByte(key.Data, 'u') >= 0
	e.Colors = e.Colors || bytes.IndexByte(key.Data, 'c') >= 0
}

func (e *terminalExercise) frame(open, status bool) {
	e.Status = e.Status || status
	if open {
		e.AdvisorOpen = true
	}
	if e.wasOpen && !open {
		e.AdvisorClose = true
	}
	e.wasOpen = open
}

func (e *terminalExercise) report(output *os.File) error {
	checks := []struct {
		name   string
		passed bool
	}{
		{"Wheel up (history moved)", e.WheelUp}, {"Wheel down (history moved)", e.WheelDown},
		{"Divider up (agent resized)", e.DividerUp}, {"Divider down (agent resized)", e.DividerDown},
		{"Advisor opened", e.AdvisorOpen}, {"Advisor closed", e.AdvisorClose}, {"Status panel rendered", e.Status},
		{"Unicode/icons (user confirmed)", e.Unicode}, {"Colors (user confirmed)", e.Colors},
	}
	var missing []string
	for _, check := range checks {
		result := "verified"
		if !check.passed {
			result = "NOT VERIFIED"
			missing = append(missing, check.name)
		}
		fmt.Fprintf(output, "  %s: %s\n", check.name, result)
	}
	resize := "not tested"
	if e.Resize {
		resize = "verified"
	}
	fmt.Fprintf(output, "  Window resize: %s\n", resize)
	fmt.Fprintln(output, "  Selection/copy: manual host check.\n  Model/token/tool statistics: require an active coding-agent session; this test checks their display area.\n  Terminal appearance preferences: unchanged by this test.")
	if len(missing) > 0 {
		return fmt.Errorf("terminal check incomplete: %s; rerun cully terminal test", strings.Join(missing, ", "))
	}
	return nil
}

func RunTerminalExercise(input, output *os.File) error {
	if !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return fmt.Errorf("live terminal checks require an interactive terminal; settings can be checked with cully terminal check")
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	exercise := &terminalExercise{}
	agent := paneAgent{Name: "terminal-check", Binary: binary}
	if err := runPane(agent, []string{"_internal", "terminal-check-output"}, input, output, exercise); err != nil {
		return err
	}
	fmt.Fprintln(output, "Cully live terminal verification")
	return exercise.report(output)
}

// RunTerminalCheckOutput is the built-in normal-screen child used by the live
// exercise. It does not start a coding agent or inspect conversation contents.
func RunTerminalCheckOutput(input, output *os.File) error {
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(input.Fd()), state)
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(output, "Terminal check history line %03d\r\n", i)
	}
	fmt.Fprint(output, "CULLY TERMINAL CHECK\r\nScroll UP then DOWN with the wheel. Drag the divider DOWN then UP.\r\nOpen the advisor with F6 or Ctrl+], then close it with Esc.\r\nOptionally resize the window and check selection/copy.\r\nHome   folder   git   arrows ← →  border ─│  Unicode café 漢字\r\nColors: \x1b[31mRED\x1b[0m \x1b[32mGREEN\x1b[0m \x1b[34mBLUE\x1b[0m\r\nPress u ONLY if the icons and text render correctly; c ONLY if colors match.\r\nPress q when finished; missing checks are reported as NOT VERIFIED.\r\n")
	buf := make([]byte, 4096)
	for {
		n, err := input.Read(buf)
		if strings.ContainsAny(string(buf[:n]), "q\x03") {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
