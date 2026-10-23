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
	program := os.Getenv("TERM_PROGRAM")
	if program == "" {
		switch {
		case os.Getenv("GNOME_TERMINAL_SERVICE") != "" || os.Getenv("GNOME_TERMINAL_SCREEN") != "":
			program = "gnome-terminal"
		case os.Getenv("PTYXIS_VERSION") != "":
			program = "ptyxis"
		case os.Getenv("KGX_VERSION") != "":
			program = "gnome-console"
		case os.Getenv("VTE_VERSION") != "":
			program = "VTE"
		}
	}
	return terminalProfile{Program: terminalField(program), Type: terminalField(os.Getenv("TERM")), Shell: terminalField(filepath.Base(os.Getenv("SHELL"))), OS: runtime.GOOS, Connection: connection}
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

// Unknown client metadata is not proof of support. In particular, SSH does
// not identify the client OS.
//
// The allowlist is capability-based, not brand-based. Cully owns the screen
// and needs SGR mouse reporting, standard modified-key encodings and
// synchronized output: Ghostty, iTerm2, Kitty, WezTerm and Alacritty all
// answer those protocols on factory settings. Warp is allowed so
// `cully run claude` works there; it reports TERM_PROGRAM=WarpTerminal and,
// like the others, owns its font settings, so setup gives manual guidance.
func (p terminalProfile) requireNative() error {
	if p.OS != "darwin" && p.OS != "linux" {
		return fmt.Errorf("Cully supports macOS and Linux only")
	}
	switch strings.ToLower(p.Program) {
	case "", "apple_terminal", "gnome-terminal", "ptyxis", "gnome-console", "kgx", "vte",
		"ghostty", "iterm.app", "kitty", "wezterm", "alacritty", "warpterminal", "warp", "warp-terminal":
		return nil
	default:
		return fmt.Errorf("%s is not supported; use macOS Terminal, Ghostty, iTerm2, Kitty, WezTerm, Alacritty or Warp, or your Linux desktop's native terminal, with zsh + Oh My Zsh", p.Program)
	}
}

func (p terminalProfile) label() string {
	program := p.Program
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
