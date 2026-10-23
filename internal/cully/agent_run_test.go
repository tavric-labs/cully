package cully

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestAgentUtilityRouting(t *testing.T) {
	for _, tc := range []struct {
		agent  string
		args   []string
		direct bool
	}{
		{"codex", []string{"mcp", "get", "cully"}, true},
		{"codex", []string{"mcp", "status", "cully"}, true},
		{"codex", []string{"-c", "model=mcp", "-p", "work", "exec", "--json", "hello"}, true},
		{"codex", []string{"--config=model=mcp", "queue", "thread", "hello"}, true},
		{"codex", []string{"resume", "--help"}, true},
		{"codex", []string{"resume", "--last"}, false},
		{"codex", []string{"fork", "thread"}, false},
		{"codex", []string{"--model", "mcp", "explain the code"}, false},
		{"codex", []string{"-p", "mcp"}, false},
		{"codex", []string{"--", "help"}, false},
		{"codex", nil, false},
		{"claude", []string{"mcp", "list"}, true},
		{"claude", []string{"-p", "explain the code"}, true},
		{"claude", []string{"--model", "mcp", "--continue"}, false},
		{"claude", []string{"--verbose"}, false},
		{"cursor", []string{"mcp", "list"}, true},
		{"cursor", []string{"--print", "hello"}, true},
		{"cursor", []string{"--resume", "thread"}, false},
		{"future-agent", []string{"--help"}, true},
	} {
		if got := agentUtilityCommand(tc.agent, tc.args); got != tc.direct {
			t.Errorf("%s %v: direct=%v, want %v", tc.agent, tc.args, got, tc.direct)
		}
	}
}

func TestRunAgentPreservesNativeStreamsArgumentsAndExit(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'arg=<%s>\\n' \"$@\"\ncat\nprintf 'native diagnostic\\n' >&2\nexit 23\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	inputPath := filepath.Join(dir, "in")
	if err := os.WriteFile(inputPath, []byte("piped input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input, _ := os.Open(inputPath)
	output, _ := os.Create(filepath.Join(dir, "out"))
	stderr, _ := os.Create(filepath.Join(dir, "err"))
	defer input.Close()
	defer output.Close()
	defer stderr.Close()
	args := []string{"mcp", "status", "cully", "space value", "$(literal)", ""}
	original := append([]string(nil), args...)
	err := RunAgent("codex", args, input, output, stderr)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("native exit lost: %v", err)
	}
	got, _ := os.ReadFile(output.Name())
	want := "arg=<mcp>\narg=<status>\narg=<cully>\narg=<space value>\narg=<$(literal)>\narg=<>\npiped input\n"
	if string(got) != want {
		t.Fatalf("stdout/argv changed: %q", got)
	}
	diagnostic, _ := os.ReadFile(stderr.Name())
	if string(diagnostic) != "native diagnostic\n" {
		t.Fatalf("stderr changed: %q", diagnostic)
	}
	if !reflect.DeepEqual(args, original) {
		t.Fatal("caller argv mutated")
	}
}

func TestRunAgentUtilityKeepsTTYWithoutPanel(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n[ -t 0 ] && [ -t 1 ] || exit 9\nprintf 'native tty output\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	_ = pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 100})
	got := make(chan string, 1)
	go func() { b := make([]byte, 4096); n, _ := master.Read(b); got <- string(b[:n]) }()
	if err := RunAgent("codex", []string{"mcp", "--help"}, slave, slave, slave); err != nil {
		t.Fatal(err)
	}
	select {
	case output := <-got:
		if output != "native tty output\r\n" {
			t.Fatalf("panel changed native output: %q", output)
		}
	case <-time.After(time.Second):
		t.Fatal("native output missing")
	}
}

func TestRunAgentPipedSessions(t *testing.T) {
	for _, args := range [][]string{nil, {"resume", "--last"}, {"future-command", "--json"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nprintf 'native\\n'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			input, _ := os.Open(os.DevNull)
			defer input.Close()
			output, _ := os.Create(filepath.Join(dir, "out"))
			defer output.Close()
			if err := RunAgent("codex", args, input, output, output); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(output.Name())
			if string(got) != "native\n" {
				t.Fatalf("non-TTY wrapper altered output: %q", got)
			}
		})
	}
}
