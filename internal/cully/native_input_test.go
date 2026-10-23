package cully

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestNativeAgentKeysAndArgumentsPassThrough(t *testing.T) {
	for _, test := range []struct{ name, binary string }{{"codex", "codex"}, {"claude", "claude"}, {"cursor", "cursor-agent"}} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CULLY_SHORTCUTS", "0")
			t.Setenv("CULLY_ANALYZE_DISABLE", "1")
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			keys := []byte("\x1b[1;2C\x1b[1;2D\x1b[1;5C\x1b[Z\x1b[13;2u\x1b\r\x1ba\x1d\x1c\x1b[17~\x1b[18~\x1b[5~\x1b[6~\x03\x1a\x07\x12")
			capture := filepath.Join(dir, "input")
			argv := filepath.Join(dir, "argv")
			script := fmt.Sprintf("#!/bin/sh\nstty raw -echo\nprintf '%%s\\n' \"$@\" > %s\nprintf 'NATIVE_KEYS_READY\\r\\n'\ndd bs=1 count=%d of=%s 2>/dev/null\n", quote(argv), len(keys), quote(capture))
			if err := os.WriteFile(filepath.Join(dir, test.binary), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 32, Cols: 120}); err != nil {
				t.Fatal(err)
			}
			ready := make(chan struct{})
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				var output strings.Builder
				sent := false
				b := make([]byte, 4096)
				for {
					n, err := master.Read(b)
					output.Write(b[:n])
					if !sent && strings.Contains(output.String(), "NATIVE_KEYS_READY") {
						close(ready)
						sent = true
					}
					if err != nil || strings.Contains(output.String(), "\x1b[?1049l") {
						return
					}
				}
			}()
			args := []string{"--model", "user-choice", "-c", `tui.status_line=["current-dir"]`}
			done := make(chan error, 1)
			go func() { done <- RunPane(test.name, args, slave, slave) }()
			select {
			case <-ready:
			case err := <-done:
				t.Fatalf("wrapper exited before input: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("native child did not start")
			}
			if _, err := master.Write(keys); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native keys were consumed")
			}
			got, err := os.ReadFile(capture)
			if err != nil || !bytes.Equal(got, keys) {
				t.Fatalf("native input changed: %q vs %q (%v)", got, keys, err)
			}
			got, err = os.ReadFile(argv)
			if err != nil || string(got) != strings.Join(args, "\n")+"\n" {
				t.Fatalf("native arguments overridden: %q (%v)", got, err)
			}
			master.Close()
			slave.Close()
			select {
			case <-drained:
			case <-time.After(time.Second):
				t.Fatal("PTY output did not finish")
			}
		})
	}
}
