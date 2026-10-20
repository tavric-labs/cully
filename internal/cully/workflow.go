package cully

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// A workflow hint compares this session with the project's earlier journals.
// It only fires on a clear habit: at least workflowMinSessions earlier sessions
// with edits, a check after the last edit in at least workflowMinShare of them.
const (
	workflowMinSessions = 3
	workflowMinSharePct = 80
	workflowMaxFiles    = 20 // newest journals read; keeps the panel cheap
)

// editsCheckedAfter reports whether a journal has edits and whether a check ran
// after the last one.
func editsCheckedAfter(events []journalEvent) (hasEdits, checked bool) {
	for _, ev := range events {
		switch ev.Class {
		case journalEdit:
			hasEdits, checked = true, false
		case journalCheck:
			if hasEdits && !ev.Unknown {
				checked = true
			}
		}
	}
	return hasEdits, checked
}

type workflowSnap struct {
	key   string
	hints []string
}

var (
	workflowMu     sync.Mutex
	workflowCached workflowSnap
)

func workflowKey(cwd string, current []journalEvent) string {
	var last time.Time
	if n := len(current); n > 0 {
		last = current[n-1].Time
	}
	files := projectJournals(cwd)
	if len(files) > workflowMaxFiles {
		files = files[:workflowMaxFiles]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|%s|", cwd, len(current), last.UTC().Format(time.RFC3339Nano))
	for _, f := range files {
		fmt.Fprintf(&b, "%s:%d;", f.Session, f.Modified.UnixNano())
	}
	return b.String()
}

func workflowHints(cwd string, current []journalEvent) []string {
	key := workflowKey(cwd, current)
	workflowMu.Lock()
	if workflowCached.key == key {
		hints := workflowCached.hints
		workflowMu.Unlock()
		return hints
	}
	workflowMu.Unlock()
	hints := computeWorkflowHints(cwd, current)
	workflowMu.Lock()
	workflowCached = workflowSnap{key: key, hints: hints}
	workflowMu.Unlock()
	return hints
}

func computeWorkflowHints(cwd string, current []journalEvent) []string {
	hasEdits, checked := editsCheckedAfter(current)
	if !hasEdits || checked {
		return nil
	}
	var currentStart time.Time
	if len(current) > 0 {
		currentStart = current[0].Time
	}
	sessions, withCheck := 0, 0
	for i, file := range projectJournals(cwd) {
		if i >= workflowMaxFiles {
			break
		}
		events := readJournal(file.Path)
		if len(events) > 0 && events[0].Time.Equal(currentStart) {
			continue // the current session's own journal
		}
		if edits, ok := editsCheckedAfter(events); edits {
			sessions++
			if ok {
				withCheck++
			}
		}
	}
	if sessions < workflowMinSessions || withCheck*100 < sessions*workflowMinSharePct {
		return nil
	}
	return []string{"ADV|You usually run checks after editing in this project. None have run since your last edit."}
}
