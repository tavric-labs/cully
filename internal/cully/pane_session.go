package cully

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/google/uuid"
)

type paneEventKind int

const (
	paneOutput paneEventKind = iota
	paneEOF
	paneExit
)

// paneEvent carries a session's output or lifecycle change to the one loop that
// owns the physical terminal.
type paneEvent struct {
	session *paneSession
	kind    paneEventKind
	data    []byte
}

// paneSession is one wrapped agent with its own process, PTY, virtual terminal,
// scroll position and panel data. Sessions live only as long as the window.
type paneSession struct {
	id    string
	name  string
	agent paneAgent

	cmd      *exec.Cmd
	child    *os.File
	emulator *vt.Emulator
	native   *nativeTerminal

	view        sessionView
	stats       toolStats
	advice      []string
	content     []string
	scroll      paneScroll
	prober      mcpProber
	lastAdvice  string
	lastContent string

	restored  bool
	lastSave  time.Time
	usageKey  string
	waitDone  chan struct{}
	waitErr   error
	exited    bool
	cancel    context.CancelFunc
	reading   chan struct{}
	repliesOK chan struct{}
}

// newPaneSession registers the advisor identity and prepares panel data. The
// process itself starts in start, once the host knows the agent's size.
func newPaneSession(ctx context.Context, agent paneAgent, args []string, name string, profile terminalProfile) (*paneSession, error) {
	if _, err := exec.LookPath(agent.Binary); err != nil {
		return nil, fmt.Errorf("%s is not installed or not on PATH", agent.Binary)
	}
	id := uuid.NewString()
	if err := registerPane(id, currentDir()); err != nil {
		return nil, fmt.Errorf("register %s advisor pane: %w", agent.Name, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &paneSession{id: id, name: name, agent: agent, cancel: cancel}
	if resume := agent.resumeID(args); resume != "" {
		bindPane(currentDir(), resume)
	}
	appendJournal(currentDir(), id, journalEvent{Agent: agent.Name, Class: journalStart})
	s.cmd = exec.Command(agent.Binary, paneUsageArgs(agent.Name, args)...)
	s.cmd.Env = append(os.Environ(), "CULLY_RUN_ACTIVE=1", "CULLY_PANE_SESSION="+id, "CULLY_SESSION="+id, "CULLY_AGENT="+agent.Name)
	s.view = sessionView{HealthBar: true, Agent: agent.Name, Project: currentDir(), Branch: gitBranch(currentDir()), Started: time.Now(), Daemon: isDaemonRunning(), Terminal: profile}
	s.refresh()
	readGitChanges(&s.view)
	go runMCPProbe(ctx, agent.Name, &s.prober)
	s.stats = s.loadStats()
	s.view.Loops, _ = sessionLoops(currentDir(), id)
	s.advice = combinedAdvice(id, s.stats, s.view)
	return s, nil
}

func (s *paneSession) refresh() {
	if !s.restored {
		s.restored = restoreCodexSessionView(s.id, &s.view)
	}
}

func (s *paneSession) persist() {
	saveCodexSessionView(s.id, s.view)
	s.lastSave = time.Now()
}

func (s *paneSession) loadStats() toolStats {
	stats := readToolStats(s.id)
	stats.Impact = ImpactSince(s.view.Started)
	if res, ok := s.prober.get(); ok {
		overlayMCPProbe(&stats.Cully, res)
	}
	return stats
}

func (s *paneSession) readFooter() {
	if s.agent.NativeFooter {
		rows, base := footerRows(s.emulator)
		update, _ := parseFooterRows(rows, base)
		s.view.mergeUpdate(update)
	}
	s.readUsage()
}

// reload refreshes everything the panel shows for this session.
func (s *paneSession) reload(statusRows func(*paneSession) []string) {
	s.refresh()
	s.readFooter()
	s.stats = s.loadStats()
	s.view.Loops, _ = sessionLoops(currentDir(), s.id)
	s.advice = combinedAdvice(s.id, s.stats, s.view)
	s.content = statusRows(s)
}

func (s *paneSession) heartbeat() {
	_ = os.Chtimes(paneRegistrationFile(s.id), time.Now(), time.Now())
	s.view.Daemon = isDaemonRunning()
	s.view.Branch = gitBranch(s.view.Project)
	readGitChanges(&s.view)
}

// start launches the agent in a PTY of the given size and begins forwarding its
// output, replies and exit to events.
func (s *paneSession) start(cols, rows int, events chan<- paneEvent, hostDone <-chan struct{}, output io.Writer) error {
	child, err := pty.StartWithSize(s.cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return fmt.Errorf("start %s: %w", s.agent.Name, err)
	}
	s.child = child
	s.emulator = vt.NewEmulator(cols, rows)
	s.native = attachNativeTerminal(s.emulator, output)
	s.waitDone = make(chan struct{})
	s.reading = make(chan struct{})
	s.repliesOK = make(chan struct{})
	send := func(event paneEvent) bool {
		select {
		case events <- event:
			return true
		case <-s.reading:
			return false
		case <-hostDone:
			return false
		}
	}
	// Wait for the process itself, not PTY EOF: a descendant can retain the
	// terminal after the agent exits. The wrapper must still release its session.
	go func() {
		s.waitErr = s.cmd.Wait()
		close(s.waitDone)
		send(paneEvent{session: s, kind: paneExit})
	}()
	go func() {
		_, _ = io.Copy(child, s.emulator) // terminal query replies
		close(s.repliesOK)
	}()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := child.Read(buf)
			if n > 0 && !send(paneEvent{session: s, kind: paneOutput, data: bytes.Clone(buf[:n])}) {
				return
			}
			if err != nil {
				send(paneEvent{session: s, kind: paneEOF})
				return
			}
		}
	}()
	return nil
}

