package cully

import "testing"

func TestBuildSnapshotKeepsStatuslineMetrics(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	const session = "claude-test-snapshot"
	want := cullySnapshot{
		CtxSize: 200000, CtxTokens: 12000, CostUSD: 1.25,
		Model: "Claude Sonnet", TokensOut: 3000,
		LinesAdded: 24, LinesRemoved: 7,
		AdvisorDiscovery: "checked", AdvisorDiscoveryAt: "2026-10-21T12:00:00Z",
	}
	writeSnapshot(session, want)
	got := buildSnapshot(Signals{ContextUsedPct: 6}, "", session, "/project")
	if got.CtxSize != want.CtxSize || got.CtxTokens != want.CtxTokens || got.CostUSD != want.CostUSD ||
		got.Model != want.Model || got.TokensOut != want.TokensOut ||
		got.LinesAdded != want.LinesAdded || got.LinesRemoved != want.LinesRemoved ||
		got.AdvisorDiscovery != want.AdvisorDiscovery || got.AdvisorDiscoveryAt != want.AdvisorDiscoveryAt {
		t.Fatalf("advisor refresh lost statusline metrics: %+v", got)
	}
}
