package cully

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLookupHitDetectionOnlyCountsRetrievalWins(t *testing.T) {
	hit := json.RawMessage(`{"content":[{"type":"text","text":"{\"notes\":[{\"summary\":\"did it\"}]}"}]}`)
	empty := json.RawMessage(`{"content":[{"type":"text","text":"{\"has_more\":false,\"notes\":[],\"source\":\"text\"}"}]}`)
	if isLookup, ok, tokens := cullyResponseHit("cully_context", hit); !isLookup || !ok || tokens == 0 {
		t.Fatalf("hit missed: %v %v %d", isLookup, ok, tokens)
	}
	if isLookup, ok, _ := cullyResponseHit("cully_context", empty); !isLookup || ok {
		t.Fatal("an empty lookup is not a win")
	}
	if isLookup, _, _ := cullyResponseHit("cully_log", hit); isLookup {
		t.Fatal("a write is not a retrieval")
	}
}

func TestImpactLedgerRoundTripAndSummary(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	recordImpact("s1", impactMemoryHit, "cully_context", 120)
	recordImpact("s1", impactMemoryEmpty, "cully_search", 10)
	recordImpact("s1", impactAnalysisSkipped, "", 1000)
	recordImpact("s1", impactAnalysisSkipped, "", 1000)
	recordImpact("s2", impactLoopCaught, "", 0)
	recordImpact("s1", impactWasteFlagged, "", 6000)
	recordImpact("s1", impactWasteFlagged, "", 9000) // a session's running total; keep the largest
	events := readImpactEvents(impactLedgerFile(), time.Now().Add(-time.Hour))
	if len(events) != 7 {
		t.Fatalf("events=%d", len(events))
	}
	im := SummarizeImpact(events, 14, 800)
	if im.MemoryHits != 1 || im.MemoryEmpty != 1 || im.AnalysesSkipped != 2 || im.LoopsCaught != 1 || im.Sessions != 2 {
		t.Fatalf("%+v", im)
	}
	if im.WasteSessions != 1 || im.WasteTokensFlagged != 9000 {
		t.Fatalf("waste %d/%d", im.WasteSessions, im.WasteTokensFlagged)
	}
	if im.MemoryTokensSaved != 800 || im.TotalSaved != 2800 || im.TotalCost != 130 || im.Net != 2670 {
		t.Fatalf("totals %+v", im)
	}
	if old := readImpactEvents(impactLedgerFile(), time.Now().Add(time.Hour)); len(old) != 0 {
		t.Fatal("age filter ignored")
	}
}

func TestImpactDoesNotClaimMemorySavingWithoutEvidence(t *testing.T) {
	im := SummarizeImpact([]impactEvent{{Kind: impactMemoryHit, Tokens: 100, At: time.Now()}}, 14, 0)
	if im.MemoryTokensSaved != 0 || !strings.Contains(im.SavedBasis, "not claimed") {
		t.Fatalf("claimed an unproven saving: %+v", im)
	}
	var out strings.Builder
	im.Write(&out, true)
	if !strings.Contains(out.String(), "Net") || !strings.Contains(out.String(), "Recent events") {
		t.Fatalf("report: %s", out.String())
	}
	if im.Net >= 0 || !strings.Contains(out.String(), "costing more") {
		t.Fatalf("a cost with no proven saving must read as a loss: %s", out.String())
	}
}

func TestImpactPanelLinesShowWinsNotToolCalls(t *testing.T) {
	im := SummarizeImpact([]impactEvent{
		{Kind: impactMemoryHit, Tokens: 100, At: time.Now()},
		{Kind: impactAnalysisSkipped, Tokens: 1350, At: time.Now()},
		{Kind: impactLoopCaught, At: time.Now()},
	}, 14, 2000)
	text := strings.Join(im.Lines(), "\n")
	for _, want := range []string{"Memory used 1×", "≈2.0k tokens saved", "Analysis avoided 1×", "Loops caught 1", "Net +3.2k"} {
		if !strings.Contains(text, want) {
			t.Errorf("panel lacks %q:\n%s", want, text)
		}
	}
	if none := strings.Join(SummarizeImpact(nil, 14, 0).Lines(), "\n"); !strings.Contains(none, "Memory not used yet") {
		t.Fatalf("empty ledger: %s", none)
	}
}

func TestImpactLedgerTrimsWhenLarge(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := impactLedgerFile()
	line := `{"at":"2026-10-20T00:00:00Z","kind":"memory_hit","tokens":1}` + "\n"
	big := strings.Repeat(line, impactLedgerMax/len(line)+10)
	if err := writeFileForTest(path, big); err != nil {
		t.Fatal(err)
	}
	recordImpact("s", impactLoopCaught, "", 0)
	events := readImpactEvents(path, time.Time{})
	if len(events) == 0 || len(events) >= len(big)/len(line) {
		t.Fatalf("ledger not trimmed: %d events", len(events))
	}
	if events[len(events)-1].Kind != impactLoopCaught {
		t.Fatal("newest event lost in trim")
	}
}

func writeFileForTest(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }

