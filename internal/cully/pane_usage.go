package cully

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Only measured instruments are persisted, under the owning pane. No native
// transcript, prompt, workspace payload, or status-line command is retained.
type paneUsage struct {
	Agent         string
	NativeKey     string
	Update        InstrumentUpdate
	ContextTokens *int64
	AtCompaction  bool
	Reset         bool
}

func paneUsageFile(session string) string {
	return filepath.Join(cullyDir(), safeSession(session)+".usage.json")
}

func usagePane(agent string) string {
	if !wrappedSessionActive() || os.Getenv("MODEL_HINT_GUARD") != "" {
		return ""
	}
	if active := os.Getenv("CULLY_AGENT"); active != "" && active != agent {
		return ""
	}
	session := os.Getenv("CULLY_PANE_SESSION")
	if session == "" {
		return ""
	}
	b, err := os.ReadFile(paneRegistrationFile(session))
	var reg paneRegistration
	if err != nil || json.Unmarshal(b, &reg) != nil || reg.Session != session || !processAlive(reg.PID) {
		return ""
	}
	return session
}

func measuredModel(model string) *string {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > 160 || strings.ContainsFunc(model, unicode.IsControl) {
		return nil
	}
	return &model
}

// The primary SessionStart establishes this binding before subagents can
// report. Only a reduced opaque key is stored, never the native session id.
func nativeUsageKey(agent, nativeID string) string {
	if nativeID == "" || len(nativeID) > 256 || strings.ContainsFunc(nativeID, unicode.IsControl) {
		return ""
	}
	sum := sha256.Sum256([]byte(agent + "\x00" + nativeID))
	return hex.EncodeToString(sum[:16])
}

func startUsageSession(agent, nativeID string) {
	session, key := usagePane(agent), nativeUsageKey(agent, nativeID)
	if session == "" || key == "" {
		return
	}
	if writeCodexPrivateJSON(paneUsageFile(session)+".session", key) {
		writeCodexPrivateJSON(paneUsageFile(session), paneUsage{Agent: agent, NativeKey: key, Reset: true})
	}
}

func acceptUsageSession(agent, nativeID string) bool {
	session, key := usagePane(agent), nativeUsageKey(agent, nativeID)
	if session == "" || key == "" {
		return false
	}
	path := paneUsageFile(session) + ".session"
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		err = json.NewEncoder(file).Encode(key)
		closeErr := file.Close()
		return err == nil && closeErr == nil
	}
	b, err := os.ReadFile(path)
	var current string
	return err == nil && json.Unmarshal(b, &current) == nil && current == key
}

func measuredTokens(n *int64) *string {
	if n == nil || *n < 0 {
		return nil
	}
	s := strconv.FormatInt(*n, 10)
	return &s
}

func measuredRemaining(used *float64) *int {
	if used == nil || *used < 0 || *used > 100 {
		return nil
	}
	left := 100 - int(math.Round(*used))
	return &left
}

