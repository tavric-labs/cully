package cully

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
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
	var failedHealth, failedAuth string
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
			failedHealth, failedAuth = "unhealthy", "unauthenticated"
		case strings.Contains(status, "Failed") || strings.Contains(status, "✘"):
			failedHealth = "unhealthy"
			if failedAuth == "" {
				failedAuth = "unknown"
			}
		}
	}
	if failedHealth != "" {
		return failedHealth, failedAuth, true
	}
	return "", "", false
}

// claudeMCPList runs the agent CLI. It is a variable so tests need no CLI.
var claudeMCPList = func(ctx context.Context) (string, error) {
	out, err := mcpProbeCommand(ctx, claudeExecutable(), "mcp", "list").Output()
	return string(out), err
}

var cursorMCPTools = func(ctx context.Context) (string, error) {
	out, err := mcpProbeCommand(ctx, "cursor-agent", "mcp", "list-tools", "cully").Output()
	return string(out), err
}

func mcpProbeCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "CULLY_PANE_SESSION", "CULLY_SESSION", "CULLY_AGENT", "MODEL_HINT_GUARD":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "MODEL_HINT_GUARD=1")
	return cmd
}

// agentMCPProbe uses the selected agent's own connection and credentials.
// Configuration or a stored OAuth token alone never proves a working connection.
var agentMCPProbe = func(ctx context.Context, agent string) (string, string, bool) {
	switch normalizeAgentID(agent) {
	case "claude":
		out, _ := claudeMCPList(ctx)
		return parseClaudeMCPList(out)
	case "codex":
		return probeCodexMCP(ctx)
	case "cursor":
		out, err := cursorMCPTools(ctx)
		if err == nil && strings.Contains(out, "cully_context") {
			return "healthy", "authenticated", true
		}
	}
	return "", "", false
}

// runMCPProbe probes now and periodically until ctx ends, without a model call.
func runMCPProbe(ctx context.Context, agent string, p *mcpProber) {
	if os.Getenv("CULLY_ANALYZE_DISABLE") == "1" {
		return
	}
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		health, auth, found := agentMCPProbe(probeCtx, agent)
		cancel()
		delay := mcpProbeInterval
		if !found {
			health, auth = "unknown", "unknown"
		}
		if health != "healthy" {
			delay = 30 * time.Second
		}
		p.set(mcpProbeResult{Health: health, Auth: auth, At: time.Now()})
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// parseCodexMCPStatus requires discovered Cully tools as connection evidence.
// authStatus describes the credential type, not whether those credentials work.
func parseCodexMCPStatus(raw json.RawMessage) (string, string, bool) {
	var result struct {
		Data []struct {
			Name          string                     `json:"name"`
			Tools         map[string]json.RawMessage `json:"tools"`
			RuntimeStatus string                     `json:"runtimeStatus"`
			ToolsError    *string                    `json:"toolsError"`
			AuthStatus    string                     `json:"authStatus"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", "", false
	}
	var failedHealth, failedAuth string
	for _, server := range result.Data {
		if server.Name != "cully" && !strings.HasPrefix(server.Name, "cully-") {
			continue
		}
		if server.RuntimeStatus == "authenticationRequired" {
			failedHealth, failedAuth = "unhealthy", "unauthenticated"
			continue
		}
		if server.RuntimeStatus == "failed" || server.ToolsError != nil {
			failedHealth = "unhealthy"
			if failedAuth == "" {
				failedAuth = "unknown"
			}
			if server.AuthStatus == "notLoggedIn" {
				failedAuth = "unauthenticated"
			}
			continue
		}
		if server.RuntimeStatus != "" && server.RuntimeStatus != "connected" {
			continue
		}
		for tool := range server.Tools {
			if tool == "cully_context" {
				return "healthy", "authenticated", true
			}
		}
	}
	if failedHealth != "" {
		return failedHealth, failedAuth, true
	}
	return "", "", false
}

// overlayMCPProbe fills only the fields that responses have not established.
func overlayMCPProbe(c *cullyStats, r mcpProbeResult) {
	known := func(v string) bool { return v != "" && v != "unknown" }
	if !known(c.Health) {
		c.Health, c.Probed = r.Health, true
	}
	if !known(c.Auth) {
		c.Auth, c.Probed = r.Auth, true
	}
}
