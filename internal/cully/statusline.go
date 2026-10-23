package cully

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	maxHintBytes = 8 * 1024
	hintMaxAge   = 24 * time.Hour
)

// reportAge is how long ago the advisor last (re)wrote the session's report —
// i.e. the age of every non-applyable note in it, since notes are regenerated
// wholesale on each run.
func reportAge(session string) time.Duration {
	if session == "" {
		return 0
	}
	info, err := os.Stat(sessionReportFile(session))
	if err != nil {
		return 0
	}
	return time.Since(info.ModTime())
}

// readSuggestions returns the given session's suggestion lines (the full
// report) so the bar can show every lever, not just the top one. Bounded by
// size and staleness. Only that session's report is ever consulted — there is
// deliberately no global fallback, so another session's advice cannot appear.
func readSuggestions(session string) []string {
	return readSuggestionsLimit(session, 4)
}

func readSuggestionsLimit(session string, limit int) []string {
	if session == "" {
		return nil
	}
	f, err := os.Open(sessionReportFile(session))
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && time.Since(st.ModTime()) > hintMaxAge {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, maxHintBytes))
	if err != nil {
		debugLog("statusline: read report for %s: %v", safeSession(session), err)
		return nil
	}
	var rep suggestionReport
	if json.Unmarshal(b, &rep) != nil {
		return nil
	}
	if rep.Session != session {
		return nil
	}
	var out []string
	for _, ln := range rep.Lines {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// formatPhaseBadge renders the session-mode pill on the primary instrument row.
func formatPhaseBadge(phase string) string {
	label := sessionPhaseDisplay(phase)
	col := phaseColor(phase)
	return col + "● " + bold + label + rst
}

func phaseColor(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "messy", "emergency", "emer":
		return red
	case "preflight":
		return blue
	case "approach":
		return yellow
	case "landing":
		return cyan
	default:
		return green
	}
}

func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sessionContextUsed returns the percent of the context window a terminal
// session has used, or -1 when the agent has not reported a window size.
func sessionContextUsed(session string) int {
	if session == "" {
		return -1
	}
	if snap := readSnapshot(session); snap.CtxSize > 0 {
		return min(100, max(0, snap.ContextUsedPct))
	}
	if path := sessionStateFile(session); path != "" {
		if state := readCodexSessionState(path); state.ContextKnown {
			return min(100, max(0, 100-state.ContextLeft))
		}
	}
	return -1
}
