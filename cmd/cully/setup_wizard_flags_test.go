package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupNewFlagsParseAndKeepOldRules(t *testing.T) {
	o, err := parseSetup([]string{"--local", "--agent", "claude", "--no-interview", "--yes", "--answers", "a.json"})
	if err != nil || !o.local || !o.noInterview || !o.yes || o.answers != "a.json" || o.target != "claude" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := parseSetup(nil); !errors.Is(err, errSetupNeedsAction) {
		t.Fatalf("no action = %v", err)
	}
	if _, err := parseSetup([]string{"--no-interview"}); !errors.Is(err, errSetupNeedsAction) {
		t.Fatalf("flags only = %v", err)
	}
	for _, args := range [][]string{{"--local", "--all"}, {"--local", "--mcp-url", "https://example.com/mcp"}, {"--local", "--terminal-only"}, {"--answers"}} {
		if _, err := parseSetup(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if o, err := parseSetup([]string{"--all", "--no-interview"}); err != nil || !o.all || !o.noInterview {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestSetupLocalWithAnswersFileWritesRulesWithoutATerminal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, "cursor"))
	t.Setenv("CURSOR_HOME", filepath.Join(home, "cursor"))
	if err := os.MkdirAll(filepath.Join(home, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	answers := filepath.Join(home, "answers.json")
	if err := os.WriteFile(answers, []byte(`{"test_cmd":"make test","review":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSetup([]string{"--local", "--agent", "claude", "--answers", answers, "--yes"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".cully", "skills", "dev-workflow-rules", "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "- Test command: make test") || strings.Contains(string(b), "Review") {
		t.Fatalf("%v %s", err, b)
	}
	if fi, err := os.Lstat(filepath.Join(home, "claude", "skills", "dev-workflow-rules")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("claude is not linked: %v", err)
	}
}
