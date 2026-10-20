package cully

import (
	"path/filepath"
	"testing"
)

func TestAgentCatalogOrderAndIdentity(t *testing.T) {
	catalog := agentCatalog()
	want := []string{"claude", "codex", "cursor"}
	if len(catalog) != len(want) {
		t.Fatalf("catalog has %d agents", len(catalog))
	}
	for i, id := range want {
		if catalog[i].ID != id {
			t.Fatalf("catalog[%d] = %q, want %q", i, catalog[i].ID, id)
		}
		if _, ok := lookupAgentSpec(id); !ok {
			t.Fatalf("lookupAgentSpec(%q) missed", id)
		}
	}
	if _, ok := lookupAgentSpec("cursor-agent"); ok {
		t.Fatal("aliases must not resolve as launch ids")
	}
	if _, ok := lookupAgentSpec("nope"); ok {
		t.Fatal("unknown agent resolved")
	}
}

func TestNormalizeAgentID(t *testing.T) {
	cases := map[string]string{
		"claude": "claude", "Claude": "claude", " claude-code ": "claude",
		"claude code": "claude", "codex": "codex", "cursor": "cursor",
		"cursor-agent": "cursor", "": "", "other": "other",
	}
	for in, want := range cases {
		if got := normalizeAgentID(in); got != want {
			t.Errorf("normalizeAgentID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentDisplayNames(t *testing.T) {
	display := map[string]string{
		"claude": "Claude", "claude-code": "Claude", "claude code": "Claude",
		"codex": "Codex", "cursor": "Cursor", "cursor-agent": "Cursor",
		"": "Agent", "other": "other", " Other ": "Other",
	}
	for in, want := range display {
		if got := agentDisplayName(in); got != want {
			t.Errorf("agentDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
	journal := map[string]string{
		"claude": "Claude Code", "codex": "Codex", "cursor": "Cursor",
		"cursor-agent": "Cursor", "": "The agent", "other": "other",
	}
	for in, want := range journal {
		if got := journalAgentName(in); got != want {
			t.Errorf("journalAgentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentLaunchTableFromCatalog(t *testing.T) {
	agent, err := lookupPaneAgent("codex")
	if err != nil || !agent.NativeFooter || agent.ResumeID == nil {
		t.Fatalf("codex launch entry = %+v %v", agent, err)
	}
	if got := agent.Launch([]string{"resume"}); len(got) < 3 || got[0] != "-c" {
		t.Fatalf("codex launch args = %q", got)
	}
	agent, err = lookupPaneAgent("cursor")
	if err != nil || agent.Binary != "cursor-agent" || agent.NativeFooter {
		t.Fatalf("cursor launch entry = %+v %v", agent, err)
	}
	cursor, ok := lookupAgentSpec("cursor")
	if !ok || cursor.HasUsageFeed {
		t.Fatalf("cursor must not claim a usage feed: %+v", cursor)
	}
	claude, _ := lookupAgentSpec("claude")
	codex, _ := lookupAgentSpec("codex")
	if !claude.HasUsageFeed || !codex.HasUsageFeed {
		t.Fatal("claude and codex must keep a usage feed")
	}
	agent, err = lookupPaneAgent("mycoder")
	if err != nil || agent.Binary != "mycoder" || agent.NativeFooter {
		t.Fatalf("generic fallback = %+v %v", agent, err)
	}
}

func TestCodingAgentsFromCatalog(t *testing.T) {
	cwd := t.TempDir()
	agents := codingAgents(cwd)
	if len(agents) != 3 || agents[0].ID != "claude" || agents[0].Name != "Claude Code" {
		t.Fatalf("agents = %+v", agents)
	}
	claude := agents[0]
	wantDirs := []string{
		filepath.Join(cwd, ".cully", "skills"),
		filepath.Join(cwd, ".claude", "agents"),
		filepath.Join(cwd, ".claude", "skills"),
	}
	for i, want := range wantDirs {
		if claude.ProjectDirs[i] != want {
			t.Fatalf("claude project dirs = %q", claude.ProjectDirs)
		}
	}
	if len(claude.MCPFiles) != 3 {
		t.Fatalf("claude MCP files = %q", claude.MCPFiles)
	}
	if codingAgentInstalled("nope") {
		t.Fatal("unknown agent installed")
	}
}
