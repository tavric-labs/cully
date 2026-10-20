package cully

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditHarnessFindsConfiguredComponents(t *testing.T) {
	root := t.TempDir()
	claude, project := filepath.Join(root, "claude"), filepath.Join(root, "proj")
	for _, dir := range []string{filepath.Join(claude, "skills", "x"), filepath.Join(claude, "agents"), project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(claude, "agents", "a.md"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("rules"), 0o600)
	os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"permissions":{},"hooks":{},"mcpServers":{"cully":{}}}`), 0o600)

	got := map[string]bool{}
	for _, it := range auditHarness(harnessDirs{Claude: claude, Codex: filepath.Join(root, "none"), Project: project}) {
		got[it.Component] = it.Present()
	}
	for _, name := range []string{"Instructions", "Guardrails", "Memory", "Skills", "Subagents", "Hooks and scripts"} {
		if !got[name] {
			t.Errorf("%s not detected", name)
		}
	}
	if got["Plugins"] {
		t.Error("plugins reported without configuration")
	}
}

func TestRunHarnessReportsMissingAndNeverPrintsContents(t *testing.T) {
	isolateWorkflow(t)
	project := t.TempDir()
	os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte("SECRET-CONTENT"), 0o600)
	var out bytes.Buffer
	if err := RunHarness(&out, []string{"--cwd", project}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "SECRET-CONTENT") || !strings.Contains(s, "missing") || !strings.Contains(s, "1 of 7") {
		t.Fatalf("unexpected report:\n%s", s)
	}
	if err := RunHarness(&out, []string{"extra"}); err == nil {
		t.Fatal("accepted stray argument")
	}
}
