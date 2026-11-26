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
	Final    string // the full, unclipped text of a result event
	MCP      []uiMCPServer
	Events   []uiEvent
	TurnDone bool
	Uses     map[string]uiToolUse
	Results  []uiToolResult
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
	case "result":
		out.TurnDone = true
		failed, _ := raw["is_error"].(bool)
		text, _ := raw["result"].(string)
		out.Final = text
		out.Events = append(out.Events, uiEvent{Kind: "result", Text: clipUIText(text), Failed: failed})
	case "assistant", "user":
		msg, _ := raw["message"].(map[string]any)
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
