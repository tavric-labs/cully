package cully

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// Saving agents to Cully memory. In disk mode an agent lives only in its folder
// on this machine. When the Cully MCP is connected, Cully also saves each
// approved agent there (Postgres, through the cully_agent_save tool), so it
// follows the user to other machines. The page has no MCP login of its own: the
// save is done by one short headless Claude call that is allowed to use that
// one tool and nothing else. Disk stays the source of truth; a failed save is
// retried and never loses the agent.

const uiAgentSyncEvery = 10 * time.Minute

// uiCullyToolCall calls one Cully MCP tool with exact arguments. It is a variable so tests need no real MCP.
var uiCullyToolCall = func(cwd, tool, argsJSON string) error {
	config, cleanup, err := claudeAdvisorMCP(cwd, false)
	if err != nil {
		return err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	full := "mcp__cully__" + tool
	cmd := exec.CommandContext(ctx, claudeExecutable(), "-p", "--model", "haiku", "--permission-mode", "dontAsk", "--no-session-persistence",
		"--tools", "ToolSearch", "--allowedTools", "ToolSearch,"+full, "--strict-mcp-config", "--mcp-config", config)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader("Call the " + tool + " tool exactly once with exactly these arguments. Copy the JSON; change nothing. Then reply with only the word Done. If the tool is not available or returns an error, reply with only Failed.\n" + argsJSON + "\n")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(out))
	if strings.Contains(text, "Failed") || !strings.Contains(text, "Done") {
		return errors.New("the Cully MCP did not accept it")
	}
	return nil
}

// uiCullyMCPConnected reports whether the Cully MCP is signed in and reachable for Claude.
var uiCullyMCPConnected = func(m *uiManager) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, a := range m.mcp.statusFor(ctx, func(agent string) bool { return agent == "claude" }) {
		if a.Agent == "claude" && a.State == "connected" {
			return true
		}
	}
	return false
}

func uiAgentSaveArgs(id uiIdentity) (string, error) {
	def := id
	def.Memory, def.Runs, def.Synced, def.SyncedAt = "", nil, false, time.Time{}
	def.Folder = "" // a path on this machine means nothing on another one
	d, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(map[string]any{"agent_id": id.ID, "name": id.Name, "scope": id.Scope, "approved": id.Approved, "definition": string(d), "notes": id.Memory})
	return string(b), err
}

// syncAgents saves every approved agent that changed since it was last saved.
// It returns how many were saved. Nothing is sent when the MCP is not connected.
func (m *uiManager) syncAgents() int {
	var todo []uiIdentity
	for _, x := range m.identityList() {
		if x.Approved && !x.Synced {
			todo = append(todo, x)
		}
	}
	if len(todo) == 0 || !uiCullyMCPConnected(m) {
		return 0
	}
	cwd := ConfigDir()
	if repos := m.repoList(); len(repos) > 0 {
		cwd = repos[0].Path
	}
	saved := 0
	for _, x := range todo {
		args, err := uiAgentSaveArgs(x)
		if err != nil || uiCullyToolCall(cwd, "cully_agent_save", args) != nil {
			continue
		}
		uiIdentityMu.Lock()
		for _, y := range m.loadIdentitiesLocked() {
			if y.ID == x.ID && y.Memory == x.Memory && y.Name == x.Name && y.Role == x.Role && y.Instructions == x.Instructions {
				y.Synced, y.SyncedAt = true, time.Now().UTC()
			}
		}
		m.saveIdentitiesLocked()
		uiIdentityMu.Unlock()
		saved++
	}
	return saved
}

// syncAgentsIfDue runs the save at most every ten minutes.
func (m *uiManager) syncAgentsIfDue() {
	m.syncMu.Lock()
	if time.Since(m.syncAt) < uiAgentSyncEvery {
		m.syncMu.Unlock()
		return
	}
	m.syncAt = time.Now()
	m.syncMu.Unlock()
	m.syncAgents()
}
