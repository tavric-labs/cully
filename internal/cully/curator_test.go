package cully

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestOnlyOneProcessHoldsTheIndexerLock(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	release, ok := uiIndexerLock()
	if !ok {
		t.Fatal("the first process did not get the lock")
	}
	if _, second := uiIndexerLock(); second {
		t.Fatal("a second process got the lock while the first held it")
	}
	release()
	again, ok := uiIndexerLock()
	if !ok {
		t.Fatal("the lock was not released")
	}
	again()
}

func TestACuratorStandsDownWhenAnotherProcessIsIndexing(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	release, ok := uiIndexerLock()
	if !ok {
		t.Fatal("no lock")
	}
	defer release()
	var out bytes.Buffer
	if err := RunCurator(&out, []string{"--once"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already keeping the library up to date") {
		t.Fatalf("output = %q", out.String())
	}
	out.Reset()
	if err := RunCurator(&out, []string{"--once", "--quiet"}); err != nil || out.Len() != 0 {
		t.Fatalf("quiet run: %v %q", err, out.String())
	}
}

func TestACuratorPassRunsWhenItHoldsTheLock(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CURSOR_HOME", t.TempDir())
	var out bytes.Buffer
	if err := RunCurator(&out, []string{"--once"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Curator pass done") {
		t.Fatalf("output = %q", out.String())
	}
	if _, ok := uiIndexerLock(); !ok {
		t.Fatal("the curator kept the lock after it finished")
	}
}

func TestAFollowingUIRereadsTheLibraryTheCuratorWrote(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.idxMu.Lock()
	m.loadIndexLocked()
	loaded := m.index != nil
	m.idxMu.Unlock()
	if !loaded {
		t.Fatal("the index did not load")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prev := uiFollowEvery
	uiFollowEvery = 20 * time.Millisecond
	defer func() { uiFollowEvery = prev }()
	go m.followIndexer(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.idxMu.Lock()
		dropped := m.index == nil
		m.idxMu.Unlock()
		if dropped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the following UI kept its stale in-memory index")
}
