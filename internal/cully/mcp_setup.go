package cully

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// AddMCP registers a remote service. It neither deploys servers nor stores OAuth tokens.
func AddMCP(w io.Writer, agent, endpoint string, oauth bool) error {
	if err := validateMCPURL(endpoint); err != nil {
		return err
	}
	if agent == "" {
		var detected []string
		for _, spec := range agentCatalog() {
			if codingAgentInstalled(spec.ID) {
				detected = append(detected, spec.ID)
			}
		}
		if len(detected) != 1 {
			return fmt.Errorf("select one agent with --agent claude, --agent codex, or --agent cursor (detected %d)", len(detected))
		}
		agent = detected[0]
	}
	spec, ok := lookupAgentSpec(agent)
	if !ok {
		return fmt.Errorf("unknown MCP agent %q; choose one of claude, codex or cursor", agent)
	}
	for _, target := range []string{agent} {
		path, err := spec.MCPPath()
		if err == nil {
			err = spec.SetupMCP(path, endpoint)
		}
		if err != nil {
			return fmt.Errorf("configure %s MCP: %w", target, err)
		}
		fmt.Fprintf(w, "Cully MCP configured for %s in %s\n", target, path)
		if oauth {
			fmt.Fprintln(w, spec.SignInHint)
		} else {
			fmt.Fprintln(w, "Connect to Cully directly; this server does not require sign-in.")
		}
	}
	fmt.Fprintf(w, "Remote MCP: %s\n", endpoint)
	fmt.Fprintln(w, "The server must be deployed before the connection can succeed. Data, PostgreSQL and Mem0 are configured by its operator.")
	return nil
}

// RemoveMCPIfMatching removes a Cully connection only when its URL matches an
// endpoint created by the local self-hosted setup. Other MCP settings remain.
func RemoveMCPIfMatching(agent string, endpoints []string) error {
	if len(endpoints) == 0 {
		return nil
	}
	spec, ok := lookupAgentSpec(agent)
	if !ok {
		return fmt.Errorf("unknown MCP agent %q", agent)
	}
	path, err := spec.MCPPath()
	if err != nil {
		return err
	}
	return spec.RemoveMCP(path, endpoints)
}

func matchingMCPEndpoint(value any, endpoints []string) bool {
	url, ok := value.(string)
	if !ok {
		return false
	}
	for _, endpoint := range endpoints {
		if url == endpoint {
			return true
		}
	}
	return false
}

func removeJSONMCPIfMatching(path string, endpoints []string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	config, err := loadSettings(path)
	if err != nil {
		return err
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}
	entry, ok := servers["cully"].(map[string]any)
	if !ok || !matchingMCPEndpoint(entry["url"], endpoints) {
		return nil
	}
	if len(entry) > 2 || len(entry) == 2 && entry["type"] != "http" && entry["type"] != "streamable-http" {
		return nil
	}
	delete(servers, "cully")
	if len(servers) == 0 {
		delete(config, "mcpServers")
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeMCPConfig(path, append(data, '\n'))
}

func removeTOMLMCPIfMatching(path string, endpoints []string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	servers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		return nil
	}
	entry, ok := servers["cully"].(map[string]any)
	if !ok || len(entry) != 1 || !matchingMCPEndpoint(entry["url"], endpoints) {
		return nil
	}
	text := string(data)
	start, end, ok := tomlTableBounds(text, "mcp_servers.cully")
	if !ok {
		return nil
	}
	header := strings.LastIndex(text[:start], "[mcp_servers.cully]")
	if header < 0 {
		return nil
	}
	updated := strings.TrimRight(text[:header], "\n") + "\n" + text[end:]
	if err := toml.Unmarshal([]byte(updated), &config); err != nil {
		return fmt.Errorf("cannot safely remove Cully MCP table: %w", err)
	}
	return writeMCPConfig(path, []byte(updated))
}

func validateMCPURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return fmt.Errorf("MCP URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	return nil
}

func claudeMCPConfigPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".claude.json"), err
}

func addJSONMCP(path, endpoint string, httpType bool) error {
	config, err := loadSettings(path)
	if err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("%s: configuration must be an object", path)
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if _, exists := config["mcpServers"]; exists && !ok {
		return fmt.Errorf("%s: mcpServers must be an object", path)
	}
	if servers == nil {
		servers = map[string]any{}
	}
	if entry, exists := servers["cully"]; exists {
		old, ok := entry.(map[string]any)
		if ok && old["url"] == endpoint && old["command"] == nil && (!httpType || old["type"] == "http" || old["type"] == "streamable-http") {
			return nil
		}
		return fmt.Errorf("%s already has a different cully server; edit that entry explicitly before adding another endpoint", path)
	}
	entry := map[string]any{"url": endpoint}
	if httpType {
		entry["type"] = "http"
	}
	servers["cully"] = entry
	config["mcpServers"] = servers
	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeMCPConfig(path, append(b, '\n'))
}

func addTOMLMCP(path, endpoint string) error {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var config map[string]any
	if err := toml.Unmarshal(b, &config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	servers, ok := config["mcp_servers"].(map[string]any)
	if _, exists := config["mcp_servers"]; exists && !ok {
		return fmt.Errorf("mcp_servers must be a table")
	}
	if entry, exists := servers["cully"]; exists {
		old, ok := entry.(map[string]any)
		if ok && old["url"] == endpoint && old["command"] == nil {
			return nil
		}
		return fmt.Errorf("%s already has a different cully server; edit that entry explicitly before adding another endpoint", path)
	}
	// Append only our table, preserving all existing formatting and comments.
	text := strings.TrimRight(string(b), "\n") + "\n\n[mcp_servers.cully]\nurl = " + strconv.Quote(endpoint) + "\n"
	if err := toml.Unmarshal([]byte(text), &config); err != nil {
		return fmt.Errorf("cannot safely append MCP table; configure cully manually: %w", err)
	}
	return writeMCPConfig(path, []byte(text))
}

func writeMCPConfig(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cully-mcp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
