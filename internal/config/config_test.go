package config

import (
	"strings"
	"testing"
)

func clearMCPEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"CULLY_MCP_AUTH_MODE", "CULLY_MCP_OWNER", "CULLY_HOST", "CULLY_PORT", "MCP_AUTH_ISSUER", "CULLY_AUTH_ISSUER", "MCP_AUTH_RESOURCE", "CULLY_AUTH_RESOURCE", "CULLY_JWKS_URL"} {
		t.Setenv(key, "")
	}
}

func TestNoOAuthDefaultsAndOwner(t *testing.T) {
	clearMCPEnv(t)
	if _, err := LoadMCP(false); err == nil || !strings.Contains(err.Error(), "CULLY_MCP_OWNER") {
		t.Fatalf("missing owner: %v", err)
	}
	t.Setenv("CULLY_MCP_OWNER", "my-user")
	s, err := LoadMCP(false)
	if err != nil {
		t.Fatal(err)
	}
	if s.AuthMode != "none" || s.Owner != "my-user" || s.Address != "127.0.0.1:8080" || s.Issuer != "" {
		t.Fatalf("unexpected no-OAuth settings: %+v", s)
	}
	t.Setenv("CULLY_MCP_OWNER", "other\nuser")
	if _, err := LoadMCP(false); err == nil {
		t.Fatal("accepted unsafe owner")
	}
}

func TestOAuthRequiresExplicitSettings(t *testing.T) {
	clearMCPEnv(t)
	if _, err := LoadMCP(true); err == nil {
		t.Fatal("OAuth started without issuer, resource or JWKS")
	}
	t.Setenv("CULLY_MCP_AUTH_MODE", "oauth")
	t.Setenv("CULLY_AUTH_ISSUER", "https://auth.example/mcp-auth")
	t.Setenv("CULLY_AUTH_RESOURCE", "https://mcp.example/mcp")
	t.Setenv("CULLY_JWKS_URL", "https://auth.example/.well-known/jwks.json")
	s, err := LoadMCP(false)
	if err != nil {
		t.Fatal(err)
	}
	if s.AuthMode != "oauth" || s.Owner != "" || s.Issuer != "https://auth.example/mcp-auth" {
		t.Fatalf("unexpected OAuth settings: %+v", s)
	}
	t.Setenv("CULLY_MCP_OWNER", "owner")
	if _, err := LoadMCP(false); err == nil {
		t.Fatal("accepted fixed owner in OAuth mode")
	}
}
