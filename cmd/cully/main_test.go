package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyFlagsAfterNumber(t *testing.T) {
	n, yes, dry, cwd, err := parseApply([]string{"2", "--dry-run", "--yes", "--cwd", "/tmp/my project"})
	if err != nil || n != 2 || !yes || !dry || cwd != "/tmp/my project" {
		t.Fatalf("parsed: %d %v %v %q %v", n, yes, dry, cwd, err)
	}
	for _, args := range [][]string{nil, {"0"}, {"x"}, {"1", "extra"}, {"1", "--unknown"}} {
		if _, _, _, _, err := parseApply(args); err == nil {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
}

func TestRemovedCommands(t *testing.T) {
	for _, name := range []string{"agent", "memory", "list", "systems", "plan", "checklist", "debrief", "daemon", "worker", "analyze", "cleanup"} {
		if err := run([]string{name}); err == nil {
			t.Fatalf("old public command %s still exists", name)
		}
	}
}

func TestSetupMCPOptionsFailBeforeChangingAgentSetup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	for _, args := range [][]string{
		{"--agent", "codex", "--mcp-url", "not-a-url"},
		{"--mcp-url", "not-a-url", "--agent", "codex"},
	} {
		if err := runSetup(args); err == nil {
			t.Fatalf("accepted invalid setup options: %v", args)
		}
		if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
			t.Fatalf("modified agent config for invalid options: %v", err)
		}
	}
}

func TestSetupExplicitModes(t *testing.T) {
	for _, test := range []struct {
		args     []string
		target   string
		oauth    bool
		endpoint string
	}{
		{args: []string{"--all"}},
		{args: []string{"--agent", "codex", "--all"}, target: "codex"},
		{args: []string{"--agent", "all", "--all"}, target: "all"},
		{args: []string{"--agent", "codex", "--mcp-url", "https://mcp.example.com/mcp", "--oauth"}, target: "codex", endpoint: "https://mcp.example.com/mcp", oauth: true},
		{args: []string{"--agent=claude", "--all", "--oauth"}, target: "claude", oauth: true},
	} {
		options, err := parseSetup(test.args)
		if err != nil || options.target != test.target || options.oauth != test.oauth || options.endpoint != test.endpoint {
			t.Fatalf("parseSetup(%v) = %+v, %v", test.args, options, err)
		}
	}
	for _, args := range [][]string{
		nil, {"codex"}, {"--agent", "codex"}, {"--all", "--mcp-url", "https://example.com/mcp"}, {"--agent"}, {"--agent", ""}, {"--agent", "other"},
		{"codex", "--agent", "claude"}, {"--agent", "codex", "cursor"},
		{"--mcp-url", ""}, {"--mcp-url"},
	} {
		if _, err := parseSetup(args); err == nil {
			t.Fatalf("accepted invalid setup args: %v", args)
		}
	}
}

func TestMCPAddRequiresDeploymentURL(t *testing.T) {
	if err := run([]string{"mcp", "add", "--agent", "codex"}); err == nil {
		t.Fatal("mcp add accepted a missing deployment URL")
	}
}

func TestTerminalOnlySetupOptions(t *testing.T) {
	options, err := parseSetup([]string{"--terminal-only", "--skip-terminal-test"})
	if err != nil || !options.terminalOnly || !options.skipTerminalTest {
		t.Fatalf("%+v %v", options, err)
	}
	for _, args := range [][]string{{"--terminal-only", "--all"}, {"--terminal-only", "--mcp-url", "https://example.com/mcp"}, {"--terminal-only", "--agent", "codex"}, {"--terminal-only", "--oauth"}} {
		if _, err := parseSetup(args); err == nil {
			t.Fatalf("accepted mixed terminal/stack setup: %v", args)
		}
	}
}
