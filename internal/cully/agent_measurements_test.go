package cully

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func cxCall(id, name, args string) string {
	b, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]string{"type": "function_call", "call_id": id, "name": name, "arguments": args}})
	return string(b)
}

func cxResult(id, body string) string {
	b, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]string{"type": "function_call_output", "call_id": id, "output": body}})
	return string(b)
}

func TestCodexSavingsPairsAndDeduplicatesNamedCalls(t *testing.T) {
	root := t.TempDir()
	meta := `{"type":"session_meta","payload":{"cwd":"/work/keep"}}`
	call := cxCall("lookup", "mcp__cully__cully_context", `{"limit":3}`)
	result := cxResult("lookup", `{"notes":[{"summary":"prior work"}]}`)
	edit := cxCall("edit", "functions.apply_patch", `{}`)
	writeSavingsSession(t, root, "rollout-hit.jsonl", meta, call, result, call, result,
		cxCall("read", "functions.exec_command", `{"cmd":"rg thing"}`), cxResult("read", strings.Repeat("x", 400)), edit)
	writeSavingsSession(t, root, "rollout-none.jsonl", meta,
		cxCall("read", "shell", `{}`), cxResult("read", strings.Repeat("x", 4000)), edit)
	writeSavingsSession(t, root, "rollout-miss.jsonl", meta,
		cxCall("lookup", "cully_context", `{}`), cxResult("lookup", `{"notes":[]}`), edit)
	writeSavingsSession(t, root, "rollout-other.jsonl", strings.ReplaceAll(meta, "keep", "other"), edit)
	s, err := computeAgentSavings(transcriptSources["codex"], root, "keep", time.Now().Add(-time.Hour))
	if err != nil || s.Sessions != 3 || s.Calls != 2 || s.WithHit != 1 || s.WithMiss != 1 || s.WithoutCully != 1 || s.EmptyContext != 1 {
		t.Fatalf("cohorts/cost: %+v, %v", s, err)
	}
	if s.ExploreHit != 100 || s.ExploreNone != 1000 || s.EstimatedSaved != 900 || s.ArgTokens != approxTokens(len(`{"limit":3}`))+approxTokens(2) {
		t.Fatalf("exploration/arguments: %+v", s)
	}
	t.Setenv("CULLY_AGENT", "codex")
	var out strings.Builder
	if err := RunSavings(&out, []string{"--dir", root, "--project", "keep"}); err != nil || !strings.Contains(out.String(), "Source: Codex") {
		t.Fatalf("CLI: %s, %v", out.String(), err)
	}
}

func TestMeasurementsNeverFallbackFromRunAgent(t *testing.T) {
	for _, agent := range []string{"cursor", "cursor-agent", "other"} {
		t.Setenv("CULLY_AGENT", agent)
		if _, err := measurementSource("", t.TempDir()); err == nil {
			t.Fatalf("silently measured another agent for %s", agent)
		}
	}
	t.Setenv("CULLY_AGENT", "cursor")
	if src, err := measurementSource("codex", ""); err != nil || src.Agent != "codex" {
		t.Fatalf("explicit override: %+v, %v", src, err)
	}
}

func TestSavingsDoesNotClaimSavingsWithoutObservedCalls(t *testing.T) {
	var out strings.Builder
	(Savings{Agent: "codex", Sessions: 1}).Write(&out, 14)
	if strings.Contains(out.String(), "Cully is saving tokens") || !strings.Contains(out.String(), "no named Cully calls") {
		t.Fatalf("report: %s", out.String())
	}
}

func TestCodexAdvisorReplayUsesOwnHistoryAndProject(t *testing.T) {
	root := t.TempDir()
	var lines []string
	lines = append(lines, `{"type":"session_meta","payload":{"cwd":"/work/keep"}}`)
	for i := 0; i < 60; i++ {
		id := fmt.Sprint(i)
		body := "small"
		if i > 40 {
			body = strings.Repeat("x", 12000)
		}
		lines = append(lines, cxCall(id, "exec_command", `{"cmd":"rg pattern"}`), cxResult(id, body))
	}
	writeSavingsSession(t, root, "rollout-a.jsonl", lines...)
	points, sessions, err := replayAgentTranscripts(transcriptSources["codex"], root, "keep", 30, 5)
	if err != nil || len(points) != 3 || len(sessions) != 1 {
		t.Fatalf("replay: %v, %d points, %d sessions", err, len(points), len(sessions))
	}
	if points[0].Advised || points[0].FutureWaste == 0 || !points[2].Advised || points[2].Project != "/work/keep" || !strings.Contains(points[2].Signals, "active_agent=codex") {
		t.Fatalf("checkpoints: %+v", points)
	}
	t.Setenv("CULLY_AGENT", "codex")
	var out strings.Builder
	if err := RunEval(&out, []string{"advisor", "--dir", root, "--project", "keep"}); err != nil || !strings.Contains(out.String(), "replayed 1 sessions") {
		t.Fatalf("CLI: %s, %v", out.String(), err)
	}
}

