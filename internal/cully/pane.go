package cully

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/term"
)

// paneOptions selects host features. The sidebar is a Cully control, so it
// is opt-in for `cully run --sidebar` and off for the single-session
// terminal check.
type paneOptions struct {
	Sidebar bool
}

// sidebarEnabled is explicit opt-in: the --sidebar flag or CULLY_SIDEBAR=1.
// The list costs 24 columns, so it stays off unless requested.
func sidebarEnabled(flag bool) bool {
	if flag {
		return true
	}
	enabled, ok := os.LookupEnv("CULLY_SIDEBAR")
	return ok && enabled == "1"
}

// RunPane owns the physical terminal and gives each coding agent a smaller
// virtual terminal. The agent never writes directly to the parent screen, so its
// clear and cursor sequences cannot erase the status area. Every agent uses this
// same terminal; only its launch command and native footer differ. Several
// named sessions can run in one window; they end with it.
func RunPane(agentName string, args []string, input, output *os.File) error {
	return RunPaneOptions(agentName, args, input, output, false)
}

// RunPaneOptions is RunPane with an explicit sidebar choice. Callers that
// accept a --sidebar flag pass it through; otherwise the CULLY_SIDEBAR=1
// environment escape hatch still opts in.
func RunPaneOptions(agentName string, args []string, input, output *os.File, sidebar bool) error {
	agent, err := lookupPaneAgent(agentName)
	if err != nil {
		return err
	}
	return runPaneOptions(agent, args, input, output, nil, paneOptions{Sidebar: sidebarEnabled(sidebar)})
}

func runPane(agent paneAgent, args []string, input, output *os.File, exercise *terminalExercise) error {
	return runPaneOptions(agent, args, input, output, exercise, paneOptions{})
}

