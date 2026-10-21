package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSetupChecksCullyProtocol(t *testing.T) {
	for _, toolName := range []string{"cully_context", "unrelated_tool"} {
		t.Run(toolName, func(t *testing.T) {
			server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "1"}, nil)
			sdk.AddTool(server, &sdk.Tool{Name: toolName, Description: "test"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, struct{}, error) {
				return nil, struct{}{}, nil
			})
			httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil))
			defer httpServer.Close()
			var output bytes.Buffer
			err := checkSetupMCP(&output, httpServer.URL, false)
			if toolName == "cully_context" {
				if err != nil || !strings.Contains(output.String(), "connection verified") {
					t.Fatalf("check: %v, %s", err, &output)
				}
			} else if err == nil {
				t.Fatal("accepted unrelated MCP server")
			}
		})
	}
}

func TestSetupCheckOAuthRequiresSignIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	var output bytes.Buffer
	if err := checkSetupMCP(&output, server.URL, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "pending") || strings.Contains(output.String(), "connection verified") {
		t.Fatalf("misreported authentication: %s", &output)
	}
	output.Reset()
	if err := checkSetupMCP(&output, server.URL, false); err != nil || !strings.Contains(output.String(), "automatically") || !strings.Contains(output.String(), "pending") || strings.Contains(output.String(), "connection verified") {
		t.Fatalf("OAuth challenge was not discovered without a mode flag: %v %s", err, &output)
	}
}

func TestSetupCheckFailsForNonMCPServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hello")) }))
	defer server.Close()
	if err := checkSetupMCP(&bytes.Buffer{}, server.URL, false); err == nil {
		t.Fatal("accepted ordinary HTTP server")
	}
}
