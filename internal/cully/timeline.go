package cully

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// The timeline is a semantic history of one coding session, derived only from
// the journal. It shows what happened (edits, checks, loops, memory saves),
// never prompts, commands, output or file names.

const timelineRecent = 10

const emptyTimeline = "No session recorded for this project yet. Run your agent with `cully run AGENT`."

// timelineGroup is a run of consecutive events that read as one line.
type timelineGroup struct {
	Time  time.Time
	Event journalEvent
	Count int
	// SameSig is true when every failed check in the group shares one signature.
	SameSig bool
}

func groupKey(e journalEvent) string {
	return e.Class + "|" + fmt.Sprint(e.Failed) + "|" + fmt.Sprint(e.Unknown) + "|" + e.Tool + "|" + e.Note
}

// groupJournal merges consecutive same-class events. Search and other-tool
// events are routine noise between meaningful steps, so they are left out.
func groupJournal(events []journalEvent) []timelineGroup {
	var groups []timelineGroup
	for _, e := range events {
		if e.Class == journalSearch || e.Class == journalOther || e.Class == journalFileOp {
			continue
		}
		if n := len(groups); n > 0 && groupKey(groups[n-1].Event) == groupKey(e) && e.Class != journalStart && e.Class != journalEnd && e.Class != journalNote {
			groups[n-1].Count++
			if groups[n-1].Event.Sig != e.Sig {
				groups[n-1].SameSig = false
			}
			continue
		}
		groups = append(groups, timelineGroup{Time: e.Time, Event: e, Count: 1, SameSig: e.Sig != ""})
	}
	return groups
}

func timesSuffix(n int) string {
	if n > 1 {
		return fmt.Sprintf(" ×%d", n)
	}
	return ""
}

func describeGroup(g timelineGroup) string {
	e := g.Event
	switch e.Class {
	case journalStart:
		return journalAgentName(e.Agent) + " started"
	case journalEnd:
		return journalAgentName(e.Agent) + " ended"
	case journalEdit:
		if e.Failed {
			return "Edit failed" + timesSuffix(g.Count)
		}
		return fmt.Sprintf("Edited ×%d", g.Count)
	case journalCheck:
		if e.Unknown {
			return "Check outcome unavailable" + timesSuffix(g.Count)
		}
		if e.Failed {
			s := "Check failed" + timesSuffix(g.Count)
			if g.Count > 1 && g.SameSig {
				s += " (same command)"
			}
			return s
		}
		return "Check passed" + timesSuffix(g.Count)
	case journalMemory:
		tool := e.Tool
		if tool == "" {
			tool = "memory"
		}
		if tool == "cully_log" {
			return "Saved to Cully memory (" + tool + ")" + timesSuffix(g.Count)
		}
		return "Used Cully memory (" + tool + ")" + timesSuffix(g.Count)
	case journalNote:
		switch e.Note {
		case noteLoop:
			return "Cully noticed a loop: the same check failed repeatedly"
		case noteVerifyGap:
			return "Cully noticed edits that were not verified"
		case noteHistoryTrimmed, noteHistoryUnreadable, noteHistoryInvalid:
			return "Journal " + journalCoverage([]journalEvent{e})
		}
		return "Cully made an observation"
	}
	return "Other activity"
}

// journalStats are the counts shown in the one-line summary.
type journalStats struct {
	Duration       time.Duration
	Edits          int
	ChecksPassed   int
	ChecksFailed   int
	Loops          int
	MemorySaves    int
	Ended          bool
	LastCheckState string // "passed", "failed" or ""
}

