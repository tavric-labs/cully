//go:build !windows

package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestPaneDividerResizesRealChildAndPreservesInput(t *testing.T) {
	for _, program := range []string{"Apple_Terminal", "gnome-terminal"} {
		t.Run(program, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", program)
			t.Setenv("CULLY_ANALYZE_DISABLE", "1")
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			script := "#!/bin/sh\nstty raw -echo\nset -- $(stty size)\nprintf 'READY rows=%s\\r\\n' \"$1\"\nfor i in 1 2 3 4; do\ndd bs=1 count=1 of=\"$CLAUDE_CONFIG_DIR/input-$i\" 2>/dev/null\nset -- $(stty size)\nprintf 'ACK%s rows=%s\\r\\n' \"$i\" \"$1\"\ndone\n"
			if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 149}); err != nil {
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
					got := captured.String()
					mu.Unlock()
					if err != nil || strings.Contains(got, "\x1b[?1049l") {
						return
					}
				}
			}()
			done := make(chan error, 1)
			go func() { done <- RunPane("codex", nil, slave, slave) }()
			waitFor := func(marker string, after ...int) string {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					got := snapshot()
					window := got
					if len(after) > 0 {
						window = got[after[0]:]
					}
					if strings.Contains(window, marker) {
						return got
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("missing %q in terminal output", marker)
				return ""
			}
			got := waitFor("READY rows=")
			match := regexp.MustCompile("READY rows=([0-9]+)").FindStringSubmatch(got)
			if len(match) != 2 {
				t.Fatalf("initial child size missing: %q", got)
			}
			initial, _ := strconv.Atoi(match[1])
			_, _ = master.Write([]byte(fmt.Sprintf("\x1b[<35;10;%dM", initial+1)))
			waitFor("\x1b]22;ns-resize\x1b\\")
			_, _ = master.Write([]byte("\x1b[<35;10;1M"))
			waitFor("\x1b]22;default\x1b\\")
			// Split drag reports across reads, then send ordinary input to the child.
			_, _ = master.Write([]byte(fmt.Sprintf("\x1b[<0;10;%dM", initial+1)))
			// A real host will not send motion until Cully asks for it. No resize
			// shortcut should be necessary to pick up the visible divider.
			waitFor("\x1b[?1002h")
			_, _ = master.Write([]byte("\x1b[<32;10;"))
			_, _ = master.Write([]byte("16M\x1b[<0;10;16ma"))
			waitFor("ACK1 rows=15")
			// Let advice refresh before shrinking; it must respect the manual height.
			time.Sleep(200 * time.Millisecond)
			_, _ = master.Write([]byte("\x1b[<0;10;16M\x1b[<32;10;39M\x1b[<0;10;39mb"))
			waitFor("ACK2 rows=38")
			// Esc also releases a picked-up divider, without leaking the control
			// into the agent or requiring a mouse release outside the window.
			before := len(snapshot())
			_, _ = master.Write([]byte("\x1b[<0;10;39M"))
			_, _ = master.Write([]byte{0x1b})
			waitFor("\x1b]22;default\x1b\\", before)
			_, _ = master.Write([]byte("c"))
			waitFor("ACK3 rows=38")
			_, _ = master.Write([]byte("q"))
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("child input swallowed")
			}
			for i, want := range []string{"a", "b", "c", "q"} {
				b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("input-%d", i+1)))
				if err != nil || string(b) != want {
					t.Fatalf("divider controls leaked into child: %q (%v)", b, err)
				}
			}
		})
	}
}
