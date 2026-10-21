package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/store/remote"
	"github.com/mcp-runtime/cully/internal/transport/datahttp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeStore struct {
	owner string
	calls int
}

func (s *fakeStore) Execute(_ context.Context, owner string, r memory.Request) (memory.Result, error) {
	s.owner = owner
	s.calls++
	if r.Operation == "recent" {
		return memory.Result{Entries: []memory.Entry{{ID: "00000000-0000-0000-0000-000000000002", Summary: "Previous project work", Assistant: "codex"}}}, nil
	}
	return memory.Result{Entry: &memory.Entry{ID: "00000000-0000-0000-0000-000000000001", Summary: r.Log.Summary}}, nil
}

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}
func TestMCPToPrivateAPI(t *testing.T) {
	backend := &fakeStore{}
	data := httptest.NewServer(datahttp.Handler(memory.Service{Store: backend}, "private-service-secret", nil))
	defer data.Close()
	store, err := remote.New(data.URL, "private-service-secret")
	if err != nil {
		t.Fatal(err)
	}
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"tools:read"}
		if token == "writer" {
			scopes = append(scopes, "tools:write")
		}
		if token != "reader" && token != "writer" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "owner-a", Scopes: scopes, Expiration: time.Now().Add(time.Hour)}, nil
	}
	server := httptest.NewServer(Handler(memory.Service{Store: store}, AuthConfig{Mode: "oauth", Verifier: verifier, Issuer: "https://issuer.test", Resource: "https://public.test/cully/mcp"}, "/mcp", "test"))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, token := range []string{"writer", "reader"} {
		client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
		session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token}}, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := session.ListTools(ctx, nil)
		if err != nil || len(tools.Tools) != len(toolRegistrations()) {
			t.Fatalf("tools=%v err=%v", tools, err)
		}
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_log", Arguments: memory.LogInput{Summary: "Fixed OAuth", Assistant: "codex"}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != (token == "reader") {
			t.Fatalf("scope not enforced: %s %+v", token, result)
		}
		if token == "reader" {
			contextResult, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_context", Arguments: ContextInput{Mode: "recent"}})
			if err != nil || contextResult.IsError {
				t.Fatalf("read-scoped context failed: %v %+v", err, contextResult)
			}
		}
		_ = session.Close()
	}
	if backend.calls != 2 || backend.owner != "owner-a" {
		t.Fatalf("owner or permissions lost: %+v", backend)
	}
	for _, path := range []string{"/mcp", "/.well-known/oauth-protected-resource/cully/mcp"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		expected := 200
		if path == "/mcp" {
			expected = 401
		}
		if resp.StatusCode != expected {
			t.Fatalf("%s status %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestNoOAuthUsesFixedOwner(t *testing.T) {
	backend := &fakeStore{}
	data := httptest.NewServer(datahttp.Handler(memory.Service{Store: backend}, "private-service-secret", nil))
	defer data.Close()
	store, err := remote.New(data.URL, "private-service-secret")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Handler(memory.Service{Store: store}, AuthConfig{Mode: "none", Owner: "personal-owner"}, "/mcp", "test"))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, name := range []string{"cully_workspace_read", "cully_workspace_write"} {
		action := "whoami"
		if name == "cully_workspace_write" {
			action = "team_create"
		}
		denied, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{"action": action, "name": "Example"}})
		if err == nil && !denied.IsError {
			t.Fatal("private no-OAuth mode enabled team identity")
		}
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_log", Arguments: memory.LogInput{Summary: "No OAuth", Assistant: "codex"}})
	if err != nil || result.IsError {
		t.Fatalf("tool result=%v error=%v", result, err)
	}
	if backend.calls != 1 || backend.owner != "personal-owner" {
		t.Fatalf("owner was not fixed: %+v", backend)
	}
	resp, err := http.Get(server.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected OAuth metadata: %d", resp.StatusCode)
	}
}

func TestWriteAcknowledgementDoesNotEchoRecordText(t *testing.T) {
	secret := "approach text"
	agent := "agent-1"
	got := acknowledgeWrite(memory.Result{Entry: &memory.Entry{ID: "e1", AgentSessionID: &agent, Summary: "s", Approach: &secret}})
	if got.Entry.ID != "e1" || got.Entry.AgentSessionID == nil || *got.Entry.AgentSessionID != agent {
		t.Fatalf("lost identifiers: %+v", got.Entry)
	}
	if got.Entry.Summary != "" || got.Entry.Approach != nil {
		t.Fatalf("echoed content: %+v", got.Entry)
	}
	if empty := acknowledgeWrite(memory.Result{Deleted: true}); empty.Entry != nil || !empty.Deleted {
		t.Fatal("results without an entry must pass through")
	}
}
