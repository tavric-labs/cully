//go:build !windows

package cully

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/google/uuid"
	"golang.org/x/term"
)

// RunPane owns the physical terminal and gives a coding agent a smaller
// virtual terminal. The agent never writes directly to the parent screen, so its
// clear and cursor sequences cannot erase the status area. Every agent uses this
// same terminal; only its launch command and native footer differ.
func RunPane(agentName string, args []string, input, output *os.File) error {
	agent, err := lookupPaneAgent(agentName)
	if err != nil {
		return err
	}
	return runPane(agent, args, input, output, nil)
}

func runPane(agent paneAgent, args []string, input, output *os.File, exercise *terminalExercise) error {
	profile := detectTerminalProfile()
	if err := profile.requireNative(); err != nil {
		return err
	}
	if !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return fmt.Errorf("cully %s requires an interactive terminal", agent.Name)
	}
	cols, rows, err := term.GetSize(int(output.Fd()))
	if err != nil {
		return err
	}
	if cols < 20 || rows < 8 {
		return fmt.Errorf("terminal is too small for cully %s", agent.Name)
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(input.Fd()), state) //nolint:errcheck
	// Conversation history belongs to the emulator while Cully owns the screen.
	startup := "\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h\x1b[?1002h\x1b[?1003h"
	if _, err := io.WriteString(output, startup); err != nil {
		return err
	}
	defer io.WriteString(output, "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[0m\x1b[?25h\x1b[?1049l") //nolint:errcheck
	reviewContext, cancelReviews := context.WithCancel(context.Background())
	defer cancelReviews()

	session := uuid.NewString()
	if err := registerPane(session, currentDir()); err != nil {
		return fmt.Errorf("register %s advisor pane: %w", agent.Name, err)
	}
	defer os.Remove(paneRegistrationFile(session))      //nolint:errcheck
	defer os.Remove(paneBindingFile(session))           //nolint:errcheck
	defer os.Remove(signalFile(session))                //nolint:errcheck
	defer os.Remove(sessionReportFile(session))         //nolint:errcheck
	defer os.Remove(sessionSnapshotFile(session))       //nolint:errcheck
	defer os.Remove(sessionSignalsFile(session))        //nolint:errcheck
	defer os.Remove(sessionSeenFile(session))           //nolint:errcheck
	defer os.Remove(discoveryFile(session, agent.Name)) //nolint:errcheck
	defer clearCodexCullyStats(session)
	if id := agent.resumeID(args); id != "" {
		bindPane(currentDir(), id)
	}
	if _, err := exec.LookPath(agent.Binary); err != nil {
		return fmt.Errorf("%s is not installed or not on PATH", agent.Binary)
	}
	appendJournal(currentDir(), session, journalEvent{Agent: agent.Name, Class: journalStart})
	defer appendJournal(currentDir(), session, journalEvent{Agent: agent.Name, Class: journalEnd})
	defer pruneJournals()
	cmd := exec.Command(agent.Binary, agent.args(args)...)
	cmd.Env = append(os.Environ(), "CULLY_PANE_SESSION="+session, "CULLY_SESSION="+session, "CULLY_AGENT="+agent.Name)
	view := sessionView{HealthBar: true, Agent: agent.Name, Project: currentDir(), Branch: gitBranch(currentDir()), Started: time.Now(), Daemon: isDaemonRunning(), Terminal: profile}
	restoredSession := false
	lastSessionSave := time.Time{}
	refreshSession := func() {
		if !restoredSession {
			restoredSession = restoreCodexSessionView(session, &view)
		}
	}
	persistSession := func() {
		saveCodexSessionView(session, view)
		lastSessionSave = time.Now()
	}
	refreshSession()
	readGitChanges(&view)
	var prober mcpProber
	go runMCPProbe(reviewContext, agent.Name, &prober)
	loadStats := func() toolStats {
		s := readToolStats(session)
		s.Impact = ImpactSince(view.Started)
		if res, ok := prober.get(); ok {
			overlayMCPProbe(&s.Cully, res)
		}
		return s
	}
	stats := loadStats()
	view.Loops, _ = sessionLoops(currentDir(), session)
	advice := combinedAdvice(session, stats, view)
	var divider paneDivider
	panelBudget := func() int {
		if divider.Manual {
			return max(0, rows-divider.top(rows, 0))
		}
		return rows - paneTop(rows, rows)
	}
	statusRows := func() []string { return compactSessionStatusRows(cols, panelBudget(), advice, stats, view) }
	content := statusRows()
	top := divider.top(rows, len(content))
	child, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(top), Cols: uint16(cols)})
	if err != nil {
		return fmt.Errorf("start %s: %w", agent.Name, err)
	}
	// Wait for the process itself, not PTY EOF: a descendant can retain the
	// terminal after Codex exits. The wrapper must still release its session.
	waitDone := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(waitDone)
	}()
	readerDone := make(chan struct{})
	emulator := vt.NewEmulator(cols, top)
	readFooter := func() {
		if agent.NativeFooter {
			rows, base := footerRows(emulator)
			upd, _ := parseFooterRows(rows, base)
			view.mergeUpdate(upd)
		}
	}
	repliesDone := make(chan struct{})
	defer func() {
		close(readerDone)
		// StartWithSize creates a child process group. Never leave that group
		// running when the owning wrapper exits or loses its terminal.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_ = child.Close()
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-waitDone
		}
		// Descendants may ignore TERM even after the main process has exited.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if pipe, ok := emulator.InputPipe().(io.Closer); ok {
			_ = pipe.Close()
		}
		<-repliesDone
		_ = emulator.Close()
	}()
	inputChunks := make(chan []byte, 16)
	go func() {
		defer close(inputChunks)
		buf := make([]byte, 4096)
		for {
			n, err := input.Read(buf)
			if n > 0 {
				select {
				case inputChunks <- bytes.Clone(buf[:n]):
				case <-readerDone:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		_, _ = io.Copy(child, emulator) // terminal query replies
		close(repliesDone)
	}()

	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := child.Read(buf)
			if n > 0 {
				select {
				case chunks <- bytes.Clone(buf[:n]):
				case <-readerDone:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	resize := make(chan os.Signal, 1)
	stop := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(resize)
	defer signal.Stop(stop)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	dirty, lastAdvice := true, ""
	lastContent := ""
	lastBeat := time.Now()
	var keyFeed panelInput
	lastInput := time.Now()
	var drawer advisorDrawer
	var scroll paneScroll
	resizeMode := false
	resizeChild := func() {
		nextTop := divider.top(rows, len(content))
		if nextTop != top || cols != emulator.Width() {
			top = nextTop
			emulator.Resize(cols, top)
			_ = pty.Setsize(child, &pty.Winsize{Rows: uint16(top), Cols: uint16(cols)})
		}
	}
	// OSC 22 asks the terminal for a vertical-resize pointer over the divider;
	// terminals without support ignore it.
	pointerOver := false
	setPointer := func(over bool) {
		if over == pointerOver {
			return
		}
		pointerOver = over
		shape := "default"
		if over {
			shape = "ns-resize"
		}
		_, _ = io.WriteString(output, "\x1b]22;"+shape+"\x1b\\")
	}
	defer func() { setPointer(false) }()
	paint := func() string {
		if exercise != nil {
			exercise.frame(drawer.Open, top < rows && len(content) > 0)
		}
		if drawer.Open {
			return drawer.render(cols, rows, stats, view)
		}
		scroll.anchor(emulator.ScrollbackLen())
		if emulator.IsAltScreen() {
			scroll.reset() // an agent on the alternate screen scrolls itself
		}
		hud := paneHUD(cols, stats, view, time.Now())
		hint := " drag ↕ · Ctrl+] advisor "
		if resizeMode {
			hint = " drag ↕ · Esc ends resize "
		}
		if scroll.active() {
			hint = fmt.Sprintf(" ↑ %d lines back · type or scroll down to return ", scroll.Offset)
		}
		frame := renderTerminalPaneView(emulator, scroll.Offset, cols, rows, content, agent.NativeFooter, hud, divider.grip(), hint)
		return frame
	}
	// Enter pastes a request into the agent's input instead of submitting it, so
	// the user keeps their draft, reads the request and decides whether to send.
	insert := func() {
		if drawer.Details || len(drawer.Items) == 0 {
			return
		}
		line := drawer.Items[drawer.Selected]
		if strings.HasPrefix(line, "MEMO|") {
			drawer.Message = dim + "Informational tip · nothing to add to the prompt" + rst
			dirty = true
			return
		}
		request := terminalSafeText(advisorRequest(line, readSessionSignals(session)))
		emulator.SendText("\x1b[200~\n" + request + "\x1b[201~")
		drawer.close()
		dirty = true
	}
	handleKeys := func(keys []panelKey) {
		for _, key := range keys {
			if key.Action == "resize" {
				if drawer.Open {
					drawer.close()
				}
				resizeMode = !resizeMode
				divider.Dragging = false
				divider.Hover = false
				setPointer(false)
				dirty = true
				continue
			}
			if (resizeMode || divider.Dragging) && key.Action == "escape" && !drawer.Open {
				resizeMode = false
				divider.Dragging = false
				divider.Hover = false
				setPointer(false)
				dirty = true
				continue
			}
			if key.Action == "focus" {
				divider.Dragging = false
				divider.Hover = false
				setPointer(false)
				if drawer.Open {
					drawer.close()
				} else {
					drawer.open(advice)
				}
				dirty = true
				continue
			}
			if key.Mouse != nil {
				mouse := key.Mouse
				// The panel belongs to the same conversation view. A wheel over
				// it must read history too, rather than silently doing nothing.
				if up, wheel := mouse.verticalWheel(); !drawer.Open && wheel {
					if emulator.IsAltScreen() {
						// An alternate-screen agent owns its history. Wheel events over
						// the compact panel must reach that agent inside its viewport.
						forwarded := *mouse
						forwarded.X = min(max(forwarded.X, 1), emulator.Width())
						forwarded.Y = min(max(forwarded.Y, 1), top)
						emulator.SendMouse(forwarded.event())
					} else if scroll.wheel(up, emulator.ScrollbackLen()) {
						if exercise != nil {
							if up {
								exercise.WheelUp = true
							} else {
								exercise.WheelDown = true
							}
						}
						dirty = true
					}
					continue
				}
				if !drawer.Open {
					if motion, changed := divider.hover(mouse, rows, top); motion {
						setPointer(divider.Hover || divider.Dragging)
						dirty = dirty || changed
						continue
					}
				}
				if !drawer.Open && divider.handle(mouse, rows, top) {
					content = statusRows()
					beforeTop := top
					resizeChild()
					if exercise != nil {
						if top < beforeTop {
							exercise.DividerUp = true
						}
						if top > beforeTop {
							exercise.DividerDown = true
						}
					}
					divider.Hover = mouse.Y == top+1
					setPointer(divider.Dragging || divider.Hover)
					dirty = true
					continue
				}
				if !drawer.Open && mouse.Y <= top {
					emulator.SendMouse(mouse.event())
					continue
				}
				if !mouse.Release {
					if !drawer.Open && mouse.Button == 0 && mouse.Y > top {
						drawer.open(advice)
						divider.Hover = false
						setPointer(false)
					} else if drawer.Open {
						switch mouse.Button &^ (4 | 8 | 16) {
						case 64:
							drawer.move(-1)
						case 65:
							drawer.move(1)
						case 0:
							if index, ok := drawer.HitRows[mouse.Y]; ok {
								drawer.Selected = index
								drawer.Message = ""
							}
							if mouse.Y == drawer.AcceptRow {
								insert()
							}
						}
					}
				}
				dirty = true
				continue
			}
			if drawer.Open {
				switch key.Action {
				case "escape":
					drawer.escape()
				case "up":
					drawer.move(-1)
				case "down":
					drawer.move(1)
				case "pageup":
					drawer.move(-max(1, rows/4))
				case "pagedown":
					drawer.move(max(1, rows/4))
				case "home":
					drawer.move(-10000)
				case "end":
					drawer.move(10000)
				case "enter":
					insert()
				case "details":
					drawer.Details = !drawer.Details
					drawer.Scroll = 0
				default:
					if bytes.Equal(key.Data, []byte{3}) {
						drawer.close()
					} // return control without killing Codex
				}
				dirty = true
				continue
			}
			if exercise != nil {
				exercise.confirmInput(key)
			}
			// Normal-screen agents leave history in the emulator. Page keys
			// keep it reachable even if the host withholds mouse reports.
			// Fullscreen agents keep their own page-key bindings and history.
			if !emulator.IsAltScreen() && emulator.ScrollbackLen() > 0 && (key.Action == "pageup" || key.Action == "pagedown") {
				lines := max(1, top-1)
				if key.Action == "pagedown" {
					lines = -lines
				}
				if scroll.move(lines, emulator.ScrollbackLen()) {
					dirty = true
				}
				continue
			}
			if scroll.reset() {
				dirty = true // typing returns to the live screen
			}
			// Mouse events and terminal replies use the emulator's input pipe.
			// Keep keys in that same stream so they cannot overtake a wheel.
			emulator.SendText(string(key.Data))
		}
	}
	var shutdown <-chan time.Time
	paintFinal := func() {
		refreshSession()
		readFooter()
		persistSession()
		stats = loadStats()
		view.Loops, _ = sessionLoops(currentDir(), session)
		advice = combinedAdvice(session, stats, view)
		content = statusRows()
		_, _ = io.WriteString(output, paint())
	}
	for {
		select {
		case chunk, ok := <-inputChunks:
			if !ok {
				inputChunks = nil
				handleKeys(keyFeed.feed(nil, true))
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				if shutdown == nil {
					shutdown = time.After(2 * time.Second)
				}
				continue
			}
			lastInput = time.Now()
			handleKeys(keyFeed.feed(chunk, false))
		case <-waitDone:
			// Preserve any output already delivered before the child exited.
			for {
				select {
				case chunk, ok := <-chunks:
					if !ok {
						paintFinal()
						return waitErr
					}
					_, _ = emulator.Write(chunk)
				default:
					paintFinal()
					return waitErr
				}
			}
		case chunk, ok := <-chunks:
			if !ok {
				if dirty {
					readFooter()
					stats = loadStats()
					view.Loops, _ = sessionLoops(currentDir(), session)
					advice = combinedAdvice(session, stats, view)
					content = statusRows()
					_, _ = io.WriteString(output, paint())
				}
				chunks = nil
				continue
			}
			_, _ = emulator.Write(chunk)
			dirty = true
		case <-resize:
			if width, height, sizeErr := term.GetSize(int(output.Fd())); sizeErr == nil && width > 0 && height > 0 {
				if exercise != nil {
					exercise.Resize = true
				}
				cols, rows = width, height
				content = statusRows()
				divider.Dragging = false
				resizeChild()
				dirty = true
			}
		case sig := <-stop:
			_ = syscall.Kill(-cmd.Process.Pid, sig.(syscall.Signal))
			if sig != syscall.SIGINT && shutdown == nil {
				shutdown = time.After(2 * time.Second)
			}
		case <-shutdown:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			shutdown = nil
		case <-ticker.C:
			refreshSession()
			if time.Since(lastInput) >= 80*time.Millisecond {
				handleKeys(keyFeed.feed(nil, true))
			}
			if time.Since(lastBeat) >= 2*time.Second {
				_ = os.Chtimes(paneRegistrationFile(session), time.Now(), time.Now())
				view.Daemon = isDaemonRunning()
				view.Branch = gitBranch(view.Project)
				readGitChanges(&view)
				lastBeat = time.Now()
			}
			stats = loadStats()
			readFooter()
			if agent.Name == "claude" {
				readClaudeContext(session, &view)
			}
			if time.Since(lastSessionSave) >= time.Second {
				persistSession()
			}
			loops, journal := sessionLoops(currentDir(), session)
			view.Loops, view.JournalCoverage = loops, journalCoverage(journal)
			advice = combinedAdvice(session, stats, view)
			joined := strings.Join(advice, "\n")
			if joined != lastAdvice {
				lastAdvice = joined
				dirty = true
			}
			content = statusRows()
			if joinedContent := strings.Join(content, "\n"); joinedContent != lastContent {
				lastContent, dirty = joinedContent, true
			}
			// Expand for new content, but do not keep reflowing Codex when
			// temporary advice clears. A physical resize recalculates both areas.
			if nextTop := divider.top(rows, len(content)); !divider.Manual && nextTop < top {
				top = nextTop
				emulator.Resize(cols, top)
				_ = pty.Setsize(child, &pty.Winsize{Rows: uint16(top), Cols: uint16(cols)})
				dirty = true
			}
			if dirty {
				if _, err := io.WriteString(output, paint()); err != nil {
					return err
				}
				dirty = false
			}
		}
	}
}

func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func renderTerminalPane(emulator *vt.Emulator, cols, rows int, content []string, nativeFooter bool, hud string, hints ...string) string {
	return renderTerminalPaneGrip(emulator, cols, rows, content, nativeFooter, hud, gripIdle, hints...)
}

func renderTerminalPaneGrip(emulator *vt.Emulator, cols, rows int, content []string, nativeFooter bool, hud string, grip gripState, hints ...string) string {
	return renderTerminalPaneView(emulator, 0, cols, rows, content, nativeFooter, hud, grip, hints...)
}

// renderTerminalPaneView draws the agent's screen, or, when offset is above 0,
// the same window that many lines further back in the emulator's scrollback.
func renderTerminalPaneView(emulator *vt.Emulator, offset, cols, rows int, content []string, nativeFooter bool, hud string, grip gripState, hints ...string) string {
	var out strings.Builder
	limit := max(0, min(cols-1, emulator.Width())) // avoid automatic terminal wrap
	top := min(emulator.Height(), rows)
	scrollbackLen := emulator.ScrollbackLen()
	offset = min(max(offset, 0), scrollbackLen)
	cellAt := func(x, y int) *uv.Cell {
		if offset == 0 {
			return emulator.CellAt(x, y)
		}
		index := scrollbackLen - offset + y
		switch {
		case index < 0:
			return nil
		case index < scrollbackLen:
			return emulator.ScrollbackCellAt(x, index)
		}
		return emulator.CellAt(x, index-scrollbackLen)
	}
	footerRow := -1
	if nativeFooter && offset == 0 {
		rows, base := footerRows(emulator)
		_, match := parseFooterRows(rows, base)
		footerRow = match.Row
	}
	out.WriteString("\x1b[?25l")
	for y := 0; y < top; y++ {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[0m\x1b[2K", y+1)
		if y == footerRow && top < rows {
			continue // the native instruments now live in the Cully status view
		}
		line := uv.NewLine(limit)
		for x := 0; x < limit; x++ {
			if cell := cellAt(x, y); cell != nil {
				// Set already fills the continuation columns of a wide glyph.
				// Copying their placeholder cells would erase that glyph again.
				width := max(1, cell.Width)
				if x+width > limit {
					break
				}
				line.Set(x, cell)
				x += width - 1
			}
		}
		out.WriteString(line.Render())
	}
	if top < rows {
		panel := panelLinesGrip(cols, rows-top, content, hud, grip, hints...)
		for i, line := range panel {
			fmt.Fprintf(&out, "\x1b[%d;1H\x1b[0m\x1b[2K%s\x1b[0m", top+i+1, line)
		}
	}
	cursor := emulator.CursorPosition()
	show := "\x1b[?25h"
	if offset > 0 {
		show = "" // the cursor belongs to the live screen
	}
	fmt.Fprintf(&out, "\x1b[%d;%dH%s", min(max(cursor.Y+1, 1), top), min(max(cursor.X+1, 1), cols), show)
	return out.String()
}
