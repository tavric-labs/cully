package cully

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The harness audit looks at the parts of an agent setup that shape its
// behaviour (instructions, guardrails, memory, skills, subagents, hooks and
// plugins) and reports which exist. It reads only file names and a few config
// keys, never contents, and changes nothing.

type harnessDirs struct{ Claude, Codex, Cursor, Project string }

type harnessItem struct {
	Component string
	Found     []string
	Hint      string
}

func (h harnessItem) Present() bool { return len(h.Found) > 0 }

func readCapped(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 1<<20))
	return string(data)
}

func firstExisting(paths ...string) []string {
	var found []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}
	return found
}

func dirHasEntries(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

func auditHarness(d harnessDirs) []harnessItem {
	claudeSettings := filepath.Join(d.Claude, "settings.json")
	codexConfig := filepath.Join(d.Codex, "config.toml")
	settings, codex := readCapped(claudeSettings), readCapped(codexConfig)

	var guard, memory, hooks, plugins []string
	if strings.Contains(settings, `"permissions"`) {
		guard = append(guard, claudeSettings)
	}
	for _, p := range []string{filepath.Join(d.Claude, "settings.local.json"), filepath.Join(d.Project, ".claude", "settings.json"), filepath.Join(d.Project, ".claude", "settings.local.json")} {
		if strings.Contains(readCapped(p), `"permissions"`) {
			guard = append(guard, p)
		}
	}
	if strings.Contains(codex, "approval_policy") || strings.Contains(codex, "sandbox_mode") {
		guard = append(guard, codexConfig)
	}
	if strings.Contains(settings, "cully") {
		memory = append(memory, claudeSettings)
	}
	if strings.Contains(codex, "cully") {
		memory = append(memory, codexConfig)
	}
	if strings.Contains(settings, `"hooks"`) {
		hooks = append(hooks, claudeSettings)
	}
	if strings.Contains(settings, `"enabledPlugins"`) {
		plugins = append(plugins, claudeSettings)
	}
	if dirHasEntries(filepath.Join(d.Claude, "plugins")) {
		plugins = append(plugins, filepath.Join(d.Claude, "plugins"))
	}
	var skills, agents []string
	for _, dir := range []string{d.Claude, d.Codex, d.Cursor, d.Project} {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, "skills"); dirHasEntries(p) {
			skills = append(skills, p)
		}
		if p := filepath.Join(dir, "agents"); dirHasEntries(p) {
			agents = append(agents, p)
		}
	}
	if d.Project != "" {
		if p := filepath.Join(d.Project, ".claude", "skills"); dirHasEntries(p) {
			skills = append(skills, p)
		}
		if p := filepath.Join(d.Project, ".claude", "agents"); dirHasEntries(p) {
			agents = append(agents, p)
		}
	}
	instructions := firstExisting(
		filepath.Join(d.Claude, "CLAUDE.md"), filepath.Join(d.Codex, "AGENTS.md"),
		filepath.Join(d.Project, "CLAUDE.md"), filepath.Join(d.Project, "AGENTS.md"),
	)
	return []harnessItem{
		{"Instructions", instructions, "Add a short CLAUDE.md or AGENTS.md with build, test and review rules for this project."},
		{"Guardrails", guard, "Set permission rules or an approval policy so risky commands need confirmation."},
		{"Memory", memory, "Run cully setup so the agent can recall earlier work through Cully MCP."},
		{"Skills", skills, "Capture a repeated workflow as a skill; cully workflow creates a private dev-workflow-rules one."},
		{"Subagents", agents, "Define a focused subagent for noisy work such as large searches, so its output stays out of the main context."},
		{"Hooks and scripts", hooks, "Run cully setup to install Cully's hooks for session health and the advisor."},
		{"Plugins", plugins, "Optional: plugins can add middleware and tools; none are configured."},
	}
}

