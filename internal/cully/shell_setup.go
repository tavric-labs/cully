package cully

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const ohMyZshRemote = "https://github.com/ohmyzsh/ohmyzsh.git"

var ohMyZshSource = regexp.MustCompile(`(?m)^\s*(source|\.)\s+[^\n]*oh-my-zsh\.sh`)

type shellSetupPlan struct {
	root, rc           string
	before, after      []byte
	packages           []string
	clone, changeShell bool
}

// Use the user's dotfile locations without evaluating their shell configuration.
func planShellSetup(home, dotdir, root string, profile terminalProfile, lookPath func(string) (string, error)) (shellSetupPlan, error) {
	p := shellSetupPlan{}
	if profile.OS != "darwin" && profile.OS != "linux" {
		return p, fmt.Errorf("Cully's terminal setup supports macOS and Linux only")
	}
	if dotdir == "" {
		dotdir = home
	}
	if root == "" {
		root = filepath.Join(home, ".oh-my-zsh")
	}
	if !filepath.IsAbs(dotdir) || !filepath.IsAbs(root) {
		return p, fmt.Errorf("ZDOTDIR and ZSH must be absolute paths for terminal setup")
	}
	p.root, p.rc = root, filepath.Join(dotdir, ".zshrc")
	var err error
	p.before, err = os.ReadFile(p.rc)
	if err != nil && !os.IsNotExist(err) {
		return p, err
	}
	if _, err := os.Stat(filepath.Join(root, "oh-my-zsh.sh")); os.IsNotExist(err) {
		if _, err := os.Lstat(root); err == nil || !os.IsNotExist(err) {
			return p, fmt.Errorf("%s exists without oh-my-zsh.sh; preserve it and repair the installation before retrying", root)
		}
		p.clone = true
	} else if err != nil {
		return p, err
	}
	if _, err := lookPath("zsh"); err != nil {
		p.packages = append(p.packages, "zsh")
	}
	if p.clone {
		if _, err := lookPath("git"); err != nil {
			p.packages = append(p.packages, "git")
		}
	}
	p.changeShell = profile.Shell != "zsh"
	if p.changeShell {
		if _, err := lookPath("chsh"); err != nil {
			return p, fmt.Errorf("chsh is required to select zsh as your login shell; ask your system administrator to configure it")
		}
	}
	p.after = bytes.Clone(p.before)
	if !ohMyZshSource.Match(p.before) {
		if len(p.after) > 0 && p.after[len(p.after)-1] != '\n' {
			p.after = append(p.after, '\n')
		}
		block := "\n# >>> cully:zsh >>>\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport ZSH=" + quote(root) + "\nZSH_THEME=${ZSH_THEME:-robbyrussell}\nif (( ! ${+plugins} )); then plugins=(git); fi\nsource \"$ZSH/oh-my-zsh.sh\"\n# <<< cully:zsh <<<\n"
		p.after = append(p.after, block...)
	}
	return p, nil
}

func (p shellSetupPlan) ready() bool {
	return len(p.packages) == 0 && !p.clone && !p.changeShell && bytes.Equal(p.before, p.after)
}

// Package managers handle privilege prompts themselves after Cully's consent.
func shellPackageCommand(goos string, packages []string, lookPath func(string) (string, error)) ([]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	if goos == "darwin" {
		if _, err := lookPath("brew"); err != nil {
			return nil, fmt.Errorf("install zsh and Git with your macOS package manager, then rerun setup")
		}
		return append([]string{"brew", "install"}, packages...), nil
	}
	for _, manager := range []struct {
		name string
		args []string
	}{{"apt-get", []string{"install", "-y"}}, {"dnf", []string{"install", "-y"}}, {"pacman", []string{"-S", "--needed", "--noconfirm"}}, {"zypper", []string{"--non-interactive", "install"}}, {"apk", []string{"add"}}} {
		if _, err := lookPath(manager.name); err != nil {
			continue
		}
		command := append([]string{manager.name}, manager.args...)
		command = append(command, packages...)
		if os.Geteuid() != 0 {
			if _, err := lookPath("sudo"); err != nil {
				return nil, fmt.Errorf("install %s with your administrator, then rerun setup", strings.Join(packages, " and "))
			}
			command = append([]string{"sudo"}, command...)
		}
		return command, nil
	}
	return nil, fmt.Errorf("no supported package manager found; install %s, then rerun setup", strings.Join(packages, " and "))
}

