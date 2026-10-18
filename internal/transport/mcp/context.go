package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
)

// ContextInput requests a small, task-oriented preview of owned records. The
// underlying search/recent request still passes through memory.Service so its
// validation and owner scoping are identical to the full MCP tools.
type ContextInput struct {
	Query      string  `json:"query,omitempty"`
	ProjectURL *string `json:"project_url,omitempty"`
	SessionRef *string `json:"session_ref,omitempty"`
	Section    *string `json:"section,omitempty"`
	Mode       string  `json:"mode,omitempty"`  // text, semantic, or recent
	Since      string  `json:"since,omitempty"` // recent mode only; inclusive RFC3339
	Until      string  `json:"until,omitempty"` // recent mode only; exclusive RFC3339
	Limit      int     `json:"limit,omitempty"`
}

type ContextNote struct {
	ID             string  `json:"id"`
	OccurredAt     string  `json:"occurred_at"`
	ProjectURL     *string `json:"project_url,omitempty"`
	Date           string  `json:"date"`
	EntryType      string  `json:"entry_type"`
	Assistant      string  `json:"assistant"`
	SessionRef     *string `json:"session_ref,omitempty"`
	AgentSessionID *string `json:"agent_session_id,omitempty"`
	Summary        string  `json:"summary"`
	Approach       string  `json:"approach,omitempty"`
	Outcome        string  `json:"outcome,omitempty"`
	Issue          string  `json:"issue,omitempty"`
	Learning       string  `json:"learning,omitempty"`
	NextSteps      string  `json:"next_steps,omitempty"`
}

type ContextResult struct {
	Since   string        `json:"since,omitempty"`
	Until   string        `json:"until,omitempty"`
	Source  string        `json:"source"`
	Notes   []ContextNote `json:"notes"`
	HasMore bool          `json:"has_more"`
}

func compactContext(ctx context.Context, service memory.Service, owner string, in ContextInput) (ContextResult, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 3
	}
	if limit > 5 {
		limit = 5
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		if strings.TrimSpace(in.Query) != "" {
			mode = "text"
		} else {
			mode = "recent"
		}
	}
	var request memory.Request
	switch mode {
	case "recent":
		if strings.TrimSpace(in.Query) != "" {
			return ContextResult{}, fmt.Errorf("%w: recent mode does not use query", memory.ErrInvalid)
		}
		request = memory.Request{Operation: "recent", Recent: &memory.RecentInput{ProjectURL: in.ProjectURL, SessionRef: in.SessionRef, Section: in.Section, Since: in.Since, Until: in.Until, Limit: limit + 1}}
	case "text", "semantic":
		if in.Since != "" || in.Until != "" {
			return ContextResult{}, fmt.Errorf("%w: time windows require recent mode", memory.ErrInvalid)
		}
		operation := "search"
		if mode == "semantic" {
			operation = "recall"
		}
		request = memory.Request{Operation: operation, Search: &memory.SearchInput{Query: in.Query, ProjectURL: in.ProjectURL, SessionRef: in.SessionRef, Section: in.Section, Limit: limit + 1}}
	default:
		return ContextResult{}, fmt.Errorf("%w: mode must be text, semantic, or recent", memory.ErrInvalid)
	}
	result, err := service.Execute(ctx, owner, request)
	if err != nil {
		return ContextResult{}, err
	}
	// Refuse mixed-version deployments whose private data API ignores window fields.
	if (in.Since != "" && result.Since != in.Since) || (in.Until != "" && result.Until != in.Until) {
		return ContextResult{}, fmt.Errorf("%w: recent store does not support the requested time window", memory.ErrUnavailable)
	}
	out := ContextResult{Since: result.Since, Until: result.Until, Source: mode, Notes: make([]ContextNote, 0, limit), HasMore: len(result.Entries) > limit}
	for i, entry := range result.Entries {
		if i >= limit {
			break
		}
		out.Notes = append(out.Notes, ContextNote{
			ID: entry.ID, OccurredAt: entry.OccurredAt.Format(time.RFC3339), ProjectURL: entry.ProjectURL, Date: entry.OccurredAt.Format(time.DateOnly), EntryType: entry.EntryType,
			Assistant: entry.Assistant, SessionRef: entry.SessionRef, AgentSessionID: entry.AgentSessionID, Summary: preview(entry.Summary, 180),
			Approach: previewPtr(entry.Approach, 120), Outcome: previewPtr(entry.Outcome, 120),
			Issue: previewPtr(entry.Issue, 120), Learning: previewPtr(entry.Learning, 120),
			NextSteps: previewPtr(entry.NextSteps, 120),
		})
	}
	return out, nil
}

func previewPtr(s *string, max int) string {
	if s == nil {
		return ""
	}
	return preview(*s, max)
}

func preview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
