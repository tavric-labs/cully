package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfHostedUninstallStopsStackAndPurgesOnlySelfHostedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CULLY_CONFIG_PATH", filepath.Join(home, ".cully", "config.json"))
	base, err := selfHostedBase()
	if err != nil {
		t.Fatal(err)
	}
	setupDir := filepath.Join(base, "releases", "v0.4.1", "deploy", "self-hosted")
	if err := os.MkdirAll(setupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "setup.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(base, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("CULLY_MCP_PORT=18080\nCULLY_MCP_HOST=memory.example.net\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cully"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".cully", "config.json"), []byte(`{"self_hosted":{"db_name":"notes"},"other":"keep"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "docker-args")
	dockerDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dockerDir, "docker"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CULLY_TEST_DOCKER_ARGS\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dockerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CULLY_TEST_DOCKER_ARGS", trace)

	if err := stopSelfHostedStack(false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "compose.custom-auth.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopSelfHostedStack(true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "--volumes") || !strings.Contains(lines[1], "down --volumes") {
		t.Fatalf("Docker Compose down calls = %q", lines)
	}
	if !strings.Contains(lines[0], "--profile oauth --profile mcp-auth down") {
		t.Fatalf("optional services were not included: %q", lines[0])
	}
	if !strings.Contains(lines[1], "compose.custom-auth.yaml") {
		t.Fatal("custom auth service was omitted from teardown")
	}
	endpoints, err := selfHostedEndpoints(base)
	if err != nil || len(endpoints) != 2 || endpoints[0] != "http://127.0.0.1:18080/mcp" || endpoints[1] != "https://memory.example.net/mcp" {
		t.Fatalf("endpoints = %v, %v", endpoints, err)
	}
	if err := purgeSelfHostedData(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("self-hosted stack was not removed: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(home, ".cully", "config.json"))
	if err != nil || strings.Contains(string(config), "self_hosted") || !strings.Contains(string(config), `"other": "keep"`) {
		t.Fatalf("unrelated configuration was changed: %s, %v", config, err)
	}
}

func TestStopSelfHostedStackWithoutDownloadedStack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := stopSelfHostedStack(false); err != nil {
		t.Fatal(err)
	}
}

func TestRunUninstallKeepsThenPurgesMemoryAndPreservesOtherMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	project := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	base, err := selfHostedBase()
	if err != nil {
		t.Fatal(err)
	}
	setupDir := filepath.Join(base, "releases", "v0.4.1", "deploy", "self-hosted")
	if err := os.MkdirAll(setupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "setup.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(base, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("CULLY_MCP_PORT=18080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".cully", "config.json")
	if err := os.WriteFile(configPath, []byte(`{"self_hosted":{"db_name":"notes"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(home, ".codex", "config.toml"): "[mcp_servers.other]\ncommand = \"other\"\n\n[mcp_servers.cully]\nurl = \"http://127.0.0.1:18080/mcp\"\n",
		filepath.Join(home, ".cursor", "mcp.json"):   `{"mcpServers":{"other":{"command":"other"},"cully":{"url":"http://127.0.0.1:18080/mcp"}}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dockerDir := t.TempDir()
	trace := filepath.Join(t.TempDir(), "docker-args")
	if err := os.WriteFile(filepath.Join(dockerDir, "docker"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CULLY_TEST_DOCKER_ARGS\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dockerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CULLY_TEST_DOCKER_ARGS", trace)

	if err := runUninstall(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("default uninstall removed saved credentials: %v", err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("default uninstall removed downloaded stack: %v", err)
	}
	for _, path := range []string{filepath.Join(home, ".codex", "config.toml"), filepath.Join(home, ".cursor", "mcp.json")} {
		content, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(content), "cully") || !strings.Contains(string(content), "other") {
			t.Fatalf("uninstall changed unrelated MCP settings in %s: %s, %v", path, content, err)
		}
	}
	if err := runUninstall([]string{"--purge-data"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("purge kept the stack directory: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("purge kept local credentials: %v", err)
	}
	commands, err := os.ReadFile(trace)
	if err != nil || !strings.Contains(string(commands), "down --volumes") {
		t.Fatalf("purge did not request Docker volume deletion: %s, %v", commands, err)
	}
}

func TestRemoveInstallerLinksPreservesOtherFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	canonical := filepath.Join(home, ".local", "bin", "cully")
	for _, agent := range []string{".claude", ".codex", ".cursor"} {
		directory := filepath.Join(home, agent, "bin")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(directory, "cully")
		switch agent {
		case ".claude":
			if err := os.Symlink(canonical, file); err != nil {
				t.Fatal(err)
			}
		case ".codex":
			if err := os.Symlink("/other/cully", file); err != nil {
				t.Fatal(err)
			}
		case ".cursor":
			if err := os.WriteFile(file, []byte("custom wrapper"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := removeInstallerLinks(home, canonical); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "bin", "cully")); !os.IsNotExist(err) {
		t.Fatalf("managed link remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "bin", "cully")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "bin", "cully")); err != nil {
		t.Fatal(err)
	}
}
