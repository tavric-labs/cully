package cully

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
)

// cullySnapshot bridges analyze → statusline (phase, plan, memos) for ONE
// session. Session/Cwd stamp the file so terminal commands can resolve which
// session they belong to; ctx/cost fields carry the session's own instruments
// so classification never reads another session's context pressure.
type cullySnapshot struct {
	Session            string  `json:"session,omitempty"`
	Cwd                string  `json:"cwd,omitempty"`
	Phase              string  `json:"phase"`
	CostIndex          string  `json:"cost_index"`
	ContextUsedPct     int     `json:"context_used_pct"`
	CtxSize            int64   `json:"ctx_size,omitempty"`
	CtxTokens          int64   `json:"ctx_tokens,omitempty"`
	CostUSD            float64 `json:"cost_usd,omitempty"`
	Model              string  `json:"model,omitempty"`
	TokensOut          int64   `json:"tokens_out,omitempty"`
	LinesAdded         int64   `json:"lines_added,omitempty"`
	LinesRemoved       int64   `json:"lines_removed,omitempty"`
	Rate5hPct          int     `json:"rate_5h_pct"`
	Rate7dPct          int     `json:"rate_7d_pct"`
	Searches           int     `json:"searches"`
	ToolErrors         int     `json:"tool_errors,omitempty"`
	NotFoundErrors     int     `json:"not_found_errors,omitempty"`
	GraphifyGraph      bool    `json:"graphify_graph"`
	ToolTop            string  `json:"tool_top"`
	PlanAnchor         string  `json:"plan_anchor"`
	PlanDeviation      string  `json:"plan_deviation"`
	PendingSuggestions int     `json:"pending_suggestions"`
	AdvisorOK          bool    `json:"advisor_ok"`
	AdvisorAgent       string  `json:"advisor_agent,omitempty"`
	AdvisorAt          string  `json:"advisor_at,omitempty"`
	AdvisorMemory      string  `json:"advisor_memory,omitempty"`
	AdvisorResearch    string  `json:"advisor_research,omitempty"`
	AdvisorDiscovery   string  `json:"advisor_discovery,omitempty"`
	AdvisorDiscoveryAt string  `json:"advisor_discovery_at,omitempty"`
	AdvisorFailure     string  `json:"advisor_failure,omitempty"`
	AdvisorDeferred    string  `json:"advisor_deferred,omitempty"`
}

func writeSnapshot(session string, s cullySnapshot) {
	if session == "" {
		return
	}
	s.Session = session
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.WriteFile(sessionSnapshotFile(session), b, 0o644)
}

func readSnapshot(session string) cullySnapshot {
	if session == "" {
		return cullySnapshot{CostIndex: costIndex()}
	}
	b, err := os.ReadFile(sessionSnapshotFile(session))
	if err != nil {
		return cullySnapshot{CostIndex: costIndex()}
	}
	var s cullySnapshot
	if json.Unmarshal(b, &s) != nil {
		return cullySnapshot{CostIndex: costIndex()}
	}
	if s.CostIndex == "" {
		s.CostIndex = costIndex()
	}
	return s
}

func buildSnapshot(s Signals, prReview, session, cwd string) cullySnapshot {
	phase := detectPhase(s, prReview)
	anchor, deviation := inferPlan(s.RecentPrompts)
	previous := readSnapshot(session)
	return cullySnapshot{
		Session:            session,
		Cwd:                cwd,
		Phase:              string(phase),
		CostIndex:          costIndex(),
		ContextUsedPct:     s.ContextUsedPct,
		CtxSize:            previous.CtxSize,
		CtxTokens:          previous.CtxTokens,
		CostUSD:            previous.CostUSD,
		Model:              previous.Model,
		TokensOut:          previous.TokensOut,
		LinesAdded:         previous.LinesAdded,
		LinesRemoved:       previous.LinesRemoved,
		Rate5hPct:          s.Rate5hPct,
		Rate7dPct:          s.Rate7dPct,
		Searches:           s.Searches,
		ToolErrors:         s.ToolErrors,
		NotFoundErrors:     s.NotFoundErrors,
		GraphifyGraph:      s.GraphifyGraph,
		ToolTop:            topTools(s.ToolHistogram, 3),
		PlanAnchor:         anchor,
		PlanDeviation:      deviation,
		PendingSuggestions: len(readSuggestions(session)),
		AdvisorOK:          previous.AdvisorOK,
		AdvisorAgent:       previous.AdvisorAgent,
		AdvisorAt:          previous.AdvisorAt,
		AdvisorMemory:      previous.AdvisorMemory,
		AdvisorResearch:    previous.AdvisorResearch,
		AdvisorDiscovery:   previous.AdvisorDiscovery,
		AdvisorDiscoveryAt: previous.AdvisorDiscoveryAt,
		AdvisorFailure:     previous.AdvisorFailure,
		AdvisorDeferred:    previous.AdvisorDeferred,
	}
}

func topTools(h map[string]int, n int) string {
	if len(h) == 0 {
		return "none"
	}
	type kv struct {
		k string
		v int
	}
	ranked := make([]kv, 0, len(h))
	for k, v := range h {
		ranked = append(ranked, kv{k, v})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].v == ranked[j].v {
			return ranked[i].k < ranked[j].k
		}
		return ranked[i].v > ranked[j].v
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	parts := make([]string, len(ranked))
	for i, r := range ranked {
		parts[i] = r.k + ":" + strconv.Itoa(r.v)
	}
	return strings.Join(parts, " ")
}

// maybeChime rings the terminal bell once when this session's context crosses
// into WARN. Chime state is per-session so a concurrent session's context level
// cannot suppress (or double-fire) this one's alert.
func maybeChime(session string, ctxPct int) string {
	if os.Getenv("CULLY_ALERT_CHIME") != "1" || session == "" {
		return ""
	}
	prev := 0
	if b, err := os.ReadFile(sessionChimeFile(session)); err == nil {
		prev, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	_ = os.WriteFile(sessionChimeFile(session), []byte(strconv.Itoa(ctxPct)), 0o644)
	if prev < 90 && ctxPct >= 90 {
		return "\a"
	}
	return ""
}
