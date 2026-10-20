package cully

import (
	"fmt"
	"strings"
	"time"
)

// sessionHealth holds only values measured from the journal.
type sessionHealth struct {
	Agent         string
	Start         time.Time
	End           time.Time // zero while the session is running
	Tools         int
	Edits         int
	ChecksPassed  int
	ChecksFailed  int
	ChecksUnknown int
	Loops         int  // notes and live detections, for the count
	Looping       bool // a loop is active now
	Recalls       int  // Cully memory lookups
	Saves         int  // Cully memory writes
	Verification  string
	Coverage      string
	Signals       string
}

func measureHealth(events []journalEvent) sessionHealth {
	var h sessionHealth
	lastEdit, lastCheck := -1, -1
	lastCheckFailed := false
	lastCheckUnknown := false
	for i, e := range events {
		if h.Start.IsZero() || (e.Class == journalStart && h.Start.After(e.Time)) {
			h.Start = e.Time
		}
		if h.Agent == "" {
			h.Agent = e.Agent
		}
		switch e.Class {
		case journalEdit:
			h.Edits++
			h.Tools++
			lastEdit = i
		case journalCheck:
			h.Tools++
			if e.Unknown {
				h.ChecksUnknown++
			} else if e.Failed {
				h.ChecksFailed++
			} else {
				h.ChecksPassed++
			}
			lastCheck, lastCheckFailed = i, e.Failed
			lastCheckUnknown = e.Unknown
		case journalSearch, journalOther, "A", "W", "G":
			h.Tools++
		case journalMemory:
			if memoryWrite(e.Tool) {
				h.Saves++
			} else {
				h.Recalls++
			}
		case journalEnd:
			h.End = e.Time
		}
	}
	switch {
	case lastCheck >= 0 && lastCheck >= lastEdit && lastCheckFailed:
		h.Verification = "failed"
	case lastCheck >= 0 && lastCheck >= lastEdit && lastCheckUnknown:
		h.Verification = "unknown: check outcome unavailable"
	case lastEdit < 0:
		h.Verification = "no edits"
	case lastCheck < lastEdit:
		h.Verification = "none since last edit"
	default:
		h.Verification = "passed since last edit"
	}
	h.Loops = loopCount(events)
	h.Looping = detectLoops(events).Active()
	h.Coverage = journalCoverage(events)
	h.Signals = signalCoverage(h.Agent, h.Tools+h.Recalls+h.Saves)
	if (h.Coverage != "" || h.Tools+h.Recalls+h.Saves == 0) && h.Verification != "failed" {
		h.Verification = "unknown: incomplete session evidence"
	}
	return h
}

func healthDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "under 1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// memoryWrite reports whether a Cully memory tool changes records.
func memoryWrite(tool string) bool {
	return tool == "cully_log" || tool == "cully_update" || tool == "cully_delete"
}

// healthInputs are the live values that the journal cannot supply.
type healthInputs struct {
	Branch      string
	Task        string // the session's accepted task; empty when none
	GitFiles    int    // negative when Git could not be read
	ContextUsed int    // percent of the context window used; negative when unknown
	Now         time.Time
}

// healthGauge draws a ten-cell gauge for a percentage.
func healthGauge(pct int) string {
	pct = min(100, max(0, pct))
	filled := (pct + 5) / 10
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled) + fmt.Sprintf(" %d%%", pct)
}

// healthRisk is a plain rule over measured values, never a score. It names the
// reason so a developer can check it.
func healthRisk(h sessionHealth, in healthInputs) (level, reason string) {
	switch {
	case h.Looping:
		return "HIGH", "the agent may be looping"
	case h.Verification == "failed":
		return "HIGH", "the last check failed"
	case in.ContextUsed >= 90:
		return "HIGH", "context almost full"
	case h.Verification == "none since last edit":
		return "MEDIUM", "edits not checked"
	case in.ContextUsed >= 75:
		return "MEDIUM", "context filling up"
	case in.GitFiles >= 15:
		return "MEDIUM", "many uncommitted files"
	case h.Coverage != "" || strings.HasPrefix(h.Signals, "unavailable:") || strings.HasPrefix(h.Signals, "partial:"):
		return "UNKNOWN", "session evidence is incomplete"
	}
	return "LOW", "nothing flagged"
}

// renderSessionHealth formats the newest session's measured health. It shows
// only values Cully can measure; a row with no data is left out.
func renderSessionHealth(events []journalEvent, in healthInputs) string {
	if len(events) == 0 {
		return "CULLY\n\n  Nothing recorded yet. Start a session with cully run AGENT.\n"
	}
	h := measureHealth(events)
	var b strings.Builder
	row := func(label, value string) { fmt.Fprintf(&b, "  %-14s %s\n", label, value) }
	b.WriteString("CULLY\n\n")
	row("Agent", journalAgentName(h.Agent))
	if in.Branch != "" {
		row("Branch", in.Branch)
	}
	if in.Task != "" {
		row("Task", in.Task)
	}
	if h.End.IsZero() {
		row("Session", healthDuration(in.Now.Sub(h.Start))+" (running)")
	} else {
		row("Session", healthDuration(h.End.Sub(h.Start))+" (ended)")
	}
	if in.ContextUsed >= 0 {
		row("Context", healthGauge(in.ContextUsed))
	}
	row("Tests", fmt.Sprintf("%d ✓  %d ✕", h.ChecksPassed, h.ChecksFailed))
	if h.ChecksUnknown > 0 {
		row("Check outcomes", fmt.Sprintf("%d unknown", h.ChecksUnknown))
	}
	if h.Coverage != "" {
		row("Journal", h.Coverage)
	}
	row("Signals", h.Signals)
	row("Verification", h.Verification)
	loops := fmt.Sprintf("%d", h.Loops)
	if h.Looping {
		loops += " ⚠"
	}
	row("Loops", loops)
	if in.GitFiles >= 0 {
		row("Uncommitted", fmt.Sprintf("%d %s", in.GitFiles, pluralWord("file", in.GitFiles)))
	}
	memory := "none this session"
	if h.Recalls+h.Saves > 0 {
		memory = fmt.Sprintf("%d %s · %d %s", h.Recalls, pluralWord("recall", h.Recalls), h.Saves, pluralWord("save", h.Saves))
	}
	row("Memory", memory)
	level, reason := healthRisk(h, in)
	row("Risk", level+" · "+reason)
	return b.String()
}
