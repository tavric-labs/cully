package cully

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookOutput(t *testing.T, agent, event, input string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	RunContinuityHook(agent, event, strings.NewReader(input), &out)
	if out.Len() == 0 {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestContinuityHookStartAndStop(t *testing.T) {
	t.Setenv("MODEL_HINT_GUARD", "")
	for _, agent := range []string{"claude", "codex"} {
		start := hookOutput(t, agent, "start", `{"session_id":"native-session-a"}`)
		specific := start["hookSpecificOutput"].(map[string]any)
		ref := continuitySessionRef(agent, continuityHookInput{SessionID: "native-session-a"})
		if specific["hookEventName"] != "SessionStart" || !strings.Contains(specific["additionalContext"].(string), "cully_context") || !strings.Contains(specific["additionalContext"].(string), ref) || !strings.Contains(specific["additionalContext"].(string), "cully_log.agent_session_id to native-session-a") {
			t.Fatalf("%s start: %#v", agent, start)
		}
		stop := hookOutput(t, agent, "stop", `{"session_id":"native-session-a","last_assistant_message":"I fixed the project configuration, tested the setup and documented the remaining issue for the next session."}`)
		if agent == "claude" {
			if stop["decision"] != "block" || !strings.Contains(stop["reason"].(string), "cully_log") || !strings.Contains(stop["reason"].(string), ref) || !strings.Contains(stop["reason"].(string), "native-session-a") {
				t.Fatalf("%s stop: %#v", agent, stop)
			}
		} else if stop != nil {
			t.Fatalf("Codex stop must not block: %#v", stop)
		}
		if got := hookOutput(t, agent, "stop", `{"stop_hook_active":true,"last_assistant_message":"I fixed the project configuration, tested the setup and documented the remaining issue for the next session."}`); got != nil {
			t.Fatalf("%s stop looped: %#v", agent, got)
		}
		if got := hookOutput(t, agent, "stop", `{"last_assistant_message":"Hello, how can I help?"}`); got != nil {
			t.Fatalf("%s trivial response: %#v", agent, got)
		}
	}
	if got := hookOutput(t, "cursor", "start", `{"session_id":"cursor-session-a"}`); !strings.Contains(got["additional_context"].(string), "cully_context") || !strings.Contains(got["additional_context"].(string), "cursor-") || !strings.Contains(got["additional_context"].(string), "cully_log.agent_session_id to cursor-session-a") {
		t.Fatalf("Cursor start: %#v", got)
	}
	if got := hookOutput(t, "codex", "start", `{"session_id":"bad\ncontext"}`); strings.Contains(got["hookSpecificOutput"].(map[string]any)["additionalContext"].(string), "bad") {
		t.Fatalf("untrusted session ID reached prompt: %#v", got)
	}
}

func TestCursorContinuityUsesOnlyCurrentResponse(t *testing.T) {
	t.Setenv("MODEL_HINT_GUARD", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	response := `{"conversation_id":"session-a","generation_id":"turn-1","text":"I implemented a useful memory workflow, tested the integration, and documented one limitation that the next session needs to know."}`
	if got := hookOutput(t, "cursor", "response", response); got != nil {
		t.Fatalf("response output: %#v", got)
	}
	if got := hookOutput(t, "cursor", "stop", `{"conversation_id":"session-a","generation_id":"turn-2","status":"completed"}`); got != nil {
		t.Fatalf("used another turn's state: %#v", got)
	}
	hookOutput(t, "cursor", "response", response)
	hookOutput(t, "cursor", "response", `{"conversation_id":"session-a","generation_id":"turn-1","text":"Done."}`)
	stop := hookOutput(t, "cursor", "stop", `{"conversation_id":"session-a","generation_id":"turn-1","status":"completed"}`)
	if !strings.Contains(stop["followup_message"].(string), "cully_log") {
		t.Fatalf("no continuity follow-up: %#v", stop)
	}
	hookOutput(t, "cursor", "response", `{"conversation_id":"session-a","generation_id":"turn-1-followup","text":"I saved the work summary in Cully after the tests completed and documented the next step."}`)
	if got := hookOutput(t, "cursor", "stop", `{"conversation_id":"session-a","generation_id":"turn-1-followup","status":"completed","loop_count":1}`); got != nil {
		t.Fatalf("Cursor stop looped: %#v", got)
	}
	hookOutput(t, "cursor", "response", `{"conversation_id":"session-a","generation_id":"turn-2","text":"I fixed the next issue, tested the behavior, and recorded the remaining deployment work for the following session."}`)
	if got := hookOutput(t, "cursor", "stop", `{"conversation_id":"session-a","generation_id":"turn-2","status":"completed","loop_count":1}`); got == nil {
		t.Fatal("Cursor did not save later task in same conversation")
	}
}

func TestContinuityHookConfigPreservesForeignHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(dir, "cursor"))
	for _, path := range []string{codexHooksPath(), cursorHooksPath()} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		initial := `{"version":1,"hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-hook"}]}],"stop":[{"command":"user-hook"}]}}`
		if path == codexHooksPath() {
			initial = `{"version":1,"hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-hook"}]},{"hooks":[{"type":"command","command":"cully _internal continuity codex stop"}]}]}}`
		}
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := installCodexContinuityHooks(); err != nil {
			t.Fatal(err)
		}
		if err := installCursorContinuityHooks(); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{codexHooksPath(), cursorHooksPath()} {
		b, err := os.ReadFile(path)
		want := 3
		if path == cursorHooksPath() {
			want = 5
		}
		if err != nil || strings.Count(string(b), "_internal continuity") != want {
			t.Fatalf("duplicate/missing continuity hooks in %s: %v %s", path, err, b)
		}
		if path == codexHooksPath() && (strings.Count(string(b), "_internal codex-signal") != 1 || !strings.Contains(string(b), `"async": true`)) {
			t.Fatalf("missing or duplicate asynchronous Codex signal hook: %s", b)
		}
	}
	if err := uninstallCodexContinuityHooks(); err != nil {
		t.Fatal(err)
	}
	if err := uninstallCursorContinuityHooks(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{codexHooksPath(), cursorHooksPath()} {
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), "user-hook") || strings.Contains(string(b), "_internal continuity") || strings.Contains(string(b), "_internal codex-signal") {
			t.Fatalf("foreign hook lost in %s: %v %s", path, err, b)
		}
	}
}

func TestContinuityHookConfigRejectsMalformedUserFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(dir, "cursor"))
	for _, path := range []string{codexHooksPath(), cursorHooksPath()} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, original := range []string{`null`, `{"hooks":[]}`, `{"hooks":{"Stop":{}}}`} {
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			var err error
			if path == codexHooksPath() {
				err = installCodexContinuityHooks()
			} else {
				err = installCursorContinuityHooks()
			}
			if err == nil {
				t.Fatalf("accepted malformed hook config %s", original)
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil || string(b) != original {
				t.Fatalf("changed malformed user hook config: %v %s", readErr, b)
			}
		}
	}
}
