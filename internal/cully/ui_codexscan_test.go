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
