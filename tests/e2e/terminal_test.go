package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// fakeAgent stands in for Codex, Claude Code or Cursor. It reports the
// environment it was given, the exact bytes of its first key press and each
// later key, and exits on q.
const fakeAgent = `#!/bin/sh
stty raw -echo
n=$(cat "$E2E_DIR/count" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "$E2E_DIR/count"
printf 'SESSION %s RUN=%s\r\n' "$n" "${CULLY_RUN_ACTIVE:-none}"
first=$(dd bs=6 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n')
printf 'KEYS=%s-%s\r\n' "$first" "$n"
while true; do
c=$(dd bs=1 count=1 2>/dev/null)
[ -z "$c" ] && exit 0
[ "$c" = q ] && exit 0
printf 'GOT%s-%s\r\n' "$c" "$n"
done
`

// TestTerminal drives the built CLI the way a user does: cully run in a native
// terminal, the session sidebar, and an untouched direct launch.
func TestTerminal(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Cully supports macOS and Linux only")
	}
	dir := t.TempDir()
	cli := filepath.Join(dir, "cully")
	build := exec.Command("go", "build", "-o", cli, "../../cmd/cully")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	agentPath := filepath.Join(dir, "codex")
	if err := os.WriteFile(agentPath, []byte(fakeAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"HOME="+home, "E2E_DIR="+dir, "CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"),
		"CODEX_HOME="+filepath.Join(home, ".codex"), "CURSOR_CONFIG_DIR="+filepath.Join(home, ".cursor"),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal", "CULLY_ANALYZE_DISABLE=1", "CULLY_SHORTCUTS=0",
		"CULLY_RUN_ACTIVE=", "CULLY_PANE_SESSION=")

	t.Run("direct launch stays native", func(t *testing.T) {
		cmd := exec.Command(agentPath)
		cmd.Env = env
		cmd.Stdin = strings.NewReader("q")
		out, _ := cmd.CombinedOutput()
		if !strings.Contains(string(out), "RUN=none") {
			t.Fatalf("a direct launch saw Cully activation: %q", out)
		}
		if err := os.Remove(filepath.Join(dir, "count")); err != nil {
			t.Fatal(err)
		}
	})

	cmd := exec.Command(cli, "run", "codex")
	cmd.Env = env
	cmd.Dir = home
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	defer cmd.Process.Kill() //nolint:errcheck
	var mu sync.Mutex
	var captured strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := terminal.Read(buf)
			mu.Lock()
			captured.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); return captured.String() }
	waitFor := func(marker string, after int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if got := snapshot(); len(got) >= after && strings.Contains(got[after:], marker) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("missing %q in %q", marker, snapshot()[after:])
	}
	send := func(s string) { _, _ = terminal.Write([]byte(s)) }
	click := func(x, y string) { send("\x1b[<0;" + x + ";" + y + "M\x1b[<0;" + x + ";" + y + "m") }

	// cully run activates Cully, and a modified arrow reaches the agent whole.
	waitFor("SESSION 1 RUN=1", 0)
	waitFor("Sessions", 0)
	send("\x1b[1;2C") // Shift+Right
	waitFor("KEYS=1b5b313b3243-1", 0)

	// + New starts a second session; each session keeps its own input.
	mark := len(snapshot())
	click("3", "2")
	waitFor("SESSION 2 RUN=1", mark)
	send("\x1b[1;2C")
	waitFor("KEYS=1b5b313b3243-2", mark)
	send("a")
	waitFor("GOTa-2", mark)

	// Click the first session to switch back, then rename it.
	mark = len(snapshot())
	click("6", "3")
	send("b")
	waitFor("GOTb-1", mark)
	mark = len(snapshot())
	click("12", "2")
	send("work\r")
	waitFor("1 work", mark)

	// Sessions end one by one, and the window ends with the last.
	send("q")
	mark = len(snapshot())
	waitFor("1 codex 2", mark) // the list renumbers once session 1 has left
	send("q")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cully run did not exit after its last session ended")
	}
}
