package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDetailListsToolsAndPromptsAndLoadsDescriptionsOnRequest(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	saved, savedCall := uiHarnessVersion, uiToolDetailsCall
	uiHarnessVersion = func(string) string { return "1.0" }
	t.Cleanup(func() { uiHarnessVersion, uiToolDetailsCall = saved, savedCall })
	_ = os.WriteFile(filepath.Join(ConfigDir(), ".claude.json"), []byte(`{"mcpServers":{"gitlab":{"type":"http","url":"https://g.example/mcp"},"cully":{"type":"http","url":"https://c.example/mcp"}}}`), 0o600)

	if d, err := m.serverDetail("claude", "gitlab"); err != nil || d.Known || len(d.Tools) != 0 {
		t.Fatalf("before any task: %+v %v", d, err)
	}
	p := parseStreamLine([]byte(`{"type":"system","subtype":"init","model":"x","tools":["Bash","mcp__gitlab__list_issues","mcp__gitlab__get_issue","mcp__cully__cully_log"],"slash_commands":["/compact","/mcp__gitlab__review"]}`))
	if len(p.InitTools) != 4 || len(p.InitCommands) != 2 {
		t.Fatalf("init = %+v", p)
	}
	m.rememberInit(p.InitTools, p.InitCommands)
	d, err := m.serverDetail("claude", "gitlab")
	if err != nil || !d.Known || len(d.Tools) != 2 || d.Tools[0].Name != "get_issue" || len(d.Prompts) != 1 || d.Prompts[0] != "review" || !d.CanLoad || d.Note == "" {
		t.Fatalf("gitlab = %+v %v", d, err)
	}
	// descriptions are loaded only when asked, and then remembered
	calls := 0
	uiToolDetailsCall = func(cwd, server string, def json.RawMessage, tools []string) ([]uiToolDetail, error) {
		calls++
		if server != "gitlab" || len(tools) != 2 || !strings.Contains(string(def), "g.example") {
			t.Fatalf("call = %s %v %s", server, tools, def)
		}
		return []uiToolDetail{{Name: "get_issue", Description: "Get one issue by number.", Params: []uiParam{{Name: "iid", Type: "number", Required: true}}}}, nil
	}
	d, err = m.loadServerDetails("claude", "gitlab")
	if err != nil || calls != 1 || d.DetailsFrom == "" || d.Tools[0].Description != "Get one issue by number." || len(d.Tools[0].Params) != 1 {
		t.Fatalf("loaded = %+v %v", d, err)
	}
	if again, _ := m.serverDetail("claude", "gitlab"); again.Tools[0].Description == "" {
		t.Fatal("the descriptions were not remembered")
	}
	// the Cully server's own tools come from its source, with parameters
	c, err := m.serverDetail("claude", "cully")
	if err != nil || c.DetailsFrom == "" || len(c.Tools) < 10 {
		t.Fatalf("cully = %+v %v", c, err)
	}
	found := false
	for _, tl := range c.Tools {
		if tl.Name == "cully_agent_save" && len(tl.Params) >= 5 && tl.Description != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("cully_agent_save is missing its description or parameters")
	}
	if _, err := m.serverDetail("claude", "../etc"); err == nil {
		t.Fatal("a bad server name was accepted")
	}
	if x, _ := m.serverDetail("codex", "gitlab"); x.Name == "gitlab" {
		t.Fatal("a Claude server was reported for Codex")
	}
}

func TestServerDetailEndpoints(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	saved := uiHarnessVersion
	uiHarnessVersion = func(string) string { return "1.0" }
	t.Cleanup(func() { uiHarnessVersion = saved })
	_ = os.WriteFile(filepath.Join(ConfigDir(), ".claude.json"), []byte(`{"mcpServers":{"cully":{"type":"http","url":"https://c.example/mcp"}}}`), 0o600)
	if r := uiDo(t, "GET", srv.URL+"/api/harness/server?agent=claude&name=cully", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var d uiServerDetail
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/harness/server?agent=claude&name=cully", nil, uiAuthed), &d)
	if d.Name != "cully" || len(d.Tools) < 10 || d.DetailsFrom == "" {
		t.Fatalf("detail = %+v", d)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/harness/server?agent=claude&name=nope", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown server: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/harness/server/details", map[string]any{"agent": "codex", "name": "cully"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("codex details: %d", r.StatusCode)
	}
}
