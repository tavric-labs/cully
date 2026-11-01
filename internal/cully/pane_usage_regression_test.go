package cully

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrappedPaneRequestsNativeUsage(t *testing.T) {
	for _, name := range []string{"codex", "claude"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
			t.Setenv("CODEX_HOME", filepath.Join(root, ".codex"))
			t.Setenv("CULLY_ANALYZE_DISABLE", "1")
			t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(root, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			agent, _ := lookupPaneAgent(name)
			s, err := newPaneSession(context.Background(), agent, []string{"--model", "chosen-model"}, "test", terminalProfile{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.release()
			args := strings.Join(s.cmd.Args, " ")
			if name == "codex" && (!strings.Contains(args, "context-remaining") || !strings.Contains(args, "total-input-tokens") || !strings.Contains(args, "total-output-tokens")) {
				t.Fatalf("wrapped Codex does not request its native instruments: %s", args)
			}
			if name == "claude" && (!strings.Contains(args, "statusLine") || !strings.Contains(args, "_internal usage")) {
				t.Fatalf("wrapped Claude has no live usage callback: %s", args)
			}
			if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); !os.IsNotExist(err) {
				t.Fatal("launch must not rewrite saved Claude settings", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".codex", "config.toml")); !os.IsNotExist(err) {
				t.Fatal("launch must not rewrite saved Codex settings", err)
			}
		})
	}
}

func TestCursorPaneUsesNativeHookModel(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_RUN_ACTIVE", "1")
	t.Setenv("CULLY_PANE_SESSION", "cursor-usage")
	t.Setenv("MODEL_HINT_GUARD", "")
	if err := registerPane("cursor-usage", "/work"); err != nil {
		t.Fatal(err)
	}
	RunPaneSignalHook("cursor", strings.NewReader(`{"hook_event_name":"afterShellExecution","conversation_id":"native-cursor","workspace_roots":["/work"],"model":"legacy-model","model_id":"claude-opus-4-7","command":"pwd","output":"private-output"}`))
	s := paneSession{id: "cursor-usage", agent: paneAgent{Name: "cursor"}, view: sessionView{Agent: "cursor"}}
	s.readFooter()
	if s.view.Model != "claude-opus-4-7" {
		t.Fatalf("Cursor model from native hook never reached the panel: %+v", s.view)
	}
	if s.view.ContextKnown || s.view.Input != "" || s.view.Output != "" {
		t.Fatal("model-only hooks must not invent token or context measurements", s.view)
	}
}
