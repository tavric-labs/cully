package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessInventoryReadsEachHarnessAndLeavesSecretsOut(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	saved := uiHarnessVersion
	uiHarnessVersion = func(agent string) string {
		if agent == "cursor" {
			return ""
		}
		return agent + " 9.9"
	}
	t.Cleanup(func() { uiHarnessVersion = saved })
	cc := ConfigDir()
	_ = os.MkdirAll(filepath.Join(cc, "skills", "deploy"), 0o755)
	_ = os.WriteFile(filepath.Join(cc, "skills", "deploy", "SKILL.md"), []byte("---\nname: deploy\ndescription: Ship a release safely\n---\nbody"), 0o600)
	_ = os.MkdirAll(filepath.Join(cc, "commands"), 0o755)
	_ = os.WriteFile(filepath.Join(cc, "commands", "ship.md"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(cc, "CLAUDE.md"), []byte("rules"), 0o600)
	_ = os.WriteFile(filepath.Join(cc, "settings.json"), []byte(`{"model":"sonnet","permissions":{"allow":["Bash(ls:*)","Read"],"deny":["Bash(rm:*)"],"defaultMode":"acceptEdits"},"enabledPlugins":{"superpowers@x":true},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/usr/local/bin/guard --token SECRET-ARG"}]}],"Stop":[{"hooks":[{"type":"command","command":"cully _internal stop"}]}]}}`), 0o600)
	_ = os.WriteFile(filepath.Join(cc, ".claude.json"), []byte(`{"mcpServers":{"cully":{"type":"http","url":"https://x.example/cully/mcp?key=SECRET-QUERY","headers":{"Authorization":"Bearer SECRET-HEADER"}},"local":{"command":"/opt/bin/tool","args":["--secret","SECRET-ARG2"],"env":{"K":"SECRET-ENV"}}}}`), 0o600)
	m.rememberMCP([]uiMCPServer{{Name: "cully", Status: "connected"}})
	c := m.harnessInfo("claude")
	if !c.Installed || c.Version != "claude 9.9" || c.Model != "sonnet" || c.Allow != 2 || c.Deny != 1 || c.Mode != "acceptEdits" || !c.TasksUseIt {
		t.Fatalf("claude = %+v", c)
	}
	if len(c.MCP) != 2 || c.MCP[0].Name != "cully" || c.MCP[0].Target != "https://x.example/cully/mcp" || c.MCP[0].Status != "connected" || c.MCP[1].Target != "tool" {
		t.Fatalf("mcp = %+v", c.MCP)
	}
	if len(c.Skills) != 1 || c.Skills[0].Name != "deploy" || c.Skills[0].Description != "Ship a release safely" || len(c.Commands) != 1 || c.Plugins[0] != "superpowers@x" || c.Instructions[0] != "CLAUDE.md" {
		t.Fatalf("claude extras = %+v", c)
	}
	var stop, pre *uiInfoHook
	for i := range c.Hooks {
		switch c.Hooks[i].Event {
		case "Stop":
			stop = &c.Hooks[i]
		case "PreToolUse":
			pre = &c.Hooks[i]
		}
	}
	if stop == nil || pre == nil || pre.Runs[0] != "guard" || stop.Runs[0] != "cully" {
		t.Fatalf("hooks = %+v", c.Hooks)
	}
	raw := strings.Join([]string{c.MCP[0].Target, c.MCP[1].Target, strings.Join(pre.Runs, ",")}, " ")
	if strings.Contains(raw, "SECRET") {
		t.Fatalf("a secret reached the inventory: %s", raw)
	}

	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	_ = os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt-x\"\napproval_policy = \"on-request\"\nsandbox_mode = \"workspace-write\"\n[mcp_servers.docs]\nurl = \"https://d.example/mcp?token=SECRET\"\n[mcp_servers.local]\ncommand = \"npx\"\nargs = [\"x\"]\n[projects.\"/p\"]\ntrust_level = \"trusted\"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("x"), 0o600)
	x := m.harnessInfo("codex")
	if x.Model != "gpt-x" || x.Mode != "on-request / workspace-write" || len(x.MCP) != 2 || x.MCP[0].Name != "docs" || x.MCP[0].Target != "https://d.example/mcp" || x.MCP[1].Target != "npx" || x.Instructions[0] != "AGENTS.md" || x.TasksUseIt {
		t.Fatalf("codex = %+v", x)
	}

	ch := t.TempDir()
	t.Setenv("CURSOR_HOME", ch)
	_ = os.WriteFile(filepath.Join(ch, "mcp.json"), []byte(`{"mcpServers":{"gitlab":{"url":"https://g.example/mcp","headers":{"X":"SECRET"}}}}`), 0o600)
	cu := m.harnessInfo("cursor")
	if cu.Installed || len(cu.MCP) != 1 || cu.MCP[0].Name != "gitlab" || cu.MCP[0].Target != "https://g.example/mcp" || len(cu.Notes) == 0 {
		t.Fatalf("cursor = %+v", cu)
	}
}

func TestHarnessEndpoint(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	saved := uiHarnessVersion
	uiHarnessVersion = func(string) string { return "1.0" }
	t.Cleanup(func() { uiHarnessVersion = saved })
	if r := uiDo(t, "GET", srv.URL+"/api/harness", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var v uiHarnessInfo
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/harness", nil, uiAuthed), &v)
	if v.Agent != "claude" || !v.TasksUseIt || v.MCP == nil || v.Skills == nil || v.Hooks == nil {
		t.Fatalf("harness = %+v", v)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/harness?agent=bogus", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bogus: %d", r.StatusCode)
	}
}

func TestHarnessPageIsInTheNavigationAndShowsServersSkillsAndHooks(t *testing.T) {
	idx, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(idx), `href="#/harness"`) || !strings.Contains(string(idx), `id="view-harness"`) {
		t.Error("index.html has no Harness page")
	}
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"function renderHarness", "Connected servers", "function openServerModal", "role: 'switch'", "Load descriptions and parameters", "Resources", "Tasks run here", "#/harness/permissions", "/api/harness?agent="} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	if strings.Contains(string(b), "'Harness permissions', 'What the underlying harness may do'") {
		t.Error("the old dashboard entry for harness permissions is still listed")
	}
}
