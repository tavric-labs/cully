package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAgentCLIExitAndDiagnostics(t *testing.T) {
	if os.Getenv("CULLY_TEST_NATIVE_MAIN") == "1" {
		os.Args = []string{"cully", "codex", "mcp", "status", "cully"}
		main()
		return
	}
	for _, tc := range []struct {
		script string
		code   int
	}{
		{"printf 'native stdout\\n'\nprintf 'native stderr\\n' >&2\nexit 23\n", 23},
		{"printf 'native stdout\\n'\nprintf 'native stderr\\n' >&2\nkill -TERM $$\n", 128 + int(syscall.SIGTERM)},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\n"+tc.script), 0755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestAgentCLIExitAndDiagnostics$")
		cmd.Env = append(os.Environ(), "CULLY_TEST_NATIVE_MAIN=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		var out, diagnostic strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != tc.code {
			t.Fatalf("exit = %v; want %d", err, tc.code)
		}
		if out.String() != "native stdout\n" || diagnostic.String() != "native stderr\n" {
			t.Fatalf("native diagnostics changed: stdout=%q stderr=%q", out.String(), diagnostic.String())
		}
	}
}
