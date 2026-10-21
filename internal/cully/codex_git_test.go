//go:build !windows

package cully

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCodexGitChangesIncludesIndexAndWorkingTree(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	// NUL-delimited numstat must cope with tabs and newlines in Git paths.
	name := "file\twith\nname.txt"
	write(name, "one\ntwo\n")
	write("binary", "one\x00two")
	git("add", ".")
	git("-c", "user.name=Cully Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "base")
	view := sessionView{Project: dir}
	readGitChanges(&view)
	if !view.ChangesKnown || view.ChangedFiles != 0 || view.LinesAdded != 0 || view.LinesRemoved != 0 {
		t.Fatalf("clean repository: %+v", view)
	}
	write(name, "one\nreplacement\nthree\n")
	git("add", "--", name)
	write(name, "one\nreplacement\nthree\nfour\n")
	write("binary", "different\x00bytes")
	write("untracked", "not a tracked diff\n")
	readGitChanges(&view)
	if !view.ChangesKnown || view.ChangedFiles != 2 || view.LinesAdded != 3 || view.LinesRemoved != 1 || view.UntrackedFiles != 1 {
		t.Fatalf("tracked aggregate against HEAD: %+v", view)
	}
	view.Project = t.TempDir()
	readGitChanges(&view)
	if view.ChangesKnown || view.LinesAdded != 0 || view.LinesRemoved != 0 || view.ChangedFiles != 0 || view.UntrackedFiles != 0 {
		t.Fatalf("non-Git directory should clear stale metrics: %+v", view)
	}
}
