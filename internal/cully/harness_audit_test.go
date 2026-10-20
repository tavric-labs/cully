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

func TestHarnessReportsMeasuredWasteAndStatesItIsNotProof(t *testing.T) {
	isolateWorkflow(t)
	project := t.TempDir()
	folder := filepath.Join(ConfigDir(), "projects", projectFolderMatch(project))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(replayTranscript(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "s.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunHarness(&out, []string{"--cwd", project}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MEASURED in 1 recent sessions", "oversized tool results", "subagent delegations    0", "not proof"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	var empty bytes.Buffer
	if err := RunHarness(&empty, []string{"--cwd", t.TempDir()}); err != nil || !strings.Contains(empty.String(), "nothing was measured") {
		t.Fatalf("missing transcripts must say nothing was measured: %v\n%s", err, empty.String())
	}
}

func TestHarnessAllProjectsRanksByOversizedOutput(t *testing.T) {
	isolateWorkflow(t)
	data, err := os.ReadFile(replayTranscript(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"-a-one", "-b-two"} {
		dir := filepath.Join(ConfigDir(), "projects", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := RunHarness(&out, []string{"--all"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MEASURED in 2 recent sessions", "TOP PROJECTS BY OVERSIZED OUTPUT", "-a-one", "-b-two"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestAuditHarnessDetectsEachExtraSource(t *testing.T) {
	cases := []struct {
		name      string
		component string
		file      string // relative to the temp root
		content   string
	}{
		{"project claude settings hooks", "Hooks and scripts", "proj/.claude/settings.json", `{"hooks":{}}`},
		{"project claude local hooks", "Hooks and scripts", "proj/.claude/settings.local.json", `{"hooks":{}}`},
		{"cursor hooks", "Hooks and scripts", "cursor/hooks.json", `{"hooks":{}}`},
		{"project cursor hooks", "Hooks and scripts", "proj/.cursor/hooks.json", `{"hooks":{}}`},
		{"codex hooks json", "Hooks and scripts", "codex/hooks.json", `{"hooks":{}}`},
		{"codex toml hooks table", "Hooks and scripts", "codex/config.toml", "[hooks.stop]\n"},
		{"project mcp json", "Memory", "proj/.mcp.json", `{"mcpServers":{"cully":{}}}`},
		{"home claude json", "Memory", ".claude.json", `{"mcpServers":{"cully":{}}}`},
		{"cursor mcp", "Memory", "cursor/mcp.json", `{"mcpServers":{"cully":{}}}`},
		{"project cursor mcp", "Memory", "proj/.cursor/mcp.json", `{"mcpServers":{"cully":{}}}`},
		{"cursorrules", "Instructions", "proj/.cursorrules", "rules"},
		{"cursor rules dir", "Instructions", "proj/.cursor/rules/a.mdc", "rules"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			d := harnessDirs{
				Claude: filepath.Join(root, ".claude"), Codex: filepath.Join(root, "codex"),
				Cursor: filepath.Join(root, "cursor"), Project: filepath.Join(root, "proj"),
			}
			path := filepath.Join(root, c.file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			present := func() bool {
				for _, it := range auditHarness(d) {
					if it.Component == c.component {
						return it.Present()
					}
				}
				t.Fatalf("no %s item", c.component)
				return false
			}
			if present() {
				t.Fatalf("%s present before any source exists", c.component)
			}
			if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if !present() {
				t.Errorf("%s not detected from %s", c.component, c.file)
			}
		})
	}
}

func TestAuditHarnessIgnoresUnrelatedConfigAndEmptyRules(t *testing.T) {
	root := t.TempDir()
	d := harnessDirs{Claude: filepath.Join(root, ".claude"), Codex: filepath.Join(root, "codex"), Cursor: filepath.Join(root, "cursor"), Project: filepath.Join(root, "proj")}
	for _, dir := range []string{d.Claude, d.Codex, d.Cursor, filepath.Join(d.Project, ".cursor", "rules")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(d.Project, ".mcp.json"), []byte(`{"mcpServers":{"other":{}}}`), 0o600)
	os.WriteFile(filepath.Join(d.Cursor, "hooks.json"), []byte(`{}`), 0o600)
	for _, it := range auditHarness(d) {
		if it.Present() {
			t.Errorf("%s reported present: %v", it.Component, it.Found)
		}
	}
}
