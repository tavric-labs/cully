package cully

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Health and auth normally come from the responses of Cully tool calls, so a
// session that has not called Cully yet, or whose hooks deliver no events,
// shows them as unknown even when the connection works. The probe asks the
// agent's own CLI for the connection state instead. It makes no model call and
// reads nothing from the project.

const mcpProbeInterval = 10 * time.Minute

type mcpProbeResult struct {
	Health, Auth string
	At           time.Time
}

type mcpProber struct {
	mu  sync.Mutex
	res mcpProbeResult
	ok  bool
}

func (p *mcpProber) set(r mcpProbeResult) {
	p.mu.Lock()
	p.res, p.ok = r, true
	p.mu.Unlock()
}

func (p *mcpProber) get() (mcpProbeResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.res, p.ok
}

// parseClaudeMCPList reads `claude mcp list` output for the Cully server. A
// connected server is healthy and authenticated; one that needs authentication
// is unauthenticated; a failure says nothing about authentication.
func parseClaudeMCPList(out string) (health, auth string, found bool) {
	for _, line := range strings.Split(out, "\n") {
		name, rest, ok := strings.Cut(line, ": ")
		if !ok || !(name == "cully" || strings.HasPrefix(name, "cully-")) {
			continue
		}
		status := rest
		if i := strings.LastIndex(rest, " - "); i >= 0 {
			status = rest[i+3:]
		}
		switch {
		case strings.Contains(status, "Connected"):
			return "healthy", "authenticated", true
		case strings.Contains(status, "Needs authentication"):
			return "unhealthy", "unauthenticated", true
		case strings.Contains(status, "Failed") || strings.Contains(status, "✘"):
			return "unhealthy", "unknown", true
		}
		return "", "", false
	}
	return "", "", false
}

// claudeMCPList runs the agent CLI. It is a variable so tests need no CLI.
var claudeMCPList = func(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, claudeExecutable(), "mcp", "list").Output()
	return string(out), err
}

// runMCPProbe probes now and then periodically until ctx ends. Only Claude
// Code reports connection and authentication state this way.
func runMCPProbe(ctx context.Context, agent string, p *mcpProber) {
	if agent != "claude" || os.Getenv("CULLY_ANALYZE_DISABLE") == "1" {
		return
	}
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		out, err := claudeMCPList(probeCtx)
		cancel()
		if err == nil || out != "" {
			if health, auth, found := parseClaudeMCPList(out); found {
				p.set(mcpProbeResult{Health: health, Auth: auth, At: time.Now()})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(mcpProbeInterval):
		}
	}
}

// overlayMCPProbe fills only the fields that responses have not established.
func overlayMCPProbe(c *cullyStats, r mcpProbeResult) {
	known := func(v string) bool { return v != "" && v != "unknown" }
	if !known(c.Health) && known(r.Health) {
		c.Health, c.Probed = r.Health, true
	}
	if !known(c.Auth) && known(r.Auth) {
		c.Auth, c.Probed = r.Auth, true
	}
}
