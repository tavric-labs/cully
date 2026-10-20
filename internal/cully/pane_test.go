//go:build !windows

package cully

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func TestCodexPaneConfinesChildScreen(t *testing.T) {
	emulator := vt.NewEmulator(60, 12)
	defer emulator.Close()
	_, _ = emulator.WriteString("\x1b[2J\x1b[1;1HCODEX\x1b[12;1Hlower edge")
	content := sessionStatusRows(60, []string{"ADV|Run a focused check."}, toolStats{Tools: 3}, sessionView{Project: "/work/cully"})
	frame := renderTerminalPane(emulator, 60, 24, content, true, "")
	for _, want := range []string{"CODEX", "lower edge", "Cully", "Run a focused check.", "\x1b[13;1H"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("pane frame missing %q", want)
		}
	}
	if strings.Contains(frame, "\x1b[2J") {
		t.Fatal("child's whole-screen clear escaped the virtual terminal")
	}
	if strings.Contains(frame, "\x1b[48;") || strings.Contains(frame, "\x1b[40m") {
		t.Fatal("panel must inherit the terminal background")
	}
	if paneTop(10, 10) != 10 || paneTop(40, 10) != 29 || paneTop(24, 40) != 12 {
		t.Fatal("unexpected pane sizing")
	}
}

