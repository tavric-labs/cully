package cully

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const uiEventTextMax = 2000

// uiEvent is one card in an agent's feed. Kinds: text, tool, tool_result,
// result, system, error, user, stage.
type uiEvent struct {
	Seq    int       `json:"seq"`
	Time   time.Time `json:"t"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text,omitempty"`
	Tool   string    `json:"tool,omitempty"`
	Failed bool      `json:"failed,omitempty"`
}

type uiToolUse struct {
	Name    string
	Path    string // as reported by the agent; made project-relative before journaling
	Command string // never stored; only its hash reaches the journal
	Sig     string
}

type uiToolResult struct {
	ID     string
	Failed bool
}

// uiMCPServer is one MCP server as the agent reported it at start.
type uiMCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// NeedsAuth reports whether the agent cannot use the server until it is
// signed in (Claude reports "needs-auth").
func (s uiMCPServer) NeedsAuth() bool {
	l := strings.ToLower(s.Status)
	return strings.Contains(l, "auth") && (strings.Contains(l, "need") || strings.Contains(l, "requir") || strings.Contains(l, "unauth"))
}

type uiParsed struct {
	Final        string // the full, unclipped text of a result event
	MCP          []uiMCPServer
	Events       []uiEvent
	TurnDone     bool
	Uses         map[string]uiToolUse
	Results      []uiToolResult
	Usage        *uiTokens // tokens the turn reported, when it did
	Model        string    // the model the agent said it is using, from its start line
	Context      int       // tokens in the context after this call, when it reported them
	Window       int       // the model's context window, as reported with a result
	Cost         float64   // what the session has cost so far at list price, as reported
	Limits       *uiLimits // the account's usage limits, when the agent reported them
	Compacted    bool      // the harness compacted the history on this line
	SessionID    string    // the harness's own session id, from its start line
	InitTools    []string  // every tool the harness started with, as it names them
	InitCommands []string  // its slash commands, which include MCP prompts
}

// uiWindow is one usage limit window: how much of it is used and when it resets.
type uiWindow struct {
	Used     int    `json:"used"`     // percent
	Known    bool   `json:"known"`    // the harness reported a figure; without one, "0" would be a guess
	ResetsAt int64  `json:"resetsAt"` // unix seconds
	Status   string `json:"status"`
}

// uiLimits are the five-hour and seven-day limits of the account the task runs on.
type uiLimits struct {
	FiveHour uiWindow `json:"fiveHour"`
	SevenDay uiWindow `json:"sevenDay"`
	Status   string   `json:"status"`
	Seen     int64    `json:"seen"` // when the page last heard it, unix seconds
}

// uiTokens is what one turn used, as the agent reported it. Cache reads cost a
// fraction of ordinary input, so a high cached share is how a task saves tokens.
type uiTokens struct {
	Input      int `json:"input"`      // fresh input tokens
	CacheRead  int `json:"cacheRead"`  // input served from the prompt cache
	CacheWrite int `json:"cacheWrite"` // input written to the cache
	Output     int `json:"output"`
}

func (u uiTokens) add(o uiTokens) uiTokens {
	return uiTokens{u.Input + o.Input, u.CacheRead + o.CacheRead, u.CacheWrite + o.CacheWrite, u.Output + o.Output}
}

// Total is every token the turn handled.
func (u uiTokens) Total() int { return u.Input + u.CacheRead + u.CacheWrite + u.Output }

// CachedShare is the part of the input that came from the cache, from 0 to 100.
func (u uiTokens) CachedShare() int {
	in := u.Input + u.CacheRead + u.CacheWrite
	if in == 0 {
		return 0
	}
	return u.CacheRead * 100 / in
}

func uiNum(v any) int {
	if f, ok := v.(float64); ok && f > 0 {
		return int(f)
	}
	return 0
}

