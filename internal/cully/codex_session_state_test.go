package cully

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodexResumeRestoresThreadMetrics(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "")
	t.Setenv("MODEL_HINT_GUARD", "")
	cwd := t.TempDir()
	open := func(pane, thread string) {
		t.Helper()
		if err := registerPane(pane, cwd); err != nil {
			t.Fatal(err)
		}
		bindPane(cwd, thread)
	}
	event := func(thread, tool, response string) {
		t.Helper()
		RunSignalHook(strings.NewReader(fmt.Sprintf(`{"cwd":%q,"session_id":%q,"tool_name":%q,"tool_response":%s}`, cwd, thread, tool, response)))
	}
	open("first-pane", "private-native-thread")
	event("private-native-thread", "apply_patch", `{}`)
	event("private-native-thread", "mcp__cully__cully_context", `{"content":[{"text":"private-memory-content"}]}`)
	view := sessionView{Model: "GPT-6.1-Sol high", Input: "123K", Output: "8K", ContextKnown: true, ContextLeft: 72, FiveHour: "5h 82% left", Weekly: "Weekly 91% left", Fast: "Fast off", Started: time.Now()}
	saveCodexSessionView("first-pane", view)
	path := sessionStateFile("first-pane")
	clearCodexCullyStats("first-pane")
	os.Remove(paneBindingFile("first-pane"))
	os.Remove(signalFile("first-pane"))
	open("resumed-pane", "private-native-thread")
	got := readToolStats("resumed-pane")
	if got.Tools != 2 || got.Edits != 1 || got.EditsSinceCheck != 1 || got.Cully.Context != 1 || got.Cully.Health != "healthy" || got.Cully.Auth != "authenticated" {
		t.Fatal("resume lost metrics", got)
	}
	var restored sessionView
	if !restoreCodexSessionView("resumed-pane", &restored) || restored.Input != "123K" || restored.Output != "8K" || restored.ContextLeft != 72 || restored.Model != view.Model || restored.Fast != view.Fast || !restored.Started.Equal(view.Started) {
		t.Fatal("resume lost instruments", restored)
	}
	// New cumulative native totals replace the saved totals without double counting.
	restored.Input = "130K"
	saveCodexSessionView("resumed-pane", restored)
	event("private-native-thread", "mcp__cully__cully_log", `{"isError":true,"content":[{"text":"Unauthorized"}]}`)
	got = readToolStats("resumed-pane")
	if got.Tools != 3 || got.Errors != 1 || got.Cully.Calls != 2 || got.Cully.Log != 1 || got.Cully.Auth != "unauthenticated" {
		t.Fatal("resume did not continue counts", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-native-thread", "private-memory-content", "Unauthorized", cwd} {
		if strings.Contains(string(data), private) {
			t.Fatal("private payload saved", private)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("state is not private", info.Mode())
	}
	clearCodexCullyStats("resumed-pane")
	os.Remove(paneBindingFile("resumed-pane"))
	open("new-pane", "different-thread")
	if got := readToolStats("new-pane"); got.Tools != 0 || got.Cully.Calls != 0 {
		t.Fatal("different thread inherited metrics", got)
	}
}

func TestCodexExplicitResumeID(t *testing.T) {
	id := "05b59298-9154-497e-9161-59372e94882e"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"resume", id}, id}, {[]string{"resume", strings.ToUpper(id)}, id},
		{[]string{"resume"}, ""}, {[]string{"resume", "--last"}, ""},
		{[]string{"resume", "thread-name"}, ""}, {[]string{"fork", id}, ""},
	} {
		if got := codexExplicitResumeID(tc.args); got != tc.want {
			t.Fatal(tc.args, got)
		}
	}
}

