package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exercise the real CLI and setup script with isolated agent configuration and
// a Docker stand-in. PATH deliberately has no Python or installed coding agents.
func TestSetupLifecycle(t *testing.T) {
	cli := filepath.Join(t.TempDir(), "cully")
	build := exec.Command("go", "build", "-o", cli, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, output)
	}
	for _, test := range []struct {
		name, endpoint, failure string
		args                    []string
		agents                  []string
		prepare, oauth, remote  bool
	}{
		{name: "detect agents", args: []string{"--all"}, agents: []string{"codex", "cursor"}},
		{name: "selected agent", args: []string{"--agent", "codex", "--all"}, agents: []string{"codex"}},
		{name: "all agents", args: []string{"--agent", "all", "--all"}, agents: []string{"claude", "codex", "cursor"}},
		{name: "OAuth", args: []string{"--agent", "codex", "--oauth", "--mcp-url", "mock"}, agents: []string{"codex"}, oauth: true, remote: true},
		{name: "existing server", args: []string{"--agent", "codex", "--mcp-url", "https://mcp.example.test/mcp"}, agents: []string{"codex"}, remote: true, endpoint: "https://mcp.example.test/mcp"},
		{name: "prepare only", args: []string{"--all", "--prepare"}, prepare: true},
		{name: "Docker failure", args: []string{"--all"}, failure: "Docker"},
		{name: "advisor failure", args: []string{"--all"}, failure: "advisor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mcpServer := sdk.NewServer(&sdk.Implementation{Name: "cully-lifecycle", Version: "1"}, nil)
			sdk.AddTool(mcpServer, &sdk.Tool{Name: "cully_context", Description: "test"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, struct{}, error) {
				return nil, struct{}{}, nil
			})
			handler := http.Handler(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return mcpServer }, nil))
			if test.oauth {
				handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://example.test/metadata"`)
					w.WriteHeader(http.StatusUnauthorized)
				})
			}
			protocolServer := httptest.NewServer(handler)
			defer protocolServer.Close()
			endpointURL := protocolServer.URL + "/mcp"
			args := append([]string(nil), test.args...)
			for i := range args {
				if i > 0 && args[i-1] == "--mcp-url" {
					args[i] = endpointURL
				}
			}

			root := t.TempDir()
			project := filepath.Join(root, "my project")
			bin := filepath.Join(root, "bin")
			setupDir := filepath.Join(root, ".cully", "self-hosted", "releases", "main", "deploy", "self-hosted")
			for _, dir := range []string{project, bin, setupDir, filepath.Join(root, ".codex"), filepath.Join(root, ".cursor")} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"setup.sh", ".env.example", "connectors.keycloak.example.json"} {
				contents, err := os.ReadFile(filepath.Join("..", "..", "deploy", "self-hosted", name))
				if err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(setupDir, name), string(contents))
			}
			for _, name := range []string{"sh", "dirname", "cp", "chmod"} {
				path, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "docker"), `#!/bin/sh
printf '%s\n' "$*" >> "$HOME/docker-calls"
case "$*" in
  *" up -d --wait db mem0-db")
    if [ "$TEST_DOCKER_FAIL" = 1 ]; then echo 'database startup failed' >&2; exit 1; fi ;;
  *" port mcp 8080") echo "$TEST_MCP_ADDRESS" ;;
esac
`)
			if test.oauth {
				config := filepath.Join(root, ".cully", "self-hosted", "config")
				if err := os.MkdirAll(filepath.Join(config, ".secrets"), 0o700); err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(config, ".env"), "CULLY_MCP_HOST=mcp.acme.test\nCULLY_AUTH_HOST=auth.acme.test\nMCP_AUTH_UPSTREAM_CLIENT_SECRET=private-value\n")
				write(filepath.Join(config, "connectors.json"), `{"org":{"client_secret_env":"MCP_AUTH_UPSTREAM_CLIENT_SECRET"}}`)
				write(filepath.Join(config, ".secrets", "signing-key.pem"), "test-key")
			}
			env := append(os.Environ(), "HOME="+root, "PATH="+bin,
				"CLAUDE_CONFIG_DIR="+filepath.Join(root, ".claude"),
				"CODEX_HOME="+filepath.Join(root, ".codex"),
				"CURSOR_CONFIG_DIR="+filepath.Join(root, ".cursor"),
				"CULLY_CONFIG_PATH="+filepath.Join(root, ".cully", "config.json"),
				"CULLY_ANALYZE_DISABLE=", "TEST_DOCKER_FAIL=0", "TEST_MCP_ADDRESS="+strings.TrimPrefix(protocolServer.URL, "http://"))
			if test.failure == "Docker" {
				env = append(env, "TEST_DOCKER_FAIL=1")
			}
			if test.failure == "advisor" {
				env = append(env, "CULLY_ANALYZE_DISABLE=1")
			}
			runCLI := func(args ...string) (string, error) {
				t.Helper()
				cmd := exec.Command(cli, args...)
				cmd.Dir, cmd.Env = project, env
				output, err := cmd.CombinedOutput()
				return string(output), err
			}
			t.Cleanup(func() {
				if output, err := runCLI("_internal", "stop-daemon"); err != nil {
					t.Errorf("stop test advisor: %v: %s", err, output)
				}
			})
			output, err := runCLI(append([]string{"setup"}, args...)...)
			if test.failure != "" {
				if err == nil || strings.Contains(output, "Setup complete.") {
					t.Fatalf("failed component reported success: %v: %s", err, output)
				}
				message := "database startup failed"
				if test.failure == "advisor" {
					message = "advisor startup failed"
				}
				if !strings.Contains(output, message) {
					t.Fatalf("missing actionable failure: %s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("setup: %v: %s", err, output)
			}
			dockerCalls, _ := os.ReadFile(filepath.Join(root, "docker-calls"))
			if test.prepare {
				if len(dockerCalls) != 0 || strings.Contains(output, "Advisor ready") {
					t.Fatalf("prepare started components: %s", output)
				}
				if _, err := os.Stat(filepath.Join(root, ".codex", "hooks.json")); !os.IsNotExist(err) {
					t.Fatalf("prepare installed agent hooks: %v", err)
				}
				return
			}
			if test.remote && len(dockerCalls) != 0 {
				t.Fatalf("remote setup started Docker: %s", dockerCalls)
			}
			if !test.remote {
				for stage := 1; stage <= 7; stage++ {
					if !strings.Contains(output, fmt.Sprintf("[%d/7]", stage)) {
						t.Fatalf("missing progress stage %d: %s", stage, output)
					}
				}
				for _, command := range []string{"up -d --wait db mem0-db", "run --rm migrate", "up -d --build --wait data-api mem0 mcp"} {
					if !strings.Contains(string(dockerCalls), command) {
						t.Fatalf("missing service startup %q: %s", command, dockerCalls)
					}
				}
			}
			endpoint := endpointURL
			if test.oauth && (!strings.Contains(output, "codex mcp login cully") || !strings.Contains(output, "pending") || strings.Contains(output, "private-value")) {
				t.Fatalf("OAuth instructions or verification failed: %s", output)
			}

			configs := map[string]string{"claude": ".claude/.claude.json", "codex": ".codex/config.toml", "cursor": ".cursor/mcp.json"}
			for _, agent := range test.agents {
				content, err := os.ReadFile(filepath.Join(root, configs[agent]))
				if err != nil || !strings.Contains(string(content), endpoint) {
					t.Fatalf("%s MCP not connected: %s, %v", agent, content, err)
				}
				if _, err := os.Stat(filepath.Join(root, "."+agent, "skills", "cully", "SKILL.md")); err != nil {
					t.Fatal(err)
				}
				hooks := "hooks.json"
				if agent == "claude" {
					hooks = "settings.json"
				}
				if _, err := os.Stat(filepath.Join(root, "."+agent, hooks)); err != nil {
					t.Fatalf("missing %s hooks: %v", agent, err)
				}
			}
			if len(test.agents) == 1 {
				if _, err := os.Stat(filepath.Join(root, configs["cursor"])); !os.IsNotExist(err) {
					t.Fatalf("explicit Codex setup modified Cursor: %v", err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, ".codex", "AGENTS.md")); err != nil {
				t.Fatalf("missing user-level integration: %v", err)
			}
			if entries, err := os.ReadDir(project); err != nil || len(entries) != 0 {
				t.Fatalf("setup changed project: %v, %v", entries, err)
			}
			if len(test.agents) > 1 {
				if _, err := os.Stat(filepath.Join(root, ".cursor", "commands", "cully.md")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(setupDir, "AGENTS.md")); !os.IsNotExist(err) {
				t.Fatalf("integration written to downloaded stack: %v", err)
			}
			status, err := runCLI("status")
			if err != nil || !strings.Contains(status, "cully advisor daemon running") || !strings.Contains(output, "Setup complete.") {
				t.Fatalf("setup did not start advisor: %v: %s\n%s", err, status, output)
			}
			pidPath := filepath.Join(root, ".claude", "cully-logs", ".cully-daemon.pid")
			pid, err := os.ReadFile(pidPath)
			if err != nil {
				t.Fatal(err)
			}
			project = filepath.Join(root, "another project")
			if err := os.MkdirAll(project, 0o700); err != nil {
				t.Fatal(err)
			}
			if output, err := runCLI(append([]string{"setup"}, args...)...); err != nil {
				t.Fatalf("repeat setup from another project: %v: %s", err, output)
			}
			if entries, err := os.ReadDir(project); err != nil || len(entries) != 0 {
				t.Fatalf("repeat setup changed project: %v, %v", entries, err)
			}
			repeatedPID, err := os.ReadFile(pidPath)
			if err != nil || string(pid) != string(repeatedPID) {
				t.Fatalf("repeat setup replaced the running advisor: %q -> %q, %v", pid, repeatedPID, err)
			}
		})
	}
}
