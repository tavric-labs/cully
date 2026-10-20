package cully

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guardEvent(t *testing.T, tool, session, cwd string, input any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"tool_name": tool, "session_id": session, "cwd": cwd, "tool_input": input})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runGuard(agent string, event []byte) string {
	var out bytes.Buffer
	RunGuardHook(agent, bytes.NewReader(event), &out)
	return out.String()
}

func TestGuardNudgesOnceOnAWholeReadOfALargeFile(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_GUARD", "")
	dir := t.TempDir()
	big := filepath.Join(dir, "big.log")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", guardBigFileBytes*2)), 0o600); err != nil {
		t.Fatal(err)
	}
	event := guardEvent(t, "Read", "sess", dir, map[string]any{"file_path": big})
	out := runGuard("claude", event)
	var decoded struct {
		Hook struct {
			Name, Context, Decision string
		} `json:"hookSpecificOutput"`
	}
	var raw map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	_ = decoded
	hook := raw["hookSpecificOutput"]
	if hook["hookEventName"] != "PreToolUse" || !strings.Contains(fmt.Sprint(hook["additionalContext"]), "big.log") || !strings.Contains(fmt.Sprint(hook["additionalContext"]), "offset and limit") {
		t.Fatalf("unexpected output %q", out)
	}
	if _, blocks := hook["permissionDecision"]; blocks {
		t.Fatal("the guard must never decide permissions")
	}
	if again := runGuard("claude", event); again != "" {
		t.Fatalf("nagged twice for the same file: %q", again)
	}
	// Another session gets its own nudge.
	if other := runGuard("claude", guardEvent(t, "Read", "other", dir, map[string]any{"file_path": big})); other == "" {
		t.Fatal("dedupe leaked across sessions")
	}
}

func TestGuardStaysSilentWhenItShould(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_GUARD", "")
	dir := t.TempDir()
	big := filepath.Join(dir, "big.log")
	_ = os.WriteFile(big, []byte(strings.Repeat("x", guardBigFileBytes*2)), 0o600)
	small := filepath.Join(dir, "small.txt")
	_ = os.WriteFile(small, []byte("tiny"), 0o600)
	for name, event := range map[string][]byte{
		"limited read":       guardEvent(t, "Read", "s1", dir, map[string]any{"file_path": big, "limit": 100}),
		"small file":         guardEvent(t, "Read", "s2", dir, map[string]any{"file_path": small}),
		"missing file":       guardEvent(t, "Read", "s3", dir, map[string]any{"file_path": filepath.Join(dir, "none")}),
		"unwatched tool":     guardEvent(t, "Edit", "s4", dir, map[string]any{"file_path": big}),
		"garbage input":      []byte("{not json"),
		"empty input":        nil,
		"bash outside Cully": guardEvent(t, "Bash", "s5", dir, map[string]any{"command": "go test"}),
	} {
		if out := runGuard("claude", event); out != "" {
			t.Errorf("%s: spoke %q", name, out)
		}
	}
	// Codex reads files through the shell, so a Read tool event means nothing there.
	if out := runGuard("codex", guardEvent(t, "Read", "s6", dir, map[string]any{"file_path": big})); out != "" {
		t.Errorf("codex read: spoke %q", out)
	}
	t.Setenv("CULLY_GUARD", "off")
	if out := runGuard("claude", guardEvent(t, "Read", "s7", dir, map[string]any{"file_path": big})); out != "" {
		t.Errorf("CULLY_GUARD=off ignored: %q", out)
	}
}

func TestGuardWarnsBeforeAThirdFailureOfTheSameCommand(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_GUARD", "")
	t.Setenv("CULLY_PANE_SESSION", "pane-1")
	cwd := t.TempDir()
	command := "go test ./... -run TestWidget"
	bash := guardEvent(t, "Bash", "agent-session", cwd, map[string]any{"command": command})
	fail := func() {
		appendJournal(cwd, "pane-1", journalEvent{Class: journalCheck, Failed: true, Sig: commandSig(command)})
	}
	fail()
	if out := runGuard("claude", bash); out != "" {
		t.Fatalf("spoke after one failure: %q", out)
	}
	fail()
	out := runGuard("claude", bash)
	if !strings.Contains(out, "failed 2 times") || strings.Contains(out, command) {
		t.Fatalf("expected a count and no echoed command: %q", out)
	}
	if again := runGuard("claude", bash); again != "" {
		t.Fatalf("nagged twice at the same count: %q", again)
	}
	fail()
	if out := runGuard("claude", bash); !strings.Contains(out, "failed 3 times") {
		t.Fatalf("a new count deserves a new nudge: %q", out)
	}
	// A passing run resets it.
	appendJournal(cwd, "pane-1", journalEvent{Class: journalCheck, Sig: commandSig(command)})
	if out := runGuard("claude", guardEvent(t, "Bash", "agent-session-2", cwd, map[string]any{"command": command})); out != "" {
		t.Fatalf("spoke after a pass: %q", out)
	}
}

func TestInstallRegistersAndUninstallRemovesTheGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	for _, install := range []func() error{installClaude, installCodex} {
		if err := install(); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range map[string]string{
		filepath.Join(home, ".claude", "settings.json"): "PreToolUse[" + claudeGuardMatcher + "]: guard claude",
		filepath.Join(home, ".codex", "hooks.json"):     "PreToolUse[" + codexGuardMatcher + "]: guard codex",
	} {
		found := false
		for _, line := range hookSubcommands(t, readJSON(t, path)) {
			found = found || line == want
		}
		if !found {
			t.Errorf("%s lacks %q", path, want)
		}
	}
	if err := installClaude(); err != nil { // reinstall must not duplicate
		t.Fatal(err)
	}
	count := 0
	for _, line := range hookSubcommands(t, readJSON(t, filepath.Join(home, ".claude", "settings.json"))) {
		if strings.Contains(line, "guard claude") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reinstall left %d guard hooks", count)
	}
	if err := uninstallClaude(); err != nil {
		t.Fatal(err)
	}
	if err := uninstallCodex(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".codex", "hooks.json")} {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), "guard ") {
			t.Errorf("%s still has a guard hook after uninstall", path)
		}
	}
}
