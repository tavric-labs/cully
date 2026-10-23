package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeInstallRegistersAsyncPaneSignalHook(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	path := filepath.Join(root, "settings.json")
	user := `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"user-hook"}]}]}}`
	if err := os.WriteFile(path, []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := installClaude(); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := loadSettings(path)
	if !hookConfigured(m, "PostToolUse", "pane-signal claude") {
		t.Fatalf("pane-signal hook missing: %v", m["hooks"])
	}
	b, _ := os.ReadFile(path)
	if strings.Count(string(b), "_internal pane-signal claude") != 1 || !strings.Contains(string(b), `"async": true`) || !strings.Contains(string(b), `"timeout": 5`) || !strings.Contains(string(b), "user-hook") {
		t.Fatalf("unexpected settings: %s", b)
	}
	if err := uninstallClaude(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "pane-signal") || !strings.Contains(string(b), "user-hook") {
		t.Fatalf("uninstall must remove only Cully's hook: %s", b)
	}
}

func TestClaudeInstallRejectsInvalidSettings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	path := filepath.Join(root, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installClaude(); err == nil {
		t.Fatal("expected error for invalid settings")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatalf("invalid settings were modified: %s", b)
	}
}

func TestCursorInstallRegistersPaneSignalHooks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	user := `{"version":1,"hooks":{"afterFileEdit":[{"command":"user-hook"}]}}`
	if err := os.WriteFile(cursorHooksPath(), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := installCursorContinuityHooks(); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(cursorHooksPath())
	if strings.Count(string(b), "_internal pane-signal cursor") != 3 || !strings.Contains(string(b), "user-hook") {
		t.Fatalf("unexpected hooks: %s", b)
	}
	if err := uninstallCursorContinuityHooks(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(cursorHooksPath())
	if strings.Contains(string(b), "pane-signal") || !strings.Contains(string(b), "user-hook") {
		t.Fatalf("uninstall must remove only Cully hooks: %s", b)
	}
}

func TestCursorPaneSignalEvents(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "cursor-pane")
	for _, payload := range []string{
		`{"hook_event_name":"afterShellExecution","conversation_id":"c","workspace_roots":["/w"],"command":"go test ./...","output":"secret","duration":5}`,
		`{"hook_event_name":"afterShellExecution","command":"rg needle","output":"x"}`,
		`{"hook_event_name":"afterShellExecution","command":"ls -la"}`,
		`{"hook_event_name":"afterFileEdit","file_path":"/w/a.go","edits":[{"old_string":"a","new_string":"b"}]}`,
		`{"hook_event_name":"afterMCPExecution","tool_name":"other_tool","mcp_server_name":"x","tool_input":"{\"a\":1}","result_json":"{}"}`,
		`{"hook_event_name":"afterMCPExecution","tool_name":"cully_context","mcp_server_name":"cully","tool_input":"{}","result_json":"{\"structuredContent\":{\"notes\":[]}}"}`,
		`{"hook_event_name":"postToolUseFailure","tool_name":"Shell","tool_input":{"command":"go vet ./..."},"error_message":"boom","failure_type":"error"}`,
		`{"hook_event_name":"beforeShellExecution","command":"rm -rf x"}`,
		`not json`,
	} {
		RunPaneSignalHook("cursor", strings.NewReader(payload))
	}
	data, _ := os.ReadFile(signalFile("cursor-pane"))
	if got, want := string(data), "T?\nS?\nS?\nE.\nO.\nO.\nT!\n"; got != want {
		t.Fatalf("signals = %q, want %q", got, want)
	}
	stats := readToolStats("cursor-pane")
	if stats.Tools != 7 || stats.Errors != 1 || stats.Checks != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatal("payload leaked")
	}
}

func TestPanelWaitingTextNamesTheRunningAgent(t *testing.T) {
	if got := waitingForAgent(sessionView{Agent: "claude"}, true); got != "waiting for Claude" {
		t.Fatalf("claude: %q", got)
	}
	if got := waitingForAgent(sessionView{Agent: "codex"}, true); got != "waiting for Codex footer" {
		t.Fatalf("codex: %q", got)
	}
}

func TestCursorMissingUsageShowsUnavailable(t *testing.T) {
	view := sessionView{Agent: "cursor", Project: "/work/cully"}
	if got := missingUsageText(view, true); got != "unavailable" {
		t.Fatalf("cursor missing usage: %q", got)
	}
	if got := missingUsageText(view, false); got != "unavailable" {
		t.Fatalf("cursor model placeholder: %q", got)
	}
	rows, _ := compactInstrumentRows(100, toolStats{}, view)
	plain := normalizedCodexPanel(strings.Join(rows, "\n"))
	for _, want := range []string{"Model unavailable", "Context unavailable", "Tokens unavailable"} {
		if !strings.Contains(plain, normalizedCodexPanel(want)) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	for _, ban := range []string{"waiting for Cursor", "waiting for agent", "waiting for Codex"} {
		if strings.Contains(plain, normalizedCodexPanel(ban)) {
			t.Fatalf("cursor panel must not wait for a usage feed: found %q", ban)
		}
	}
	if !agentHasUsageFeed("claude") || !agentHasUsageFeed("codex") || !agentHasUsageFeed("") {
		t.Fatal("claude, codex and empty agent must keep a usage feed")
	}
	if agentHasUsageFeed("cursor") || agentHasUsageFeed("other-cli") {
		t.Fatal("cursor and unknown agents must not claim a usage feed")
	}
}
