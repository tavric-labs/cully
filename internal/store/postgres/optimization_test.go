package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/mem0"
	"github.com/mcp-runtime/cully/internal/memory"
)

func TestOptimizationDeduplicationOwnershipDecisionsAndProjection(t *testing.T) {
	s := testStore(t)
	s.Mem0Enabled = true
	service := memory.Service{Store: s}
	in := memory.OptimizationSaveInput{PatternKey: "release-checks", Summary: "Repeated release validation", Evidence: "Repeated the documented validation sequence.", SessionRef: "codex-0123456789abcdef", WindowID: "prompts-1-12", Assistant: "codex"}
	request := memory.Request{Operation: "optimization_save", OptimizationSave: &in}
	first := execute(t, s, "owner-a", request).Entry
	if first == nil || first.Optimization.ObservationCount != 1 || first.Optimization.SessionCount != 1 {
		t.Fatalf("missing initial evidence: %+v", first)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := in
			_, err := service.Execute(context.Background(), "owner-a", memory.Request{Operation: "optimization_save", OptimizationSave: &local})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	second := execute(t, s, "owner-a", request).Entry
	if second.Optimization.ObservationCount != 1 || second.Optimization.Version != first.Optimization.Version {
		t.Fatalf("retry inflated evidence: %+v", second.Optimization)
	}
	in.SessionRef = "claude-1123456789abcdef"
	in.WindowID = "prompts-13-24"
	in.Proposal = &memory.OptimizationProposal{Kind: "workflow", Title: "Reuse release validation", Benefit: "Avoid reconstructing the checks", Steps: []string{"Use the existing validation script", "Run the acceptance checks"}, Validation: "Compare the same fixture before and after"}
	proposed := execute(t, s, "owner-a", request).Entry
	if proposed.Optimization.ObservationCount != 2 || proposed.Optimization.SessionCount != 2 || proposed.Optimization.Status != "proposed" {
		t.Fatalf("cross-session evidence missing: %+v", proposed.Optimization)
	}
	other := execute(t, s, "owner-b", request).Entry
	if other.ID == first.ID || other.Optimization.SessionCount != 1 {
		t.Fatal("pattern identity leaked across owners")
	}
	denied := execute(t, s, "owner-b", memory.Request{Operation: "optimization_decide", OptimizationDecide: &memory.OptimizationDecideInput{EntryID: first.ID, Version: proposed.Optimization.Version, Status: "dismissed"}})
	if denied.Entry != nil {
		t.Fatal("another owner decided a pattern")
	}
	decision := memory.OptimizationDecideInput{EntryID: first.ID, Version: proposed.Optimization.Version, Status: "dismissed", Reason: "Already covered by an existing workflow"}
	dismissed := execute(t, s, "owner-a", memory.Request{Operation: "optimization_decide", OptimizationDecide: &decision}).Entry
	if dismissed.Optimization.Status != "dismissed" {
		t.Fatal("decision lost")
	}
	if _, err := service.Execute(context.Background(), "owner-a", memory.Request{Operation: "optimization_decide", OptimizationDecide: &decision}); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale decision accepted: %v", err)
	}
	in.WindowID = "prompts-25-36"
	ignored := execute(t, s, "owner-a", request).Entry
	if ignored.Optimization.Version != dismissed.Optimization.Version || ignored.Optimization.ObservationCount != 2 {
		t.Fatal("worker reopened or changed a dismissed pattern")
	}
	active := execute(t, s, "owner-a", memory.Request{Operation: "optimization_list", OptimizationQuery: &memory.OptimizationQueryInput{}})
	if len(active.Entries) != 0 {
		t.Fatal("dismissed pattern included in active recommendations")
	}
	all := execute(t, s, "owner-a", memory.Request{Operation: "optimization_list", OptimizationQuery: &memory.OptimizationQueryInput{Query: "release validation", Status: "all"}})
	if len(all.Entries) != 1 || all.Entries[0].ID != first.ID {
		t.Fatalf("text recall failed: %+v", all)
	}
	var queued bool
	if err := s.Pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM cully_mem0_jobs WHERE owner_subject=$1 AND entry_id=$2)", "owner-a", first.ID).Scan(&queued); err != nil || !queued {
		t.Fatalf("source/projection transaction lost: %v", err)
	}
	execute(t, s, "owner-a", memory.Request{Operation: "delete", ID: &memory.IDInput{EntryID: first.ID}})
	var count int
	if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM cully_optimization_observations WHERE entry_id=$1", first.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("evidence not deleted with source: %d %v", count, err)
	}
}

