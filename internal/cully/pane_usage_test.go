package cully

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func usageTestPane(t *testing.T, agent string) *paneSession {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_RUN_ACTIVE", "1")
	t.Setenv("CULLY_PANE_SESSION", "usage-pane")
	t.Setenv("CULLY_AGENT", agent)
	t.Setenv("MODEL_HINT_GUARD", "")
	if err := registerPane("usage-pane", "/work"); err != nil {
		t.Fatal(err)
	}
	return &paneSession{id: "usage-pane", agent: paneAgent{Name: agent}, view: sessionView{Agent: agent}}
}

func TestClaudeUsageCallbackReachesPanel(t *testing.T) {
	s := usageTestPane(t, "claude")
	payload := `{"session_id":"native-primary","model":{"display_name":"Sonnet 4.6"},"context_window":{"used_percentage":27.5,"total_input_tokens":54000,"total_output_tokens":1200},"rate_limits":{"five_hour":{"used_percentage":0}},"transcript_path":"private-transcript","private":"never-store-this"}`
	var out bytes.Buffer
	RunUsageFeed(strings.NewReader(payload), &out, "cat")
	if out.String() != payload {
		t.Fatal("existing native status line did not receive its identical input")
	}
	s.readFooter()
	if s.view.Model != "Sonnet 4.6" || !s.view.ContextKnown || s.view.ContextLeft != 72 || s.view.Input != "54000" || s.view.Output != "1200" || s.view.FiveHour != "5h 100% left" || s.view.Weekly != "" {
		t.Fatalf("native metrics did not reach panel: %+v", s.view)
	}
	rows, _ := compactInstrumentRows(160, toolStats{}, s.view)
	plain := normalizedCodexPanel(strings.Join(rows, "\n"))
	for _, want := range []string{"Sonnet 4.6", "28% used", "54000", "1200"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("panel omits %q: %s", want, plain)
		}
	}
	b, err := os.ReadFile(paneUsageFile(s.id))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"native-primary", "private-transcript", "never-store-this", "cat"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("usage state retained native payload or command: %q", secret)
		}
	}
	if info, _ := os.Stat(paneUsageFile(s.id)); info.Mode().Perm() != 0o600 {
		t.Fatal("usage state must be private")
	}
	// A new measured total replaces the old one; it is never summed.
	RunUsageFeed(strings.NewReader(`{"session_id":"native-primary","context_window":{"used_percentage":30,"total_input_tokens":60000,"total_output_tokens":0}}`), &out, "")
	s.readFooter()
	if s.view.Input != "60000" || s.view.Output != "0" || s.view.FiveHour != "" {
		t.Fatal("native totals accumulated or absent rate limit was retained", s.view)
	}
	// Null context after /compact means unknown, not zero or the previous fill.
	RunUsageFeed(strings.NewReader(`{"session_id":"native-primary","context_window":{"used_percentage":null}}`), &out, "")
	s.readFooter()
	if s.view.ContextKnown || s.view.Input != "" || s.view.Output != "" {
		t.Fatal("missing native data retained stale measurements", s.view)
	}
}

func TestUsageIsBoundToLivePrimaryPane(t *testing.T) {
	s := usageTestPane(t, "claude")
	acceptUsageSession("claude", "primary")
	for _, test := range []struct {
		name, active, guard, native string
	}{
		{"unwrapped", "", "", "primary"},
		{"advisor", "1", "1", "primary"},
		{"another native session", "1", "", "subagent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CULLY_RUN_ACTIVE", test.active)
			t.Setenv("MODEL_HINT_GUARD", test.guard)
			RunUsageFeed(strings.NewReader(fmt.Sprintf(`{"session_id":%q,"model":{"display_name":"wrong"}}`, test.native)), &bytes.Buffer{}, "")
			s.readFooter()
			if s.view.Model != "" {
				t.Fatal("foreign native session polluted the panel")
			}
		})
	}
	os.Remove(paneRegistrationFile(s.id))
	RunUsageFeed(strings.NewReader(`{"session_id":"primary","model":{"display_name":"late"}}`), &bytes.Buffer{}, "")
	if _, err := os.Stat(paneUsageFile(s.id)); !os.IsNotExist(err) {
		t.Fatal("late callback revived a closed pane")
	}
}

func TestUsageRejectsMalformedAndOutOfRangeMeasurements(t *testing.T) {
	s := usageTestPane(t, "claude")
	for _, payload := range []string{"not JSON", strings.Repeat("x", (1<<20)+1), `{"session_id":"primary","model":{"display_name":"bad\u001b[2J"},"context_window":{"used_percentage":101,"total_input_tokens":-1}}`} {
		RunUsageFeed(strings.NewReader(payload), &bytes.Buffer{}, "")
		s.readFooter()
		if s.view.Model != "" || s.view.ContextKnown || s.view.Input != "" {
			t.Fatal("invalid native metrics were rendered", s.view)
		}
	}
}