func TestCodexPaneStopsOnTerminalHangup(t *testing.T) {
	if os.Getenv("CULLY_PANE_HANGUP_HELPER") == "1" {
		if RunPane("codex", nil, os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	// Exercise the bounded fallback when a child ignores graceful shutdown.
	script := "#!/bin/sh\ntrap '' HUP TERM\nprintf 'hangup child ready\\n'\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(os.Args[0], "-test.run=^TestCodexPaneStopsOnTerminalHangup$")
	helper.Env = append(os.Environ(), "CULLY_PANE_HANGUP_HELPER=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CLAUDE_CONFIG_DIR="+t.TempDir())
	terminal, err := pty.StartWithSize(helper, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	defer helper.Process.Kill() //nolint:errcheck
	ready := make(chan struct{})
	go func() {
		var captured strings.Builder
		signaled := false
		buf := make([]byte, 4096)
		for {
			n, readErr := terminal.Read(buf)
			captured.Write(buf[:n])
			if !signaled && strings.Contains(captured.String(), "hangup child ready") {
				close(ready)
				signaled = true
			}
			// Drain frames so output backpressure cannot block signal handling.
			if readErr != nil {
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not become ready")
	}
	if err := helper.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	select {
	case err := <-done:
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			t.Fatalf("hangup should end child and restore wrapper: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrapper failed to stop an unresponsive child after hangup")
	}
}

func TestCodexPaneLaunchesChildInPTY(t *testing.T) {
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	dir := t.TempDir()
	command := filepath.Join(dir, "codex")
	// The second tool event arrives while Codex is idle. Its advice is unchanged,
	// but the count must still repaint. Later warnings remain compact and must
	// leave most of the terminal for the coding session.
	script := `#!/bin/sh
set -- $(stty size)
initial_rows=$1
printf '\033[2J\033[1;1Hcodex child\033[%s;1HGPT-6.1-Sol medium · Context 73%% left · 120 in · 24 out' "$(( $1 - 1 ))"
printf 'O.\n' >> "$CLAUDE_CONFIG_DIR/cully-logs/$CULLY_PANE_SESSION.codex-events"
sleep 0.3
printf 'O.\n' >> "$CLAUDE_CONFIG_DIR/cully-logs/$CULLY_PANE_SESSION.codex-events"
sleep 0.3
printf 'O!\nO!\nO!\nE.\n' >> "$CLAUDE_CONFIG_DIR/cully-logs/$CULLY_PANE_SESSION.codex-events"
sleep 0.3
set -- $(stty size)
if [ "$1" -lt 33 ]; then exit 2; fi
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 55, Cols: 80}); err != nil {
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
	go func() { done <- RunPane("codex", nil, slave, slave) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Codex pane did not exit with its child")
	}
	select {
	case got := <-output:
		plain := normalizedCodexPanel(got)
		for _, want := range []string{"codex child", "Cully", "Setup ", "73% left", "GPT-6.1-Sol medium"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("pane missing %q", want)
			}
		}
		if !strings.Contains(got, "\x1b[?1049l") {
			t.Fatal("pane did not restore the terminal")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pane output did not close")
	}
}

func TestWarpPaneDefersMouseTrackingUntilAdvisorOpens(t *testing.T) {
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf 'warp child ready\n'
sleep 2
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var captured strings.Builder
	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return captured.String()
	}
	ready := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		notified := false
		for {
			n, readErr := master.Read(buf)
			mu.Lock()
			captured.Write(buf[:n])
			got := captured.String()
			mu.Unlock()
			if !notified && strings.Contains(got, "warp child ready") {
				close(ready)
				notified = true
			}
			if strings.Contains(got, "\x1b[?1049l") || readErr != nil {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- RunPane("codex", nil, slave, slave) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("warp child did not start")
	}
	startup := snapshot()
	if strings.Contains(startup, "\x1b[?1000h") || strings.Contains(startup, "\x1b[?1006h") {
		t.Fatal("Warp must not enable mouse tracking at startup; that steals scroll mode")
	}
	if !strings.Contains(startup, "\x1b[?1049h") {
		t.Fatal("Warp pane still uses the alternate screen")
	}
	_, _ = master.Write([]byte{0x1d}) // Ctrl+] opens advisor
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := snapshot()
		if strings.Contains(got, "\x1b[?1000h") && strings.Contains(got, "\x1b[?1006h") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := snapshot()
	if !strings.Contains(got, "\x1b[?1000h") || !strings.Contains(got, "\x1b[?1006h") {
		t.Fatal("opening the advisor in Warp must enable mouse tracking")
	}
	_, _ = master.Write([]byte{0x1b}) // Esc closes advisor
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(snapshot(), "\x1b[?1000l") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(snapshot(), "\x1b[?1000l") {
		t.Fatal("closing the advisor in Warp must release mouse tracking for scroll mode")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("warp pane did not exit")
	}
}

func TestCodexPaneAdvisorControlsDoNotReachChild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	received := filepath.Join(dir, "received")
	t.Setenv("CULLY_TEST_INPUT_FILE", received)
	script := `#!/bin/sh
stty raw -echo
printf 'scroll input ready\n'
dd bs=1 count=2 of="$CULLY_TEST_INPUT_FILE" 2>/dev/null
`
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
	ready := make(chan struct{})
	go func() {
		var captured strings.Builder
		buf := make([]byte, 4096)
		notified := false
		for {
			n, err := master.Read(buf)
			captured.Write(buf[:n])
			if !notified && strings.Contains(captured.String(), "scroll input ready") {
				close(ready)
				notified = true
			}
			if err != nil || strings.Contains(captured.String(), "\x1b[?1049l") {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- RunPane("codex", nil, slave, slave) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	// Click the compact panel, preview, inspect instruments, navigate, return.
	// None of these controls should reach the coding agent's prompt.
	_, _ = master.Write([]byte("\x1b[<0;20;40M\r\t\t\x1b[A\x1b[6~\x1b[F\x1b[17~ok"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("controls swallowed ordinary input")
	}
	data, err := os.ReadFile(received)
	if err != nil || string(data) != "ok" {
		t.Fatalf("advisor controls leaked: %q (%v)", data, err)
	}
}

func TestCodexPaneAcceptHandoffPreservesDraftAndChildSize(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	received := filepath.Join(dir, "received")
	line := statsAdvice(toolStats{Tools: 1, Edits: 1, EditsSinceCheck: 1})[0]
	expected := "draft\x1b[200~\n" + advisorHandoff(line).Handoff + "\x1b[201~"
	t.Setenv("CULLY_TEST_RECEIVED", received)
	t.Setenv("CULLY_TEST_BYTES", fmt.Sprint(len(expected)))
	script := `#!/bin/sh
stty raw -echo
initial_size=$(stty size)
printf 'E.\n' >> "$CLAUDE_CONFIG_DIR/cully-logs/$CULLY_PANE_SESSION.codex-events"
printf 'handoff ready\n'
dd bs=1 count="$CULLY_TEST_BYTES" of="$CULLY_TEST_RECEIVED" 2>/dev/null
if [ "$(stty size)" != "$initial_size" ]; then exit 2; fi
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 149})
	ready := make(chan struct{})
	go func() {
		var captured strings.Builder
		buf := make([]byte, 4096)
		notified := false
		for {
			n, err := master.Read(buf)
			captured.Write(buf[:n])
			if !notified && strings.Contains(captured.String(), "Files changed.") {
				close(ready)
				notified = true
			}
			if err != nil || strings.Contains(captured.String(), "\x1b[?1049l") {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- RunPane("codex", nil, slave, slave) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	// The first Enter previews; the second accepts the clearly labeled input
	// handoff. It does not submit it or erase the user's existing draft.
	master.Write([]byte("draft\x1d\r\r"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		got, _ := os.ReadFile(received)
		t.Fatalf("handoff was not delivered, received %q expected %q", got, expected)
	}
	got, err := os.ReadFile(received)
	if err != nil || string(got) != expected {
		t.Fatalf("handoff/draft mismatch: %q (%v)", got, err)
	}
}

func TestCodexPaneExitsWhenDescendantHoldsPTY(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "codex")
	// The background process retains stdout after the main process exits.
	// Waiting for terminal EOF would keep the wrapper alive for 30 seconds.
	script := "#!/bin/sh\nsleep 30 &\nprintf 'Codex finished\\n'\nexit 0\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
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
	go func() { done <- RunPane("codex", nil, slave, slave) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrapper stayed alive after Codex exited")
	}
	select {
	case got := <-output:
		if !strings.Contains(got, "\x1b[?1049l") {
			t.Fatal("wrapper did not restore the terminal")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal restoration was not rendered")
	}
}
