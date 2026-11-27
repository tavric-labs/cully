package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fastBinaryWait(t *testing.T) {
	t.Helper()
	w, p := uiBinaryWait, uiBinaryPoll
	uiBinaryWait, uiBinaryPoll = 400*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { uiBinaryWait, uiBinaryPoll = w, p })
}

func TestAProgramThatAppearsDuringAnUpdateIsWaitedFor(t *testing.T) {
	fastBinaryWait(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	}()
	got, err := uiAwaitBinary(bin)
	if err != nil || got != bin {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestADanglingLinkRepointedByAnUpdaterIsFollowed(t *testing.T) {
	fastBinaryWait(t)
	dir := t.TempDir()
	link := filepath.Join(dir, "claude")
	if err := os.Symlink(filepath.Join(dir, "versions", "old"), link); err != nil { // the target is gone, as mid-update
		t.Fatal(err)
	}
	next := filepath.Join(dir, "claude-next")
	if err := os.WriteFile(next, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = os.Remove(link)
		_ = os.Symlink(next, link)
	}()
	got, err := uiAwaitBinary(link)
	if err != nil {
		t.Fatal(err)
	}
	if st, e := os.Stat(got); e != nil || st.IsDir() {
		t.Fatalf("returned a path that does not exist: %q", got)
	}
}

func TestAProgramThatNeverAppearsGetsAnExplanation(t *testing.T) {
	fastBinaryWait(t)
	_, err := uiAwaitBinary(filepath.Join(t.TempDir(), "claude"))
	if err == nil || !strings.Contains(err.Error(), "updating itself") || !strings.Contains(err.Error(), "claude --version") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "fork/exec") {
		t.Fatalf("the raw system error leaked: %v", err)
	}
}

func TestATaskWhoseHarnessIsMissingFailsWithTheExplanation(t *testing.T) {
	fastBinaryWait(t)
	m, repo, _ := uiTestManager(t, filepath.Join(t.TempDir(), "claude"), "tok-aaa1") // a path that does not exist
	a, err := m.start(uiStartRequest{Repo: repo, Title: "missing", Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the task to fail", func() bool { return a.snapshot().State == "failed" })
	if last := a.snapshot().Last; !strings.Contains(last, "updating itself") {
		t.Fatalf("last = %q", last)
	}
}

func TestAnExplicitProgramIsNeverSwappedForOneOnPath(t *testing.T) {
	fastBinaryWait(t)
	onPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(onPath, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", onPath)
	if got, err := uiAwaitBinary(filepath.Join(t.TempDir(), "claude")); err == nil {
		t.Fatalf("an explicit path that does not exist was replaced by %q", got)
	}
	// The same name, but the missing program was in a PATH folder: that is the updater case, so look it up again.
	if got, err := uiAwaitBinary(filepath.Join(onPath, "claude")); err != nil || got != filepath.Join(onPath, "claude") {
		t.Fatalf("got %q, %v", got, err)
	}
}
