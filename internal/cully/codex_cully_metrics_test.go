package cully

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestCullyMetricsCountOnlyObservedSessionTools(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "mcp-pane")
	t.Setenv("MODEL_HINT_GUARD", "")
	if err := registerPane("mcp-pane", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mcp__cully__cully_log", "functions.mcp__cully__cully_context", "mcp__cully.cully_recall", "mcp.cully.cully_search", "mcp__cully__cully_get", "mcp__cully__cully_projects"} {
		input := fmt.Sprintf(`{"tool_name":%q,"tool_input":{"query":"private-query"},"tool_response":{"content":[{"type":"text","text":"private-memory-body"}]}}`, name)
		RunSignalHook(strings.NewReader(input))
	}
	RunSignalHook(strings.NewReader(`{"tool_name":"functions.exec","tool_input":{"code":"await tools.mcp__cully__cully_log({summary:'not necessarily executed'})"},"tool_response":{}}`))
	stats := readToolStats("mcp-pane")
	if stats.Cully.Calls != 6 || stats.Cully.Log != 1 || stats.Cully.Context != 1 || stats.Cully.Recall != 1 || stats.Cully.Search != 1 || stats.Cully.Get != 1 || stats.Cully.Other != 1 {
		t.Fatal(stats.Cully)
	}
	if stats.Cully.Health != "healthy" || stats.Cully.Auth != "authenticated" || stats.Cully.CheckedAt == "" {
		t.Fatal(stats.Cully)
	}
	data, _ := os.ReadFile(cullyStatsFile("mcp-pane"))
	if strings.Contains(string(data), "private-") || strings.Contains(string(data), "not necessarily") {
		t.Fatal("payload persisted", string(data))
	}
	info, _ := os.Stat(cullyStatsFile("mcp-pane"))
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	if got := readCodexCullyStats("another-pane"); got.Calls != 0 {
		t.Fatal("cross-session metrics", got)
	}
	// These counters are cumulative, separate from the rolling generic window.
	os.WriteFile(signalFile("mcp-pane"), []byte(strings.Repeat("O.\n", 5000)), 0o600)
	if got := readToolStats("mcp-pane"); got.Cully.Calls != 6 {
		t.Fatal("session totals lost", got.Cully)
	}
}

func TestCullyMetricsResponseEvidenceAndRecovery(t *testing.T) {
	for _, tc := range []struct{ raw, health, auth string }{
		{`{}`, "unknown", "unknown"},
		{`"Unauthorized"`, "unknown", "unknown"},
		{`{"content":[{"text":"Discuss unauthenticated errors"}]}`, "healthy", "authenticated"},
		{`{"isError":true,"content":[{"text":"memory service unavailable"}]}`, "unhealthy", "unknown"},
		{`{"isError":true,"content":[{"text":"Authentication required"}]}`, "unhealthy", "unauthenticated"},
		{`{"status_code":401}`, "unhealthy", "unauthenticated"},
		{`{"error":{"code":-32001,"message":"Unauthorized"}}`, "unhealthy", "unauthenticated"},
		{`{"structuredContent":{"notes":[]}}`, "healthy", "authenticated"},
		{`[{"type":"text","text":"{\"entries\":[]}"}]`, "healthy", "authenticated"},
		{`[]`, "unknown", "unknown"},
		{`"{\"entries\":[]}"`, "healthy", "authenticated"},
	} {
		health, auth := cullyResponseState(json.RawMessage(tc.raw))
		if health != tc.health || auth != tc.auth {
			t.Fatalf("%s: %s/%s", tc.raw, health, auth)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if err := registerPane("recovery", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	recordCodexCullyCall("recovery", "cully_context", "unhealthy", "unauthenticated")
	if got := readCodexCullyStats("recovery"); got.Auth != "unauthenticated" {
		t.Fatal(got)
	}
	recordCodexCullyCall("recovery", "cully_context", "healthy", "authenticated")
	if got := readCodexCullyStats("recovery"); got.Calls != 2 || got.Health != "healthy" || got.Auth != "authenticated" {
		t.Fatal("recovery failed", got)
	}
}

func TestCullyMetricsConcurrentHooksAndEndedPane(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if err := registerPane("concurrent", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); recordCodexCullyCall("concurrent", "cully_log", "healthy", "authenticated") }()
	}
	wg.Wait()
	if got := readCodexCullyStats("concurrent"); got.Calls != 40 || got.Log != 40 {
		t.Fatal("lost concurrent counts", got)
	}
	clearCodexCullyStats("concurrent")
	recordCodexCullyCall("concurrent", "cully_log", "healthy", "authenticated")
	if _, err := os.Stat(cullyStatsFile("concurrent")); !os.IsNotExist(err) {
		t.Fatal("closed pane recreated")
	}
}

func TestCullyMetricsShutdownDuringConcurrentEvents(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if err := registerPane("closing", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); recordCodexCullyCall("closing", "cully_context", "healthy", "authenticated") }()
	}
	clearCodexCullyStats("closing")
	wg.Wait()
	if _, err := os.Stat(cullyStatsFile("closing")); !os.IsNotExist(err) {
		t.Fatal("late hook recreated metrics", err)
	}
}

func TestCullyMetricsBackgroundWorkerDoesNotCount(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "worker")
	t.Setenv("MODEL_HINT_GUARD", "1")
	if err := registerPane("worker", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"content":[{"text":"success"}]}}`))
	if got := readCodexCullyStats("worker"); got.Calls != 0 {
		t.Fatal("worker activity counted", got)
	}
}
