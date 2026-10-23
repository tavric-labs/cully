package cully

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// Sessions in one window each keep their own process and input; the sidebar
// creates, switches and renames them with the pointer.
func TestPaneSidebarNewSwitchRename(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	t.Setenv("CULLY_SHORTCUTS", "0")
	t.Setenv("CULLY_SIDEBAR", "1") // the sidebar is opt-in; this exercises the env path
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\nstty raw -echo\nn=$(cat \"$CLAUDE_CONFIG_DIR/count\" 2>/dev/null || echo 0)\nn=$((n+1))\necho $n > \"$CLAUDE_CONFIG_DIR/count\"\nprintf 'SESSION %s\\r\\n' \"$n\"\nwhile true; do\nc=$(dd bs=1 count=1 2>/dev/null)\n[ -z \"$c\" ] && exit 0\n[ \"$c\" = q ] && exit 0\nprintf 'GOT%s-%s\\r\\n' \"$c\" \"$n\"\ndone\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var captured strings.Builder
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); return captured.String() }
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			mu.Lock()
			captured.Write(b[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- RunPane("codex", nil, slave, slave) }()
	waitFor := func(marker string, after int) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if got := snapshot(); len(got) >= after && strings.Contains(got[after:], marker) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		got := snapshot()[after:]
		t.Fatalf("missing %q; frames mentioning input: %q", marker, got[max(0, strings.Index(got, "✎")-40):min(len(got), max(0, strings.Index(got, "✎"))+200)])
	}
	send := func(s string) { _, _ = master.Write([]byte(s)) }
	click := func(x, y string) { send("\x1b[<0;" + x + ";" + y + "M\x1b[<0;" + x + ";" + y + "m") }

	waitFor("SESSION 1", 0)
	waitFor("Sessions", 0)
	mark := len(snapshot())
	click("3", "2") // + New
	waitFor("SESSION 2", mark)
	waitFor("2 codex 2", mark)
	mark = len(snapshot())
	send("a")
	waitFor("GOTa-2", mark)

	mark = len(snapshot())
	click("6", "3") // first session
	waitFor("SESSION 1", mark)
	mark = len(snapshot())
	send("b")
	waitFor("GOTb-1", mark)

	mark = len(snapshot())
	click("12", "2") // Rename
	send("work\r")
	waitFor("1 work", mark)
	mark = len(snapshot())
	send("c")
	waitFor("GOTc-1", mark)

	// An agent that exits leaves the list; the window ends with the last one.
	mark = len(snapshot())
	send("q")
	waitFor("SESSION 2", mark)
	send("q")
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("pane did not stop after its last session exited")
	}
}
