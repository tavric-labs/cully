package e2e

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcp-runtime/cully/internal/evals"
)

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
	// Fresh projects keep earlier or concurrent runs out of the scores.
	run := uuid.NewString()[:8]
	project := "https://github.com/cully-eval/app-" + run
	other := "https://github.com/cully-eval/other-" + run
	report, err := evals.Run(ctx, store, ds, project, other, "eval-"+run, 5)
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
