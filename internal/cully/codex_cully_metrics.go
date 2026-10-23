package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// These are reduced counters for directly observed Cully MCP calls. They
// never contain tool arguments, responses, credentials or retrieved memory.
type cullyStats struct {
	Calls, Log, Context, Recall, Search, Get, Other int
	Health, Auth                                    string
	Probed                                          bool // health or auth came from a connection check, not a tool response
	CheckedAt                                       string
	Foreground, Advisor, Startup, SemanticRecall    int
	ByTool                                          map[string]int
}

var cullyToolName = regexp.MustCompile(`^cully_[a-z0-9_]{1,80}$`)

func cullyStatsFile(session string) string {
	return filepath.Join(cullyDir(), safeSession(session)+".codex-mcp")
}

func cullyTool(name string) string {
	name = strings.ToLower(name)
	if cullyToolName.MatchString(name) {
		return name
	}
	if index := strings.LastIndex(name, "__cully_"); index >= 0 {
		if start := strings.LastIndex(name[:index], "mcp__"); start >= 0 && cullyMCPServerName(name[start+5:index]) {
			tool := name[index+2:]
			if cullyToolName.MatchString(tool) {
				return tool
			}
		}
	}
	for _, prefix := range []string{"mcp__cully__", "mcp__cully.", "mcp.cully."} {
		if index := strings.LastIndex(name, prefix); index >= 0 {
			tool := name[index+len(prefix):]
			if cullyToolName.MatchString(tool) {
				return tool
			}
		}
	}
	return ""
}

// Health is the last observed response, not a background network probe. A
// successful owner-scoped Cully tool establishes authentication at that time.
func cullyResponseState(raw json.RawMessage) (health, auth string) {
	// Claude Code reports an MCP result as a bare array of content blocks;
	// Codex reports an object. Read both through the object shape.
	if trimmed := strings.TrimSpace(string(raw)); strings.HasPrefix(trimmed, "[") {
		raw = json.RawMessage(`{"content":` + trimmed + `}`)
	} else if strings.HasPrefix(trimmed, `"`) {
		// Some agents report the result as a string of tool text. Plain text
		// can be an error message, so only a JSON payload counts as evidence.
		var text string
		if json.Unmarshal(raw, &text) == nil && json.Valid([]byte(text)) && strings.ContainsAny(strings.TrimSpace(text)[:1], "{[") {
			block, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
			raw = json.RawMessage(`{"content":` + string(block) + `}`)
		}
	}
	var response struct {
		IsError      bool            `json:"isError"`
		IsErrorSnake bool            `json:"is_error"`
		Status       int             `json:"status_code"`
		HTTPStatus   int             `json:"status"`
		Error        json.RawMessage `json:"error"`
		Content      []struct {
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return "unknown", "unknown"
	}
	failed := response.IsError || response.IsErrorSnake || response.Status >= 400 || response.HTTPStatus >= 400 || len(response.Error) > 0 && string(response.Error) != "null"
	if failed {
		message := strings.ToLower(string(response.Error))
		for _, part := range response.Content {
			if len(message) < 2048 {
				message += " " + strings.ToLower(part.Text)
			}
		}
		unauth := response.Status == 401 || response.HTTPStatus == 401
		for _, phrase := range []string{"unauthenticated", "authentication required", "login required", "not authenticated", "not logged in", "unauthorized", "invalid token", "expired token", "invalid_token", "expired_token", "token_expired", "token is expired", "token has expired"} {
			unauth = unauth || strings.Contains(message, phrase)
		}
		if unauth {
			return "unhealthy", "unauthenticated"
		}
		return "unhealthy", "unknown"
	}
	if len(response.Content) > 0 || len(response.Structured) > 0 && string(response.Structured) != "null" {
		return "healthy", "authenticated"
	}
	return "unknown", "unknown"
}

func readCodexCullyStats(session string) cullyStats {
	if path := sessionStateFile(session); path != "" {
		if _, err := os.Stat(path); err == nil {
			return readCodexSessionState(path).Stats.Cully
		}
	}
	var stats cullyStats
	data, err := os.ReadFile(cullyStatsFile(session))
	if err == nil && len(data) <= 128*1024 {
		_ = json.Unmarshal(data, &stats)
	}
	return stats
}

func recordCodexCullyCall(session, tool, health, auth string) {
	recordCodexCullyOriginCall(session, tool, health, auth, "foreground", false)
}

func recordCodexCullyOriginCall(session, tool, health, auth, origin string, semantic bool) {
	if sessionStateFile(session) != "" {
		updateCodexSessionState(session, func(state *sessionState) {
			incrementCodexCullyStats(&state.Stats.Cully, tool, health, auth)
			incrementCodexCullyOrigin(&state.Stats.Cully, origin, tool, semantic)
		})
		return
	}
	// Late asynchronous hooks must not recreate a closed pane's telemetry.
	if _, err := os.Stat(paneRegistrationFile(session)); err != nil {
		return
	}
	path := cullyStatsFile(session)
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
	stats := readCodexCullyStats(session)
	incrementCodexCullyStats(&stats, tool, health, auth)
	incrementCodexCullyOrigin(&stats, origin, tool, semantic)
	data, err := json.Marshal(stats)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cully-mcp-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr == nil && closeErr == nil {
		_ = os.Rename(tmp.Name(), path)
	}
}

func incrementCodexCullyStats(stats *cullyStats, tool, health, auth string) {
	if stats.ByTool == nil {
		stats.ByTool = map[string]int{}
		if stats.SemanticRecall == 0 {
			stats.SemanticRecall = stats.Recall
		}
		for name, n := range map[string]int{"cully_log": stats.Log, "cully_context": stats.Context, "cully_recall": stats.Recall, "cully_search": stats.Search, "cully_get": stats.Get, "earlier_other": stats.Other} {
			if n > 0 {
				stats.ByTool[name] = n
			}
		}
		if stats.Foreground+stats.Advisor+stats.Startup == 0 {
			stats.Foreground = stats.Calls
		}
	}
	key := tool
	if _, exists := stats.ByTool[key]; !exists && len(stats.ByTool) >= 512 {
		key = "other_tools"
	}
	stats.ByTool[key]++
	stats.Calls++
	switch tool {
	case "cully_log":
		stats.Log++
	case "cully_context":
		stats.Context++
	case "cully_recall":
		stats.Recall++
	case "cully_search":
		stats.Search++
	case "cully_get":
		stats.Get++
	default:
		stats.Other++
	}
	if health != "unknown" {
		stats.Health = health
		stats.Auth = auth
		stats.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	}
}

func incrementCodexCullyOrigin(stats *cullyStats, origin, tool string, semantic bool) {
	switch origin {
	case "advisor":
		stats.Advisor++
	case "startup":
		stats.Startup++
	default:
		stats.Foreground++
	}
	if tool == "cully_recall" || tool == "cully_context" && semantic {
		stats.SemanticRecall++
	}
}

func clearCodexCullyStats(session string) {
	// Stop accepting new events before waiting for any in-flight writer. This
	// prevents a late rename from recreating telemetry after pane shutdown.
	_ = os.Remove(paneRegistrationFile(session))
	path := cullyStatsFile(session)
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		_ = os.Remove(path)
		return
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) == nil {
		_ = os.Remove(path)
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	}
	_ = lock.Close()
	_ = os.Remove(path + ".lock")
}