func (s *paneSession) signal(sig syscall.Signal) {
	if s.cmd != nil && s.cmd.Process != nil && !s.exited {
		_ = syscall.Kill(-s.cmd.Process.Pid, sig)
	}
}

func (s *paneSession) resize(cols, rows int) {
	if s.emulator == nil {
		return
	}
	if cols != s.emulator.Width() || rows != s.emulator.Height() {
		s.emulator.Resize(cols, rows)
		_ = pty.Setsize(s.child, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	}
}

// drain applies output that was already delivered before the process exited.
func (s *paneSession) drain(events <-chan paneEvent) {
	for {
		select {
		case event := <-events:
			if event.session == s && event.kind == paneOutput {
				_, _ = s.emulator.Write(event.data)
			}
		default:
			return
		}
	}
}

// release stops the process group and removes every file registered for the
// session. It is safe to call once the session is no longer needed.
func (s *paneSession) release() {
	s.cancel()
	if s.reading != nil {
		close(s.reading)
		// StartWithSize creates a child process group. Never leave that group
		// running when the owning wrapper exits or loses its terminal.
		if !s.exited {
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
		}
		_ = s.child.Close()
		select {
		case <-s.waitDone:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
			<-s.waitDone
		}
		// Descendants may ignore TERM even after the main process has exited.
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		if pipe, ok := s.emulator.InputPipe().(io.Closer); ok {
			_ = pipe.Close()
		}
		<-s.repliesOK
		_ = s.emulator.Close()
		s.native.close()
	}
	_ = os.Remove(paneRegistrationFile(s.id))
	_ = os.Remove(paneBindingFile(s.id))
	_ = os.Remove(signalFile(s.id))
	_ = os.Remove(sessionReportFile(s.id))
	_ = os.Remove(sessionSnapshotFile(s.id))
	_ = os.Remove(paneUsageFile(s.id))
	_ = os.Remove(paneUsageFile(s.id) + ".session")
	_ = os.Remove(sessionSignalsFile(s.id))
	_ = os.Remove(sessionSeenFile(s.id))
	_ = os.Remove(discoveryFile(s.id, s.agent.Name))
	clearCodexCullyStats(s.id)
	appendJournal(currentDir(), s.id, journalEvent{Agent: s.agent.Name, Class: journalEnd})
	pruneJournals()
}
