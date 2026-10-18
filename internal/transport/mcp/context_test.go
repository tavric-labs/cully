package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
)

type contextStore struct {
	owner   string
	request memory.Request
	entries []memory.Entry
}

func (s *contextStore) Execute(_ context.Context, owner string, request memory.Request) (memory.Result, error) {
	s.owner, s.request = owner, request
	return memory.Result{Entries: s.entries}, nil
}

func TestCompactContextBoundsAndOwner(t *testing.T) {
	long := strings.Repeat("useful detail ", 90)
	ref := "codex-0123456789abcdef"
	agentID := "01a13b57-f08a-77a1-a18f-5daaf9d50ec4"
	store := &contextStore{}
	for i := 0; i < 4; i++ {
		store.entries = append(store.entries, memory.Entry{
			ID: "id", OccurredAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
			Summary: long, Approach: &long, Outcome: &long, Issue: &long, Learning: &long, NextSteps: &long, SessionRef: &ref, AgentSessionID: &agentID,
		})
	}
	project := "https://github.com/Owner/Repo"
	out, err := compactContext(context.Background(), memory.Service{Store: store}, "owner-a", ContextInput{Query: "memory", ProjectURL: &project, SessionRef: &ref})
	if err != nil {
		t.Fatal(err)
	}
	if store.owner != "owner-a" || store.request.Operation != "search" || store.request.Search.Limit != 4 {
		t.Fatalf("owner or bounded request lost: %+v", store)
	}
	if store.request.Search.ProjectURL == nil || *store.request.Search.ProjectURL != "https://github.com/owner/repo" {
		t.Fatalf("project validation skipped: %+v", store.request.Search)
	}
	if store.request.Search.SessionRef == nil || *store.request.Search.SessionRef != ref || out.Notes[0].SessionRef == nil || *out.Notes[0].SessionRef != ref {
		t.Fatal("session reference lost")
	}
	if out.Notes[0].AgentSessionID == nil || *out.Notes[0].AgentSessionID != agentID {
		t.Fatal("resumable agent session ID lost")
	}
	if len(out.Notes) != 3 || !out.HasMore || out.Source != "text" {
		t.Fatalf("bad context page: %+v", out)
	}
	if len([]rune(out.Notes[0].Summary)) > 180 || len([]rune(out.Notes[0].Approach)) > 120 || strings.Contains(out.Notes[0].Summary, long) {
		t.Fatalf("unbounded context: %+v", out.Notes[0])
	}
}

func TestCompactContextUsesSharedValidation(t *testing.T) {
	store := &contextStore{}
	service := memory.Service{Store: store}
	badProject := "https://github.com.evil.test/org/repo"
	badRef := "native-session-id"
	for _, input := range []ContextInput{
		{Mode: "semantic"},
		{Mode: "text", Query: "  "},
		{Mode: "recent", Query: "oauth"},
		{Mode: "recent", ProjectURL: &badProject},
		{Mode: "recent", SessionRef: &badRef},
		{Mode: "bogus"},
	} {
		if _, err := compactContext(context.Background(), service, "owner-a", input); !errors.Is(err, memory.ErrInvalid) {
			t.Fatalf("accepted invalid context %+v: %v", input, err)
		}
	}
	if store.owner != "" {
		t.Fatal("invalid request reached store")
	}
}

func TestCompactContextRecentWindowAndEvidence(t *testing.T) {
	project := "https://github.com/tavric-labs/cully"
	when := time.Date(2026, 10, 19, 8, 0, 0, 0, time.FixedZone("local", 19800))
	store := &contextStore{entries: []memory.Entry{{ID: "note", ProjectURL: &project, OccurredAt: when, Summary: "Learned about PKCE"}}}
	in := ContextInput{Mode: "recent", Since: "2026-10-18T08:00:00+05:30", Until: "2026-10-19T08:00:00+05:30", Limit: 3}
	out, err := compactContext(context.Background(), memory.Service{Store: store}, "owner-a", in)
	if err != nil {
		t.Fatal(err)
	}
	if store.request.Recent.Since != in.Since || store.request.Recent.Until != in.Until || store.owner != "owner-a" {
		t.Fatal("window or owner was lost")
	}
	if out.Since != in.Since || out.Until != in.Until || out.Notes[0].OccurredAt != when.Format(time.RFC3339) || *out.Notes[0].ProjectURL != project {
		t.Fatal("evidence metadata was lost")
	}
	for _, bad := range []ContextInput{
		{Mode: "recent", Since: "yesterday"},
		{Mode: "recent", Until: "2026-10-19"},
		{Mode: "recent", Since: in.Until, Until: in.Since},
		{Mode: "recent", Since: in.Since, Until: in.Since},
		{Mode: "text", Query: "learning", Since: in.Since},
		{Mode: "semantic", Query: "learning", Until: in.Until},
	} {
		if _, err := compactContext(context.Background(), memory.Service{Store: store}, "owner-a", bad); !errors.Is(err, memory.ErrInvalid) {
			t.Fatalf("invalid window accepted: %+v %v", bad, err)
		}
	}
}
