package cully

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The guard is a PreToolUse hook that speaks only when a tool call is about to
// repeat a known waste, and never blocks or changes a permission. LivePlan
// (Liu et al., 2026, arXiv:2608.06701) found that a rule-based monitor which
// consults an advisor only on a flag steers agents at low cost; this is that
// monitor's cheapest form. Alongside whole-file and retry nudges it checks for
// stalled approaches and redundant reads using only journal facts/metadata.
//
//	whole-file read   a Read with no offset or limit of a file over
//	                  guardBigFileBytes, once per file per session
//	repeated failure  a Bash command whose signature already failed
//	                  guardRepeatFailures times in the Cully terminal's session
//	                  journal since it last passed, once per count
//
// Any error leaves the call untouched. CULLY_GUARD=off disables it.
const (
	// guardBigFileBytes is about four times the size at which a tool result
	// counts as oversized, so the nudge is rare and the file is plainly large.
	guardBigFileBytes = 4 * 4 * bigResultTokens
	// guardRepeatFailures is one fewer than loopMinAttempts: speak before the
	// failure that would complete a loop.
	guardRepeatFailures = loopMinAttempts - 1
)

// Tools each agent's guard hook subscribes to. Codex reads files through the
// shell, so only its commands are watched.
const (
	claudeGuardMatcher = "Read|Bash"
	codexGuardMatcher  = "Bash"
)

// guardReadAdvice returns a nudge for a whole-file read of a large file.
func guardReadAdvice(path string, offset, limit int) string {
	if path == "" || offset != 0 || limit != 0 {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < guardBigFileBytes {
		return ""
	}
	tokens := approxTokens(int(info.Size()))
	return fmt.Sprintf("Cully: %s is %d KB, about %dk tokens. Read the part you need with offset and limit, or search it first, rather than the whole file.",
		filepath.Base(path), info.Size()/1024, (tokens+500)/1000)
}

// guardBashAdvice returns a nudge when the command has already failed
// guardRepeatFailures times since it last passed, and that count.
func guardBashAdvice(events []journalEvent, command string) (string, int) {
	sig := commandSig(command)
	if sig == "" {
		return "", 0
	}
	failures := 0
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Class != journalCheck || e.Sig != sig {
			continue
		}
		if e.Unknown {
			continue
		}
		if !e.Failed {
			break
		}
		failures++
	}
	if failures < guardRepeatFailures {
		return "", 0
	}
	return fmt.Sprintf("Cully: this command has failed %d times in this session. Read the first failure's output and change something before running it again.", failures), failures
}

// guardSeen records that a nudge was given, and reports whether it already had
// been. State is one empty file per nudge, so the hook never reads or parses it.
func guardSeen(session, key string) bool {
	sum := sha256.Sum256([]byte(session + "\x00" + key))
	path := filepath.Join(cullyDir(), "guard", hex.EncodeToString(sum[:6]))
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return true
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return true
	}
	_ = f.Close()
	return false
}

// RunGuardHook implements `cully _internal guard AGENT` for PreToolUse.
func RunGuardHook(agent string, r io.Reader, w io.Writer) {
	if os.Getenv("MODEL_HINT_GUARD") != "" || strings.EqualFold(os.Getenv("CULLY_GUARD"), "off") {
		return
	}
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return
	}
	var event toolEvent
	if json.Unmarshal(data, &event) != nil || event.ToolName == "" {
		return
	}
	advice := guardAdvice(agent, event)
	if advice == "" {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "additionalContext": advice}})
}

func guardAdvice(agent string, event toolEvent) string {
	session := os.Getenv("CULLY_PANE_SESSION")
	if session == "" {
		session = activePaneSession(event.Cwd, event.SessionID)
	}
	var events []journalEvent
	if session != "" {
		events = readSessionJournal(event.Cwd, session)
		if n := guardCheckpoint(guardTaskEvents(session, events)); n.text != "" && !guardSeen(session, "checkpoint:"+readTask(session)+":"+n.reason+":"+n.epoch) {
			return n.text
		}
	}
	switch name := toolBaseName(event.ToolName); {
	case name == "read" && agent == "claude":
		var input struct {
			FilePath string `json:"file_path"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
		}
		if json.Unmarshal(event.ToolInput, &input) != nil {
			return ""
		}
		if session != "" && input.Offset == 0 && input.Limit == 0 {
			if advice, key := guardReread(event.Cwd, input.FilePath, events); advice != "" && !guardSeen(session, key) {
				return advice
			}
		}
		advice := guardReadAdvice(input.FilePath, input.Offset, input.Limit)
		if advice == "" || guardSeen(event.SessionID, "read:"+input.FilePath) {
			return ""
		}
		return advice
	case name == "bash" || name == "exec_command" || name == "shell":
		command := shellCommandOf(event.ToolInput)
		if session == "" || command == "" {
			return "" // no journal outside the Cully terminal
		}
		advice, failures := guardBashAdvice(events, command)
		if advice == "" || guardSeen(event.SessionID, fmt.Sprintf("bash:%s:%d", commandSig(command), failures)) {
			return ""
		}
		return advice
	}
	return ""
}
