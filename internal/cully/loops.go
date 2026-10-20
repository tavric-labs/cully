package cully

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Loop detection reads only the session journal (classes, success, command
// hashes, times). It can say that the same thing failed repeatedly; it never
// claims to know why.
//
// Rules, each reset by a passing check:
//
//	repeat-failure  the same failing command signature (Sig set, Failed) occurs
//	                loopMinAttempts times within loopWindowEvents events or
//	                loopWindowTime. A passing check with the same Sig resets
//	                that signature.
//	edit-cycle      loopMinAttempts edit→failed-check cycles (an E followed by a
//	                failed T) with no passing check in between. Any passing
//	                check resets the count.
const (
	loopMinAttempts  = 3
	loopWindowEvents = 20
	loopWindowTime   = 15 * time.Minute
)

const (
	loopRepeatFailure = "repeat-failure"
	loopEditCycle     = "edit-cycle"
)

type loopFinding struct {
	Kind        string
	Attempts    int
	First, Last time.Time
	// EditBetween reports whether an edit happened between the first and last attempt.
	EditBetween bool
}

type loopReport struct {
	Loops []loopFinding
}

func (r loopReport) Active() bool { return len(r.Loops) > 0 }

// Summary is a short factual description for panel rows.
func (r loopReport) Summary() string {
	if len(r.Loops) == 0 {
		return ""
	}
	top := r.Loops[0]
	return fmt.Sprintf("%d %s, last %s", len(r.Loops), pluralWord("loop", len(r.Loops)), top.Last.Local().Format("15:04"))
}

func pluralWord(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// Advice returns one CAUT line per finding, limited to what was observed.
func (r loopReport) Advice() []string {
	var lines []string
	for _, loop := range r.Loops {
		switch loop.Kind {
		case loopRepeatFailure:
			after := ""
			if loop.EditBetween {
				after = " after edits"
			}
			lines = append(lines, fmt.Sprintf("CAUT|Cully noticed something: the same command failed %d times%s. Inspect the first failure before editing again.", loop.Attempts, after))
		case loopEditCycle:
			lines = append(lines, fmt.Sprintf("CAUT|Cully noticed something: %d edits were each followed by a failed check, with no passing check between. Inspect the first failure before editing again.", loop.Attempts))
		}
	}
	return lines
}

func detectLoops(events []journalEvent) loopReport {
	var report loopReport
	if f, ok := detectRepeatFailures(events); ok {
		report.Loops = append(report.Loops, f...)
	}
	if f, ok := detectEditCycles(events); ok {
		report.Loops = append(report.Loops, f)
	}
	return report
}

func detectRepeatFailures(events []journalEvent) ([]loopFinding, bool) {
	recent := map[string][]int{} // sig -> indexes of failures still in the window
	found := map[string]loopFinding{}
	var order []string
	for i, ev := range events {
		switch {
		case ev.Class == journalCheck && !ev.Failed:
			if ev.Sig != "" {
				delete(recent, ev.Sig)
				delete(found, ev.Sig)
			}
		case ev.Failed && ev.Sig != "":
			list := append(recent[ev.Sig], i)
			start := 0
			for start < len(list) && !inLoopWindow(events, list[start], i) {
				start++
			}
			list = list[start:]
			recent[ev.Sig] = list
			if len(list) < loopMinAttempts {
				continue
			}
			finding := loopFinding{Kind: loopRepeatFailure, Attempts: len(list), First: events[list[0]].Time, Last: ev.Time}
			for _, between := range events[list[0]:i] {
				if between.Class == journalEdit {
					finding.EditBetween = true
				}
			}
			if _, seen := found[ev.Sig]; !seen {
				order = append(order, ev.Sig)
			}
			found[ev.Sig] = finding
		}
	}
	var out []loopFinding
	for _, sig := range order {
		if f, ok := found[sig]; ok {
			out = append(out, f)
		}
	}
	return out, len(out) > 0
}

// inLoopWindow reports whether event j is close enough to event i, by position
// or by time, to count toward the same loop.
func inLoopWindow(events []journalEvent, j, i int) bool {
	return i-j < loopWindowEvents || events[i].Time.Sub(events[j].Time) <= loopWindowTime
}

func detectEditCycles(events []journalEvent) (loopFinding, bool) {
	var finding loopFinding
	edited := false
	for _, ev := range events {
		switch {
		case ev.Class == journalEdit:
			edited = true
		case ev.Class == journalCheck && !ev.Failed:
			finding, edited = loopFinding{}, false
		case ev.Class == journalCheck && ev.Failed && edited:
			edited = false
			if finding.Attempts == 0 {
				finding = loopFinding{Kind: loopEditCycle, First: ev.Time, EditBetween: true}
			}
			finding.Attempts++
			finding.Last = ev.Time
		}
	}
	return finding, finding.Attempts >= loopMinAttempts
}

// loopNoted reports whether a loop note already exists for every finding, so a
// loop is written to the journal once until a passing check ends it.
func loopNoted(events []journalEvent, report loopReport) bool {
	var lastNote time.Time
	for _, ev := range events {
		if ev.Class == journalNote && ev.Note == noteLoop {
			lastNote = ev.Time
		}
	}
	for _, loop := range report.Loops {
		if loop.First.After(lastNote) {
			return false
		}
	}
	return true
}

type journalSnapshot struct {
	path   string
	size   int64
	mod    time.Time
	events []journalEvent
}

var (
	journalCacheMu sync.Mutex
	journalCache   journalSnapshot
)

// readSessionJournal re-reads a session journal only when the file changed,
// since the panel asks for advice on every terminal update.
func readSessionJournal(cwd, session string) []journalEvent {
	if cwd == "" || session == "" {
		return nil
	}
	path := journalPath(cwd, session)
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	journalCacheMu.Lock()
	defer journalCacheMu.Unlock()
	if journalCache.path == path && journalCache.size == info.Size() && journalCache.mod.Equal(info.ModTime()) {
		return journalCache.events
	}
	events := readJournal(path)
	journalCache = journalSnapshot{path: path, size: info.Size(), mod: info.ModTime(), events: events}
	return events
}

// sessionLoops detects loops in the current session and records the first
// detection as one journal note. Notes are ignored by detection, so writing
// one cannot trigger another.
func sessionLoops(cwd, session string) (loopReport, []journalEvent) {
	events := readSessionJournal(cwd, session)
	report := detectLoops(events)
	if report.Active() && !loopNoted(events, report) {
		appendJournal(cwd, session, journalEvent{Class: journalNote, Note: noteLoop})
		recordImpact(session, impactLoopCaught, "", 0)
		events = readSessionJournal(cwd, session)
	}
	return report, events
}

// loopCount is how many loops a session had: those Cully noted while the
// terminal was running, or those the journal shows now if that is more. Status,
// timeline and handoff use it so they agree with rescue even when the session
// ran outside the terminal's advice loop.
func loopCount(events []journalEvent) int {
	noted := 0
	for _, e := range events {
		if e.Class == journalNote && e.Note == noteLoop {
			noted++
		}
	}
	return max(noted, len(detectLoops(events).Loops))
}
