package cully

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// A fullscreen agent decides how it hears the wheel. Without mouse reports it
// must still scroll, as arrow keys; with them it must get the real wheel.
func TestPaneWheelReachesAlternateScreenAgent(t *testing.T) {
	for _, tc := range []struct {
		name, enable, want string
	}{
		{"no mouse reports", "", strings.Repeat("\x1b[A", wheelLines) + strings.Repeat("\x1b[B", wheelLines)},
		{"mouse reports", `\033[?1000h\033[?1006h`, "\x1b[<64;10;5M\x1b[<65;10;5M"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", "Apple_Terminal")
			t.Setenv("CULLY_ANALYZE_DISABLE", "1")
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			script := "#!/bin/sh\nstty raw -echo\nprintf '\\033[?1049h" + tc.enable + "READY\\r\\n'\ndd bs=1 count=" +
				strconv.Itoa(len(tc.want)) + " of=\"$CLAUDE_CONFIG_DIR/input\" 2>/dev/null\n"
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
			deadline := time.Now().Add(5 * time.Second)
			for {
				mu.Lock()
				ready := strings.Contains(captured.String(), "READY")
				mu.Unlock()
				if ready {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("agent never became ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Trackpad-style input: one notch up, one down, over the agent.
			_, _ = master.Write([]byte("\x1b[<64;10;5M"))
			time.Sleep(100 * time.Millisecond)
			_, _ = master.Write([]byte("\x1b[<65;10;5M"))
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wheel never reached the agent")
			}
			got, err := os.ReadFile(filepath.Join(dir, "input"))
			if err != nil || string(got) != tc.want {
				t.Fatalf("agent received %q, want %q (%v)", got, tc.want, err)
			}
		})
	}
}
