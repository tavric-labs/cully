package cully

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// agentConfigPaths groups the configuration locations of one agent. It is
// metadata only; install and setup own the behavior that reads or writes
// these paths.
type agentConfigPaths struct {
	ProjectDirs []string
	UserDirs    []string
	MCPFiles    []string
}

// agentSpec is the single catalog entry for a known coding agent. Identity,
// launch, presence, configuration, MCP wiring and display metadata live here
// so consumers never repeat per-agent switches. Entries hold no mutable
// session state. Feeds (footer parsing, hook snapshots) stay with the
// terminal pipeline, which selects them through NativeFooter and the agent id.
type agentSpec struct {
	ID          string // canonical id: claude, codex, cursor
	DisplayName string // Claude Code, Codex, Cursor
	ShortName   string // Claude, Codex, Cursor (compact panel text)
	JournalName string // Claude Code, Codex, Cursor (timeline, replay and health rows)
	// ConfigDir returns the agent's home configuration directory.
	ConfigDir func() string
	Aliases   []string
	Binary    string
	ResumeID  func(args []string) string
	// NativeFooter reports a terminal footer the wrapper can read.
	NativeFooter bool
	// HasUsageFeed is true when the agent can eventually populate model,
	// context, token or rate-limit instruments (Codex footer or Claude
	// statusline callback). Cursor reports a model and occasional preCompact
	// samples, not a continuous usage feed; missing values stay unavailable.
	HasUsageFeed bool
	// AdvisorMCPScope runs the headless advisor under this agent's MCP scope.
	AdvisorMCPScope bool
	// PaneRegistration keeps a pane binding the worker checks before acting
	// on a late advisor result.
	PaneRegistration bool
	MCPKind          string // json or toml
	MCPPath          func() (string, error)
	SetupMCP         func(path, endpoint string) error
	RemoveMCP        func(path string, endpoints []string) error
	SignInHint       string
	ConfigPaths      func(cwd string) agentConfigPaths
	Present          func(cwd string) bool
}

// agentCatalog lists every known agent in a stable order. Generic executables
// stay outside the catalog; lookupPaneAgent still runs them with common
// instruments.
func agentCatalog() []agentSpec { return agentCatalogOnce() }

// agentCatalogOnce builds the catalog once; panel paints and lookups reuse
// it. Callers must not mutate the returned slice.
var agentCatalogOnce = sync.OnceValue(buildAgentCatalog)

