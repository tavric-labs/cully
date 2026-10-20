package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcp-runtime/cully/internal/evals"
	"github.com/mcp-runtime/cully/internal/memory"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpStore adapts a live MCP connection to the retrieval eval.
type mcpStore struct{ session *sdk.ClientSession }

func (s mcpStore) call(ctx context.Context, name string, input any) (memory.Result, error) {
	response, err := s.session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return memory.Result{}, err
	}
	data, err := json.Marshal(response.StructuredContent)
	if err != nil {
		return memory.Result{}, err
	}
	if response.IsError {
		return memory.Result{}, fmt.Errorf("%s returned a tool error: %s", name, data)
	}
	var result memory.Result
	return result, json.Unmarshal(data, &result)
}

func (s mcpStore) Log(ctx context.Context, in memory.LogInput) (string, error) {
	result, err := s.call(ctx, "cully_log", in)
	if err != nil || result.Entry == nil || result.Entry.ID == "" {
		return "", fmt.Errorf("log: %v", err)
	}
	return result.Entry.ID, nil
}

func (s mcpStore) Delete(ctx context.Context, id string) error {
	_, err := s.call(ctx, "cully_delete", memory.IDInput{EntryID: id})
	return err
}

// Search runs text search once. Semantic recall depends on asynchronous Mem0
// indexing, so it retries empty and failed answers until the deadline.
func (s mcpStore) Search(ctx context.Context, mode string, in memory.SearchInput) ([]memory.Entry, error) {
	if mode == evals.ModeText {
		result, err := s.call(ctx, "cully_search", in)
		return result.Entries, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		result, err := s.call(ctx, "cully_recall", in)
		if err == nil && len(result.Entries) > 0 || time.Now().After(deadline) {
			return result.Entries, err
		}
		time.Sleep(2 * time.Second)
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
	client := sdk.NewClient(&sdk.Implementation{Name: "cully-eval", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ds, err := evals.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Fresh projects keep earlier or concurrent runs out of the scores.
	run := uuid.NewString()[:8]
	project := "https://github.com/cully-eval/app-" + run
	other := "https://github.com/cully-eval/other-" + run
	report, err := evals.Run(ctx, mcpStore{session}, ds, project, other, "eval-"+run, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + report.Format())
	if err := report.Check(evals.ModeText, evals.Thresholds{Recall: 0.85, MRR: 0.8, Silence: 1, MaxLeaked: 0}); err != nil {
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
