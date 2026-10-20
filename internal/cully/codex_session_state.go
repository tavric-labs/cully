//go:build !windows

package cully

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// Only reduced instruments live here: never prompts, screen text, tool payloads,
// credentials, or retrieved memory. The opaque key follows the native thread.
type sessionState struct {
	Version                                      int
	Stats                                        toolStats
	Model, Input, Output, FiveHour, Weekly, Fast string
	ContextLeft                                  int
	ContextKnown                                 bool
	Started                                      time.Time
}

// An explicit native ID hydrates the first frame before the child opens. Names,
// flags and the interactive resume picker bind through the actual SessionStart.
func codexExplicitResumeID(args []string) string {
	if len(args) < 2 || args[0] != "resume" {
		return ""
	}
	if id, err := uuid.Parse(args[1]); err == nil {
		return id.String()
	}
	return ""
}

func sessionStateFile(session string) string {
	b, err := os.ReadFile(paneBindingFile(session))
	if err != nil || len(b) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(string(b)); err != nil {
		return ""
	}
	return filepath.Join(cullyDir(), "codex-session-"+string(b)+".json")
}

func readCodexSessionState(path string) sessionState {
	var state sessionState
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 128*1024 || json.Unmarshal(b, &state) != nil || state.Version != 1 {
		return sessionState{Version: 1}
	}
	// Older running wrappers rewrite the original schema and discard fields
	// they do not know. Keep the richer Cully breakdown in its own private
	// snapshot, under the same thread lock, until those wrappers are reopened.
	var cully cullyStats
	if data, err := os.ReadFile(path + ".cully-mcp"); err == nil && len(data) <= 128*1024 && json.Unmarshal(data, &cully) == nil && cully.ByTool != nil && cully.Calls >= state.Stats.Cully.Calls {
		state.Stats.Cully = cully
	}
	return state
}

func updateCodexSessionState(session string, update func(*sessionState)) {
	path := sessionStateFile(session)
	if path == "" {
		return
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		return
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if _, err := os.Stat(paneRegistrationFile(session)); err != nil {
		return
	}
	state := readCodexSessionState(path)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Upgrade an already open pane without discarding its available counters.
		state.Stats = readToolStats(session)
	}
	update(&state)
	if state.Stats.Cully.ByTool != nil {
		if !writeCodexPrivateJSON(path+".cully-mcp", state.Stats.Cully) {
			return
		}
	}
	writeCodexPrivateJSON(path, state)
}

func writeCodexPrivateJSON(path string, value any) bool {
	b, err := json.Marshal(value)
	if err != nil {
		return false
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codex-session-*")
	if err != nil {
		return false
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	_, writeErr := tmp.Write(b)
	closeErr := tmp.Close()
	return writeErr == nil && closeErr == nil && os.Rename(tmp.Name(), path) == nil
}

func recordCodexSessionTool(session string, class, failure byte) {
	updateCodexSessionState(session, func(state *sessionState) {
		state.Stats.Tools++
		switch class {
		case 'S':
			state.Stats.Searches++
		case 'A':
			state.Stats.Agents++
		case 'W':
			state.Stats.Web++
		case 'G':
			state.Stats.Commits++
		case 'E':
			state.Stats.Edits++
			state.Stats.EditsSinceCheck++
		case 'T':
			state.Stats.Checks++
			if failure == '?' {
				state.Stats.UnknownChecks++
			}
			if failure == '.' {
				state.Stats.EditsSinceCheck = 0
			}
		}
		if failure == '!' {
			state.Stats.Errors++
		}
	})
}

func restoreCodexSessionView(session string, view *sessionView) bool {
	path := sessionStateFile(session)
	if path == "" {
		return false
	}
	state := readCodexSessionState(path)
	if state.Model != "" {
		view.Model = state.Model
	}
	if state.Input != "" {
		view.Input = state.Input
	}
	if state.Output != "" {
		view.Output = state.Output
	}
	if state.FiveHour != "" {
		view.FiveHour = state.FiveHour
	}
	if state.Weekly != "" {
		view.Weekly = state.Weekly
	}
	if state.Fast != "" {
		view.Fast = state.Fast
	}
	if state.ContextKnown {
		view.ContextLeft, view.ContextKnown = state.ContextLeft, true
	}
	if !state.Started.IsZero() {
		view.Started = state.Started
	}
	return true
}

func saveCodexSessionView(session string, view sessionView) {
	updateCodexSessionState(session, func(state *sessionState) {
		if state.Started.IsZero() {
			state.Started = view.Started
		}
		if view.Model != "" {
			state.Model = strings.TrimSpace(view.Model)
		}
		if view.ContextKnown {
			state.ContextLeft, state.ContextKnown = view.ContextLeft, true
		}
		// A narrow footer may temporarily omit instruments. Keep the last native
		// total until a new observed value replaces it; never sum cumulative totals.
		if view.Input != "" {
			state.Input = view.Input
		}
		if view.Output != "" {
			state.Output = view.Output
		}
		if view.FiveHour != "" {
			state.FiveHour = view.FiveHour
		}
		if view.Weekly != "" {
			state.Weekly = view.Weekly
		}
		if view.Fast != "" {
			state.Fast = view.Fast
		}
	})
}
