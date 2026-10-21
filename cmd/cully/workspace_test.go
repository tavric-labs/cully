package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
	mcpt "github.com/mcp-runtime/cully/internal/transport/mcp"
	"github.com/mcp-runtime/cully/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

type workspaceCLIStore struct{}

func (workspaceCLIStore) Execute(_ context.Context, owner string, r memory.Request) (memory.Result, error) {
	if r.Workspace.Action != "whoami" {
		return memory.Result{}, memory.ErrForbidden
	}
	return memory.Result{Workspace: &workspace.Result{Principal: owner}}, nil
}
func TestWorkspaceCLIUsesPublicIdentity(t *testing.T) {
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token != "test-user-token" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "dev", Scopes: []string{"tools:read"}, Expiration: time.Now().Add(time.Hour)}, nil
	}
	server := httptest.NewServer(mcpt.Handler(memory.Service{Store: workspaceCLIStore{}}, mcpt.AuthConfig{Mode: "oauth", Issuer: "https://issuer.test", Resource: "https://public.test/mcp", Verifier: verifier}, "/mcp", "test"))
	defer server.Close()
	t.Setenv("CULLY_WORKSPACE_TOKEN", "test-user-token")
	file := filepath.Join(t.TempDir(), "action.json")
	if err := os.WriteFile(file, []byte(`{"action":"whoami"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	err = runWorkspace([]string{"--mcp-url", server.URL + "/mcp", "--input", file})
	os.Stdout = old
	w.Close()
	data, readErr := io.ReadAll(r)
	r.Close()
	if err != nil || readErr != nil {
		t.Fatalf("CLI: %v %v", err, readErr)
	}
	var result memory.Result
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Workspace == nil || result.Workspace.Principal != workspace.Principal("https://issuer.test", "dev") {
		t.Fatal("CLI lost authenticated principal")
	}
	if strings.Contains(string(data), "test-user-token") {
		t.Fatal("token escaped into CLI output")
	}
	if err = os.WriteFile(file, []byte(`{"action":"team_create","name":"Forbidden"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = runWorkspace([]string{"--mcp-url", server.URL + "/mcp", "--input", file}); err == nil {
		t.Fatal("read-only CLI identity wrote workspace")
	}
}
func TestWorkspaceCLIRejectsAmbiguousInputAndEndpoints(t *testing.T) {
	t.Setenv("CULLY_WORKSPACE_TOKEN", "test-user-token")
	for _, endpoint := range []string{"http://remote.test/mcp", "https://user:pass@remote.test/mcp", "https://remote.test/mcp?token=x"} {
		if err := runWorkspace([]string{"--mcp-url", endpoint}); err == nil {
			t.Fatal("accepted unsafe endpoint")
		}
	}
	file := filepath.Join(t.TempDir(), "action.json")
	for _, body := range []string{`{"action":"whoami"} {"action":"team_create"}`, `{"action":"whoami","owner":"spoof"}`, `{"action":"unknown"}`} {
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := runWorkspace([]string{"--mcp-url", "https://example.test/mcp", "--input", file}); err == nil {
			t.Fatal("accepted ambiguous input")
		}
	}
}
