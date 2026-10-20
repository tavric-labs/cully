package cully

import (
	"encoding/json"
	"strings"
)

// The repository ships one plugin per agent so users can install Cully from the
// agent's own plugin marketplace as well as through `cully setup`. Every file is
// generated from the definitions setup already uses, and a test keeps the
// committed copies identical, so the two install paths cannot drift.
//
// Plugin hooks run `cully` from PATH, where setup writes an absolute path. A
// plugin cannot set Claude Code's status line or the MCP server URL, so those
// stay with `cully setup` and `cully mcp add`.

const (
	pluginDescription = "Session health, an advisor, loop detection and durable memory for coding agents. Needs the cully CLI on PATH."
	pluginHomepage    = "https://docs.cully.net/plugins"
	pluginRepository  = "https://github.com/tavric-labs/cully"
)

type pluginHook struct {
	event, command, matcher string
	timeout                 int  // seconds; async hooks default to 5
	async                   bool // run without blocking the agent, for per-tool signals
	noLoop                  bool // Cursor stop hook: our marker prevents its own follow-up loop
}

func pluginClaudeHooks() []pluginHook {
	return []pluginHook{
		{event: "Stop", command: "cully _internal analyze"},
		{event: "SessionStart", command: "cully _internal continuity claude start"},
		{event: "Stop", command: "cully _internal continuity claude stop"},
		{event: "SessionEnd", command: "cully _internal cleanup"},
		{event: "PostToolUse", command: "cully _internal pane-signal claude", async: true},
		{event: "PreToolUse", command: "cully _internal guard claude", matcher: claudeGuardMatcher, timeout: 5},
	}
}

func pluginCodexHooks() []pluginHook {
	return []pluginHook{
		{event: "SessionStart", command: "cully _internal continuity codex start"},
		{event: "PostToolUse", command: "cully _internal codex-signal", async: true},
		{event: "PreToolUse", command: "cully _internal guard codex", matcher: codexGuardMatcher, timeout: 5},
	}
}

func pluginCursorHooks() []pluginHook {
	hooks := []pluginHook{
		{event: "sessionStart", command: "cully _internal continuity cursor start"},
		{event: "afterAgentResponse", command: "cully _internal continuity cursor response"},
		{event: "stop", command: "cully _internal continuity cursor stop", noLoop: true},
		{event: "sessionEnd", command: "cully _internal continuity cursor end"},
	}
	for _, event := range cursorSignalEvents {
		hooks = append(hooks, pluginHook{event: event, command: "cully _internal pane-signal cursor", async: true})
	}
	return hooks
}

// groupedHooks renders hooks in the nested shape Claude Code and Codex read.
func groupedHooks(hooks []pluginHook) map[string]any {
	events := map[string]any{}
	for _, h := range hooks {
		handler := map[string]any{"type": "command", "command": h.command}
		if h.async {
			handler["async"] = true
			handler["timeout"] = 5
		} else if h.timeout > 0 {
			handler["timeout"] = h.timeout
		}
		group := map[string]any{"hooks": []any{handler}}
		if h.matcher != "" {
			group["matcher"] = h.matcher
		}
		list, _ := events[h.event].([]any)
		events[h.event] = append(list, group)
	}
	return map[string]any{"hooks": events}
}

// flatHooks renders hooks in the shape Cursor reads: a command per event.
func flatHooks(hooks []pluginHook) map[string]any {
	events := map[string]any{}
	for _, h := range hooks {
		entry := map[string]any{"command": h.command}
		if h.async {
			entry["timeout"] = 5
		}
		if h.noLoop {
			entry["loop_limit"] = nil
		}
		list, _ := events[h.event].([]any)
		events[h.event] = append(list, entry)
	}
	return map[string]any{"hooks": events}
}

func jsonFile(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

func pluginAuthor() map[string]any {
	return map[string]any{"name": "Tavric Labs", "url": "https://github.com/tavric-labs"}
}

// stripMarkers removes the ownership comments setup uses to recognise its own
// command files; a plugin is owned by the agent's plugin manager instead.
func stripMarkers(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "cully:cursor-command:") {
			out = append(out, line)
		}
	}
	return strings.TrimLeft(strings.Join(out, "\n"), "\n")
}

// PluginFiles returns every plugin and marketplace file keyed by its path
// relative to the repository root. The plugin manifests omit a version so each
// agent tracks the repository instead of pinning a stale one.
func PluginFiles() map[string]string {
	skill := strings.TrimSpace(cullySkill()) + "\n"
	claudeCommand := strings.ReplaceAll(cullyCommandMD, "{{EXE}}", "cully")
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{
			"name": "cully", "description": pluginDescription, "author": pluginAuthor(),
			"homepage": pluginHomepage, "repository": pluginRepository, "license": "Apache-2.0",
			"keywords": []string{"memory", "advisor", "session-health", "handoff"},
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	owner := map[string]any{"name": "Tavric Labs", "url": "https://github.com/tavric-labs"}
	return map[string]string{
		// Claude Code
		"plugins/claude/.claude-plugin/plugin.json": jsonFile(base(nil)),
		"plugins/claude/hooks/hooks.json":           jsonFile(groupedHooks(pluginClaudeHooks())),
		"plugins/claude/skills/cully/SKILL.md":      skill,
		"plugins/claude/commands/cully.md":          claudeCommand,
		".claude-plugin/marketplace.json": jsonFile(map[string]any{
			"name": "cully", "owner": owner, "description": "Cully plugins for coding agents.",
			"plugins": []any{map[string]any{"name": "cully", "source": "./plugins/claude", "description": pluginDescription, "category": "productivity"}},
		}),
		// Cursor
		"plugins/cursor/.cursor-plugin/plugin.json": jsonFile(base(nil)),
		"plugins/cursor/hooks/hooks.json":           jsonFile(flatHooks(pluginCursorHooks())),
		"plugins/cursor/skills/cully/SKILL.md":      skill,
		"plugins/cursor/commands/cully.md":          stripMarkers(cursorCullyCommand),
		".cursor-plugin/marketplace.json": jsonFile(map[string]any{
			"name": "cully", "owner": owner,
			"plugins": []any{map[string]any{"name": "cully", "source": "plugins/cursor", "description": pluginDescription}},
		}),
		// Codex
		"plugins/codex/.codex-plugin/plugin.json": jsonFile(base(map[string]any{
			"skills":     "./skills/",
			"extensions": map[string]any{"com.openai": map[string]any{"hooks": "./hooks/hooks.json"}},
			"interface":  map[string]any{"displayName": "Cully", "shortDescription": "Session health, advisor and memory", "category": "Developer Tools"},
		})),
		"plugins/codex/hooks/hooks.json":      jsonFile(groupedHooks(pluginCodexHooks())),
		"plugins/codex/skills/cully/SKILL.md": skill,
		".agents/plugins/marketplace.json": jsonFile(map[string]any{
			"name": "cully", "interface": map[string]any{"displayName": "Cully"},
			"plugins": []any{map[string]any{
				"name": "cully", "source": map[string]any{"source": "local", "path": "./plugins/codex"},
				"policy": map[string]any{"installation": "AVAILABLE"}, "category": "Developer Tools",
			}},
		}),
	}
}
