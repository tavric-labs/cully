package cully

import "testing"

func TestParseClaudeMCPListFindsTheCullyServer(t *testing.T) {
	for _, tc := range []struct {
		name, out, health, auth string
		found                   bool
	}{
		{"connected", "tgit: twpython /x - ✔ Connected\ncully: https://mcp.example/cully/mcp (HTTP) - ✔ Connected\n", "healthy", "authenticated", true},
		{"needs auth", "cully: https://mcp.example/cully/mcp (HTTP) - ! Needs authentication\n", "unhealthy", "unauthenticated", true},
		{"failed", "cully: https://mcp.example/cully/mcp (HTTP) - ✘ Failed to connect — endpoint not found\n", "unhealthy", "unknown", true},
		{"other servers only", "claude.ai Figma: https://mcp.figma.com/mcp - ✔ Connected\ngit-cully-tools: x - ✔ Connected\n", "", "", false},
		{"unrecognized status", "cully: https://x - Starting\n", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			health, auth, found := parseClaudeMCPList(tc.out)
			if health != tc.health || auth != tc.auth || found != tc.found {
				t.Fatalf("got %q %q %v", health, auth, found)
			}
		})
	}
}

func TestOverlayMCPProbeNeverOverridesObservedResponses(t *testing.T) {
	probe := mcpProbeResult{Health: "healthy", Auth: "authenticated"}
	c := cullyStats{}
	overlayMCPProbe(&c, probe)
	if c.Health != "healthy" || c.Auth != "authenticated" || !c.Probed {
		t.Fatalf("unknown state not filled: %+v", c)
	}
	observed := cullyStats{Health: "unhealthy", Auth: "unauthenticated"}
	overlayMCPProbe(&observed, probe)
	if observed.Health != "unhealthy" || observed.Auth != "unauthenticated" || observed.Probed {
		t.Fatalf("a tool response was overridden: %+v", observed)
	}
	partial := cullyStats{Health: "healthy", Auth: "unknown"}
	overlayMCPProbe(&partial, probe)
	if partial.Auth != "authenticated" || !partial.Probed {
		t.Fatalf("unknown auth not filled: %+v", partial)
	}
}
