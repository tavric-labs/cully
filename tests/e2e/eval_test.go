package e2e

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcp-runtime/cully/internal/evals"
	"github.com/mcp-runtime/cully/internal/memory"
)

func retrievalEvalSessions() (string, string) {
	run := strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	return "eval-" + run, "eval-other-" + run
}

func TestRetrievalEvalSessionsMatchMemoryContract(t *testing.T) {
	seen := map[string]bool{}
	for range 2 {
		primary, other := retrievalEvalSessions()
		for _, ref := range []string{primary, other} {
			if seen[ref] {
				t.Fatal("retrieval fixtures reused a session reference")
			}
			seen[ref] = true
			req := memory.Request{Operation: "log", Log: &memory.LogInput{Summary: "retrieval fixture", Assistant: "other", SessionRef: &ref}}
			if err := req.Validate(); err != nil {
				t.Fatalf("retrieval fixture is rejected by the current memory contract: %v", err)
			}
		}
	}
}

// TestRetrievalEval scores real text search and semantic recall against the
// embedded dataset. Text retrieval is gated. Semantic recall depends on the
// embedding model, so it is reported and gated only when
// CULLY_EVAL_SEMANTIC_MIN names a minimum recall (for example 0.5).
func TestRetrievalEval(t *testing.T) {
	endpoint := os.Getenv("CULLY_E2E_MCP_URL")
	if endpoint == "" {
		t.Skip("CULLY_E2E_MCP_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	store, err := evals.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ds, err := evals.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Fresh session references keep earlier or concurrent runs out of the scores.
	primary, other := retrievalEvalSessions()
	report, err := evals.Run(ctx, store, ds, primary, other, primary, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + report.Format())
	if err := report.Check(evals.ModeText, evals.RetrievalThresholds); err != nil {
		t.Error(err)
	}
	if v := os.Getenv("CULLY_EVAL_SEMANTIC_MIN"); v != "" {
		min, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatal(err)
		}
		if err := report.Check(evals.ModeSemantic, evals.Thresholds{Recall: min, MaxLeaked: 0}); err != nil {
			t.Error(err)
		}
	}
}
