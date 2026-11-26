package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiWriteCodexRollout(t *testing.T, id, cwd string) string {
	t.Helper()
	home := os.Getenv("CODEX_HOME")
	dir := filepath.Join(home, "sessions", "2026", "11", "26")
	_ = os.MkdirAll(dir, 0o755)
	base := time.Now().Add(-3 * time.Hour).UTC()
	ts := func(m int) string { return base.Add(time.Duration(m) * time.Minute).Format(time.RFC3339Nano) }
	item := func(m int, p map[string]any) map[string]any {
		return map[string]any{"type": "response_item", "timestamp": ts(m), "payload": p}
	}
	recs := []map[string]any{
		{"type": "session_meta", "timestamp": ts(0), "payload": map[string]any{"id": id, "cwd": cwd, "timestamp": ts(0), "originator": "codex-tui", "source": "cli"}},
		item(0, map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "<skills_instructions> hidden"}}}),
		item(0, map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "<environment_context>x</environment_context>"}}}),
		item(1, map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "SECRET fix the parser"}}}),
		item(2, map[string]any{"type": "custom_tool_call", "name": "exec", "input": "tools.exec_command({cmd:\"ls\"})"}),
		item(3, map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: pkg/parser.go\n@@\n*** Add File: pkg/new.go\n*** End Patch"}),
		item(4, map[string]any{"type": "custom_tool_call", "name": "exec", "input": "tools.exec_command({cmd:\"go test ./...\"})"}),
		item(5, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "Parser fixed."}}}),
		item(300, map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "one more"}}}),
	}
	var b strings.Builder
	for _, r := range recs {
		l, _ := json.Marshal(r)
		b.Write(l)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, "rollout-2026-11-26T01-00-00-"+id+".jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(p, mt, mt)
	return p
}

func TestCodexSessionsAreCountedLikeClaudeOnes(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	p := uiWriteCodexRollout(t, uiTestSessA, repo)
	f, dg, ok := scanCodex(p, true)
	if !ok || f.Turns != 2 || f.Tools != 3 || f.Edits != 1 || f.Checks == 0 || f.Files != 2 || f.Unverified {
		t.Fatalf("facts = %+v ok=%v", f, ok)
	}
	if f.ToolNames["exec"] != 2 || f.ToolNames["apply_patch"] != 1 {
		t.Fatalf("tools = %v", f.ToolNames)
	}
	if f.ActiveSec < 4*60 || f.ActiveSec > 7*60 {
		t.Fatalf("active = %d", f.ActiveSec)
	}
	if !strings.Contains(dg.First, "fix the parser") || dg.Last != "Parser fixed." || len(dg.Files) != 2 {
		t.Fatalf("digest = %+v", dg)
	}
	raw, _ := json.Marshal(f)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("conversation text in the facts")
	}
	m.indexSessions(10)
	lib := m.sessionLibrary()
	if len(lib.Sessions) != 1 || lib.Sessions[0].Agent != "codex" || lib.Sessions[0].Origin != "Terminal" || lib.Sessions[0].File != "" {
		t.Fatalf("library = %+v", lib.Sessions)
	}
}

func TestLibraryCoversEveryHarnessAndNamesThem(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	uiWriteRichTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessB, repo)
	uiWriteCodexRollout(t, uiTestSessA, repo)
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: x\nCHECKS: y", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	m.analyzeNext()
	m.analyzeNext()
	agents := map[string]bool{}
	for _, s := range m.sessionLibrary().Sessions {
		agents[s.Agent] = true
		if s.Source != "ai" {
			t.Fatalf("%s session %s was not named", s.Agent, s.ID)
		}
	}
	if !agents["claude"] || !agents["codex"] {
		t.Fatalf("harnesses = %v", agents)
	}
}

