//go:build !windows

package cully

import (
	"fmt"
	"strings"
	"time"
)

// Combine native context pressure with the bounded hook counters. Advice is
// limited to what these two sources actually observe, never a transcript.
func statsAdviceWithStatus(stats toolStats, view sessionView) []string {
	var advice []string
	if stats.Cully.Auth == "unauthenticated" {
		advice = append(advice, "WARN|Cully MCP needs authentication. Reconnect Cully in this coding agent before using memory tools.")
	} else if stats.Cully.Health == "unhealthy" {
		advice = append(advice, "CAUT|The last Cully MCP call failed. Inspect the connection or memory service before retrying.")
	}
	if view.ContextKnown && view.ContextLeft <= 10 {
		advice = append(advice, fmt.Sprintf("WARN|Context is nearly full (%d%% left). Use /compact before continuing a long task.", view.ContextLeft))
	} else if view.ContextKnown && view.ContextLeft <= 25 {
		advice = append(advice, fmt.Sprintf("CAUT|Context is getting tight (%d%% left). Save a concise handoff and consider /compact.", view.ContextLeft))
	}
	workflow := statsAdvice(stats)
	if len(advice) > 0 && stats.Tools > 0 && len(workflow) == 1 && strings.HasPrefix(workflow[0], "MEMO|") {
		return advice
	}
	if len(advice) == 0 && !view.ContextKnown && stats.Tools > 0 && len(workflow) == 1 && strings.HasPrefix(workflow[0], "MEMO|") {
		return []string{"MEMO|No tool-workflow warning in observed signals. Context pressure is unavailable: " + missingUsageText(view, true) + "."}
	}
	return append(advice, workflow...)
}

func advisorSignals(stats toolStats, view sessionView) string {
	ctx := "context_used_pct=unknown"
	if view.ContextKnown {
		ctx = fmt.Sprintf("context_used_pct=%d", 100-view.ContextLeft)
	}
	agent := fallback(view.Agent, "codex")
	return fmt.Sprintf("agent=%s\n%s\ntools=%d searches=%d edits=%d checks=%d tool_errors=%d edits_since_check=%d\ncully_mcp_calls=%d cully_log_calls=%d cully_context_calls=%d cully_recall_calls=%d cully_search_calls=%d cully_get_calls=%d cully_mcp_health=%s cully_mcp_auth=%s\nsignal_scope=aggregate_counters_and_native_usage; task_intent=unknown\nsignal_coverage=%s\nunknown_check_outcomes=%d\nsignal_history_partial=%t\njournal_coverage=%s\n", agent, ctx, stats.Tools, stats.Searches, stats.Edits, stats.Checks, stats.Errors, stats.EditsSinceCheck, stats.Cully.Calls, stats.Cully.Log, stats.Cully.Context, stats.Cully.Recall, stats.Cully.Search, stats.Cully.Get, fallback(stats.Cully.Health, "unknown"), fallback(stats.Cully.Auth, "unknown"), signalCoverage(agent, stats.Tools), stats.UnknownChecks, stats.Partial, fallback(view.JournalCoverage, "no known history gap; completeness unconfirmed")) + view.Terminal.signals()
}

func combinedAdvice(session string, stats toolStats, view sessionView) []string {
	local := statsAdviceWithStatus(stats, view)
	cwd := currentDir()
	loops, events := sessionLoops(cwd, session)
	if n := guardCheckpoint(guardTaskEvents(session, events)); n.text != "" {
		local = append(local, "CAUT|"+n.text)
	}
	loopLines, hints := loops.Advice(), workflowHints(cwd, events)
	if len(loopLines)+len(hints) > 0 {
		// A nominal memo is redundant with an actual observation.
		if len(local) == 1 && strings.HasPrefix(local[0], "MEMO|") {
			local = nil
		}
		merged := append([]string{}, loopLines...)
		merged = append(merged, local...)
		local = append(merged, hints...)
	}
	deep := readSuggestionsLimit(session, maxReportLines)
	if len(deep) > 0 {
		// A nominal local memo is redundant with actual deeper findings.
		if len(local) == 1 && strings.HasPrefix(local[0], "MEMO|") {
			local = nil
		}
		for _, line := range deep {
			found := false
			for _, existing := range local {
				if line == existing {
					found = true
					break
				}
			}
			if !found {
				local = append(local, line)
			}
		}
	}
	snap := readSnapshot(session)
	if snap.AdvisorDeferred != "" {
		local = append(local, "MEMO|Automatic advisor deferred: "+snap.AdvisorDeferred+"; local warnings remain active.")
	}
	if snap.AdvisorAt == "" {
		return append(local, "MEMO|🔎 Deeper analysis is awaiting session activity.")
	}
	when, err := time.Parse(time.RFC3339, snap.AdvisorAt)
	age := "unknown age"
	if err == nil {
		age = fmt.Sprintf("%ds ago", int(time.Since(when).Seconds()))
	}
	mode := "completed"
	if !snap.AdvisorOK {
		mode = "unavailable (" + fallback(snap.AdvisorFailure, "launch or analysis failed") + "); local warnings active"
	}
	return append(local, fmt.Sprintf("MEMO|🔎 %s advisor %s · %s · memory %s · research %s (agent-reported)", snap.AdvisorAgent, mode, age, fallback(snap.AdvisorMemory, "unconfirmed"), fallback(snap.AdvisorResearch, "unconfirmed")))
}
