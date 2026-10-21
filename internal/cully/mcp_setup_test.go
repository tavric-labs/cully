package cully

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testMCPURL = "https://mcp.example.test/mcp"

func TestRemoveLocalMCPPreservesOtherAgentSettings(t *testing.T) {
	const localURL = "http://127.0.0.1:18080/mcp"
	jsonPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(jsonPath, []byte(`{"theme":"dark","mcpServers":{"other":{"command":"other"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addJSONMCP(jsonPath, localURL, true); err != nil {
		t.Fatal(err)
	}
	if err := removeJSONMCPIfMatching(jsonPath, []string{"http://127.0.0.1:8080/mcp"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(jsonPath)
	if err != nil || !strings.Contains(string(before), `"cully"`) {
		t.Fatalf("unrelated endpoint removed: %s, %v", before, err)
	}
	if err := removeJSONMCPIfMatching(jsonPath, []string{localURL}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(jsonPath)
	if err != nil || strings.Contains(string(after), `"cully"`) || !strings.Contains(string(after), `"theme": "dark"`) || !strings.Contains(string(after), `"other"`) {
		t.Fatalf("JSON settings changed: %s, %v", after, err)
	}

	tomlPath := filepath.Join(t.TempDir(), "config.toml")
	const original = "# user settings\nmodel = \"test\"\n\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(tomlPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addTOMLMCP(tomlPath, localURL); err != nil {
		t.Fatal(err)
	}
	if err := removeTOMLMCPIfMatching(tomlPath, []string{localURL}); err != nil {
		t.Fatal(err)
	}
	after, err = os.ReadFile(tomlPath)
	if err != nil || strings.Contains(string(after), "[mcp_servers.cully]") || !strings.Contains(string(after), "[mcp_servers.other]") || !strings.Contains(string(after), "# user settings") {
		t.Fatalf("TOML settings changed: %s, %v", after, err)
	}
}

func TestMCPJSONPreservesOtherConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	original := `{"theme":"dark","mcpServers":{"other":{"command":"other"}}}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := addJSONMCP(path, testMCPURL, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var config map[string]any
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	servers := config["mcpServers"].(map[string]any)
	if config["theme"] != "dark" || servers["other"].(map[string]any)["command"] != "other" || servers["cully"].(map[string]any)["type"] != "http" {
		t.Fatalf("changed config: %s", b)
	}
	if err := addJSONMCP(path, testMCPURL, true); err != nil {
		t.Fatal(err)
	}
	if err := addJSONMCP(path, "https://self.example/mcp", true); err == nil {
		t.Fatal("overwrote existing Cully server")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(b, after) {
		t.Fatal("existing configuration changed")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("MCP config must be private")
	}
}

func TestMCPQuotedTOMLAndConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# keep this comment\nmodel = \"custom\"\n[mcp_servers.\"other\"]\ncommand = \"other\"\n"
	os.WriteFile(path, []byte(original), 0600)
	if err := addTOMLMCP(path, testMCPURL); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), original) {
		t.Fatalf("lost original formatting: %s", b)
	}
	if err := addTOMLMCP(path, testMCPURL); err != nil {
		t.Fatal(err)
	}
	if err := addTOMLMCP(path, "https://self.example/mcp"); err == nil {
		t.Fatal("overwrote existing Cully server")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(b, after) {
		t.Fatal("conflict changed config")
	}
	quoted := []byte("[mcp_servers.\"cully\"]\nurl = \"https://user.example/mcp\"\n")
	os.WriteFile(path, quoted, 0600)
	if err := addTOMLMCP(path, testMCPURL); err == nil {
		t.Fatal("missed quoted Cully table")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(quoted, after) {
		t.Fatal("quoted table changed")
	}
}

func TestMCPInvalidConfigPreserved(t *testing.T) {
	for _, initial := range []string{`null`, `{"mcpServers":[]}`, `{bad json`, `{"mcpServers":{"cully":{"command":"user-owned"}}}`} {
		path := filepath.Join(t.TempDir(), "mcp.json")
		os.WriteFile(path, []byte(initial), 0600)
		if err := addJSONMCP(path, testMCPURL, true); err == nil {
			t.Fatalf("accepted: %s", initial)
		}
		after, _ := os.ReadFile(path)
		if string(after) != initial {
			t.Fatal("modified invalid/user-owned config")
		}
	}
}

func TestAddMCPSingleAgentSelfHosted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(dir, "cursor"))
	endpoint := "https://self.example/mcp"
	var out bytes.Buffer
	if err := AddMCP(&out, "all", endpoint, false); err == nil {
		t.Fatal("accepted all")
	}
	for _, agent := range []string{"claude", "codex", "cursor"} {
		if err := AddMCP(&out, agent, endpoint, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"claude/.claude.json", "codex/config.toml", "cursor/mcp.json"} {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || !strings.Contains(string(b), endpoint) {
			t.Fatalf("%s: %s %v", path, b, err)
		}
	}
	if strings.Contains(out.String(), "codex mcp login cully") || !strings.Contains(out.String(), "discovers whether") || strings.Contains(out.String(), "does not require sign-in") {
		t.Fatal("unspecified authentication should defer to server discovery")
	}
	var oauthOut bytes.Buffer
	if err := AddMCP(&oauthOut, "codex", endpoint, true); err != nil || !strings.Contains(oauthOut.String(), "codex mcp login cully") {
		t.Fatal("missing opt-in OAuth instructions")
	}
	for _, endpoint := range []string{"not-a-url", "file:///tmp/a", "https://user:secret@host/mcp", "https://host/mcp?secret=x"} {
		if err := AddMCP(&out, "codex", endpoint, false); err == nil {
			t.Fatalf("accepted invalid endpoint %s", endpoint)
		}
	}
}

func TestAddMCPAutoDetectionChoosesOneAgent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PATH", dir)
	claude := filepath.Join(dir, "claude")
	codex := filepath.Join(dir, "codex")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(dir, "cursor"))
	os.Mkdir(claude, 0700)
	os.Mkdir(codex, 0700)
	var out bytes.Buffer
	if err := AddMCP(&out, "", testMCPURL, false); err == nil {
		t.Fatal("ambiguous detection should require an explicit agent")
	}
	if _, err := os.Stat(filepath.Join(claude, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("ambiguous detection wrote config")
	}
	os.Remove(codex)
	if err := AddMCP(&out, "", testMCPURL, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claude, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(codex); !os.IsNotExist(err) {
		t.Fatal("single-agent setup configured another agent")
	}
}

func TestHookConfiguredUsesActualSettings(t *testing.T) {
	settings := map[string]any{}
	if hookConfigured(settings, "Stop", "analyze") {
		t.Fatal("missing hook reported installed")
	}
	setEventHook(settings, "Stop", "'/tmp/cully' _internal analyze", "analyze")
	if !hookConfigured(settings, "Stop", "analyze") {
		t.Fatal("installed hook not detected")
	}
	if hookConfigured(settings, "SessionEnd", "cleanup") {
		t.Fatal("missing cleanup reported installed")
	}
}
