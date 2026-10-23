package cully

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func shellTestLookup(name string) (string, error) { return "/usr/bin/" + name, nil }

func TestRequiredShellSetupPreservesConfigAndIsRepeatable(t *testing.T) {
	home := t.TempDir()
	dotdir := filepath.Join(home, "dot files")
	root := filepath.Join(home, "oh my ' zsh")
	if err := os.Mkdir(dotdir, 0700); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(dotdir, ".zshrc")
	before := []byte("# my settings\nexport EDITOR=vim\nalias ll='ls -l'\n")
	if err := os.WriteFile(rc, before, 0640); err != nil {
		t.Fatal(err)
	}
	profile := terminalProfile{OS: "linux", Shell: "bash"}
	plan, err := planShellSetup(home, dotdir, root, profile, shellTestLookup)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.clone || !plan.changeShell || !bytes.HasPrefix(plan.after, before) {
		t.Fatalf("invalid required setup: %+v", plan)
	}
	var commands []string
	run := func(command []string) error {
		commands = append(commands, strings.Join(command, " "))
		if command[0] == "git" {
			dest := command[len(command)-1]
			if err := os.Mkdir(dest, 0700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dest, "oh-my-zsh.sh"), nil, 0600)
		}
		if command[1] == "-f" {
			if zsh, err := exec.LookPath("zsh"); err == nil {
				return exec.Command(zsh, command[1:]...).Run()
			}
		}
		return nil
	}
	var output bytes.Buffer
	if err := applyShellSetup(plan, &output, nil, shellTestLookup, run); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rc)
	if err != nil || !bytes.HasPrefix(data, before) || !strings.Contains(string(data), "source \"$ZSH/oh-my-zsh.sh\"") {
		t.Fatalf("configuration: %q (%v)", data, err)
	}
	info, _ := os.Stat(rc)
	if info.Mode().Perm() != 0640 {
		t.Fatal("shell file permissions changed")
	}
	backups, _ := filepath.Glob(rc + ".cully-backup-*")
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	saved, _ := os.ReadFile(backups[0])
	if !bytes.Equal(saved, before) {
		t.Fatal("original shell configuration not backed up")
	}
	if !strings.Contains(strings.Join(commands, "\n"), "chsh -s /usr/bin/zsh") {
		t.Fatal("did not select required login shell")
	}
	profile.Shell = "zsh"
	again, err := planShellSetup(home, dotdir, root, profile, shellTestLookup)
	if err != nil || !again.ready() {
		t.Fatalf("repeat setup should need no changes: %+v (%v)", again, err)
	}
}

func TestRequiredShellSetupFailurePreservesOriginal(t *testing.T) {
	home := t.TempDir()
	before := []byte("export EDITOR=vim\n")
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, before, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := planShellSetup(home, "", "", terminalProfile{OS: "linux", Shell: "zsh"}, shellTestLookup)
	if err != nil {
		t.Fatal(err)
	}
	err = applyShellSetup(plan, &bytes.Buffer{}, nil, shellTestLookup, func([]string) error { return fmt.Errorf("network unavailable") })
	if err == nil {
		t.Fatal("download failure reported success")
	}
	data, _ := os.ReadFile(rc)
	if !bytes.Equal(data, before) {
		t.Fatal("failed download modified shell configuration")
	}
	if _, err := os.Stat(plan.root); !os.IsNotExist(err) {
		t.Fatal("failed download left an installed framework")
	}
	if err := os.Mkdir(plan.root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := planShellSetup(home, "", "", terminalProfile{OS: "linux"}, shellTestLookup); err == nil {
		t.Fatal("accepted an unrelated existing framework directory")
	}
}

func TestShellConfigSymlinkAndConcurrentChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-zshrc")
	path := filepath.Join(dir, ".zshrc")
	before := []byte("original\n")
	if err := os.WriteFile(target, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	backup, err := writeTerminalSettings(path, before, []byte("updated\n"))
	if err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(path); err != nil || link != target {
		t.Fatal("rewrote shell symlink")
	}
	saved, _ := os.ReadFile(backup)
	if !bytes.Equal(saved, before) {
		t.Fatal("backup lost original")
	}
	if _, err := writeTerminalSettings(path, before, before); err == nil {
		t.Fatal("overwrote a concurrent shell edit")
	}
}

func TestSupportedNativeTerminalsOnly(t *testing.T) {
	for _, test := range []struct {
		os, program string
		allowed     bool
	}{
		{"darwin", "Apple_Terminal", true}, {"linux", "gnome-terminal", true}, {"linux", "ptyxis", true}, {"linux", "gnome-console", true},
		{"linux", "", true}, {"darwin", "WarpTerminal", false}, {"linux", "Warp", false}, {"darwin", "iTerm.app", false}, {"windows", "", false},
	} {
		if err := (terminalProfile{OS: test.os, Program: test.program}).requireNative(); (err == nil) != test.allowed {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}

func TestShellDependencyPlans(t *testing.T) {
	lookup := func(name string) (string, error) {
		if name == "apt-get" || name == "sudo" {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	}
	command, err := shellPackageCommand("linux", []string{"zsh", "git"}, lookup)
	if err != nil || !strings.Contains(strings.Join(command, " "), "apt-get install -y zsh git") {
		t.Fatalf("%v: %v", command, err)
	}
	if _, err := shellPackageCommand("darwin", []string{"zsh"}, lookup); err == nil {
		t.Fatal("silently installed a new package manager")
	}
}

func TestHeadlessServiceSetupDoesNotProvisionDesktop(t *testing.T) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(filepath.Join(t.TempDir(), "setup-output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	// An unsupported profile and missing tools would fail explicit terminal
	// setup. Headless service setup must never reach those mutations.
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	t.Setenv("PATH", t.TempDir())
	if err := SetupInteractiveTerminal(input, output, false, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil || !strings.Contains(string(data), "not run") {
		t.Fatalf("missing honest desktop-setup result: %q %v", data, err)
	}
}