func TestCodexStartupHealthProbeCountsCullyWithoutForegroundActivity(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("MODEL_HINT_GUARD", "")
	t.Setenv("CULLY_PANE_SESSION", "probe")
	cwd := t.TempDir()
	registerPane("probe", cwd)
	bindPane(cwd, "native-thread")
	RunSignalHook(strings.NewReader(`{"tool_name":"apply_patch","tool_response":{}}`))
	t.Setenv("MODEL_HINT_GUARD", "1")
	t.Setenv("CULLY_MCP_PROBE_SESSION", "probe")
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"content":[{"text":"private-probe-result"}]}}`))
	got := readToolStats("probe")
	if got.Tools != 1 || got.Edits != 1 || got.Cully.Calls != 1 || got.Cully.Startup != 1 || got.Cully.Health != "healthy" || got.Cully.Auth != "authenticated" {
		t.Fatal("probe inflated counters or lost health", got)
	}
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"isError":true,"content":[{"text":"Unauthorized"}]}}`))
	got = readToolStats("probe")
	if got.Tools != 1 || got.Cully.Calls != 2 || got.Cully.Startup != 2 || got.Cully.Auth != "unauthenticated" {
		t.Fatal("probe auth failure lost", got)
	}
	clearCodexCullyStats("probe")
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"content":[{"text":"late"}]}}`))
	if readToolStats("probe").Cully.Auth != "unauthenticated" {
		t.Fatal("late probe changed closed thread")
	}
}

func TestCodexHealthProbeCannotTrustWorkerClaims(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CODEX_HOME", root)
	t.Setenv("PATH", root)
	t.Setenv("CULLY_ANALYZE_DISABLE", "")
	registerPane("probe-claims", root)
	bindPane(root, "thread")
	script := `#!/bin/sh
[ "$CULLY_MCP_PROBE_SESSION" = probe-claims ] || exit 10
[ "$MODEL_HINT_GUARD" = 1 ] || exit 11
[ -z "$CULLY_PANE_SESSION" ] || exit 12
printf 'SOURCE|MEMORY|checked\nhealthy authenticated\n'
`
	os.WriteFile(filepath.Join(root, "codex"), []byte(script), 0o755)
	checkCodexMCPHealth(context.Background(), "probe-claims", root)
	if got := readToolStats("probe-claims"); got.Cully.Health != "" || got.Cully.Calls != 0 {
		t.Fatal("worker output invented health", got)
	}
}

func TestCodexDurableTotalsSurviveConcurrentHooksAndWindow(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "durable")
	t.Setenv("MODEL_HINT_GUARD", "")
	cwd := t.TempDir()
	if err := registerPane("durable", cwd); err != nil {
		t.Fatal(err)
	}
	bindPane(cwd, "thread")
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"structuredContent":{"notes":[]}}}`))
			saveCodexSessionView("durable", sessionView{Input: "2K"})
		}()
	}
	wg.Wait()
	// The bounded events file no longer determines a bound thread's totals.
	os.WriteFile(signalFile("durable"), []byte(strings.Repeat("O.\n", 5000)), 0o600)
	got := readToolStats("durable")
	if got.Tools != 40 || got.Cully.Calls != 40 || got.Cully.Context != 40 {
		t.Fatal("lost aggregate updates", got)
	}
	saveCodexSessionView("durable", sessionView{})
	var restored sessionView
	restoreCodexSessionView("durable", &restored)
	if restored.Input != "2K" {
		t.Fatal("missing footer erased saved tokens", restored)
	}
	clearCodexCullyStats("durable")
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"structuredContent":{"notes":[]}}}`))
	if after := readToolStats("durable"); after.Tools != 40 || after.Cully.Calls != 40 {
		t.Fatal("late hook changed closed thread", after)
	}
}

func TestCodexSessionStateRejectsMalformedBinding(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	registerPane("bad", t.TempDir())
	for _, value := range []string{"../../other", strings.Repeat("z", 32), ""} {
		os.WriteFile(paneBindingFile("bad"), []byte(value), 0o600)
		if sessionStateFile("bad") != "" {
			t.Fatal("unsafe binding accepted", value)
		}
	}
}

func TestCodexSessionStateMigratesActiveLegacyPane(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "legacy")
	t.Setenv("MODEL_HINT_GUARD", "")
	registerPane("legacy", t.TempDir())
	os.WriteFile(paneBindingFile("legacy"), []byte(codexSessionKey("thread")), 0o600)
	os.WriteFile(signalFile("legacy"), []byte("E.\nO.\n"), 0o600)
	os.WriteFile(cullyStatsFile("legacy"), []byte(`{"Calls":1,"Context":1,"Health":"healthy","Auth":"authenticated"}`), 0o600)
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_log","tool_response":{"structuredContent":{"entry":{}}}}`))
	got := readToolStats("legacy")
	if got.Tools != 3 || got.Edits != 1 || got.Cully.Calls != 2 || got.Cully.Context != 1 || got.Cully.Log != 1 {
		t.Fatal("upgrade lost or duplicated legacy metrics", got)
	}
}
