package cully

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolPolicyDefaultsMatchWhatAgentsGetToday(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	allowed, denied := m.toolPolicy(false)
	for _, want := range []string{"Read", "Edit", "Bash", "mcp__cully__cully_context", "mcp__cully__cully_log"} {
		if !strings.Contains(","+allowed+",", ","+want+",") {
			t.Errorf("default allow lacks %s: %s", want, allowed)
		}
	}
	if strings.Contains(allowed, "WebSearch") {
		t.Errorf("web search is on by default: %s", allowed)
	}
	if denied != uiDeniedTools {
		t.Errorf("deny = %s", denied)
	}
	if plan, _ := m.toolPolicy(true); strings.Contains(plan, "cully_log") {
		t.Errorf("plan-only agents may save memory: %s", plan)
	}
}

func TestToolPolicyChangesApplyToNewAgentsAndSurviveARestart(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	err := m.setToolPolicy(uiPolicyInput{Presets: map[string]bool{"web": true, "commands": false}, Allow: []string{"mcp__gitlab__get_issue", "Bash(go test:*)"}, Deny: []string{"Bash(rm:*)"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	for _, want := range []string{"WebSearch", "mcp__gitlab__get_issue", "Bash(go test:*)", "--disallowedTools Bash(rm:*)"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	if strings.Contains(string(args), "Read,Glob,Grep,Edit,Write,Bash,") {
		t.Errorf("run commands is still on: %s", args)
	}
	m2 := newUIManager(m.cfg)
	if allowed, denied := m2.toolPolicy(false); !strings.Contains(allowed, "WebSearch") || denied != "Bash(rm:*)" {
		t.Fatalf("settings were not kept: %s | %s", allowed, denied)
	}
	if info, err := os.Stat(filepath.Join(cullyDir(), "ui-settings.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file = %v, %v", info, err)
	}
	if err := m.setToolPolicy(uiPolicyInput{Presets: nil, Allow: nil, Deny: nil}); err != nil {
		t.Fatal(err)
	}
	if _, denied := m.toolPolicy(false); denied != uiDeniedTools {
		t.Fatalf("reset did not restore the default block list: %s", denied)
	}
}

func TestToolPolicyRejectsBadEntries(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for _, bad := range []string{"", "a,b", "Bash(", "Bash(a,b)", "has space", "(x)", strings.Repeat("a", 200), "--flag"} {
		if err := m.setToolPolicy(uiPolicyInput{Allow: []string{bad}}); err == nil {
			t.Errorf("allow entry %q accepted", bad)
		}
	}
	if err := m.setToolPolicy(uiPolicyInput{Presets: map[string]bool{"nonsense": true}}); err == nil {
		t.Error("unknown preset accepted")
	}
	many := make([]string, 60)
	for i := range many {
		many[i] = "Tool" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if err := m.setToolPolicy(uiPolicyInput{Allow: many}); err == nil {
		t.Error("60 entries accepted")
	}
}

func TestAdminToolsEndpoint(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/admin/tools", nil, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var v uiToolsView
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/admin/tools", nil, uiAuthed), &v)
	if len(v.Presets) < 6 || len(v.DefaultDeny) == 0 || len(v.Effective.Allowed) == 0 {
		t.Fatalf("view = %+v", v)
	}
	r := uiDo(t, "PUT", srv.URL+"/api/admin/tools", map[string]any{"presets": map[string]bool{"web": true}, "allow": []string{"mcp__x__y"}, "deny": nil}, uiAuthed)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("put = %d", r.StatusCode)
	}
	uiReadJSON(t, r, &v)
	on := false
	for _, p := range v.Presets {
		on = on || (p.ID == "web" && p.On)
	}
	if !on || len(v.Allow) != 1 {
		t.Fatalf("view = %+v", v)
	}
	if r := uiDo(t, "PUT", srv.URL+"/api/admin/tools", map[string]any{"allow": []string{"bad entry"}}, uiAuthed); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad = %d", r.StatusCode)
	}
}

func TestTasksCanLoadOnlyTheChosenMCPServers(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	cfg := `{"mcpServers":{"cully":{"type":"http","url":"https://x/cully/mcp","headers":{"Authorization":"Bearer SECRET-VALUE"}},"gitlab":{"type":"http","url":"https://g"},"tgit":{"type":"http","url":"https://t"}}}`
	if err := os.WriteFile(filepath.Join(ConfigDir(), ".claude.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.mcpFilterArgs(); got != nil {
		t.Fatalf("filter applied with no choice made: %v", got)
	}
	if v := m.toolsView(); len(v.Mcp.Available) != 3 || len(v.Mcp.Only) != 0 {
		t.Fatalf("mcp = %+v", v.Mcp)
	}
	if err := m.setToolPolicy(uiPolicyInput{McpOnly: []string{"cully", "nonexistent"}, Deny: nil}); err != nil {
		t.Fatal(err)
	}
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(args), "--strict-mcp-config --mcp-config "+filepath.Join(cullyDir(), "ui-mcp.json")) {
		t.Fatalf("args = %s", args)
	}
	if strings.Contains(string(args), "SECRET-VALUE") {
		t.Fatal("a credential reached the command line")
	}
	b, _ := os.ReadFile(filepath.Join(cullyDir(), "ui-mcp.json"))
	if !strings.Contains(string(b), `"cully"`) || strings.Contains(string(b), "gitlab") || strings.Contains(string(b), "tgit") {
		t.Fatalf("filtered config = %s", b)
	}
	if st, _ := os.Stat(filepath.Join(cullyDir(), "ui-mcp.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode())
	}
	if err := m.setToolPolicy(uiPolicyInput{McpOnly: []string{"nonexistent"}}); err != nil {
		t.Fatal(err)
	}
	if got := m.mcpFilterArgs(); got != nil {
		t.Fatalf("a task would start with no Cully tools: %v", got)
	}
	if err := m.setToolPolicy(uiPolicyInput{McpOnly: []string{"bad name!"}}); err == nil {
		t.Fatal("bad server name accepted")
	}
}

func TestPermissionsPageOffersTheConnectedToolChoiceAndLibraryInsights(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"Connected tools to load", "mcpOnly: state.mcp", "function libraryInsights", "Your last two weeks", "Most used tools", "Look at these first"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
