package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPStore runs the retrieval eval against a live Cully MCP server.
type MCPStore struct{ session *sdk.ClientSession }

// Connect opens an MCP session to a server that needs no sign-in, such as the
// local stack started by `cully setup --all`. Close it when done.
func Connect(ctx context.Context, endpoint string) (*MCPStore, error) {
	client := sdk.NewClient(&sdk.Implementation{Name: "cully-eval", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, err
	}
	return &MCPStore{session}, nil
}

func (s *MCPStore) Close() error { return s.session.Close() }

func (s *MCPStore) call(ctx context.Context, name string, input any) (memory.Result, error) {
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

func (s *MCPStore) Log(ctx context.Context, in memory.LogInput) (string, error) {
	result, err := s.call(ctx, "cully_log", in)
	if err != nil || result.Entry == nil || result.Entry.ID == "" {
		return "", fmt.Errorf("log: %v", err)
	}
	return result.Entry.ID, nil
}

func (s *MCPStore) Delete(ctx context.Context, id string) error {
	_, err := s.call(ctx, "cully_delete", memory.IDInput{EntryID: id})
	return err
}

// Search runs text search once. Semantic recall depends on asynchronous Mem0
// indexing, so it retries empty and failed answers for up to a minute.
func (s *MCPStore) Search(ctx context.Context, mode string, in memory.SearchInput) ([]memory.Entry, error) {
	if mode == ModeText {
		result, err := s.call(ctx, "cully_search", in)
		return result.Entries, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		result, err := s.call(ctx, "cully_recall", in)
		if err == nil && len(result.Entries) > 0 || time.Now().After(deadline) || ctx.Err() != nil {
			return result.Entries, err
		}
		time.Sleep(2 * time.Second)
	}
}

// RetrievalThresholds are the gates the text-search eval must clear.
var RetrievalThresholds = Thresholds{Recall: 0.85, MRR: 0.8, Silence: 1, MaxLeaked: 0}