var uiCheckCommand = regexp.MustCompile(`(^|[\s;&|])(go (test|vet|build)|pytest|npm (run )?test|yarn test|make (test|check)|cargo (test|check|build)|tsc|jest|vitest)\b`)

// journalClassFor maps an agent tool call to a journal class.
func journalClassFor(tool, command string) string {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return journalEdit
	case "Read", "Glob", "Grep", "LS":
		return journalSearch
	case "Bash":
		if uiCheckCommand.MatchString(command) {
			return journalCheck
		}
	}
	return journalOther
}

func clipUIText(s string) string {
	r := []rune(s)
	if len(r) <= uiEventTextMax {
		return s
	}
	return string(r[:uiEventTextMax]) + "…"
}

// parseStreamLine turns one line of `claude --output-format stream-json`
// into feed events. It never fails: an unreadable line becomes a system event.
func parseStreamLine(line []byte) uiParsed {
	out := uiParsed{Uses: map[string]uiToolUse{}}
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		out.Events = append(out.Events, uiEvent{Kind: "system", Text: "unreadable agent output line"})
		return out
	}
	kind, _ := raw["type"].(string)
	switch kind {
	case "system":
		sub, _ := raw["subtype"].(string)
		out.Events = append(out.Events, uiEvent{Kind: "system", Text: "agent " + sub})
		if sub == "init" {
			out.Model, _ = raw["model"].(string)
			out.SessionID, _ = raw["session_id"].(string)
			for _, v := range []struct {
				key string
				dst *[]string
			}{{"tools", &out.InitTools}, {"slash_commands", &out.InitCommands}} {
				if l, ok := raw[v.key].([]any); ok {
					for _, x := range l {
						if s, ok := x.(string); ok && len(*v.dst) < 2000 {
							*v.dst = append(*v.dst, s)
						}
					}
				}
			}
		}
		if sub == "compact_boundary" {
			out.Compacted = true
			out.Events = append(out.Events, uiEvent{Kind: "system", Text: "The harness compacted this task's history to make room"})
		}
		if servers, ok := raw["mcp_servers"].([]any); ok && sub == "init" {
			for _, item := range servers {
				entry, _ := item.(map[string]any)
				name, _ := entry["name"].(string)
				status, _ := entry["status"].(string)
				if name == "" {
					continue
				}
				srv := uiMCPServer{Name: name, Status: status}
				out.MCP = append(out.MCP, srv)
				if srv.NeedsAuth() {
					out.Events = append(out.Events, uiEvent{Kind: "system", Text: "MCP server " + name + " needs authentication; its tools are unavailable to this agent until you sign in"})
				}
			}
		}
	case "rate_limit_event":
		if info, ok := raw["rate_limit_info"].(map[string]any); ok {
			lim := &uiLimits{}
			lim.Status, _ = info["status"].(string)
			if w, ok := info["unifiedWindows"].(map[string]any); ok {
				lim.FiveHour = uiWindowOf(w["five_hour"])
				lim.SevenDay = uiWindowOf(w["seven_day"])
			}
			out.Limits = lim
		}
	case "result":
		out.TurnDone = true
		if mu, ok := raw["modelUsage"].(map[string]any); ok {
			for _, v := range mu {
				if m, ok := v.(map[string]any); ok {
					if w := uiNum(m["contextWindow"]); w > out.Window {
						out.Window = w
					}
				}
			}
		}
		if c, ok := raw["total_cost_usd"].(float64); ok {
			out.Cost = c
		}
		failed, _ := raw["is_error"].(bool)
		text, _ := raw["result"].(string)
		out.Final = text
		if u, ok := raw["usage"].(map[string]any); ok {
			out.Usage = &uiTokens{Input: uiNum(u["input_tokens"]), CacheRead: uiNum(u["cache_read_input_tokens"]), CacheWrite: uiNum(u["cache_creation_input_tokens"]), Output: uiNum(u["output_tokens"])}
		}
		out.Events = append(out.Events, uiEvent{Kind: "result", Text: clipUIText(text), Failed: failed})
	case "assistant", "user":
		msg, _ := raw["message"].(map[string]any)
		if u, ok := msg["usage"].(map[string]any); ok && kind == "assistant" {
			out.Context = uiNum(u["input_tokens"]) + uiNum(u["cache_read_input_tokens"]) + uiNum(u["cache_creation_input_tokens"])
		}
		blocks, _ := msg["content"].([]any)
		if s, ok := msg["content"].(string); ok && kind == "assistant" {
			out.Events = append(out.Events, uiEvent{Kind: "text", Text: clipUIText(s)})
		}
		for _, b := range blocks {
			block, _ := b.(map[string]any)
			switch block["type"] {
			case "text":
				if kind == "assistant" {
					text, _ := block["text"].(string)
					out.Events = append(out.Events, uiEvent{Kind: "text", Text: clipUIText(text)})
				}
			case "tool_use":
				name, _ := block["name"].(string)
				id, _ := block["id"].(string)
				input, _ := block["input"].(map[string]any)
				use := uiToolUse{Name: name}
				summary := ""
				if cmd, ok := input["command"].(string); ok {
					use.Command, summary = cmd, cmd
					use.Sig = commandSig(cmd)
				}
				if p, ok := input["file_path"].(string); ok {
					use.Path = p
					if summary == "" {
						summary = p
					}
				}
				if summary == "" {
					for _, k := range []string{"query", "url", "pattern", "description", "prompt"} {
						if s, ok := input[k].(string); ok && s != "" {
							summary = s
							break
						}
					}
				}
				if summary == "" {
					for k, v := range input {
						if s, ok := v.(string); ok {
							summary = fmt.Sprintf("%s=%s", k, s)
							break
						}
					}
				}
				if id != "" {
					out.Uses[id] = use
				}
				if name == "TodoWrite" {
					// The agent's own plan: shown as a checklist that updates in place, not as a tool call.
					if text := uiPlanText(input["todos"]); text != "" {
						out.Events = append(out.Events, uiEvent{Kind: "plan", Text: text})
						continue
					}
				}
				out.Events = append(out.Events, uiEvent{Kind: "tool", Tool: name, Text: clipUIText(strings.TrimSpace(summary))})
			case "tool_result":
				id, _ := block["tool_use_id"].(string)
				failed, _ := block["is_error"].(bool)
				out.Results = append(out.Results, uiToolResult{ID: id, Failed: failed})
				out.Events = append(out.Events, uiEvent{Kind: "tool_result", Text: clipUIText(uiToolResultText(block["content"])), Failed: failed})
			}
		}
	}
	return out
}

func uiToolResultText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func uiWindowOf(v any) uiWindow {
	m, _ := v.(map[string]any)
	w := uiWindow{}
	if u, ok := m["utilization"].(float64); ok {
		w.Used, w.Known = int(u*100+0.5), true
	}
	if r, ok := m["resetsAt"].(float64); ok {
		w.ResetsAt = int64(r)
	}
	w.Status, _ = m["status"].(string)
	return w
}

// uiPlanText turns the todo list an agent keeps (Claude's TodoWrite) into compact JSON for the page:
// [{"c": what, "s": pending, in_progress or completed}], at most 40 items of 160 characters.
func uiPlanText(v any) string {
	list, _ := v.([]any)
	type item struct {
		C string `json:"c"`
		S string `json:"s"`
	}
	var out []item
	for _, x := range list {
		m, _ := x.(map[string]any)
		c, _ := m["content"].(string)
		st, _ := m["status"].(string)
		if strings.TrimSpace(c) == "" {
			continue
		}
		if st != "completed" && st != "in_progress" {
			st = "pending"
		}
		if len(out) < 40 {
			out = append(out, item{C: uiClip(strings.TrimSpace(c), 160), S: st})
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}
