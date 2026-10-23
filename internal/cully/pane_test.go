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

func TestPaneWheelReachesHistoryBeforeAndAfterAdvisor(t *testing.T) {
	for _, program := range []string{"Apple_Terminal", "gnome-terminal", "ptyxis", ""} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%t", program, legacy), func(t *testing.T) {
				testPaneWheelReachesHistoryBeforeAndAfterAdvisor(t, program, legacy)
			})
		}
	}
}

func testPaneWheelReachesHistoryBeforeAndAfterAdvisor(t *testing.T, program string, legacy bool) {
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	t.Setenv("TERM_PROGRAM", program)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
stty raw -echo
i=1
while [ "$i" -le 80 ]; do printf 'history line %02d\r\n' "$i"; i=$((i+1)); done
while :; do
  c=$(dd bs=1 count=1 2>/dev/null)
  case "$c" in
    n) printf 'new tail\r\n';;
    a)
      set -- $(stty size)
      printf '\033[?1049h\033[?1000h\033[?1006hALT rows=%s' "$1"
      expected=$(printf '\033[<80;5;1M\033[<81;%s;%sM\033[5~\033[6~' "$2" "$1")
      dd bs=1 count="${#expected}" of="$CLAUDE_CONFIG_DIR/alt-wheel" 2>/dev/null
      printf '\r\nALT wheel received'
      ;;
    q) exit 0;;
  esac
done
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	host := vt.NewEmulator(120, 40)
	var mu sync.Mutex
	var captured strings.Builder
	snapshot := func() (string, string) { mu.Lock(); defer mu.Unlock(); return captured.String(), host.String() }
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			captured.Write(buf[:n])
			_, _ = host.Write(buf[:n])
			restored := strings.Contains(captured.String(), "\x1b[?1049l")
			mu.Unlock()
			if err != nil || restored {
				return
			}
		}
	}()
	defer func() {
		_, _ = master.Write([]byte("q"))
		_ = slave.Close()
		_ = master.Close()
		select {
		case <-readDone:
			_ = host.Close()
		case <-time.After(time.Second):
		}
	}()
	done := make(chan error, 1)
	exercise := &terminalExercise{}
	go func() {
		agent, _ := lookupPaneAgent("codex")
		done <- runPane(agent, nil, slave, slave, exercise)
		close(done)
	}()
	defer func() {
		_ = master.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("pane did not stop during cleanup")
		}
	}()
	wait := func(check func(string, string) bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			raw, screen := snapshot()
			if check(raw, screen) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, screen := snapshot()
		t.Fatalf("terminal state did not arrive: %s", screen)
	}
	wait(func(_, screen string) bool { return strings.Contains(screen, "history line 80") })
	raw, _ := snapshot()
	if !strings.Contains(raw, "\x1b[?1000h") || !strings.Contains(raw, "\x1b[?1006h") || !strings.Contains(raw, "\x1b[?1003h") {
		t.Fatal("mouse, wheel and hover reporting must be enabled")
	}
	encodeWheel := func(button, x, y int) string {
		if legacy {
			return string([]byte{'\x1b', '[', 'M', byte(button + 32), byte(x + 32), byte(y + 32)})
		}
		return fmt.Sprintf("\x1b[<%d;%d;%dM", button, x, y)
	}
	wheel := func(up bool, y int) {
		t.Helper()
		button := 81 // Ctrl modifier must not hide the wheel from Cully.
		if up {
			button = 80
		}
		_, err := master.Write([]byte(strings.Repeat(encodeWheel(button, 5, y), 40)))
		if err != nil {
			t.Fatal(err)
		}
	}
	wheel(true, 1)
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "history line 01") && strings.Contains(screen, "lines back") && !strings.Contains(screen, "history line 80")
	})
	_, _ = master.Write([]byte("n"))
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "new tail") && !strings.Contains(screen, "lines back")
	})
	wheel(true, 40) // the compact panel must not swallow conversation scrolling
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "history line 01") && strings.Contains(screen, "lines back")
	})
	wheel(false, 40)
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "new tail") && !strings.Contains(screen, "lines back")
	})
	_, _ = master.Write([]byte("\x1b[5~"))
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "lines back") && !strings.Contains(screen, "new tail")
	})
	_, _ = master.Write([]byte("\x1b[6~"))
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "new tail") && !strings.Contains(screen, "lines back")
	})
	_, _ = master.Write([]byte{0x1d})
	wait(func(raw, screen string) bool {
		return strings.Contains(raw, "\x1b[?1003h") && strings.Contains(screen, "Cully Advisor")
	})
	_, _ = master.Write([]byte{0x1b})
	wait(func(raw, screen string) bool {
		return strings.Contains(screen, "new tail") && !strings.Contains(screen, "Cully Advisor")
	})
	raw, _ = snapshot()
	if strings.Contains(raw, "\x1b[?1000l") || strings.Contains(raw, "\x1b[?1006l") {
		t.Fatal("closing advisor disabled wheel reports")
	}
	wheel(true, 1)
	wait(func(_, screen string) bool {
		return strings.Contains(screen, "history line 01") && strings.Contains(screen, "lines back")
	})
	// Alternate-screen agents scroll themselves, including when the pointer is
	// over Cully's compact panel rather than over the agent viewport.
	_, _ = master.Write([]byte("a"))
	wait(func(_, screen string) bool { return strings.Contains(screen, "ALT rows=") })
	_, screen := snapshot()
	var altRows int
	if _, err := fmt.Sscanf(screen[strings.Index(screen, "ALT rows="):], "ALT rows=%d", &altRows); err != nil {
		t.Fatal(err)
	}
	_, _ = master.Write([]byte(encodeWheel(80, 5, 1) + encodeWheel(81, 200, 40) + "\x1b[5~\x1b[6~"))
	wait(func(_, screen string) bool { return strings.Contains(screen, "ALT wheel received") })
	received, err := os.ReadFile(filepath.Join(dir, "alt-wheel"))
	agentCols := 120
	if want := fmt.Sprintf("\x1b[<80;5;1M\x1b[<81;%d;%dM\x1b[5~\x1b[6~", agentCols, altRows); err != nil || string(received) != want {
		t.Fatalf("alternate-screen wheel reports=%q, want %q (%v)", received, want, err)
	}
	_, _ = master.Write([]byte("q"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pane did not exit")
	}
	wait(func(raw, _ string) bool {
		return strings.Contains(raw, "\x1b[?1000l") && strings.Contains(raw, "\x1b[?1006l") && strings.Contains(raw, "\x1b[?1049l")
	})
	if !exercise.WheelUp || !exercise.WheelDown || !exercise.AdvisorOpen || !exercise.AdvisorClose || !exercise.Status {
		t.Fatalf("live check missed observed features: %+v", exercise)
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

func TestCodexPaneEnterAddsRequestToPromptAndPreservesDraft(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CULLY_ANALYZE_DISABLE", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	received := filepath.Join(dir, "received")
	line := statsAdvice(toolStats{Tools: 1, Edits: 1, EditsSinceCheck: 1})[0]
	expected := "draft\x1b[200~\n" + advisorRequest(line, "") + "\x1b[201~"
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
	// One Enter adds the request to the input. It neither submits it nor
	// erases the user's existing draft.
	master.Write([]byte("draft\x1d\r"))
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
