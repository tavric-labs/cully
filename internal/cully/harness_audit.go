package cully

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// RunHarness implements `cully harness`.
func RunHarness(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("harness", flag.ContinueOnError)
	fs.SetOutput(w)
	cwd := fs.String("cwd", "", "project directory (default: current directory)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: cully harness [--cwd DIR]")
	}
	if *cwd == "" {
		*cwd, _ = os.Getwd()
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
	fmt.Fprintf(w, "\n%d of %d components configured. Read-only: only file names and config keys were checked, never contents.\n", len(items)-missing, len(items))
	return nil
}
