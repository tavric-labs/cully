package cully

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Continuity stays in the connected agent. The hook only supplies instructions;
// the agent uses its own MCP connection and owner identity for reads and writes.
const continuityStart = "Cully: For the first substantive task and when the project or task changes, call cully_context with a focused query and project (limit 3). Try semantic mode if text search misses; use cully_get only for needed details. On older servers, use bounded cully_search or cully_recent. Before finishing substantive work, cully_log one concise task, approach, result, issue or missed step, and next step. Skip trivial or duplicate notes. Never save transcripts or secrets. If MCP fails, continue and report unsaved memory."

const continuityFinish = "Cully check: If this turn produced substantive work, a reusable blocker, or a decision, call cully_log through your connected MCP identity. Save one concise task, project (from GitHub remote when known), approach, outcome, issue or missed step, and next step. Set assistant and personal/company section from context; ask only if unclear. Skip a duplicate already saved. Never save transcripts or secrets. If MCP fails, say the shared note was not saved. Then give your normal final response."

var substantiveResponse = regexp.MustCompile(`(?i)\b(implement(?:ed)?|fix(?:ed)?|chang(?:e|ed)|add(?:ed)?|remov(?:e|ed)|refactor(?:ed)?|test(?:ed|s)?|deploy(?:ed)?|publish(?:ed)?|document(?:ed)?|decid(?:e|ed)|block(?:ed|er)?|discover(?:ed)?|learn(?:ed|t)|investigat(?:e|ed)|resolv(?:e|ed)|updat(?:e|ed))\b`)

type continuityHookInput struct {
	TurnID               string `json:"turn_id"`
	Cwd                  string `json:"cwd"`
	StopHookActive       bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
	Text                 string `json:"text"`
	Status               string `json:"status"`
	LoopCount            int    `json:"loop_count"`
	ConversationID       string `json:"conversation_id"`
	SessionID            string `json:"session_id"`
	GenerationID         string `json:"generation_id"`
}

func usefulResponse(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) >= 80 && substantiveResponse.MatchString(s)
}

func continuitySessionRef(agent string, in continuityHookInput) string {
	id := in.SessionID
	if id == "" && agent == "cursor" {
		id = in.ConversationID
	}
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(agent + "\x00" + id))
	return fmt.Sprintf("%s-%x", agent, sum[:8])
}

var nativeSessionIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func continuityNativeSessionID(agent string, in continuityHookInput) string {
	id := in.SessionID
	if id == "" && agent == "cursor" {
		id = in.ConversationID
	}
	if !nativeSessionIDRE.MatchString(id) {
		return ""
	}
	return id
}

func continuityPrompt(base, agent string, in continuityHookInput) string {
	base = "Cully session active. For session controls, read " + runtimeSkillPath(agent) + ". " + base
	if ref := continuitySessionRef(agent, in); ref != "" {
		prompt := base + " For this session, set cully_log.session_ref to " + ref
		if id := continuityNativeSessionID(agent, in); id != "" {
			prompt += " and cully_log.agent_session_id to " + id + ". The native ID lets the user resume this coding-agent session"
		}
		return prompt + ". The opaque reference groups its notes."
	}
	return base
}

func cursorSessionID(in continuityHookInput) string {
	if in.SessionID != "" {
		return in.SessionID
	}
	return in.ConversationID
}