func TestUsageSessionStartClearsPreviousNativeSession(t *testing.T) {
	s := usageTestPane(t, "claude")
	RunUsageFeed(strings.NewReader(`{"session_id":"first","model":{"display_name":"old-model"},"context_window":{"used_percentage":80,"total_input_tokens":160000}}`), &bytes.Buffer{}, "")
	s.readFooter()
	RunContinuityHook("claude", "start", strings.NewReader(`{"session_id":"second"}`), &bytes.Buffer{})
	s.readFooter()
	if s.view.ContextKnown || s.view.Model != "" || s.view.Input != "" {
		t.Fatal("a new native session inherited the old instruments", s.view)
	}
	RunUsageFeed(strings.NewReader(`{"session_id":"second","model":{"display_name":"new-model"}}`), &bytes.Buffer{}, "")
	RunUsageFeed(strings.NewReader(`{"session_id":"first","model":{"display_name":"late-old-model"}}`), &bytes.Buffer{}, "")
	s.readFooter()
	if s.view.Model != "new-model" {
		t.Fatal("native session switch rejected its new feed or accepted a late old feed", s.view)
	}
	// Simulate an old callback whose write completes after the new start. Its
	// key must also be checked when reading, not only before it writes.
	oldModel := "racing-old-model"
	writeCodexPrivateJSON(paneUsageFile(s.id), paneUsage{Agent: "claude", NativeKey: nativeUsageKey("claude", "first"), Update: InstrumentUpdate{Model: &oldModel}})
	s.readFooter()
	if s.view.Model != "new-model" {
		t.Fatal("a late old-session write bypassed read-time binding validation", s.view)
	}
}

func TestCursorCompactionUsageIsHistoricalAndDoesNotCountAsTool(t *testing.T) {
	s := usageTestPane(t, "cursor")
	RunPaneSignalHook("cursor", strings.NewReader(`{"hook_event_name":"preCompact","conversation_id":"cursor-primary","model_id":"model-current","context_usage_percent":85,"context_tokens":108800,"context_window_size":128000}`))
	s.readFooter()
	if s.view.Model != "model-current" || s.view.ContextKnown || s.view.ContextTokens != "108800" || !s.view.ContextAtCompaction || s.view.Input != "" || s.view.Output != "" {
		t.Fatal("Cursor context sample was lost or mislabeled as input/output/live usage", s.view)
	}
	rows, _ := compactInstrumentRows(160, toolStats{}, s.view)
	plain := normalizedCodexPanel(strings.Join(rows, "\n"))
	if !strings.Contains(plain, "85% used before compaction") || !strings.Contains(plain, "108800") {
		t.Fatal("panel does not identify the compaction sample", plain)
	}
	if got := readToolStats(s.id); got.Tools != 0 {
		t.Fatal("compaction metadata inflated the tool counter", got)
	}
	// Older hooks can omit model; they still invalidate a pre-compaction sample.
	RunPaneSignalHook("cursor", strings.NewReader(`{"hook_event_name":"afterShellExecution","conversation_id":"cursor-primary","command":"pwd"}`))
	s.readFooter()
	if s.view.ContextAtCompaction || s.view.ContextTokens != "" || s.view.ContextKnown || s.view.Model != "model-current" {
		t.Fatal("later activity left a stale pre-compaction sample", s.view)
	}
}

func TestUsageLaunchPreservesUserChoices(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, ".codex"))
	if got := paneUsageArgs("codex", []string{"explain tui.status_line"}); len(got) < 3 {
		t.Fatal("prompt text was mistaken for a native config override", got)
	}
	for agent, args := range map[string][]string{
		"codex":  {"-c", `tui.status_line=["current-dir"]`, "--model", "user-model"},
		"claude": {"--settings", `{"statusLine":null,"permissions":{"defaultMode":"plan"}}`},
		"cursor": {"--model", "cursor-model"},
	} {
		if got := paneUsageArgs(agent, args); !reflect.DeepEqual(got, args) {
			t.Fatalf("%s explicit arguments changed: %v", agent, got)
		}
	}
	if err := os.MkdirAll(CodexConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "[tui]\nstatus_line = []\n"
	if err := os.WriteFile(codexConfigPath(), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := paneUsageArgs("codex", nil); len(got) != 0 {
		t.Fatal("a saved disabled footer was overridden", got)
	}
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	saved := `{"statusLine":{"type":"command","command":"cat","padding":3},"permissions":{"defaultMode":"plan"}}`
	path := filepath.Join(ConfigDir(), "settings.json")
	if err := os.WriteFile(path, []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	args := paneUsageArgs("claude", []string{"--model", "user-model"})
	var overlay map[string]any
	if len(args) != 4 || args[0] != "--model" || args[1] != "user-model" || args[2] != "--settings" || json.Unmarshal([]byte(args[3]), &overlay) != nil {
		t.Fatal("session-only overlay lost native arguments", args)
	}
	status := overlay["statusLine"].(map[string]any)
	if !strings.Contains(status["command"].(string), "--forward 'cat'") || status["padding"] != float64(3) {
		t.Fatal("existing native callback/appearance not preserved", status)
	}
	if b, _ := os.ReadFile(path); string(b) != saved {
		t.Fatal("saved Claude settings changed")
	}
	if b, _ := os.ReadFile(codexConfigPath()); string(b) != config {
		t.Fatal("saved Codex settings changed")
	}
}