func applyShellSetup(p shellSetupPlan, output io.Writer, packages []string, lookPath func(string) (string, error), run func([]string) error) error {
	if len(packages) > 0 {
		if err := run(packages); err != nil {
			return fmt.Errorf("install shell prerequisites: %w; rerun cully terminal shell to retry", err)
		}
	}
	zsh, err := lookPath("zsh")
	if err != nil {
		return fmt.Errorf("zsh is still unavailable after installation: %w", err)
	}
	if p.clone {
		if err := os.MkdirAll(filepath.Dir(p.root), 0700); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(filepath.Dir(p.root), ".cully-ohmyzsh-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		checkout := filepath.Join(stage, "ohmyzsh")
		if err := run([]string{"git", "clone", "--depth=1", "--", ohMyZshRemote, checkout}); err != nil {
			return fmt.Errorf("download Oh My Zsh: %w; existing shell configuration is unchanged", err)
		}
		if _, err := os.Stat(filepath.Join(checkout, "oh-my-zsh.sh")); err != nil {
			return fmt.Errorf("downloaded Oh My Zsh is incomplete: %w", err)
		}
		if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
			return fmt.Errorf("Oh My Zsh destination changed during setup; retry")
		}
		if err := os.Rename(checkout, p.root); err != nil {
			return err
		}
	}
	if !bytes.Equal(p.before, p.after) {
		// Syntax-check without loading the user's interactive startup files.
		file, err := os.CreateTemp("", "cully-zshrc-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(p.after)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := run([]string{zsh, "-f", "-n", file.Name()}); err != nil {
			return fmt.Errorf("zsh configuration has syntax errors; original configuration preserved: %w", err)
		}
		backup, err := writeTerminalSettings(p.rc, p.before, p.after)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "Configured Oh My Zsh in %s. Existing settings preserved.\n", p.rc)
		if backup != "" {
			fmt.Fprintf(output, "Shell backup: %s\nRestore with: cp %s %s\n", backup, quote(backup), quote(p.rc))
		} else {
			fmt.Fprintln(output, "To undo the shell configuration, remove the marked cully:zsh block from that file.")
		}
	}
	if p.changeShell {
		if err := run([]string{"chsh", "-s", zsh}); err != nil {
			return fmt.Errorf("zsh and Oh My Zsh are installed, but the login-shell change failed: %w; run chsh -s %s and reopen your terminal", err, quote(zsh))
		}
	}
	fmt.Fprintln(output, "zsh + Oh My Zsh configured. Open a new terminal before starting your next Cully session.")
	return nil
}

func setupShell(input, output *os.File, dryRun bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	profile := detectTerminalProfile()
	p, err := planShellSetup(home, os.Getenv("ZDOTDIR"), os.Getenv("ZSH"), profile, exec.LookPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "Cully terminal requirements: macOS or Linux, zsh + Oh My Zsh, in macOS Terminal, Ghostty, iTerm2, Kitty, WezTerm, Alacritty or a native Linux terminal.")
	if p.ready() {
		fmt.Fprintln(output, "zsh + Oh My Zsh: already configured.")
		return nil
	}
	packages, err := shellPackageCommand(runtime.GOOS, p.packages, exec.LookPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "Applying required shell setup (use --dry-run to preview):")
	if len(packages) > 0 {
		fmt.Fprintf(output, "  Install prerequisites: %s (may request administrator authentication).\n", strings.Join(packages, " "))
	}
	if p.clone {
		fmt.Fprintf(output, "  Download Oh My Zsh from %s into %s.\n", ohMyZshRemote, p.root)
	}
	if !bytes.Equal(p.before, p.after) {
		fmt.Fprintf(output, "  Back up %s if it exists and add Oh My Zsh initialization, Git completion and a prompt.\n", p.rc)
	}
	if p.changeShell {
		fmt.Fprintf(output, "  Change your login shell from %s to zsh using chsh (may request your password).\n", fallback(profile.Shell, "unknown"))
	}
	if dryRun {
		return nil
	}
	return applyShellSetup(p, output, packages, exec.LookPath, func(command []string) error {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, output
		return cmd.Run()
	})
}
