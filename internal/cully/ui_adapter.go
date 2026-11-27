package cully

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// What each harness lets Cully do, written down in one place and, for Claude Code, checked against the
// installed program. Cully steers a harness through its own flags and event formats, which can change
// in a release. Each feature checks a capability, not a harness name, and when the installed Claude
// Code no longer lists a flag Cully depends on, the feature turns itself off and says so, instead of
// failing silently or passing a flag the program would reject.

type uiCapability struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Why    string `json:"why"`
	Claude string `json:"claude"` // yes, no or partial: detected from the installed program where it can be
	Codex  string `json:"codex"`
	Cursor string `json:"cursor"`
	Note   string `json:"note,omitempty"`
	// Why each harness has the value it has, in one line, for the rows that say so.
	ClaudeWhy string `json:"claudeWhy,omitempty"`
	CodexWhy  string `json:"codexWhy,omitempty"`
	CursorWhy string `json:"cursorWhy,omitempty"`
}

type uiCapabilities struct {
	List    []uiCapability `json:"capabilities"`
	Checked int64          `json:"checked"`
	Flags   bool           `json:"flagsChecked"` // the installed Claude Code's flags were read
	Missing []string       `json:"missing"`      // flags Cully relies on that this Claude Code does not list
	Version string         `json:"version,omitempty"`
}

// The flags Cully passes to Claude Code, and what each one is for.
var uiClaudeNeeds = []struct{ Flag, For string }{
	{"--input-format", "keep talking to a running task"},
	{"--output-format", "read what the task does"},
	{"--permission-prompt-tool", "ask you before a task uses a tool it is not allowed to"},
	{"--settings", "turn risky commands into questions"},
	{"--strict-mcp-config", "load only the servers you chose"},
	{"--resume", "continue a session"},
	{"--fork-session", "start a new session from an old one"},
	{"--allowedTools", "set what a task may use"},
	{"--disallowedTools", "block what a task must not use"},
	{"--append-system-prompt", "give a named agent its role"},
	{"--add-dir", "let a task read a session's record"},
}

var (
	uiFlagsMu   sync.Mutex
	uiFlagsAt   time.Time
	uiFlagsHelp string
	// uiClaudeHelp runs `claude --help`. A variable so tests need no real program.
	uiClaudeHelp = func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, claudeExecutable(), "--help").Output()
		if err != nil && len(out) == 0 {
			return ""
		}
		return string(out)
	}
)

// uiClaudeHas reports whether the installed Claude Code lists a flag. When its help cannot be read it
// returns true, so a working setup is never switched off by a failed check.
func uiClaudeHas(flag string) bool {
	uiFlagsMu.Lock()
	if time.Since(uiFlagsAt) > 15*time.Minute {
		uiFlagsHelp, uiFlagsAt = uiClaudeHelp(), time.Now()
	}
	help := uiFlagsHelp
	uiFlagsMu.Unlock()
	if help == "" {
		return true
	}
	return strings.Contains(help, flag)
}

func (m *uiManager) capabilities() uiCapabilities {
	uiFlagsMu.Lock()
	if time.Since(uiFlagsAt) > 15*time.Minute {
		uiFlagsHelp, uiFlagsAt = uiClaudeHelp(), time.Now()
	}
	help, at := uiFlagsHelp, uiFlagsAt
	uiFlagsMu.Unlock()
	out := uiCapabilities{Checked: at.Unix(), Flags: help != "", Missing: []string{}, Version: uiHarnessVersion("claude")}
	have := func(flag string) bool { return help == "" || strings.Contains(help, flag) }
	for _, n := range uiClaudeNeeds {
		if !have(n.Flag) {
			out.Missing = append(out.Missing, n.Flag+" ("+n.For+")")
		}
	}
	yn := func(ok bool) string {
		if ok {
			return "yes"
		}
		return "no"
	}
	out.List = []uiCapability{
		{ID: "run", Label: "Run a task from Cully", Why: "Start a task from the browser and keep talking to it.", Claude: yn(have("--input-format") && have("--output-format")), Codex: "yes", Cursor: "no", Note: "Tasks run in Claude Code or Codex (through its app-server). Cursor has no verified headless protocol, so its sessions are read, continued by handoff and compared."},
		{ID: "permission", Label: "Ask you before a risky call", Why: "A card in the task window instead of a refusal.", Claude: yn(have("--permission-prompt-tool") && have("--settings")), Codex: "yes", Cursor: "no", Note: "Codex asks through its app-server and the same card answers it; Cursor decides in its own app."},
		{ID: "mcp", Label: "Choose which servers a task loads", Why: "Fewer servers means fewer tool descriptions sent every turn.", Claude: yn(have("--strict-mcp-config")), Codex: "no", Cursor: "no"},
		{ID: "resume", Label: "Resume and fork a session", Why: "Carry on the same conversation, or branch from it.", Claude: yn(have("--resume") && have("--fork-session")), Codex: "yes", Cursor: "partial", Note: "A Codex task resumes its thread by id. Cursor sessions continue as a new task from a handoff."},
		{ID: "events", Label: "See each tool call as it happens", Why: "Plan checklist, tool rows, stuck detection and live activity.", Claude: "yes", Codex: "yes", Cursor: "partial", Note: "Claude and Codex tasks stream them. Cursor records them in transcripts, read afterwards."},
		{ID: "midturn", Label: "Send a message while it works", Why: "Steer a task now, or queue a follow-up for when its turn ends.", Claude: yn(have("--input-format")), Codex: "no", Cursor: "no", Note: "Claude Code queues a message written mid-turn and applies it at its next step. Elsewhere a message waits for the turn to end."},
		{ID: "usage", Label: "Usage limits and context size", Why: "How much room is left in the window and in your plan.", Claude: "yes", Codex: "partial", Cursor: "no", Note: "Cursor does not record limits or tokens."},
		{ID: "history", Label: "Session library, health and evals", Why: "Every past session, scored, searchable and turned into tests.", Claude: "yes", Codex: "yes", Cursor: "yes"},
	}
	out.List = append(out.List, uiSteeringCaps(yn, have)...)
	return out
}

func (s *uiServer) capabilitiesGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.capabilities())
}