func TestAnEarlierSessionCanBeContinuedInANewTaskWithItsHandoff(t *testing.T) {
	m, repo, out := uiIndexManager(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	uiWriteCodexRollout(t, uiTestSessA, repo)
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		return "TITLE: Fix the parser\nSUMMARY: Patched the parser and ran tests.\nDIFFERENTLY: Nothing notable.\nCOMPLETED: partly\nON_TASK: yes\nDRIFT: None.\nTASK: Fix the parser.\nCHECKS: tests pass", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	m.analyzeNext()
	h, err := m.sessionEntry(uiTestSessA)
	if err != nil {
		t.Fatal(err)
	}
	text := uiSessionHandoff(h)
	for _, want := range []string{"earlier Codex session", "Fix the parser.", "Patched the parser", "only partly finished", "rollout-"} {
		if !strings.Contains(text, want) {
			t.Errorf("handoff lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "SECRET") {
		t.Fatal("conversation text in the handoff")
	}
	a, err := m.continueFromSession(uiTestSessA, "")
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	in, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
	if !strings.Contains(string(in), "Continue the work of an earlier Codex session") {
		t.Fatalf("the new task was not given the handoff: %s", in)
	}
	if _, err := m.continueFromSession("not-a-session", ""); err == nil {
		t.Fatal("bad id accepted")
	}
	m.cfg.HideSessionsIn = []string{repo}
	if _, err := m.continueFromSession(uiTestSessA, ""); err == nil {
		t.Fatal("a hidden folder session was continued")
	}
}

func uiWriteCursorChat(t *testing.T, id, cwd string) string {
	t.Helper()
	slug := strings.ReplaceAll(strings.TrimPrefix(cwd, "/"), "/", "-")
	dir := filepath.Join(os.Getenv("CURSOR_HOME"), "projects", slug, "agent-transcripts", id)
	_ = os.MkdirAll(dir, 0o755)
	rec := func(role string, blocks ...map[string]any) string {
		b, _ := json.Marshal(map[string]any{"role": role, "message": map[string]any{"content": blocks}})
		return string(b) + "\n"
	}
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	tool := func(name string, in map[string]any) map[string]any {
		return map[string]any{"type": "tool_use", "name": name, "input": in}
	}
	body := rec("user", text("<timestamp>Friday, Oct 9, 2026, 3:56 PM (UTC)</timestamp>\n<user_query>\nSECRET add the export button\n</user_query>")) +
		rec("assistant", text("Looking at the code."), tool("Read", map[string]any{"path": cwd + "/a.go"})) +
		rec("assistant", tool("StrReplace", map[string]any{"path": cwd + "/ui/export.go", "old_string": "a", "new_string": "b"})) +
		rec("assistant", tool("Shell", map[string]any{"command": "go test ./..."})) +
		rec("assistant", text("Export button added.")) +
		rec("user", text("<user_query>now style it</user_query>"))
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(p, mt, mt)
	return p
}

func TestCursorChatsAreCountedAndTheirFolderIsFoundFromTheProjectName(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	t.Setenv("CURSOR_HOME", t.TempDir())
	p := uiWriteCursorChat(t, uiTestSessC, repo)
	f, dg, ok := scanCursor(p, true)
	if !ok || f.Turns != 2 || f.Tools != 3 || f.Edits != 1 || f.Checks != 1 || f.Files != 1 || f.Unverified {
		t.Fatalf("facts = %+v ok=%v", f, ok)
	}
	if !strings.Contains(dg.First, "add the export button") || strings.Contains(dg.First, "<user_query>") || dg.Last != "Export button added." {
		t.Fatalf("digest = %+v", dg)
	}
	if got := uiCursorSlugPath(strings.ReplaceAll(strings.TrimPrefix(repo, "/"), "/", "-")); got != repo {
		t.Fatalf("slug path = %q, want %q", got, repo)
	}
	m.indexSessions(10)
	lib := m.sessionLibrary()
	if len(lib.Sessions) != 1 || lib.Sessions[0].Agent != "cursor" || lib.Sessions[0].Cwd != repo || lib.Sessions[0].Origin != "Cursor" {
		t.Fatalf("library = %+v", lib.Sessions)
	}
	raw, _ := json.Marshal(lib)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("conversation text in the library")
	}
	h := m.history("cursor", 7)
	if len(h.Sessions) != 1 || !strings.Contains(h.Sessions[0].Resume, "cursor-agent --resume "+uiTestSessC) {
		t.Fatalf("history = %+v", h.Sessions)
	}
}

func TestCodexCommandFailuresAreCountedFromTheirExitCode(t *testing.T) {
	_, repo, _ := uiIndexManager(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "11", "26")
	_ = os.MkdirAll(dir, 0o755)
	call := func(id string) string {
		b, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "exec", "call_id": id, "input": "tools.exec_command({cmd:\"make build\"})"}})
		return string(b) + "\n"
	}
	out := func(id, text string) string {
		b, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call_output", "call_id": id, "output": []any{map[string]any{"type": "input_text", "text": text}}}})
		return string(b) + "\n"
	}
	body := `{"type":"session_meta","payload":{"id":"` + uiTestSessA + `","cwd":"` + repo + `"}}` + "\n" +
		call("c1") + out("c1", "Process exited with code 1\nboom") + call("c2") + out("c2", "Process exited with code 1") + call("c3") + out("c3", "Process exited with code 1") + call("c4") + out("c4", "exit code 0\nfine")
	p := filepath.Join(dir, "rollout-x-"+uiTestSessA+".jsonl")
	_ = os.WriteFile(p, []byte(body), 0o600)
	f, _, ok := scanCodex(p, false)
	if !ok || f.Errors != 3 || f.MaxFailRepeat != 3 {
		t.Fatalf("facts = %+v", f)
	}
}

func TestPagesShowContextHarnessInfoOverviewAndSessionActions(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"Start clean from a handoff", "function openRunInfo", "How this task runs", "attachSessionActions", "Start a new task from its handoff", "Right now", "Worth a look", "Your agents", "Sessions by harness, 7 days", "Context window"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
