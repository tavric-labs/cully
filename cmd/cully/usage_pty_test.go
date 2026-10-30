package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// Exercise the actual CLI dispatch, native launch arguments, callback process,
// private state, and terminal repaint without installed agents or model calls.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "_internal":
			if err := runInternal(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "_test_wrap":
			if err := run([]string{"run", os.Args[2]}); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "_test_native_agent":
			fakeUsageAgent(os.Args[2], os.Args[3:])
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func fakeUsageAgent(agent string, args []string) {
	// Native connection probes must not start a foreground fixture session.
	if os.Getenv("MODEL_HINT_GUARD") != "" || agent == "codex" && len(args) > 0 && args[0] == "app-server" {
		return
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	var command *exec.Cmd
	switch agent {
	case "codex":
		if !strings.Contains(strings.Join(args, " "), "total-input-tokens") {
			return
		}
		_, height, _ := term.GetSize(int(os.Stdout.Fd()))
		fmt.Printf("\x1b[%d;1HFixture Codex · Context 73%% left · 12000 in · 240 out", height-1)
	case "claude":
		var settings struct {
			StatusLine struct{ Command string } `json:"statusLine"`
		}
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--settings" {
				_ = json.Unmarshal([]byte(args[i+1]), &settings)
			}
		}
		if settings.StatusLine.Command == "" {
			return
		}
		command = exec.Command("sh", "-c", settings.StatusLine.Command)
		command.Stdin = strings.NewReader(`{"session_id":"fixture-native-claude","model":{"display_name":"Fixture Claude"},"context_window":{"used_percentage":27,"total_input_tokens":12000,"total_output_tokens":240}}`)
	case "cursor":
		exe, _ := os.Executable()
		command = exec.Command(exe, "_internal", "pane-signal", "cursor")
		command.Stdin = strings.NewReader(`{"hook_event_name":"preCompact","conversation_id":"fixture-native-cursor","model_id":"Fixture Cursor","context_usage_percent":85,"context_tokens":108800,"context_window_size":128000}`)
	}
	if command != nil {
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
	}
	<-stop
}

func TestWrappedUsageReachesPTYPanel(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"codex", "claude", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			root := t.TempDir()
			binary := agent
			if agent == "cursor" {
				binary = "cursor-agent"
			}
			script := fmt.Sprintf("#!/bin/sh\nexec '%s' _test_native_agent %s \"$@\"\n", strings.ReplaceAll(exe, "'", "'\\''"), agent)
			if err := os.WriteFile(filepath.Join(root, binary), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "_test_wrap", agent)
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "CULLY_") && !strings.HasPrefix(value, "MODEL_HINT_") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, "HOME="+root, "CLAUDE_CONFIG_DIR="+filepath.Join(root, ".claude"), "CODEX_HOME="+filepath.Join(root, ".codex"), "CURSOR_CONFIG_DIR="+filepath.Join(root, ".cursor"), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "CULLY_ANALYZE_DISABLE=1", "CULLY_SHORTCUTS=0", "TERM=xterm-256color")
			master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 180})
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer cmd.Process.Kill() // cleanup also on timeout
			var mu sync.Mutex
			var output bytes.Buffer
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				buf := make([]byte, 32*1024)
				for {
					n, err := master.Read(buf)
					mu.Lock()
					if output.Len() < 4<<20 {
						output.Write(buf[:n])
					}
					mu.Unlock()
					if err != nil {
						return
					}
				}
			}()
			want := []string{"Fixture " + strings.ToUpper(agent[:1]) + agent[1:], "Tokens in 12000", "out 240"}
			if agent == "cursor" {
				want = []string{"Fixture Cursor", "85% used before compaction", "Context tokens 108800"}
			}
			deadline := time.Now().Add(8 * time.Second)
			for {
				mu.Lock()
				plain := ansi.Strip(output.String())
				mu.Unlock()
				found := true
				for _, text := range want {
					found = found && strings.Contains(plain, text)
				}
				if found {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("native usage never reached %s panel; want %q; output tail: %s", agent, want, plain[max(0, len(plain)-5000):])
				}
				time.Sleep(20 * time.Millisecond)
			}
			_, _ = master.Write([]byte{3})
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wrapped fixture did not release its terminal")
			}
			master.Close()
			<-drained
		})
	}
}