func TestSignalHookRecordsMemoryLookupsInLedger(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_PANE_SESSION", "pane-impact")
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"content":[{"type":"text","text":"{\"notes\":[{\"summary\":\"prior work\"}]}"}]}}`))
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_context","tool_response":{"content":[{"type":"text","text":"{\"notes\":[]}"}]}}`))
	RunSignalHook(strings.NewReader(`{"tool_name":"mcp__cully__cully_log","tool_response":{"content":[{"type":"text","text":"{}"}]}}`))
	im := SummarizeImpact(readImpactEvents(impactLedgerFile(), time.Now().Add(-time.Hour)), 1, 0)
	if im.MemoryHits != 1 || im.MemoryEmpty != 1 {
		t.Fatalf("hits=%d empty=%d", im.MemoryHits, im.MemoryEmpty)
	}
}

func TestSkippedAnalysisIsCreditedWithTheTokensItAvoided(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_ANALYZE_CADENCE", "")
	for i := 0; i < 3; i++ {
		bumpCounter("s-skip") // the early turns run; the fourth is skipped
	}
	RunAnalyze(strings.NewReader(`{"transcript_path":"/nonexistent.jsonl","cwd":"/tmp","session_id":"s-skip"}`))
	im := SummarizeImpact(readImpactEvents(impactLedgerFile(), time.Now().Add(-time.Hour)), 1, 0)
	if im.AnalysesSkipped != 1 || im.AnalysisTokensAvoided < typicalSignalTokens {
		t.Fatalf("skipped=%d avoided=%d", im.AnalysesSkipped, im.AnalysisTokensAvoided)
	}
}

func TestPanelLeadsWithWinsInsteadOfLastToolCall(t *testing.T) {
	isolateWorkflow(t)
	stats := toolStats{Cully: cullyStats{Calls: 12, Health: "healthy", Auth: "authenticated", CheckedAt: "2026-10-07T12:34:56Z"}}
	stats.Impact = SummarizeImpact([]impactEvent{
		{Kind: impactMemoryHit, Tokens: 100, At: time.Now()},
		{Kind: impactAnalysisSkipped, Tokens: 1350, At: time.Now()},
		{Kind: impactLoopCaught, At: time.Now()},
	}, 0, 0)
	view := sessionView{Project: "/work/cully", Daemon: true, Started: time.Now()}
	for _, cols := range []int{149, 80} {
		text := normalizedCodexPanel(strings.Join(compactSessionStatusRows(cols, 43, nil, stats, view), "\n"))
		for _, want := range []string{"Cully impact", "Memory used 1×", "Analysis avoided 1×", "Loops caught 1", "Net +", "MCP 12 calls"} {
			if !strings.Contains(text, want) {
				t.Errorf("cols=%d panel lacks %q:\n%s", cols, want, text)
			}
		}
		if strings.Contains(text, "2026-10-07T12:34:56Z") || strings.Contains(text, "Last observed") {
			t.Errorf("cols=%d panel still shows the last tool call timestamp", cols)
		}
	}
}

func TestImpactSinceCachesUntilTheLedgerChanges(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	since := time.Now().Add(-time.Hour)
	if im := ImpactSince(since); im.MemoryHits != 0 {
		t.Fatalf("no ledger yet: %+v", im)
	}
	recordImpact("s", impactMemoryHit, "cully_context", 50)
	if im := ImpactSince(since); im.MemoryHits != 1 {
		t.Fatalf("new event not seen: %+v", im)
	}
	recordImpact("s", impactMemoryHit, "cully_context", 50)
	if im := ImpactSince(since); im.MemoryHits != 2 {
		t.Fatalf("second event not seen: %+v", im)
	}
}

func TestImpactReportDoesNotClaimSavingsWhenNothingWasRecorded(t *testing.T) {
	var out strings.Builder
	SummarizeImpact(nil, 7, 0).Write(&out, false)
	if strings.Contains(out.String(), "is saving tokens") || !strings.Contains(out.String(), "nothing recorded yet") {
		t.Fatalf("an empty ledger must not read as a win:\n%s", out.String())
	}
}

func TestImpactExplainShowsFormulaAndLimits(t *testing.T) {
	events := []impactEvent{
		{Kind: impactAnalysisSkipped, Tokens: 1000, At: time.Now()},
		{Kind: impactAnalysisSkipped, Tokens: 3000, At: time.Now()},
		{Kind: impactWasteFlagged, Session: "s1", Tokens: 9000, At: time.Now()},
	}
	var out strings.Builder
	SummarizeImpact(events, 14, 0).WriteExplain(&out)
	for _, want := range []string{"Net +4.0k = saved 4.0k - cost 0", "2 × ≈2.0k tokens", "(estimated)", "no saving claimed", "Cost is 0 because no Cully memory lookup", "not a saving", "not subtracted"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("explanation lacks %q:\n%s", want, out.String())
		}
	}
	var empty strings.Builder
	SummarizeImpact(nil, 14, 0).WriteExplain(&empty)
	if !strings.Contains(empty.String(), "none recorded") {
		t.Fatalf("empty ledger: %s", empty.String())
	}
}
