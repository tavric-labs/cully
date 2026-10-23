package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

func TestLookupPaneAgent(t *testing.T) {
	cases := []struct {
		name, binary, id string
		footer           bool
	}{
		{"claude", "claude", "claude", false},
		{"codex", "codex", "codex", true},
		{"cursor", "cursor-agent", "cursor", false},
		{"aider", "aider", "aider", false},
		{"/opt/tools/My Agent", "/opt/tools/My Agent", "", false}, // rejected below
	}
	for _, c := range cases[:4] {
		agent, err := lookupPaneAgent(c.name)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if agent.Binary != c.binary || agent.Name != c.id || agent.NativeFooter != c.footer {
			t.Fatalf("%s: got %+v", c.name, agent)
		}
	}
	for _, bad := range []string{"", "  ", "two words"} {
		if _, err := lookupPaneAgent(bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}

func TestSafeAgentName(t *testing.T) {
	for in, want := range map[string]string{
		"Cursor-Agent":       "cursor-agent",
		"/usr/local/bin/foo": "foo",
		"we!rd name":         "we-rd-name",
		"":                   "agent",
	} {
		if got := safeAgentName(in); got != want {
			t.Fatalf("safeAgentName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every agent runs in the same terminal: the child sees a PTY, receives the
// agent id in its environment, and the Cully panel is drawn around it.
func TestPaneRunsEveryAgentInTheSharedTerminal(t *testing.T) {
	for _, tc := range []struct{ name, binary, id, display string }{
		{"claude", "claude", "claude", "Claude Code"},
		{"cursor", "cursor-agent", "cursor", "Cursor"},
		{"future-agent", "future-agent", "future-agent", "future-agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CULLY_ANALYZE_DISABLE", "1")
			t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			script := "#!/bin/sh\nprintf 'child sees agent=%s arg=%s\\n' \"$CULLY_AGENT\" \"$1\"\nsleep 0.3\n"
			if err := os.WriteFile(filepath.Join(dir, tc.binary), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 100}); err != nil {
				t.Fatal(err)
			}
			output := make(chan string, 1)
			go func() {
				var captured strings.Builder
				buf := make([]byte, 4096)
				for {
					n, readErr := master.Read(buf)
					captured.Write(buf[:n])
					if strings.Contains(captured.String(), "\x1b[?1049l") || readErr != nil {
						output <- captured.String()
						return
					}
				}
			}()
			done := make(chan error, 1)
			go func() { done <- RunPane(tc.name, []string{"hello"}, slave, slave) }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("terminal did not exit with its child")
			}
			select {
			case got := <-output:
				want := "child sees agent=" + tc.id + " arg=hello"
				if !strings.Contains(normalizedCodexPanel(got), want) {
					t.Fatalf("missing %q in output", want)
				}
				if !strings.Contains(got, "Cully") {
					t.Fatal("the Cully panel was not drawn")
				}
				if !strings.Contains(ansi.Strip(got), tc.display+" ") {
					t.Fatalf("the health bar should name %q", tc.display)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("output did not close")
			}
		})
	}
}

func TestPaneReportsMissingAgent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	_ = pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 100})
	err = RunPane("not-installed-agent", nil, slave, slave)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
}