func TestOptimizationDistinctConcurrentWindows(t *testing.T) {
	s := testStore(t)
	service := memory.Service{Store: s}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := memory.OptimizationSaveInput{PatternKey: "same-procedure", Summary: "Repeat procedure", Evidence: "A bounded observation", SessionRef: fmt.Sprintf("codex-%016x", i), WindowID: "prompts-1-12", Assistant: "codex"}
			_, err := service.Execute(context.Background(), "owner", memory.Request{Operation: "optimization_save", OptimizationSave: &in})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	all := execute(t, s, "owner", memory.Request{Operation: "optimization_list", OptimizationQuery: &memory.OptimizationQueryInput{}})
	if len(all.Entries) != 1 || all.Entries[0].Optimization.SessionCount != 8 || all.Entries[0].Optimization.ObservationCount != 8 || len(all.Entries[0].Optimization.Evidence) != 3 {
		t.Fatalf("concurrent evidence lost or unbounded: %+v", all)
	}
}

func TestOptimizationSemanticRecallRevalidatesOwnerSourceAndDecision(t *testing.T) {
	s := testStore(t)
	in := memory.OptimizationSaveInput{PatternKey: "release-pattern", Summary: "Repeated release procedure", Evidence: "A recorded procedure", SessionRef: "codex-0123456789abcdef", WindowID: "prompts-1-12", Assistant: "codex"}
	e := execute(t, s, "owner-a", memory.Request{Operation: "optimization_save", OptimizationSave: &in}).Entry
	var mu sync.Mutex
	hits := []mem0.Hit{{ID: "hit", UserID: mem0.UserID("owner-a"), RunID: e.ID, Metadata: map[string]any{"cully_updated_at": e.UpdatedAt.Format(time.RFC3339Nano)}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"results": hits})
	}))
	defer server.Close()
	s.Mem0, _ = mem0.New(server.URL, "test-key")
	q := memory.Request{Operation: "optimization_list", OptimizationQuery: &memory.OptimizationQueryInput{Query: "release procedure", Mode: "semantic"}}
	if got := execute(t, s, "owner-a", q); len(got.Entries) != 1 {
		t.Fatal("live pattern not recalled")
	}
	if got := execute(t, s, "owner-b", q); len(got.Entries) != 0 {
		t.Fatal("semantic pattern leaked across owners")
	}
	d := execute(t, s, "owner-a", memory.Request{Operation: "optimization_decide", OptimizationDecide: &memory.OptimizationDecideInput{EntryID: e.ID, Version: e.Optimization.Version, Status: "dismissed"}}).Entry
	if got := execute(t, s, "owner-a", q); len(got.Entries) != 0 {
		t.Fatal("stale semantic candidate returned")
	}
	mu.Lock()
	hits[0].Metadata["cully_updated_at"] = d.UpdatedAt.Format(time.RFC3339Nano)
	mu.Unlock()
	if got := execute(t, s, "owner-a", q); len(got.Entries) != 0 {
		t.Fatal("dismissed pattern returned as active")
	}
	q.OptimizationQuery.Status = "all"
	if got := execute(t, s, "owner-a", q); len(got.Entries) != 1 || got.Entries[0].Optimization.Status != "dismissed" {
		t.Fatal("explicit all-status recall lost decision")
	}
	execute(t, s, "owner-a", memory.Request{Operation: "delete", ID: &memory.IDInput{EntryID: e.ID}})
	if got := execute(t, s, "owner-a", q); len(got.Entries) != 0 {
		t.Fatal("deleted semantic source returned")
	}
}
