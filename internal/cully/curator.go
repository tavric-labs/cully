package cully

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// The curator keeps the session library up to date with no browser open: it indexes new sessions, names and
// summarises them, retitles weak task names, refreshes suggestions and saves summaries to memory. It is the same
// loop the UI runs, started on its own, the way the advisor runs on its own.
//
// Only one process may index at a time, because the UI keeps the index in memory and two writers would
// overwrite each other. A lock file decides which. The process without the lock does not index: the UI then
// re-reads what the curator wrote every few seconds.

const curatorUsage = "usage: cully curator [--once] [--quiet] [--hide-sessions-in DIR]..."

// uiFollowEvery is how often a UI that is not the indexer re-reads the library from the database. It is a
// variable only so a test need not wait.
var uiFollowEvery = 15 * time.Second

// uiIndexerLock takes the lock that makes this process the one that indexes. It does not wait: it reports
// whether it got it. The lock is released when release is called or the process exits.
func uiIndexerLock() (release func(), ok bool) {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return func() {}, false
	}
	f, err := os.OpenFile(filepath.Join(dir, "indexer.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return func() {}, false
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true
}

// followIndexer keeps a UI that is not the indexer in step with the process that is: the index is dropped and
// loaded again from the database on the next use.
func (m *uiManager) followIndexer(ctx context.Context) {
	t := time.NewTicker(uiFollowEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.idxMu.Lock()
			m.index = nil
			m.idxMu.Unlock()
		}
	}
}

// curatorPass runs the indexer's work once, and keeps analysing until nothing is left or the hourly limit is
// reached, so a single pass from a scheduled job makes real progress.
func (m *uiManager) curatorPass() (analysed int) {
	m.indexSessions(200)
	for i := 0; i < 20; i++ {
		n := m.analyzeNext()
		if n == 0 {
			break
		}
		analysed += n
	}
	m.coachIfDue()
	m.scoutIfDue()
	m.identitiesDue()
	m.agentSuggestIfDue()
	m.jobSuggestIfDue()
	m.rulesIfDue()
	m.syncAgentsIfDue()
	m.retitleTasks()
	m.syncTasksIfDue()
	return analysed
}

// RunCurator implements `cully curator`.
func RunCurator(w io.Writer, args []string) error {
	once, quiet := false, false
	var rest []string
	for _, a := range args {
		switch a {
		case "--once":
			once = true
		case "--quiet":
			quiet = true
		case "-h", "--help":
			fmt.Fprintln(w, curatorUsage)
			return nil
		default:
			rest = append(rest, a)
		}
	}
	o, err := parseUIFlags(rest)
	if err != nil {
		return errors.New(curatorUsage + ": " + err.Error())
	}
	release, ok := uiIndexerLock()
	if !ok {
		if !quiet {
			fmt.Fprintln(w, "Another Cully process (the UI or another curator) is already keeping the library up to date, so there is nothing to do here.")
		}
		return nil
	}
	defer release()
	m := newUIManager(uiConfig{ScoutStream: uiStreamScout, HideSessionsIn: o.HideSessionsIn})
	if once {
		n := m.curatorPass()
		if !quiet {
			fmt.Fprintf(w, "Curator pass done: %d %s analysed.\n", n, map[bool]string{true: "session", false: "sessions"}[n == 1])
		}
		return nil
	}
	if !quiet {
		fmt.Fprintln(w, "Cully curator running. It names sessions, retitles tasks and refreshes suggestions in the background; analysis follows the switch on the Sessions page. Stop it with Ctrl+C.")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	m.runIndexer(ctx)
	return nil
}
