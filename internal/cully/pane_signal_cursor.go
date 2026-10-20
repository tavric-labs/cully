package cully

import (
	"encoding/json"
	"strings"
)

// Cursor hook payloads differ from Claude/Codex PostToolUse JSON. Schema as
// documented at https://cursor.com/docs/agent/hooks (checked 2026-10):
//
//	afterShellExecution: command, output, duration (no exit code)
//	afterFileEdit:       file_path, edits[]
//	afterMCPExecution:   tool_name, tool_input (JSON string), mcp_server_name, result_json (JSON string)
//	postToolUse:         tool_name, tool_input (object), tool_output (JSON string), cwd
//	postToolUseFailure:  tool_name, tool_input, error_message, failure_type
//	common:              conversation_id, workspace_roots, hook_event_name
//
// Assumptions: tool_input / result_json may arrive as a JSON string or an
// object, so both are decoded defensively; a shell command carries no exit
// status, so it is never marked failed; only project-relative file paths and command
// labels are recorded (see journal.go); edits and output are never retained. hooks.json has no async option, so the handler stays fast.
type cursorHookPayload struct {
	HookEvent      string          `json:"hook_event_name"`
	ConversationID string          `json:"conversation_id"`
	WorkspaceRoots []string        `json:"workspace_roots"`
	Cwd            string          `json:"cwd"`
	Command        string          `json:"command"`
	FilePath       string          `json:"file_path"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolOutput     json.RawMessage `json:"tool_output"`
	ResultJSON     json.RawMessage `json:"result_json"`
	MCPServer      string          `json:"mcp_server_name"`
	ErrorMessage   string          `json:"error_message"`
	FailureType    string          `json:"failure_type"`
}

// cursorRawJSON turns a JSON string holding JSON into that JSON; objects pass
// through; anything else becomes an empty object so decoders stay quiet.
func cursorRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if json.Valid([]byte(s)) {
			return json.RawMessage(s)
		}
		return json.RawMessage(`{}`)
	}
	return raw
}

// cursorToolEvent maps a Cursor hook payload onto the shared tool event. It
// reports false for payloads that carry no recognizable tool call.
func cursorToolEvent(data []byte) (toolEvent, bool) {
	var p cursorHookPayload
	if json.Unmarshal(data, &p) != nil {
		return toolEvent{}, false
	}
	event := toolEvent{Cwd: p.Cwd, SessionID: p.ConversationID, ToolResponse: json.RawMessage(`{}`)}
	if event.Cwd == "" && len(p.WorkspaceRoots) > 0 {
		event.Cwd = p.WorkspaceRoots[0]
	}
	switch p.HookEvent {
	case "afterShellExecution":
		event.ToolName = "Bash"
		event.OutcomeUnknown = true
		event.ToolInput, _ = json.Marshal(map[string]string{"command": p.Command})
	case "afterFileEdit":
		event.ToolName = "Edit"
		event.ToolInput, _ = json.Marshal(map[string]string{"file_path": p.FilePath})
	case "afterMCPExecution":
		if p.ToolName == "" {
			return toolEvent{}, false
		}
		event.ToolName = "mcp__" + p.MCPServer + "__" + p.ToolName
		event.ToolInput, event.ToolResponse = cursorRawJSON(p.ToolInput), cursorRawJSON(p.ResultJSON)
	case "postToolUse", "postToolUseFailure":
		if p.ToolName == "" {
			return toolEvent{}, false
		}
		event.ToolName = cursorNormalToolName(p.ToolName)
		if strings.EqualFold(p.ToolName, "delete") {
			event.ToolName = "Delete"
		}
		event.ToolInput, event.ToolResponse = cursorRawJSON(p.ToolInput), cursorRawJSON(p.ToolOutput)
		if p.HookEvent == "postToolUseFailure" || p.ErrorMessage != "" || p.FailureType != "" {
			event.ToolResponse = json.RawMessage(`{"is_error":true}`)
		}
	default:
		return toolEvent{}, false
	}
	return event, true
}

// cursorNormalToolName maps Cursor's built-in tool names to the shell/edit
// names the shared classifier knows.
func cursorNormalToolName(name string) string {
	switch strings.ToLower(name) {
	case "shell":
		return "Bash"
	case "edit", "strreplace", "str_replace":
		return "Edit"
	case "write":
		return "Write"
	}
	return name
}
