package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexRollout builds a Codex session: a session_meta line, then paired tool
// calls and outputs, padded so it counts as a real session.
func codexRollout(cwd string, bigOutputs int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"session_meta","payload":{"cwd":%q}}`+"\n", cwd)
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, `{"type":"event_msg","payload":{"type":"token_count"}}`+"\n")
	}
	for i := 0; i < bigOutputs; i++ {
		id := fmt.Sprintf("call_%d", i)
		fmt.Fprintf(&b, `{"type":"response_item","payload":{"type":"function_call","name":"shell","call_id":%q,"arguments":"{}"}}`+"\n", id)
		fmt.Fprintf(&b, `{"type":"response_item","payload":{"type":"function_call_output","call_id":%q,"output":%q}}`+"\n", id, strings.Repeat("x", 12000))
	}
	id := "call_custom"
	fmt.Fprintf(&b, `{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":%q,"input":"x"}}`+"\n", id)
	fmt.Fprintf(&b, `{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":%q,"output":[{"type":"input_text","text":%q}]}}`+"\n", id, strings.Repeat("y", 9000))
	return b.String()
}

func TestMeasureCodexSessionsByProject(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "10", "16")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(day, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("rollout-a.jsonl", codexRollout("/work/project", 3))
	write("rollout-b.jsonl", codexRollout("/work/project", 1))
	write("rollout-other.jsonl", codexRollout("/work/elsewhere", 5))
	write("notes.txt", "not a rollout")
	src, ok := transcriptSourceFor("codex")
	if !ok {
		t.Fatal("codex transcripts are not readable")
	}
	ev := measureAgentEvidence(src, root, "/work/project", 30, 10)
	// 3+1 shell outputs of 12,000 chars (3,000 tokens) plus two custom outputs
	// of 9,000 chars (2,250 tokens); every one is oversized, all through the shell.
	if ev.Sessions != 2 || ev.BigTokens != 4*3000+2*2250 || ev.BashTokens != ev.BigTokens || ev.ReadTokens != 0 {
		t.Fatalf("codex measurement: %+v", ev)
	}
	total, by := measureAgentProjects(src, root, "work", 30, 10)
	if total.Sessions != 3 || len(by) != 2 || by["/work/elsewhere"].Sessions != 1 {
		t.Fatalf("projects: %+v %v", total, by)
	}
	if none := measureAgentEvidence(src, root, "/work/missing", 30, 10); none.Sessions != 0 {
		t.Fatalf("matched another project: %+v", none)
	}
	// A repeated entry is counted once, as for Claude.
	write("rollout-dup.jsonl", codexRollout("/work/dup", 2)+strings.Repeat(`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_0","output":"`+strings.Repeat("x", 12000)+`"}}`+"\n", 3))
	one := measureAgentEvidence(src, root, "/work/dup", 30, 10)
	if one.BigTokens != 2*3000+2250 {
		t.Fatalf("repeated codex output counted again: %+v", one)
	}
}

func TestDetectTranscriptAgentFollowsTheNewestTranscripts(t *testing.T) {
	t.Setenv("CULLY_AGENT", "")
	claude, codex := t.TempDir(), t.TempDir()
	if got := detectTranscriptAgent("/work/p", t.TempDir()); got != "claude" {
		t.Fatalf("nothing found should default to claude: %s", got)
	}
	day := filepath.Join(codex, "2026", "10", "16")
	_ = os.MkdirAll(day, 0o755)
	_ = os.WriteFile(filepath.Join(day, "rollout-x.jsonl"), []byte(codexRollout("/work/p", 1)), 0o600)
	if got := detectTranscriptAgent("/work/p", codex); got != "codex" {
		t.Fatalf("codex transcripts for this project should win: %s", got)
	}
	_ = claude
	t.Setenv("CULLY_AGENT", "codex")
	if got := detectTranscriptAgent("/work/p", t.TempDir()); got != "codex" {
		t.Fatalf("CULLY_AGENT should win: %s", got)
	}
	t.Setenv("CULLY_AGENT", "cursor")
	if got := detectTranscriptAgent("/work/p", t.TempDir()); got != "claude" {
		t.Fatalf("an unreadable agent must not be chosen: %s", got)
	}
}

func TestHarnessCLIRefusesAnAgentItCannotRead(t *testing.T) {
	var out strings.Builder
	err := RunHarness(&out, []string{"--agent", "cursor", "--cwd", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "Cursor") {
		t.Fatalf("err = %v", err)
	}
	_ = time.Now
}