// RunUsageFeed is Claude's session-only statusLine callback. An existing user
// command receives the identical stdin and keeps its stdout. Nothing is printed
// by Cully itself, and no saved agent setting is changed.
func RunUsageFeed(r io.Reader, w io.Writer, forward string) {
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return
	}
	session := usagePane("claude")
	if session == "" {
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
		Model     struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Context struct {
			Used   *float64 `json:"used_percentage"`
			Input  *int64   `json:"total_input_tokens"`
			Output *int64   `json:"total_output_tokens"`
		} `json:"context_window"`
		RateLimits struct {
			FiveHour *struct {
				Used *float64 `json:"used_percentage"`
			} `json:"five_hour"`
			Weekly *struct {
				Used *float64 `json:"used_percentage"`
			} `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if json.Unmarshal(data, &in) == nil && acceptUsageSession("claude", in.SessionID) {
		u := InstrumentUpdate{Source: sourceSnapshot, Model: measuredModel(fallback(in.Model.DisplayName, in.Model.ID)), ContextLeft: measuredRemaining(in.Context.Used), Input: measuredTokens(in.Context.Input), Output: measuredTokens(in.Context.Output)}
		if in.RateLimits.FiveHour != nil {
			if left := measuredRemaining(in.RateLimits.FiveHour.Used); left != nil {
				s := fmt.Sprintf("5h %d%% left", *left)
				u.FiveHour = &s
			}
		}
		if in.RateLimits.Weekly != nil {
			if left := measuredRemaining(in.RateLimits.Weekly.Used); left != nil {
				s := fmt.Sprintf("Weekly %d%% left", *left)
				u.Weekly = &s
			}
		}
		writeCodexPrivateJSON(paneUsageFile(session), paneUsage{Agent: "claude", NativeKey: nativeUsageKey("claude", in.SessionID), Update: u})
	}
	if forward != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", forward)
		cmd.WaitDelay = time.Second
		cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(data), w, os.Stderr
		_ = cmd.Run()
	}
}

// Cursor exposes its selected model in hooks. preCompact additionally reports
// a context sample; it does not report cumulative input/output token totals.
func recordCursorUsage(data []byte) {
	session := usagePane("cursor")
	if session == "" {
		return
	}
	var in struct {
		ConversationID string   `json:"conversation_id"`
		SessionID      string   `json:"session_id"`
		Event          string   `json:"hook_event_name"`
		Model          string   `json:"model"`
		ModelID        string   `json:"model_id"`
		Used           *float64 `json:"context_usage_percent"`
		ContextTokens  *int64   `json:"context_tokens"`
		Window         int64    `json:"context_window_size"`
	}
	if json.Unmarshal(data, &in) != nil || !acceptUsageSession("cursor", fallback(in.ConversationID, in.SessionID)) {
		return
	}
	u := InstrumentUpdate{Source: sourceSnapshot, Model: measuredModel(fallback(in.ModelID, in.Model))}
	sample := paneUsage{Agent: "cursor", NativeKey: nativeUsageKey("cursor", fallback(in.ConversationID, in.SessionID)), Update: u}
	if in.Event == "preCompact" && in.Window > 0 && in.ContextTokens != nil && *in.ContextTokens >= 0 && *in.ContextTokens <= in.Window {
		sample.Update.ContextLeft = measuredRemaining(in.Used)
		sample.ContextTokens, sample.AtCompaction = in.ContextTokens, true
	}
	// A later hook drops the pre-compaction sample rather than displaying it
	// as live post-compaction usage. Preserve only the last reported model.
	if sample.Update.Model == nil {
		if old, ok := readPaneUsage(session, "cursor"); ok {
			sample.Update.Model = old.Update.Model
		}
	}
	if sample.Update.Model == nil && sample.Update.ContextLeft == nil && sample.ContextTokens == nil {
		return
	}
	writeCodexPrivateJSON(paneUsageFile(session), sample)
}

func readPaneUsage(session, agent string) (paneUsage, bool) {
	data, err := os.ReadFile(paneUsageFile(session))
	var sample paneUsage
	if err != nil || len(data) > 16*1024 || json.Unmarshal(data, &sample) != nil || sample.Agent != agent || sample.NativeKey == "" || sample.NativeKey != readUsageKey(session) {
		return paneUsage{}, false
	}
	return sample, true
}

func readUsageKey(session string) string {
	data, err := os.ReadFile(paneUsageFile(session) + ".session")
	var key string
	if err != nil || len(data) > 128 || json.Unmarshal(data, &key) != nil || len(key) != 32 {
		return ""
	}
	return key
}

func (s *paneSession) readUsage() {
	if s.agent.Name != "claude" && s.agent.Name != "cursor" {
		return
	}
	if key := readUsageKey(s.id); key != "" && key != s.usageKey {
		s.usageKey = key
		s.view.Model, s.view.Input, s.view.Output, s.view.FiveHour, s.view.Weekly = "", "", "", "", ""
		s.view.ContextKnown, s.view.ContextTokens, s.view.ContextAtCompaction = false, "", false
	}
	sample, ok := readPaneUsage(s.id, s.agent.Name)
	if !ok {
		return
	}
	if sample.Reset {
		s.view.Model = ""
	}
	// These callbacks are complete native samples. Missing/null measurements
	// must clear stale values, especially before the first call and after compact.
	s.view.Input, s.view.Output, s.view.FiveHour, s.view.Weekly = "", "", "", ""
	s.view.ContextKnown, s.view.ContextTokens, s.view.ContextAtCompaction = false, "", false
	update := sample.Update
	if sample.AtCompaction {
		// A before-compaction sample is historical context, not a live pressure
		// signal for the HUD or advisor after compaction finishes.
		update.ContextLeft = nil
		if sample.Update.ContextLeft != nil {
			s.view.ContextLeft = *sample.Update.ContextLeft
		}
	}
	s.view.mergeUpdate(update)
	if sample.ContextTokens != nil {
		s.view.ContextTokens = strconv.FormatInt(*sample.ContextTokens, 10)
		s.view.ContextAtCompaction = sample.AtCompaction
	}
}