func computeStats(events []journalEvent) journalStats {
	var s journalStats
	if len(events) == 0 {
		return s
	}
	s.Duration = events[len(events)-1].Time.Sub(events[0].Time)
	if s.Duration < 0 {
		s.Duration = 0
	}
	for _, e := range events {
		switch e.Class {
		case journalEdit:
			if !e.Failed {
				s.Edits++
			}
		case journalCheck:
			if e.Unknown {
				s.LastCheckState = "unknown"
				continue
			}
			if e.Failed {
				s.ChecksFailed++
				s.LastCheckState = "failed"
			} else {
				s.ChecksPassed++
				s.LastCheckState = "passed"
			}
		case journalMemory:
			if e.Tool == "cully_log" {
				s.MemorySaves++
			}
		case journalEnd:
			s.Ended = true
		case journalStart:
			s.Ended = false
		}
	}
	s.Loops = loopCount(events)
	return s
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func summaryLine(s journalStats) string {
	return fmt.Sprintf("Summary: %s, %s, checks %d passed / %d failed, %s, %s",
		formatDuration(s.Duration), plural(s.Edits, "edit", "edits"), s.ChecksPassed, s.ChecksFailed,
		plural(s.Loops, "loop", "loops"), plural(s.MemorySaves, "memory save", "memory saves"))
}

// renderTimeline is the pure renderer for one session.
func renderTimeline(events []journalEvent, color bool) string {
	if len(events) == 0 {
		return emptyTimeline + "\n"
	}
	var b strings.Builder
	for _, g := range groupJournal(events) {
		stamp := g.Time.Local().Format("15:04")
		if color {
			stamp = dim + stamp + rst
		}
		b.WriteString(stamp + "  " + describeGroup(g) + "\n")
	}
	b.WriteString("\n" + summaryLine(computeStats(events)) + "\n")
	if coverage := journalCoverage(events); coverage != "" {
		b.WriteString("Journal " + coverage + "; summary covers observed events only.\n")
	}
	return b.String()
}

func outcomeWords(s journalStats) string {
	var words []string
	if s.Ended {
		words = append(words, "ended")
	} else {
		words = append(words, "open")
	}
	switch s.LastCheckState {
	case "passed":
		words = append(words, "last check passed")
	case "failed":
		words = append(words, "last check failed")
	case "unknown":
		words = append(words, "last check outcome unavailable")
	default:
		words = append(words, "no checks")
	}
	if s.Loops > 0 {
		words = append(words, plural(s.Loops, "loop", "loops"))
	}
	return strings.Join(words, ", ")
}

type sessionRow struct {
	ID     string
	Events []journalEvent
}

func tableSession(id string) string {
	if len(id) > 20 {
		return id[:20]
	}
	return id
}

func sessionAgent(events []journalEvent) string {
	for _, e := range events {
		if e.Agent != "" {
			return journalAgentName(e.Agent)
		}
	}
	return "-"
}

// renderSessionTable is the pure renderer for `timeline --all`.
func renderSessionTable(rows []sessionRow) string {
	if len(rows) == 0 {
		return emptyTimeline + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-20s  %-12s  %-16s  %-8s  %s\n", "SESSION", "AGENT", "START", "DURATION", "OUTCOME")
	for _, r := range rows {
		start := "-"
		if len(r.Events) > 0 {
			start = r.Events[0].Time.Local().Format("2006-01-02 15:04")
		}
		s := computeStats(r.Events)
		fmt.Fprintf(&b, "%-20s  %-12s  %-16s  %-8s  %s\n", tableSession(r.ID), sessionAgent(r.Events), start, formatDuration(s.Duration), outcomeWords(s))
	}
	return b.String()
}

// RunTimeline implements `cully timeline [--all] [--session ID] [--cwd DIR]`.
func RunTimeline(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("timeline", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "list recent sessions")
	session := fs.String("session", "", "session id (or unique prefix)")
	cwd := fs.String("cwd", "", "project directory")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: cully timeline [--all] [--session ID] [--cwd DIR]")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: cully timeline [--all] [--session ID] [--cwd DIR]")
	}
	if *cwd == "" {
		*cwd = currentDir()
	}
	color := false
	if f, ok := w.(*os.File); ok {
		color = term.IsTerminal(int(f.Fd())) && os.Getenv("NO_COLOR") == ""
	}
	files := projectJournals(*cwd)
	if *all {
		var rows []sessionRow
		for i, f := range files {
			if i >= timelineRecent {
				break
			}
			rows = append(rows, sessionRow{ID: f.Session, Events: readJournal(f.Path)})
		}
		_, err := io.WriteString(w, renderSessionTable(rows))
		return err
	}
	var events []journalEvent
	if *session != "" {
		var match []journalFile
		for _, f := range files {
			if f.Session == *session {
				match = []journalFile{f}
				break
			}
			if strings.HasPrefix(f.Session, *session) {
				match = append(match, f)
			}
		}
		if len(match) != 1 {
			return fmt.Errorf("no single session matches %q; run cully timeline --all", *session)
		}
		events = readJournal(match[0].Path)
	} else if len(files) > 0 {
		events = readJournal(files[0].Path)
	}
	_, err := io.WriteString(w, renderTimeline(events, color))
	return err
}