// harnessEvidence is what Cully measured in recent Claude transcripts for the
// project. It is a baseline to compare against after a setup change, not proof
// that any component causes less waste.
type harnessEvidence struct {
	Sessions                          int
	BigTokens, BashTokens, ReadTokens int
	Delegations                       int
}

func (e harnessEvidence) perSession(n int) int {
	if e.Sessions == 0 {
		return 0
	}
	return n / e.Sessions
}

// projectFolderMatch is how Claude names a project's transcript folder.
func projectFolderMatch(cwd string) string {
	return strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(cwd)
}

func measureHarnessEvidence(root, match string, days, max int) harnessEvidence {
	since := time.Now().AddDate(0, 0, -days)
	type found struct {
		path string
		mod  time.Time
	}
	var paths []found
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") || !strings.Contains(filepath.Base(filepath.Dir(path)), match) {
			return nil
		}
		if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
			paths = append(paths, found{path, info.ModTime()})
		}
		return nil
	})
	sort.Slice(paths, func(i, j int) bool { return paths[i].mod.After(paths[j].mod) })
	if len(paths) > max {
		paths = paths[:max]
	}
	var ev harnessEvidence
	for _, p := range paths {
		lines := readTranscriptLines(p.path)
		if len(lines) < 20 {
			continue
		}
		w := scanWaste(lines)
		ev.Sessions++
		ev.BigTokens += w.BigTokens
		ev.BashTokens += w.BigBashTokens
		ev.ReadTokens += w.FullReadTokens
		for _, line := range lines {
			ev.Delegations += bytes.Count(line, []byte(`"name":"Task"`)) + bytes.Count(line, []byte(`"name":"Agent"`))
		}
	}
	return ev
}

func (e harnessEvidence) write(w io.Writer, days int) {
	if e.Sessions == 0 {
		fmt.Fprintf(w, "\nNo Claude transcripts from the last %d days were found for this project, so nothing was measured.\n", days)
		return
	}
	fmt.Fprintf(w, "\nMEASURED in %d recent sessions (last %d days)\n", e.Sessions, days)
	fmt.Fprintf(w, "  oversized tool results  %d tokens total, %d per session (Bash %d, whole-file reads %d)\n", e.BigTokens, e.perSession(e.BigTokens), e.BashTokens, e.ReadTokens)
	fmt.Fprintf(w, "  subagent delegations    %d total\n", e.Delegations)
	fmt.Fprintln(w, "  This is a baseline, not proof. Change one component, then rerun after a similar amount of work and compare tokens per session.")
}

// RunHarness implements `cully harness`.
func RunHarness(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("harness", flag.ContinueOnError)
	fs.SetOutput(w)
	cwd := fs.String("cwd", "", "project directory (default: current directory)")
	days := fs.Int("days", 30, "look back this many days when measuring")
	dir := fs.String("dir", "", "Claude projects directory (default ~/.claude/projects)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: cully harness [--cwd DIR] [--days N]")
	}
	if *cwd == "" {
		*cwd, _ = os.Getwd()
	}
	if *dir == "" {
		*dir = filepath.Join(ConfigDir(), "projects")
	}
	items := auditHarness(harnessDirs{Claude: ConfigDir(), Codex: CodexConfigDir(), Cursor: CursorConfigDir(), Project: *cwd})
	missing := 0
	fmt.Fprintf(w, "HARNESS AUDIT  (%s)\n\n", *cwd)
	for _, it := range items {
		if it.Present() {
			fmt.Fprintf(w, "  ✓ %-18s %s\n", it.Component, strings.Join(it.Found, ", "))
			continue
		}
		missing++
		fmt.Fprintf(w, "  · %-18s missing — %s\n", it.Component, it.Hint)
	}
	measureHarnessEvidence(*dir, projectFolderMatch(*cwd), *days, 40).write(w, *days)
	fmt.Fprintf(w, "\n%d of %d components configured. Read-only: only file names and config keys were checked, never contents.\n", len(items)-missing, len(items))
	return nil
}
