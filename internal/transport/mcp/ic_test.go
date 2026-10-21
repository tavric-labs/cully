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

type contributorStore struct {
	mu    sync.Mutex
	notes map[string]map[string]memory.Entry
}

func (s *contributorStore) Execute(_ context.Context, owner string, r memory.Request) (memory.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Log != nil {
		if s.notes[owner] == nil {
			s.notes[owner] = map[string]memory.Entry{}
		}
		e := memory.Entry{ID: "00000000-0000-0000-0000-000000000001", Summary: r.Log.Summary}
		s.notes[owner][e.ID] = e
		return memory.Result{Entry: &e}, nil
	}
	if r.ID != nil {
		if e, ok := s.notes[owner][r.ID.EntryID]; ok {
			return memory.Result{Entry: &e}, nil
		}
	}
	return memory.Result{}, nil
}

func TestICContributorsStayPrivateAcrossTransports(t *testing.T) {
	backend := &contributorStore{notes: map[string]map[string]memory.Entry{}}
	data := httptest.NewServer(datahttp.Handler(memory.Service{Store: backend}, "test-service-token", nil))
	defer data.Close()
	store, err := remote.New(data.URL, "test-service-token")
	if err != nil {
		t.Fatal(err)
	}
	verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token != "contributor-a" && token != "contributor-b" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: token, Scopes: []string{"tools:read", "tools:write"}, Expiration: time.Now().Add(time.Hour)}, nil
	}
	server := httptest.NewServer(Handler(memory.Service{Store: store}, AuthConfig{Mode: "oauth", Verifier: verify, Issuer: "https://issuer.example", Resource: "https://mcp.example/mcp"}, "/mcp", "test"))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connect := func(token string) *sdk.ClientSession {
		t.Helper()
		client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
		session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}}, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	a, b := connect("contributor-a"), connect("contributor-b")
	tools, err := b.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "cully_projects" || tool.Name == "cully_workspace_read" || tool.Name == "cully_workspace_write" {
			t.Fatalf("IC exposes sharing or removed metadata: %s", tool.Name)
		}
	}
	if result, err := a.CallTool(ctx, &sdk.CallToolParams{Name: "cully_log", Arguments: memory.LogInput{Summary: "Private work", Assistant: "codex"}}); err != nil || result.IsError {
		t.Fatalf("write: %v %+v", err, result)
	}
	// The same endpoint and a known record ID must not expose another contributor's note.
	result, err := b.CallTool(ctx, &sdk.CallToolParams{Name: "cully_get", Arguments: memory.IDInput{EntryID: "00000000-0000-0000-0000-000000000001"}})
	if err != nil || result.IsError {
		t.Fatalf("read: %v %+v", err, result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var read memory.Result
	if err := json.Unmarshal(encoded, &read); err != nil || read.Entry != nil {
		t.Fatalf("cross-contributor note leaked: %s %v", encoded, err)
	}
	for _, field := range []string{"project", "project_url", "section", "category", "theme", "tags", "owner_subject"} {
		input := map[string]any{"summary": "Do not save obsolete metadata", "assistant": "codex", field: "spoof"}
		denied, err := b.CallTool(ctx, &sdk.CallToolParams{Name: "cully_log", Arguments: input})
		if err == nil && !denied.IsError {
			t.Fatalf("accepted obsolete or caller-selected ownership field %s", field)
		}
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.notes["contributor-a"]) != 1 || len(backend.notes["contributor-b"]) != 0 {
		t.Fatal("verified owner lost across private API")
	}
}
