package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/store/remote"
	"github.com/mcp-runtime/cully/internal/transport/datahttp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type optimizationTransportStore struct {
	sync.Mutex
	owners     []string
	operations []string
}

func (s *optimizationTransportStore) Execute(_ context.Context, owner string, r memory.Request) (memory.Result, error) {
	s.Lock()
	defer s.Unlock()
	s.owners = append(s.owners, owner)
	s.operations = append(s.operations, r.Operation)
	if r.OptimizationQuery != nil {
		return memory.Result{Entries: []memory.Entry{}}, nil
	}
	return memory.Result{Entry: &memory.Entry{ID: "00000000-0000-0000-0000-000000000001", EntryType: "optimization", Summary: "do not echo source evidence", Optimization: &memory.Optimization{PatternKey: "release-checks", Version: 1, Status: "observing", Evidence: []memory.OptimizationEvidence{{Summary: "do not echo evidence"}}}}}, nil
}

func TestOptimizationToolsPreserveScopeAndValidationAcrossPrivateAPI(t *testing.T) {
	backend := &optimizationTransportStore{}
	data := httptest.NewServer(datahttp.Handler(memory.Service{Store: backend}, "test-private-token", nil))
	defer data.Close()
	store, err := remote.New(data.URL, "test-private-token")
	if err != nil {
		t.Fatal(err)
	}
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"tools:read"}
		if token == "writer" {
			scopes = append(scopes, "tools:write")
		}
		return &auth.TokenInfo{UserID: "verified-" + token, Scopes: scopes, Expiration: time.Now().Add(time.Hour)}, nil
	}
	server := httptest.NewServer(Handler(memory.Service{Store: store}, AuthConfig{Mode: "oauth", Verifier: verifier}, "/mcp", "test"))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, token := range []string{"writer", "reader"} {
		client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
		session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}}, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name  string
			write bool
			input any
		}{
			{"cully_optimizations", false, memory.OptimizationQueryInput{}},
			{"cully_optimization_save", true, memory.OptimizationSaveInput{PatternKey: "release-checks", Summary: "Release validation", Evidence: "Repeated procedure", SessionRef: "codex-0123456789abcdef", WindowID: "prompts-1-12", Assistant: "codex"}},
			{"cully_optimization_decide", true, memory.OptimizationDecideInput{EntryID: "00000000-0000-0000-0000-000000000001", Version: 1, Status: "dismissed"}},
		} {
			result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: tc.name, Arguments: tc.input})
			if err != nil || result.IsError != (tc.write && token == "reader") {
				t.Fatalf("scope %s %s: %+v %v", token, tc.name, result, err)
			}
			if !result.IsError && tc.write {
				b, _ := json.Marshal(result.StructuredContent)
				var out memory.Result
				_ = json.Unmarshal(b, &out)
				if out.Entry == nil || out.Entry.Optimization == nil || out.Entry.Optimization.Version != 1 || out.Entry.Summary != "" || len(out.Entry.Optimization.Evidence) != 0 {
					t.Fatalf("write acknowledgement echoed evidence or lost decision version: %s", b)
				}
			}
		}
		if result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_optimizations", Arguments: map[string]any{"owner_subject": "spoof"}}); err == nil && !result.IsError {
			t.Fatal("caller-selected ownership accepted")
		}
		_ = session.Close()
	}
	backend.Lock()
	defer backend.Unlock()
	if len(backend.operations) != 4 {
		t.Fatalf("unauthorized writes reached private API: %+v", backend.operations)
	}
	for i, owner := range backend.owners {
		want := "verified-writer"
		if i == 3 {
			want = "verified-reader"
		}
		if owner != want {
			t.Fatal("verified owner lost across transports")
		}
	}
}