func buildAgentCatalog() []agentSpec {
	return []agentSpec{
		{
			ID:               "claude",
			DisplayName:      "Claude Code",
			ShortName:        "Claude",
			JournalName:      "Claude Code",
			ConfigDir:        ConfigDir,
			Aliases:          []string{"claude-code", "claude code"},
			Binary:           "claude",
			HasUsageFeed:     true,
			AdvisorMCPScope:  false,
			PaneRegistration: false,
			MCPKind:          "json",
			MCPPath:          claudeMCPConfigPath,
			SetupMCP:         func(path, endpoint string) error { return addJSONMCP(path, endpoint, true) },
			RemoveMCP:        func(path string, endpoints []string) error { return removeJSONMCPIfMatching(path, endpoints) },
			SignInHint:       "Restart Claude Code, then use /mcp to sign in to cully.",
			ConfigPaths: func(cwd string) agentConfigPaths {
				claudeDir := ConfigDir()
				return agentConfigPaths{
					ProjectDirs: []string{sharedSkillRoot(cwd), filepath.Join(cwd, ".claude", "agents"), filepath.Join(cwd, ".claude", "skills")},
					UserDirs:    []string{filepath.Join(claudeDir, "agents"), filepath.Join(claudeDir, "skills"), filepath.Join(claudeDir, "plugins")},
					MCPFiles:    []string{filepath.Join(cwd, ".mcp.json"), filepath.Join(claudeDir, "settings.json"), homeClaudeJSON()},
				}
			},
			Present: func(cwd string) bool {
				return commandExists("claude") ||
					dirExists(ConfigDir()) ||
					fileExists(filepath.Join(cwd, "CLAUDE.md")) ||
					dirExists(filepath.Join(cwd, ".claude"))
			},
		},
		{
			ID:               "codex",
			DisplayName:      "Codex",
			ShortName:        "Codex",
			JournalName:      "Codex",
			ConfigDir:        CodexConfigDir,
			Binary:           "codex",
			ResumeID:         codexExplicitResumeID,
			NativeFooter:     true,
			HasUsageFeed:     true,
			AdvisorMCPScope:  true,
			PaneRegistration: true,
			MCPKind:          "toml",
			MCPPath:          func() (string, error) { return codexConfigPath(), nil },
			SetupMCP:         func(path, endpoint string) error { return addTOMLMCP(path, endpoint) },
			RemoveMCP:        func(path string, endpoints []string) error { return removeTOMLMCPIfMatching(path, endpoints) },
			SignInHint:       "Sign in: codex mcp login cully",
			ConfigPaths: func(cwd string) agentConfigPaths {
				codexDir := CodexConfigDir()
				return agentConfigPaths{
					ProjectDirs: []string{sharedSkillRoot(cwd), filepath.Join(cwd, ".codex", "agents"), filepath.Join(cwd, ".codex", "skills"), filepath.Join(cwd, ".agents")},
					UserDirs:    []string{filepath.Join(codexDir, "agents"), filepath.Join(codexDir, "skills"), filepath.Join(codexDir, "plugins"), filepath.Join(codexDir, "prompts")},
					MCPFiles:    []string{filepath.Join(cwd, ".mcp.json"), filepath.Join(codexDir, "mcp.json")},
				}
			},
			Present: func(cwd string) bool {
				return commandExists("codex") ||
					dirExists(CodexConfigDir()) ||
					fileExists(filepath.Join(cwd, "AGENTS.md")) ||
					dirExists(filepath.Join(cwd, ".codex"))
			},
		},
		{
			ID:               "cursor",
			DisplayName:      "Cursor",
			ShortName:        "Cursor",
			JournalName:      "Cursor",
			ConfigDir:        CursorConfigDir,
			Aliases:          []string{"cursor-agent"},
			Binary:           "cursor-agent",
			AdvisorMCPScope:  false,
			PaneRegistration: false,
			MCPKind:          "json",
			MCPPath:          func() (string, error) { return filepath.Join(CursorConfigDir(), "mcp.json"), nil },
			SetupMCP:         func(path, endpoint string) error { return addJSONMCP(path, endpoint, false) },
			RemoveMCP:        func(path string, endpoints []string) error { return removeJSONMCPIfMatching(path, endpoints) },
			SignInHint:       "Restart Cursor, then sign in to cully in MCP settings.",
			ConfigPaths: func(cwd string) agentConfigPaths {
				cursorDir := CursorConfigDir()
				return agentConfigPaths{
					ProjectDirs: []string{sharedSkillRoot(cwd), filepath.Join(cwd, ".cursor", "commands")},
					UserDirs:    []string{filepath.Join(cursorDir, "rules"), filepath.Join(cursorDir, "skills"), filepath.Join(cursorDir, "commands")},
					MCPFiles:    []string{filepath.Join(cwd, ".cursor", "mcp.json"), filepath.Join(cursorDir, "mcp.json")},
				}
			},
			Present: func(cwd string) bool {
				return commandExists("cursor") ||
					dirExists(CursorConfigDir()) ||
					dirExists(filepath.Join(cwd, ".cursor"))
			},
		},
	}
}

// lookupAgentSpec resolves a canonical agent id. Aliases and unknown names
// do not resolve here; display helpers normalize those separately so launch
// behavior for generic executables never changes.
func lookupAgentSpec(id string) (agentSpec, bool) {
	for _, spec := range agentCatalog() {
		if spec.ID == id {
			return spec, true
		}
	}
	return agentSpec{}, false
}

// normalizeAgentID maps an alias to its canonical id. Unknown and empty
// inputs pass through unchanged.
func normalizeAgentID(input string) string {
	id := strings.ToLower(strings.TrimSpace(input))
	for _, spec := range agentCatalog() {
		if id == spec.ID {
			return spec.ID
		}
		for _, alias := range spec.Aliases {
			if id == alias {
				return spec.ID
			}
		}
	}
	return strings.TrimSpace(input)
}

// agentDisplayName maps an agent id to its compact panel name. Unknown names
// pass through; empty means the agent is not known yet.
func agentDisplayName(agent string) string {
	if spec, ok := lookupAgentSpec(normalizeAgentID(agent)); ok {
		return spec.ShortName
	}
	if strings.TrimSpace(agent) == "" {
		return "Agent"
	}
	return strings.TrimSpace(agent)
}

// journalAgentName maps a journal agent id to the name timeline, replay and
// health rows show. Surface labels stay exactly as before.
func journalAgentName(agent string) string {
	if spec, ok := lookupAgentSpec(normalizeAgentID(agent)); ok {
		return spec.JournalName
	}
	if normalizeAgentID(agent) == "" {
		return "The agent"
	}
	return agent
}

// installedAgentID returns the first catalog agent with an executable on
// PATH, in catalog order.
func installedAgentID() (string, error) {
	for _, spec := range agentCatalog() {
		if commandExists(spec.Binary) {
			return spec.ID, nil
		}
	}
	return "", fmt.Errorf("no claude, codex or cursor agent installed")
}
