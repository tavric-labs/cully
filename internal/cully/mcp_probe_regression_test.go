package cully

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCodexProbePrefersWorkingRegistration(t *testing.T) {
	failed := `{"name":"cully","authStatus":"notLoggedIn","toolsError":"authentication failed","tools":{}}`
	working := `{"name":"cully-work","authStatus":"oAuth","tools":{"cully_context":{}}}`
	for _, servers := range []string{failed + "," + working, working + "," + failed} {
		h, a, found := parseCodexMCPStatus(json.RawMessage(`{"data":[` + servers + `]}`))
		if !found || h != "healthy" || a != "authenticated" {
			t.Fatalf("working registration hidden: %s %s %v", h, a, found)
		}
	}
	var request strings.Builder
	response := "{\"id\":1,\"result\":{}}\n" + `{"id":2,"result":{"data":[` + failed + `],"nextCursor":"page2"}}` + "\n" + `{"id":2,"result":{"data":[` + working + `]}}` + "\n"
	if h, a, found := codexMCPStatusExchange(&request, strings.NewReader(response)); !found || h != "healthy" || a != "authenticated" {
		t.Fatalf("healthy later page hidden: %s %s %v", h, a, found)
	}
}

func TestCodexProbeCheckedAuthenticationStatus(t *testing.T) {
	h, a, found := parseCodexMCPStatus(json.RawMessage(`{"data":[{"name":"cully","authStatus":"notLoggedIn","toolsError":"authentication failed"}]}`))
	if !found || h != "unhealthy" || a != "unauthenticated" {
		t.Fatalf("missing sign-in not recognized: %s %s %v", h, a, found)
	}
	for _, state := range []cullyStats{
		{Probed: true, Health: "healthy", Auth: "authenticated"},
		{Probed: true, Health: "unhealthy", Auth: "unknown"},
		{Probed: true, Health: "unhealthy", Auth: "unauthenticated"},
		{Probed: true, Health: "unsupported", Auth: "unknown"},
		{Probed: true, Health: "agent_signin", Auth: "unknown"},
	} {
		text := normalizedCodexPanel(strings.Join(cullyInstrumentRows(toolStats{Cully: state}, 180), "\n"))
		if strings.Contains(text, "first call") || strings.Contains(text, "Auth unknown") {
			t.Fatalf("checked status still looks unobserved: %s", text)
		}
	}
}

func TestCursorNativeSignInAndUnavailableChecks(t *testing.T) {
	h, a, found := parseCursorMCPTools("Authentication required. Run agent mcp login cully.", errors.New("exit 1"))
	if !found || h != "unhealthy" || a != "unauthenticated" {
		t.Fatalf("Cursor MCP sign-in not recognized: %s %s %t", h, a, found)
	}
	h, a, found = agentMCPProbe(t.Context(), "another-agent")
	if !found || h != "unsupported" || a != "unknown" {
		t.Fatalf("unsupported agent was not explicit: %s %s %t", h, a, found)
	}
}
