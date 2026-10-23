package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SSH can forward the terminal program without identifying the client's OS.
// Keep client terminal metadata separate from the host that executes commands.
type terminalProfile struct{ Program, Type, Shell, OS, Connection string }

func detectTerminalProfile() terminalProfile {
	connection := "local"
	if os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != "" {
		connection = "ssh"
	}
	return terminalProfile{Program: terminalField(os.Getenv("TERM_PROGRAM")), Type: terminalField(os.Getenv("TERM")), Shell: terminalField(filepath.Base(os.Getenv("SHELL"))), OS: runtime.GOOS, Connection: connection}
}

func terminalField(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/-", r) {
			return r
		}
		return -1
	}, value)
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

// isWarp reports Warp's terminal program. Cully defers continuous mouse motion
// there until the advisor or divider needs it, while keeping wheel reports on
// so the emulator's history remains reachable.
func (p terminalProfile) isWarp() bool {
	return strings.EqualFold(p.Program, "WarpTerminal") || strings.EqualFold(p.Program, "Warp")
}

func (p terminalProfile) label() string {
	program := p.Program
	if strings.EqualFold(program, "WarpTerminal") {
		program = "Warp"
	}
	if program == "" {
		program = fallback(p.Type, "unknown terminal")
	}
	shell := p.Shell
	if shell == "." || shell == "" {
		shell = "unknown"
	}
	return fmt.Sprintf("%s · %s · login shell %s · %s · %s", program, fallback(p.Type, "unknown TERM"), shell, p.OS, p.Connection)
}

func (p terminalProfile) signals() string {
	return fmt.Sprintf("terminal_program=%s\nterminal_type=%s\nlogin_shell=%s\nexecution_os=%s\nconnection=%s\n", p.Program, p.Type, p.Shell, p.OS, p.Connection)
}
