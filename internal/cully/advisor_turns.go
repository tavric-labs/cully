package cully

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Only counters and opaque event hashes are retained, never prompt text.
type advisorTurns struct {
	Submitted, Completed, Analyzed int
	LastPrompt, LastStop           string
	Counts                         advisorWindowCounts
	CountedAt                      time.Time
}

type advisorWindowCounts struct{ Tools, Searches, Errors int }

func advisorWindowEvidence(ref, session, cwd string, counts advisorWindowCounts) string {
	var out string
	updateAdvisorTurns(ref, func(s *advisorTurns) {
		out = fmt.Sprintf("\nwindow_tools=%d window_searches=%d window_errors=%d\n", max(0, counts.Tools-s.Counts.Tools), max(0, counts.Searches-s.Counts.Searches), max(0, counts.Errors-s.Counts.Errors))
		if session != "" {
			_, events := sessionLoops(cwd, session)
			procedures := map[string]int{}
			for _, event := range events {
				if event.Time.After(s.CountedAt) && event.Cmd != "" {
					procedures[event.Cmd]++
				}
			}
			var names []string
			for name, n := range procedures {
				if n >= 2 {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			if len(names) > 8 {
				names = names[:8]
			}
			var repeated []string
			for _, name := range names {
				repeated = append(repeated, fmt.Sprintf("%s (%d observed calls)", name, procedures[name]))
			}
			if len(repeated) > 0 {
				b, _ := json.Marshal(repeated)
				out += "window_repeated_procedures=" + string(b) + "\nrepetition_is_not_proof_of_waste=true\n"
			}
		}
		s.Counts, s.CountedAt = counts, time.Now().UTC()
	})
	return out
}

func advisorTurnsFile(ref string) string {
	return filepath.Join(cullyDir(), "advisor-turns-"+safeSession(ref)+".json")
}

func updateAdvisorTurns(ref string, update func(*advisorTurns)) bool {
	if ref == "" {
		return false
	}
	f, err := os.OpenFile(advisorTurnsFile(ref), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if lockFile(f) != nil {
		return false
	}
	defer unlockFile(f)
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	var state advisorTurns
	if err != nil || len(b) > 4096 || len(b) > 0 && json.Unmarshal(b, &state) != nil {
		return false
	}
	update(&state)
	b, _ = json.Marshal(state)
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	if _, err = f.Write(b); err != nil {
		return false
	}
	return f.Truncate(int64(len(b))) == nil
}

func advisorTurnHash(in continuityHookInput) string {
	id := in.TurnID
	if id == "" {
		id = in.GenerationID
	}
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}

func noteAdvisorPrompt(agent string, in continuityHookInput) {
	ref, event := continuitySessionRef(agent, in), advisorTurnHash(in)
	updateAdvisorTurns(ref, func(s *advisorTurns) {
		if event != "" && event == s.LastPrompt {
			return
		}
		if s.Submitted == 0 && s.Completed > 0 {
			s.Submitted = s.Completed
		}
		s.Submitted++
		s.LastPrompt = event
	})
}

// A completion closes the submitted prompts it consumed. Older hosts without
// prompt hooks fall back to explicitly labeled completed turns. Tools and idle
// time are never counted as user prompts.
func completeAdvisorWindow(agent string, in continuityHookInput) (int, string) {
	ref, event := continuitySessionRef(agent, in), advisorTurnHash(in)
	var completed int
	var signals string
	ok := updateAdvisorTurns(ref, func(s *advisorTurns) {
		completed = s.Completed
		if event != "" && event == s.LastStop {
			return
		}
		origin := "user_prompts"
		if s.Submitted > s.Completed {
			s.Completed = s.Submitted
		} else if s.Submitted == 0 {
			s.Completed++
			origin = "completed_turns"
		} else {
			return
		}
		s.LastStop = event
		completed = s.Completed
		if s.Completed-s.Analyzed < analyzeCadence(s.Completed) {
			return
		}
		start := s.Analyzed + 1
		s.Analyzed = s.Completed
		signals = fmt.Sprintf("\nagent=%s\nsession_ref=%s\nprompts_completed=%d\ncadence_source=%s\nobservation_window=prompts-%d-%d\nwindow_start=%d\nwindow_end=%d\n", agent, ref, completed, origin, start, completed, start, completed)
		if native := continuityNativeSessionID(agent, in); native != "" {
			signals += "agent_session_id=" + native + "\n"
		}
	})
	if !ok {
		return 0, ""
	}
	return completed, signals
}

func dispatchCompletedAdvisorWindow(agent string, in continuityHookInput) {
	n, window := completeAdvisorWindow(agent, in)
	if in.Cwd == "" {
		return
	}
	session := os.Getenv("CULLY_PANE_SESSION")
	if session == "" && agent == "codex" {
		session = activePaneSession(in.Cwd, in.SessionID)
		if session == "" {
			return
		} // Codex outside the opt-in terminal has no report pane.
	}
	if session == "" {
		session = continuitySessionRef(agent, in)
	}
	snap := readSnapshot(session)
	snap.AdvisorPrompts, snap.AdvisorCadence = n, analyzeCadence(n)
	writeSnapshot(session, snap)
	if window == "" {
		return
	}
	signals := "signal_scope=completed_prompts_only; context=unknown tools=unknown verification=unknown\n"
	signals = completedWindowSignals(session, agent, in.Cwd, signals)
	if strings.Contains(signals, "tools=") && !strings.Contains(signals, "tools=unknown") {
		stats := readToolStats(session)
		signals += advisorWindowEvidence(continuitySessionRef(agent, in), session, in.Cwd, advisorWindowCounts{Tools: stats.Tools, Searches: stats.Searches, Errors: stats.Errors})
	}
	dispatchAdvisor(signals+window, session, in.Cwd)
}
