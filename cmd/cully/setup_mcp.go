package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// checkSetupMCP verifies the protocol without writing a memory record or handling
// the agent's OAuth credentials. OAuth sign-in stays in the coding agent.
func checkSetupMCP(w io.Writer, endpoint string, oauth bool) error {
	fmt.Fprintf(w, "Checking MCP initialization and tool discovery at %s (timeout: 30 seconds).\n", endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	transport := &setupMCPTransport{base: http.DefaultTransport}
	client := sdk.NewClient(&sdk.Implementation{Name: "cully-setup", Version: version}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: transport}}, nil)
	if err != nil {
		if transport.requiresOAuth.Load() {
			if !oauth {
				fmt.Fprintln(w, "Detected an OAuth-protected deployment automatically; no client mode flag or --oauth option is required to connect.")
			}
			fmt.Fprintln(w, "MCP endpoint reached and requires OAuth. Authenticated tool discovery is pending: sign in through your agent's MCP settings (Codex: codex mcp login cully), then check Cully there. Private ownership is assigned by the server.")
			return nil
		}
		return fmt.Errorf("MCP initialization failed (agent configuration saved): %w; check the server URL/authentication and rerun setup", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("MCP tool discovery failed (agent configuration saved): %w", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "cully_context" || tool.Name == "cully_search" {
			fmt.Fprintf(w, "MCP connection verified: initialized server and discovered %d tools, including Cully memory tools. Restart the agent to load its Cully integration.\n", len(tools.Tools))
			return nil
		}
	}
	return fmt.Errorf("MCP server initialized but did not advertise Cully memory tools; check that --mcp-url points to a Cully server")
}

// Observe the SDK's initialization response; no second session or OAuth token is needed.
type setupMCPTransport struct {
	base          http.RoundTripper
	requiresOAuth atomic.Bool
}

func (t *setupMCPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && response.StatusCode == http.StatusUnauthorized && strings.HasPrefix(strings.ToLower(response.Header.Get("WWW-Authenticate")), "bearer ") {
		t.requiresOAuth.Store(true)
	}
	return response, err
}
