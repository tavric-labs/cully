package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSavingsSession(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func svUse(id, name string) string {
	return `{"message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":{}}]}}`
}

func svResult(id, body string) string {
	return `{"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"` + body + `"}]}}`
}

func TestSavingsMeasuresCostAndCohorts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-proj")
	big := strings.Repeat("x", 4000)
	hit := `{\"notes\":[{\"summary\":\"did it\"}]}`
	for i := 0; i < 2; i++ {
		writeSavingsSession(t, dir, "hit"+string(rune('a'+i))+".jsonl",
			svUse("1", "mcp__cully__cully_context"), svResult("1", hit),
			svUse("2", "Read"), svResult("2", strings.Repeat("y", 400)),
			svUse("3", "Edit"), svResult("3", "ok"))
		writeSavingsSession(t, dir, "none"+string(rune('a'+i))+".jsonl",
			svUse("2", "Read"), svResult("2", big),
			svUse("3", "Edit"), svResult("3", "ok"))
	}
	writeSavingsSession(t, dir, "miss.jsonl",
		svUse("1", "mcp__cully__cully_context"), svResult("1", `{\"notes\":[]}`),
		svUse("2", "Read"), svResult("2", big),
		svUse("3", "Write"), svResult("3", "ok"))
	writeSavingsSession(t, dir, "chat.jsonl", svUse("1", "Read"), svResult("1", big)) // never edits: ignored

	s, err := ComputeSavings(root, "", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if s.Sessions != 5 || s.WithHit != 2 || s.WithMiss != 1 || s.WithoutCully != 2 {
		t.Fatalf("cohorts: %+v", s)
	}
	if s.Calls != 3 || s.EmptyContext != 1 || s.ResultTokens <= 0 || s.WastedTokens <= 0 {
		t.Fatalf("cost: %+v", s)
	}
	if s.ExploreHit != 100 || s.ExploreNone != 1000 {
		t.Fatalf("explore hit=%d none=%d", s.ExploreHit, s.ExploreNone)
	}
	if s.EstimatedSaved != 1800 || s.NetTokens != s.EstimatedSaved-s.ResultTokens-s.ArgTokens-s.SchemaTokens {
		t.Fatalf("saved=%d net=%d", s.EstimatedSaved, s.NetTokens)
	}
	if !strings.HasPrefix(s.Confidence, "low") {
		t.Fatalf("tiny sample must be low confidence: %q", s.Confidence)
	}
	var out strings.Builder
	s.Write(&out, 14)
	if !strings.Contains(out.String(), "saving tokens") || strings.Contains(out.String(), "did it") {
		t.Fatalf("report: %s", out.String())
	}
}

func TestSavingsProjectAndAgeFilters(t *testing.T) {
	root := t.TempDir()
	writeSavingsSession(t, filepath.Join(root, "-keep"), "a.jsonl", svUse("1", "Edit"), svResult("1", "ok"))
	writeSavingsSession(t, filepath.Join(root, "-skip"), "b.jsonl", svUse("1", "Edit"), svResult("1", "ok"))
	if s, _ := ComputeSavings(root, "keep", time.Now().Add(-time.Hour)); s.Sessions != 1 {
		t.Fatalf("project filter: %d", s.Sessions)
	}
	if s, _ := ComputeSavings(root, "", time.Now().Add(time.Hour)); s.Sessions != 0 {
		t.Fatalf("age filter: %d", s.Sessions)
	}
}