func TestCodexProbeRequiresDiscoveryRatherThanStoredOAuth(t *testing.T) {
	for _, raw := range []string{`{"data":[{"name":"cully","authStatus":"oAuth","tools":{}}]}`, `{"data":[{"name":"other","tools":{"cully_context":{}}}]}`} {
		if _, _, ok := parseCodexMCPStatus(json.RawMessage(raw)); ok {
			t.Fatalf("configuration accepted as health: %s", raw)
		}
	}
	var in strings.Builder
	h, a, ok := codexMCPStatusExchange(&in, strings.NewReader("{\"id\":1,\"result\":{}}\n"+`{"id":2,"result":{"data":[{"name":"cully","tools":{"cully_context":{}}}]}}`+"\n"))
	if !ok || h != "healthy" || a != "authenticated" || !strings.Contains(in.String(), "mcpServerStatus/list") {
		t.Fatalf("exchange: %q %q %v %s", h, a, ok, in.String())
	}
}

func TestCodexProbeStatusAndPagination(t *testing.T) {
	for _, tc := range []struct {
		status, health, auth string
		found                bool
	}{
		{"authenticationRequired", "unhealthy", "unauthenticated", true},
		{"failed", "unhealthy", "unknown", true},
		{"disabled", "", "", false},
		{"starting", "", "", false},
	} {
		raw := json.RawMessage(fmt.Sprintf(`{"data":[{"name":"cully","runtimeStatus":%q,"tools":{"cully_context":{}}}]}`, tc.status))
		h, a, found := parseCodexMCPStatus(raw)
		if h != tc.health || a != tc.auth || found != tc.found {
			t.Fatalf("%s: %s %s %v", tc.status, h, a, found)
		}
	}
	var in strings.Builder
	response := "{\"id\":1,\"result\":{}}\n" + `{"id":2,"result":{"data":[],"nextCursor":"page2"}}` + "\n" + `{"id":2,"result":{"data":[{"name":"cully-work","tools":{"cully_context":{}}}]}}` + "\n"
	if _, _, found := codexMCPStatusExchange(&in, strings.NewReader(response)); !found || !strings.Contains(in.String(), `"cursor":"page2"`) {
		t.Fatalf("pagination: %s", in.String())
	}
}

func TestReplayPairsResultsAcrossCheckpointAndIgnoresRepeatedOutput(t *testing.T) {
	lines := make([][]byte, 40)
	for i := range lines {
		lines[i] = []byte(`{"message":{"role":"assistant","content":[]}}`)
	}
	lines[11] = []byte(svUse("cross", "Read"))
	lines[12] = []byte(svResult("cross", strings.Repeat("x", 24000)))
	lines[35] = lines[12]
	points := evalSessionLines(lines, "cross.jsonl", t.TempDir(), t.TempDir())
	if len(points) != 3 || points[0].SeenWaste != 0 || points[0].FutureWaste != 6000 || points[1].FutureWaste != 0 {
		t.Fatalf("cross-checkpoint result: %+v", points)
	}
}

func TestMCPProbeFollowsSelectedAgent(t *testing.T) {
	old := agentMCPProbe
	t.Cleanup(func() { agentMCPProbe = old })
	t.Setenv("CULLY_ANALYZE_DISABLE", "0")
	for _, agent := range []string{"claude", "codex", "cursor", "custom"} {
		ctx, cancel := context.WithCancel(context.Background())
		var got string
		agentMCPProbe = func(_ context.Context, selected string) (string, string, bool) {
			got = selected
			cancel()
			return "healthy", "authenticated", true
		}
		var p mcpProber
		runMCPProbe(ctx, agent, &p)
		if got != agent {
			t.Fatalf("%s used %s", agent, got)
		}
	}
}

func TestCursorProbeRequiresSuccessfulToolDiscovery(t *testing.T) {
	old := cursorMCPTools
	t.Cleanup(func() { cursorMCPTools = old })
	for _, tc := range []struct {
		output string
		err    error
		want   bool
	}{
		{"cully_context(limit, project)", nil, true},
		{"configured: cully", nil, false},
		{"cully_context", errors.New("authentication failed"), false},
	} {
		cursorMCPTools = func(context.Context) (string, error) { return tc.output, tc.err }
		h, a, found := agentMCPProbe(context.Background(), "cursor-agent")
		if found != tc.want || found && (h != "healthy" || a != "authenticated") {
			t.Fatalf("discovery: %q %q %v", h, a, found)
		}
	}
}

func TestImpactExplainsUnsupportedAgentWithoutClaudeCost(t *testing.T) {
	t.Setenv("CULLY_AGENT", "cursor")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var out strings.Builder
	if err := RunImpact(&out, []string{"--explain"}); err != nil || !strings.Contains(out.String(), "not available for Cursor") || strings.Contains(out.String(), "your Claude transcripts") {
		t.Fatalf("%s, %v", out.String(), err)
	}
}
