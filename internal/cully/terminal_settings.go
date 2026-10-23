package cully

import (
	"bytes"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"path/filepath"
)

// Back up before replacing, preserve permissions and symlinks, and refuse to
// overwrite a file changed since inspection. Backups are private local files.
func writeTerminalSettings(path string, before, after []byte) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if !bytes.Equal(current, before) {
		return "", fmt.Errorf("terminal settings changed during configuration; retry")
	}
	mode := os.FileMode(0600)
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("terminal settings are not a regular file")
		}
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	backup := ""
	if current != nil {
		file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".cully-backup-*")
		if err != nil {
			return "", err
		}
		backup = file.Name()
		_, err = file.Write(before)
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".cully-terminal-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(after)
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return backup, nil
}

func terminalDiagnostics(w io.Writer, profile terminalProfile) {
	fmt.Fprintf(w, "Terminal: %s\nSupported setup: macOS/Linux native terminal, zsh + Oh My Zsh.\n", profile.label())
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LC_CTYPE")
	}
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	fmt.Fprintf(w, "Character locale: %s\n", terminalSafeText(fallback(locale, "not set; UTF-8 must be selected in the terminal")))
	fmt.Fprintln(w, "Glyph preview: home \uf015  folder \uf07b  git \ue0a0  arrows ← →  border ─│")
	fmt.Fprintln(w, "A missing home icon (U+F015) needs a Nerd Font selected in the terminal profile. Run cully terminal configure.")
	fmt.Fprintln(w, "Mouse and wheel: standard terminal protocols. Colors, keymaps and padding are preserved.")
	if profile.Program == "" || profile.Program == "VTE" {
		fmt.Fprintln(w, "Terminal app not identified. Use --terminal gnome-terminal, ptyxis or gnome-console for font configuration on Linux.")
	}
}

func RunTerminal(input, output *os.File, args []string) error {
	command := "check"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	if command != "check" && command != "configure" && command != "shell" && command != "test" && command != "restore" {
		return fmt.Errorf("usage: cully terminal check|configure|shell|test|restore [--terminal PROGRAM] [--dry-run] [--backup PATH]")
	}
	fs := flag.NewFlagSet("terminal "+command, flag.ContinueOnError)
	fs.SetOutput(output)
	program := fs.String("terminal", "", "native terminal: Apple_Terminal, gnome-terminal, ptyxis, gnome-console")
	dry := fs.Bool("dry-run", false, "preview changes without writing or downloading")
	backup := fs.String("backup", "", "native terminal font backup created by Cully")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("terminal: unexpected arguments")
	}
	profile := detectTerminalProfile()
	if *program != "" {
		profile.Program = terminalField(*program)
	}
	if err := profile.requireNative(); err != nil {
		return err
	}
	if command == "shell" {
		return setupShell(input, output, *dry)
	}
	if command == "test" {
		return RunTerminalExercise(input, output)
	}
	if command == "restore" {
		return restoreNativeFont(input, output, profile, *backup, *dry)
	}
	terminalDiagnostics(output, profile)
	if command == "check" {
		return setupShell(input, output, true)
	}
	return configureNativeFont(input, output, profile, *dry)
}

// Required shell provisioning is separate from the visual check.
// --skip-terminal-test never bypasses required shell/font installation.
func SetupTerminal(input, output *os.File, skipLive bool, program string) error {
	if err := RunTerminal(input, output, []string{"shell", "--terminal=" + program}); err != nil {
		return err
	}
	if err := RunTerminal(input, output, []string{"configure", "--terminal=" + program}); err != nil {
		return err
	}
	if skipLive || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		fmt.Fprintln(output, "Live Unicode/color/wheel/divider/advisor checks: not tested. Run cully terminal test in your terminal.")
		return nil
	}
	return RunTerminalExercise(input, output)
}
