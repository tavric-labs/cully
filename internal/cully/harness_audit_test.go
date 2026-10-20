package cully

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{"shared project skill", "Skills", "proj/.cully/skills/cully/SKILL.md", "rules"},
		{"codex project skill", "Skills", "proj/.codex/skills/review/SKILL.md", "rules"},
		{"cursor project skill", "Skills", "proj/.cursor/skills/review/SKILL.md", "rules"},
		{"shared agent skill", "Skills", "proj/.agents/skills/review/SKILL.md", "rules"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			d := harnessDirs{
				Claude: filepath.Join(root, ".claude"), Codex: filepath.Join(root, "codex"),
				Cursor: filepath.Join(root, "cursor"), Project: filepath.Join(root, "proj"),
			}
			path := filepath.Join(root, c.file)
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
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
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

func TestHarnessRejectsInvalidLimits(t *testing.T) {
	for _, args := range [][]string{{"--max", "-1"}, {"--max", "0"}, {"--days", "0"}, {"--days", "-1", "--all"}} {
		var out bytes.Buffer
		if err := RunHarness(&out, args); err == nil || !strings.Contains(err.Error(), "positive") {
			t.Errorf("%v: expected positive-limit error, got %v", args, err)
		}
	}
}

func TestAuditHarnessEmptyRootsDoNotReadWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"settings.json", ".claude.json", "config.toml", "AGENTS.md", "CLAUDE.md"} {
		if err := os.WriteFile(name, []byte(`{"permissions":{},"hooks":{},"cully":{},"enabledPlugins":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll("plugins/example", 0o700); err != nil {
		t.Fatal(err)
	}
	for _, it := range auditHarness(harnessDirs{}) {
		if it.Present() {
			t.Errorf("unset root read unrelated working directory: %+v", it)
		}
	}
}

func TestHarnessMeasurementUsesExactProjectAndRecentBoundedSessions(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile(replayTranscript(t))
	if err != nil {
		t.Fatal(err)
	}
	write := func(folder, name string, age time.Duration) {
		t.Helper()
		dir := filepath.Join(root, folder)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	write("-project", "one", time.Hour)
	write("-project", "two", 2*time.Hour)
	write("-project", "old", 60*24*time.Hour)
	write("-project-extra", "other", time.Hour)
	write("-project/session/subagents", "child", time.Hour)
	if got := measureHarnessEvidence(root, "-project", 30, 40); got.Sessions != 2 {
		t.Fatalf("exact-project measurement included unrelated or old sessions: %+v", got)
	}
	if got := measureHarnessEvidence(root, "-project", 30, 1); got.Sessions != 1 {
		t.Fatalf("session limit ignored: %+v", got)
	}
	total, by := measureAllProjects(root, "", 30, 40)
	if total.Sessions != 3 || by["-project"].Sessions != 2 || by["-project-extra"].Sessions != 1 {
		t.Fatalf("all-project totals overlap: total=%+v projects=%+v", total, by)
	}
}

func TestHarnessDelegationsCountCallsOnly(t *testing.T) {
	w := newTokenWaste()
	for _, tc := range []struct {
		line string
		want int
	}{
		{`{"message":{"content":[{"type":"tool_use","id":"a","name": "Agent","input":{}}]}}`, 1},
		{`{"message":{"content":[{"type":"tool_use","id":"b","name":"Task","input":{}}]}}`, 1},
		{`{"message":{"content":[{"type":"text","text":"Agent Task"}]}}`, 0},
		{`{"name":"Agent","message":{"content":[]}}`, 0},
		{`invalid`, 0},
	} {
		if got := scanWasteLine(w, []byte(tc.line)); got != tc.want {
			t.Errorf("%s: delegations=%d want=%d", tc.line, got, tc.want)
		}
	}
}

func TestRunHarnessResolvesRelativeProject(t *testing.T) {
	isolateWorkflow(t)
	project, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunHarness(&out, []string{"--cwd", "."}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "HARNESS AUDIT  ("+project+")") {
		t.Fatalf("relative project was not resolved: %s", out.String())
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

func TestApplyEvidence(t *testing.T) {
	base := func() []harnessItem {
		return []harnessItem{
			{Component: "Skills", Hint: "skills hint"},
			{Component: "Subagents", Hint: "generic hint"},
			{Component: "Memory", Hint: "memory hint"},
		}
	}
	tests := []struct {
		name        string
		ev          harnessEvidence
		subPresent  bool
		subContains string
		skillsHint  string
	}{
		{"delegation present", harnessEvidence{Sessions: 2, BigTokens: 900, Delegations: 7}, true, "7 delegations in recent sessions", "skills hint"},
		{"delegation absent with waste", harnessEvidence{Sessions: 2, BigTokens: 1000, BashTokens: 600, ReadTokens: 400}, false, "Bash 300 and whole-file reads 200 tokens per session", "200 tokens per session"},
		{"no evidence", harnessEvidence{}, false, "generic hint", "skills hint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := applyEvidence(base(), tc.ev)
			skills, sub := items[0], items[1]
			if sub.Present() != tc.subPresent {
				t.Fatalf("Subagents present = %v, want %v", sub.Present(), tc.subPresent)
			}
			got := sub.Hint
			if sub.Present() {
				got = sub.Found[0]
			}
			if !strings.Contains(got, tc.subContains) {
				t.Errorf("Subagents = %q, want %q", got, tc.subContains)
			}
			if !strings.Contains(skills.Hint, tc.skillsHint) {
				t.Errorf("Skills = %q, want %q", skills.Hint, tc.skillsHint)
			}
			if items[2].Hint != "memory hint" {
				t.Errorf("unrelated item changed: %q", items[2].Hint)
			}
		})
	}
}