// RunContinuityHook handles native hooks for Claude Code, Codex, and Cursor.
// It never sends a transcript or calls MCP itself: OAuth credentials stay with
// the agent, and the agent chooses a concise record from work it actually did.
func RunContinuityHook(agent, event string, r io.Reader, w io.Writer) {
	if !wrappedSessionActive() {
		return
	}
	if os.Getenv("MODEL_HINT_GUARD") != "" {
		return // background advisor analysis must not start continuity loops
	}
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return
	}
	var in continuityHookInput
	if len(data) != 0 && json.Unmarshal(data, &in) != nil {
		return
	}
	var out any
	switch event {
	case "prompt":
		noteAdvisorPrompt(agent, in)
	case "turn":
		if !in.StopHookActive {
			dispatchCompletedAdvisorWindow(agent, in)
		}
	case "start":
		switch agent {
		case "claude":
			out = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "SessionStart", "additionalContext": continuityPrompt(continuityStart, agent, in)}}
		case "codex":
			bindPane(in.Cwd, in.SessionID)
			out = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "SessionStart", "additionalContext": continuityPrompt(continuityStart, agent, in)}}
		case "cursor":
			out = map[string]any{"additional_context": continuityPrompt(continuityStart, agent, in)}
		}
	case "response":
		if agent == "cursor" {
			writeCursorContinuityState(in, usefulResponse(in.Text))
		}
	case "end":
		if agent == "cursor" {
			for _, path := range []string{cursorContinuityPath(cursorSessionID(in)), cursorFollowupPath(cursorSessionID(in))} {
				if path != "" {
					_ = os.Remove(path)
				}
			}
		}
	case "stop":
		switch agent {
		case "claude":
			if !in.StopHookActive && usefulResponse(in.LastAssistantMessage) {
				out = map[string]any{"decision": "block", "reason": continuityPrompt(continuityFinish, agent, in)}
			}
		case "cursor":
			useful := takeCursorContinuityState(in)
			isFollowup := takeCursorFollowup(in)
			if in.Status == "completed" && in.LoopCount == 0 && !isFollowup {
				dispatchCompletedAdvisorWindow(agent, in)
			}
			if in.Status == "completed" && useful && !isFollowup {
				writeCursorFollowup(in)
				out = map[string]any{"followup_message": continuityPrompt(continuityFinish, agent, in)}
			}
		}
	}
	if out != nil {
		_ = json.NewEncoder(w).Encode(out)
	}
}

type cursorContinuityState struct {
	GenerationID string    `json:"generation_id"`
	Useful       bool      `json:"useful"`
	At           time.Time `json:"at"`
}

func cursorContinuityPath(conversationID string) string {
	if conversationID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(conversationID))
	return filepath.Join(cullyDir(), ".continuity-"+hex.EncodeToString(sum[:16]))
}

func cursorFollowupPath(conversationID string) string {
	path := cursorContinuityPath(conversationID)
	if path == "" {
		return ""
	}
	return path + "-followup"
}

func writeCursorContinuityState(in continuityHookInput, useful bool) {
	path := cursorContinuityPath(cursorSessionID(in))
	if path == "" || in.GenerationID == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if b, err := os.ReadFile(path); err == nil {
		var previous cursorContinuityState
		if json.Unmarshal(b, &previous) == nil && previous.GenerationID == in.GenerationID && time.Since(previous.At) < 5*time.Minute {
			useful = useful || previous.Useful
		}
	}
	b, _ := json.Marshal(cursorContinuityState{GenerationID: in.GenerationID, Useful: useful, At: time.Now()})
	_ = os.WriteFile(path, b, 0o600)
}

func takeCursorContinuityState(in continuityHookInput) bool {
	path := cursorContinuityPath(cursorSessionID(in))
	if path == "" || in.GenerationID == "" {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path)
	var state cursorContinuityState
	if json.Unmarshal(b, &state) != nil {
		return false
	}
	return state.GenerationID == in.GenerationID && state.Useful && time.Since(state.At) < 5*time.Minute
}

type cursorFollowupState struct {
	ExpectedLoopCount int       `json:"expected_loop_count"`
	At                time.Time `json:"at"`
}

func writeCursorFollowup(in continuityHookInput) {
	path := cursorFollowupPath(cursorSessionID(in))
	if path == "" {
		return
	}
	b, _ := json.Marshal(cursorFollowupState{ExpectedLoopCount: in.LoopCount + 1, At: time.Now()})
	_ = os.WriteFile(path, b, 0o600)
}

func takeCursorFollowup(in continuityHookInput) bool {
	path := cursorFollowupPath(cursorSessionID(in))
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path)
	var state cursorFollowupState
	if json.Unmarshal(b, &state) != nil {
		return false
	}
	return state.ExpectedLoopCount == in.LoopCount && time.Since(state.At) < 5*time.Minute
}