func runPaneOptions(agent paneAgent, args []string, input, output *os.File, exercise *terminalExercise, opts paneOptions) error {
	shortcuts := os.Getenv("CULLY_SHORTCUTS") == "1"
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
	hostContext, cancelHost := context.WithCancel(context.Background())
	defer cancelHost()
	hostDone := make(chan struct{})
	defer close(hostDone)
	events := make(chan paneEvent, 64)

	var (
		sessions []*paneSession
		active   int
		sidebar  = sidebarState{Visible: opts.Sidebar}
		divider  paneDivider
		drawer   advisorDrawer
		notice   string
		noticeAt time.Time
	)
	defer func() {
		for _, s := range sessions {
			s.release()
		}
	}()
	sideWidth := func() int {
		if sidebar.Visible && cols >= sidebarMinCols {
			return sidebarWidth
		}
		return 0
	}
	agentCols := func() int { return cols - sideWidth() }
	cur := func() *paneSession { return sessions[active] }
	panelBudget := func() int {
		if divider.Manual {
			return max(0, rows-divider.top(rows, 0))
		}
		return rows - paneTop(rows, rows)
	}
	statusRows := func(s *paneSession) []string {
		return compactSessionStatusRows(cols, panelBudget(), s.advice, s.stats, s.view)
	}
	top := 0
	resizeAll := func() {
		top = divider.top(rows, len(cur().content))
		for _, s := range sessions {
			s.resize(agentCols(), top)
		}
	}
	launch := func(base paneAgent, launchArgs []string, name string) (*paneSession, error) {
		s, err := newPaneSession(hostContext, base, launchArgs, name, profile)
		if err != nil {
			return nil, err
		}
		s.content = statusRows(s)
		return s, nil
	}
	nextName := func(base string) string {
		for n := 1; ; n++ {
			name := base
			if n > 1 {
				name = fmt.Sprintf("%s %d", base, n)
			}
			taken := false
			for _, s := range sessions {
				taken = taken || s.name == name
			}
			if !taken {
				return name
			}
		}
	}

	first, err := launch(agent, args, nextName(agent.Name))
	if err != nil {
		return err
	}
	sessions = append(sessions, first)
	top = divider.top(rows, len(first.content))
	if err := first.start(agentCols(), top, events, hostDone, output); err != nil {
		first.release()
		sessions = nil
		return err
	}

	inputChunks := make(chan []byte, 16)
	go func() {
		defer close(inputChunks)
		buf := make([]byte, 4096)
		for {
			n, err := input.Read(buf)
			if n > 0 {
				select {
				case inputChunks <- bytes.Clone(buf[:n]):
				case <-hostDone:
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
	dirty := true
	lastBeat := time.Now()
	var keyFeed panelInput
	lastInput := time.Now()
	resizeMode := false
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
	say := func(text string) {
		notice, noticeAt = text, time.Now()
		dirty = true
	}
	sessionNames := func() []string {
		names := make([]string, len(sessions))
		for i, s := range sessions {
			names[i] = fmt.Sprintf("%d %s", i+1, s.name)
		}
		return names
	}
	paint := func() string {
		s := cur()
		if exercise != nil {
			exercise.frame(drawer.Open, top < rows && len(s.content) > 0)
		}
		if drawer.Open {
			return drawer.render(cols, rows, s.stats, s.view)
		}
		s.scroll.anchor(s.emulator.ScrollbackLen())
		if s.emulator.IsAltScreen() {
			s.scroll.reset() // an agent on the alternate screen scrolls itself
		}
		hud := paneHUD(cols, s.stats, s.view, time.Now())
		hint := " drag ↕ · click advisor "
		if shortcuts {
			hint = " drag ↕ · Ctrl+] advisor "
			if opts.Sidebar {
				hint += "· F7 sessions "
			}
		}
		if resizeMode {
			hint = " drag ↕ · Esc ends resize "
		}
		if sidebar.Renaming {
			hint = " type a name · Enter saves · Esc cancels "
		} else if sidebar.Focus {
			hint = " sessions: ↑↓ move · Enter switch · n new · r rename · Esc back "
		}
		if s.scroll.active() {
			hint = fmt.Sprintf(" ↑ %d lines back · type or scroll down to return ", s.scroll.Offset)
		}
		if notice != "" && time.Since(noticeAt) < 4*time.Second {
			hint = " " + notice + " "
		}
		var side []string
		if sideWidth() > 0 {
			side = sidebarLines(sessionNames(), active, sidebar, top)
		}
		return renderTerminalPaneSide(s.emulator, s.scroll.Offset, cols, rows, s.content, s.native.Visible && !sidebar.Focus, hud, divider.grip(), side, hint)
	}
	switchTo := func(index int) {
		if index < 0 || index >= len(sessions) || index == active {
			return
		}
		cur().native.deactivate()
		active = index
		sidebar.Selected = index
		cur().native.activate()
		drawer.close()
		cur().reload(statusRows)
		resizeAll()
		dirty = true
	}
	addSession := func() {
		s, err := launch(cur().agent, nil, nextName(cur().agent.Name))
		if err != nil {
			say("new session failed: " + err.Error())
			return
		}
		if err := s.start(agentCols(), top, events, hostDone, output); err != nil {
			s.release()
			say("new session failed: " + err.Error())
			return
		}
		s.native.gate.on = false
		sessions = append(sessions, s)
		switchTo(len(sessions) - 1)
	}
	startRename := func(index int) {
		sidebar.Selected = index
		sidebar.Renaming = true
		sidebar.Focus = true
		sidebar.Input = nil // typing replaces the name; Enter on an empty field keeps it
		dirty = true
	}
	finishRename := func(save bool) {
		if save {
			names := make([]string, len(sessions))
			for i, s := range sessions {
				names[i] = s.name
			}
			if name, ok := renameSessionName(names, sidebar.Selected, sidebar.Input); ok {
				sessions[sidebar.Selected].name = name
			} else if cleanSessionName(sidebar.Input) != "" {
				say("session name already used")
			}
		}
		sidebar.Renaming = false
		sidebar.Focus = false // the keyboard goes back to the agent
		sidebar.Input = nil
		dirty = true
	}
	// Enter pastes a request into the agent's input instead of submitting it, so
	// the user keeps their draft, reads the request and decides whether to send.
	insert := func() {
		if drawer.Details || len(drawer.Items) == 0 {
			return
		}
		s := cur()
		line := drawer.Items[drawer.Selected]
		if strings.HasPrefix(line, "MEMO|") {
			drawer.Message = dim + "Informational tip · nothing to add to the prompt" + rst
			dirty = true
			return
		}
		request := terminalSafeText(advisorRequest(line, readSessionSignals(s.id)))
		s.emulator.SendText("\x1b[200~\n" + request + "\x1b[201~")
		drawer.close()
		dirty = true
	}
	// sidebarKey handles a key while the session list has focus. Nothing reaches
	// the agent: the list owns the keyboard until focus returns.
	sidebarKey := func(key panelKey) {
		dirty = true
		if sidebar.Renaming {
			switch {
			case key.Action == "enter":
				finishRename(true)
			case key.Action == "escape" || bytes.Equal(key.Data, []byte{3}):
				finishRename(false)
			case bytes.Equal(key.Data, []byte{0x7f}) || bytes.Equal(key.Data, []byte{8}):
				if len(sidebar.Input) > 0 {
					sidebar.Input = sidebar.Input[:len(sidebar.Input)-1]
				}
			case key.Action == "":
				for _, r := range string(key.Data) {
					if r >= ' ' && r != 0x7f && r != utf8.RuneError && len(sidebar.Input) < sidebarMaxName {
						sidebar.Input = append(sidebar.Input, r)
					}
				}
			}
			return
		}
		switch {
		case key.Action == "up":
			sidebar.Selected = max(0, sidebar.Selected-1)
		case key.Action == "down" || key.Action == "details":
			sidebar.Selected = min(len(sessions)-1, sidebar.Selected+1)
		case key.Action == "enter":
			sidebar.Focus = false
			switchTo(sidebar.Selected)
		case key.Action == "escape" || bytes.Equal(key.Data, []byte{3}):
			sidebar.Focus = false
		case bytes.Equal(key.Data, []byte("n")):
			sidebar.Focus = false
			addSession()
		case bytes.Equal(key.Data, []byte("r")):
			startRename(sidebar.Selected)
		}
	}
	// sidebarMouse handles a pointer event over the session list.
	sidebarMouse := func(mouse *panelMouse) {
		dirty = true
		if mouse.Release || mouse.Button != 0 {
			return
		}
		if sidebar.Renaming {
			finishRename(true)
		}
		switch kind, index := sidebarHit(mouse.X, mouse.Y, len(sessions), sidebar.Selected, top); kind {
		case sidebarHitNew:
			sidebar.Focus = false
			addSession()
		case sidebarHitRename:
			startRename(active)
		case sidebarHitSession:
			sidebar.Focus = false
			sidebar.Selected = index
			switchTo(index)
		}
	}
	handleKeys := func(keys []panelKey) {
		for _, key := range keys {
			s := cur()
			if !key.Pasted && nativeTerminalReply(key.Data) {
				s.emulator.SendText(string(key.Data))
				continue
			}
			if shortcuts && opts.Sidebar && key.Action == "sessions" && !drawer.Open {
				switch {
				case !sidebar.Visible:
					sidebar.Visible, sidebar.Focus = true, true
					sidebar.Selected = active
				case !sidebar.Focus:
					sidebar.Focus = true
					sidebar.Selected = active
				default:
					if sidebar.Renaming {
						finishRename(false)
					}
					sidebar.Visible, sidebar.Focus = false, false
				}
				resizeAll()
				dirty = true
				continue
			}
			if sidebar.Focus && !drawer.Open && key.Mouse == nil {
				sidebarKey(key)
				continue
			}
			if shortcuts && key.Action == "resize" {
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
			if (shortcuts || drawer.Open) && key.Action == "focus" {
				divider.Dragging = false
				divider.Hover = false
				setPointer(false)
				if drawer.Open {
					drawer.close()
				} else {
					drawer.open(s.advice)
				}
				dirty = true
				continue
			}
			if key.Mouse != nil {
				mouse := key.Mouse
				side := sideWidth()
				overSidebar := side > 0 && !drawer.Open && mouse.X <= side && mouse.Y <= top && !divider.Dragging
				if overSidebar {
					if motion, changed := divider.hover(mouse, rows, top); motion {
						setPointer(divider.Hover || divider.Dragging)
						dirty = dirty || changed
						continue
					}
					sidebarMouse(mouse)
					continue
				}
				if side > 0 && !drawer.Open && mouse.Y <= top && sidebar.Focus && !mouse.Release && mouse.Button == 0 {
					if sidebar.Renaming {
						finishRename(true)
					}
					sidebar.Focus = false // a click in the agent returns the keyboard to it
				}
				// Agent coordinates start after the sidebar.
				agentMouse := *mouse
				if mouse.Y <= top && !drawer.Open {
					agentMouse.X = mouse.X - side
				}
				// The panel belongs to the same conversation view. A wheel over
				// it must read history too, rather than silently doing nothing.
				if up, wheel := agentMouse.verticalWheel(); !drawer.Open && wheel {
					if s.emulator.IsAltScreen() && !s.native.mouseRequested() {
						// The agent never asked for mouse reports, so the emulator
						// would drop the wheel. Do what terminals do for alternate
						// scroll: turn each notch into arrow keys.
						key := "\x1b[B"
						if up {
							key = "\x1b[A"
						}
						s.emulator.SendText(strings.Repeat(key, wheelLines))
					} else if s.emulator.IsAltScreen() || s.native.mouseRequested() {
						// An alternate-screen agent owns its history. Wheel events over
						// the compact panel must reach that agent inside its viewport.
						forwarded := agentMouse
						forwarded.X = min(max(forwarded.X, 1), s.emulator.Width())
						forwarded.Y = min(max(forwarded.Y, 1), top)
						s.emulator.SendMouse(forwarded.event())
					} else if s.scroll.wheel(up, s.emulator.ScrollbackLen()) {
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
					s.content = statusRows(s)
					beforeTop := top
					resizeAll()
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
					s.emulator.SendMouse(agentMouse.event())
					continue
				}
				if !mouse.Release {
					if !drawer.Open && mouse.Button == 0 && mouse.Y > top {
						drawer.open(s.advice)
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
					} // return control without killing the agent
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
			if shortcuts && !s.emulator.IsAltScreen() && s.emulator.ScrollbackLen() > 0 && (key.Action == "pageup" || key.Action == "pagedown") {
				lines := max(1, top-1)
				if key.Action == "pagedown" {
					lines = -lines
				}
				if s.scroll.move(lines, s.emulator.ScrollbackLen()) {
					dirty = true
				}
				continue
			}
			if s.scroll.reset() {
				dirty = true // typing returns to the live screen
			}
			// Mouse events and terminal replies use the emulator's input pipe.
			// Keep keys in that same stream so they cannot overtake a wheel.
			s.emulator.SendText(string(key.Data))
		}
	}
	signalAll := func(sig syscall.Signal) {
		for _, s := range sessions {
			s.signal(sig)
		}
	}
	remove := func(s *paneSession) {
		index := 0
		for i, other := range sessions {
			if other == s {
				index = i
			}
		}
		wasActive := index == active
		if wasActive {
			s.native.deactivate()
		}
		sessions = append(sessions[:index], sessions[index+1:]...)
		switch {
		case index < active:
			active--
		case wasActive:
			active = min(active, len(sessions)-1)
			cur().native.activate()
			drawer.close()
		}
		sidebar.Selected = min(sidebar.Selected, len(sessions)-1)
		if sidebar.Renaming {
			finishRename(false)
		}
		s.release()
		cur().reload(statusRows)
		resizeAll()
		dirty = true
	}
	var shutdown <-chan time.Time
	paintFinal := func() {
		s := cur()
		s.refresh()
		s.readFooter()
		s.persist()
		s.stats = s.loadStats()
		s.view.Loops, _ = sessionLoops(currentDir(), s.id)
		s.advice = combinedAdvice(s.id, s.stats, s.view)
		s.content = statusRows(s)
		_, _ = io.WriteString(output, paint())
	}
	for {
		select {
		case chunk, ok := <-inputChunks:
			if !ok {
				inputChunks = nil
				handleKeys(keyFeed.feed(nil, true))
				signalAll(syscall.SIGTERM)
				if shutdown == nil {
					shutdown = time.After(2 * time.Second)
				}
				continue
			}
			lastInput = time.Now()
			handleKeys(keyFeed.feed(chunk, false))
		case event := <-events:
			s := event.session
			if s.exited {
				continue
			}
			switch event.kind {
			case paneOutput:
				_, _ = s.emulator.Write(event.data)
				dirty = dirty || s == cur()
			case paneEOF:
				dirty = dirty || s == cur()
			case paneExit:
				s.exited = true
				if len(sessions) == 1 {
					// Preserve any output already delivered before the process exited.
					s.drain(events)
					paintFinal()
					return s.waitErr
				}
				remove(s)
			}
		case <-resize:
			if width, height, sizeErr := term.GetSize(int(output.Fd())); sizeErr == nil && width > 0 && height > 0 {
				if exercise != nil {
					exercise.Resize = true
				}
				cols, rows = width, height
				for _, s := range sessions {
					s.content = statusRows(s)
				}
				divider.Dragging = false
				resizeAll()
				dirty = true
			}
		case sig := <-stop:
			signalAll(sig.(syscall.Signal))
			if sig != syscall.SIGINT && shutdown == nil {
				shutdown = time.After(2 * time.Second)
			}
		case <-shutdown:
			signalAll(syscall.SIGKILL)
			shutdown = nil
		case <-ticker.C:
			s := cur()
			s.refresh()
			if time.Since(lastInput) >= 80*time.Millisecond {
				handleKeys(keyFeed.feed(nil, true))
				s = cur()
			}
			if time.Since(lastBeat) >= 2*time.Second {
				for _, other := range sessions {
					if other == s {
						other.heartbeat()
					} else {
						_ = os.Chtimes(paneRegistrationFile(other.id), time.Now(), time.Now())
					}
				}
				lastBeat = time.Now()
			}
			s.stats = s.loadStats()
			s.readFooter()
			if time.Since(s.lastSave) >= time.Second {
				s.persist()
			}
			loops, journal := sessionLoops(currentDir(), s.id)
			s.view.Loops, s.view.JournalCoverage = loops, journalCoverage(journal)
			s.advice = combinedAdvice(s.id, s.stats, s.view)
			joined := strings.Join(s.advice, "\n")
			if joined != s.lastAdvice {
				s.lastAdvice = joined
				dirty = true
			}
			s.content = statusRows(s)
			if joinedContent := strings.Join(s.content, "\n"); joinedContent != s.lastContent {
				s.lastContent, dirty = joinedContent, true
			}
			// Expand for new content, but do not keep reflowing the agent when
			// temporary advice clears. A physical resize recalculates both areas.
			if nextTop := divider.top(rows, len(s.content)); !divider.Manual && nextTop < top {
				resizeAll()
				dirty = true
			}
			if notice != "" && time.Since(noticeAt) >= 4*time.Second {
				notice, dirty = "", true
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

func renderTerminalPane(emulator *vt.Emulator, cols, rows int, content []string, cursorVisible bool, hud string, hints ...string) string {
	return renderTerminalPaneGrip(emulator, cols, rows, content, cursorVisible, hud, gripIdle, hints...)
}

func renderTerminalPaneGrip(emulator *vt.Emulator, cols, rows int, content []string, cursorVisible bool, hud string, grip gripState, hints ...string) string {
	return renderTerminalPaneView(emulator, 0, cols, rows, content, cursorVisible, hud, grip, hints...)
}

// renderTerminalPaneView draws the agent's screen, or, when offset is above 0,
// the same window that many lines further back in the emulator's scrollback.
func renderTerminalPaneView(emulator *vt.Emulator, offset, cols, rows int, content []string, cursorVisible bool, hud string, grip gripState, hints ...string) string {
	return renderTerminalPaneSide(emulator, offset, cols, rows, content, cursorVisible, hud, grip, nil, hints...)
}

// renderTerminalPaneSide also draws the session list down the left edge. The
// agent's screen is then cols-sidebarWidth wide and starts after it, while the
// Cully panel keeps the whole window width.
func renderTerminalPaneSide(emulator *vt.Emulator, offset, cols, rows int, content []string, cursorVisible bool, hud string, grip gripState, side []string, hints ...string) string {
	var out strings.Builder
	x0 := 0
	if len(side) > 0 {
		x0 = sidebarWidth
	}
	limit := max(0, min(cols-x0, emulator.Width()))
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
	out.WriteString("\x1b[?7l\x1b[?25l")
	for y := 0; y < top; y++ {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[0m\x1b[2K", y+1)
		if x0 > 0 {
			if y < len(side) {
				out.WriteString(side[y])
			}
			fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[0m", y+1, x0+1)
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
	if offset > 0 || !cursorVisible {
		show = "" // the cursor belongs to the live screen
	}
	fmt.Fprintf(&out, "\x1b[?7h\x1b[%d;%dH%s", min(max(cursor.Y+1, 1), top), min(max(cursor.X+1, 1), cols-x0)+x0, show)
	return out.String()
}
