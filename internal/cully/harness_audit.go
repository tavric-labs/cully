package cully

import (
	"bufio"
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

// under joins parts onto root, or returns "" (which nothing reads) when root
// is unset, so an empty directory never resolves relative to the working one.
func under(root string, parts ...string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

func auditHarness(d harnessDirs) []harnessItem {
	claudeSettings := under(d.Claude, "settings.json")
	codexConfig := under(d.Codex, "config.toml")
	settings, codex := readCapped(claudeSettings), readCapped(codexConfig)

	var guard, memory, hooks, plugins []string
	claudeHome := ""
	if d.Claude != "" {
		claudeHome = filepath.Dir(d.Claude)
	}
	if strings.Contains(settings, `"permissions"`) {
		guard = append(guard, claudeSettings)
	}
	for _, p := range []string{under(d.Claude, "settings.local.json"), under(d.Project, ".claude", "settings.json"), under(d.Project, ".claude", "settings.local.json")} {
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
	for _, p := range []string{
		under(d.Project, ".mcp.json"),
		under(claudeHome, ".claude.json"), under(d.Claude, ".claude.json"),
		under(d.Cursor, "mcp.json"), under(d.Project, ".cursor", "mcp.json"),
	} {
		if strings.Contains(readCapped(p), "cully") {
			memory = append(memory, p)
		}
	}
	if strings.Contains(settings, `"hooks"`) {
		hooks = append(hooks, claudeSettings)
	}
	for _, p := range []string{
		under(d.Project, ".claude", "settings.json"), under(d.Project, ".claude", "settings.local.json"),
		under(d.Cursor, "hooks.json"), under(d.Project, ".cursor", "hooks.json"),
		under(d.Codex, "hooks.json"),
	} {
		if strings.Contains(readCapped(p), `"hooks"`) {
			hooks = append(hooks, p)
		}
	}
	if strings.Contains(codex, "[hooks") {
		hooks = append(hooks, codexConfig)
	}
	if strings.Contains(settings, `"enabledPlugins"`) {
		plugins = append(plugins, claudeSettings)
	}
	if p := under(d.Claude, "plugins"); dirHasEntries(p) {
		plugins = append(plugins, p)
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
		for _, agentDir := range []string{".claude", ".codex", ".cursor", ".agents", ".cully"} {
			if p := under(d.Project, agentDir, "skills"); dirHasEntries(p) {
				skills = append(skills, p)
			}
			if p := under(d.Project, agentDir, "agents"); dirHasEntries(p) {
				agents = append(agents, p)
			}
		}
	}
	instructions := firstExisting(
		under(d.Claude, "CLAUDE.md"), under(d.Codex, "AGENTS.override.md"), under(d.Codex, "AGENTS.md"),
		under(d.Project, "CLAUDE.md"), under(d.Project, "AGENTS.md"),
		under(d.Project, ".cursorrules"),
		// Other harnesses read their own instruction files; a project that
		// serves several agents should not look uninstructed to the audit.
		under(d.Project, "GEMINI.md"), under(d.Project, "CONVENTIONS.md"),
		under(d.Project, ".windsurfrules"), under(d.Project, ".github", "copilot-instructions.md"),
	)
	for _, p := range []string{under(d.Project, ".cursor", "rules"), under(d.Project, ".openhands", "microagents")} {
		if dirHasEntries(p) {
			instructions = append(instructions, p)
		}
	}
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
	if match == "" || days <= 0 || max <= 0 {
		return harnessEvidence{}
	}
	since := time.Now().AddDate(0, 0, -days)
	type found struct {
		path string
		mod  time.Time
	}
	var paths []found
	folder := filepath.Join(root, match)
	entries, _ := os.ReadDir(folder)
	for _, d := range entries {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			continue
		}
		if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
			paths = append(paths, found{filepath.Join(folder, d.Name()), info.ModTime()})
		}
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].mod.After(paths[j].mod) })
	if len(paths) > max {
		paths = paths[:max]
	}
	var ev harnessEvidence
	for _, p := range paths {
		f, err := os.Open(p.path)
		if err != nil {
			continue
		}
		w := newTokenWaste()
		lines, delegations := 0, 0
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 8<<20)
		for sc.Scan() {
			if len(strings.TrimSpace(string(sc.Bytes()))) == 0 {
				continue
			}
			lines++
			delegations += scanWasteLine(w, sc.Bytes())
		}
		f.Close()
		if sc.Err() != nil || lines < 20 {
			continue
		}
		ev.Sessions++
		ev.BigTokens += w.BigTokens
		ev.BashTokens += w.BigBashTokens
		ev.ReadTokens += w.FullReadTokens
		ev.Delegations += delegations
	}
	return ev
}

// measureAllProjects measures every project's transcripts, so one report shows
// where the oversized output actually comes from across all past sessions.
func measureAllProjects(root, only string, days, max int) (harnessEvidence, map[string]harnessEvidence) {
	var total harnessEvidence
	by := map[string]harnessEvidence{}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), only) {
			continue
		}
		ev := measureHarnessEvidence(root, e.Name(), days, max)
		if ev.Sessions == 0 {
			continue
		}
		by[e.Name()] = ev
		total.Sessions += ev.Sessions
		total.BigTokens += ev.BigTokens
		total.BashTokens += ev.BashTokens
		total.ReadTokens += ev.ReadTokens
		total.Delegations += ev.Delegations
	}
	return total, by
}

func writeAllProjects(w io.Writer, total harnessEvidence, by map[string]harnessEvidence, days int) {
	if total.Sessions == 0 {
		fmt.Fprintf(w, "No Claude transcripts from the last %d days were found, so nothing was measured.\n", days)
		return
	}
	total.write(w, days)
	names := make([]string, 0, len(by))
	for n := range by {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return by[names[i]].BigTokens > by[names[j]].BigTokens })
	fmt.Fprintf(w, "\nTOP PROJECTS BY OVERSIZED OUTPUT\n")
	for i, n := range names {
		if i == 10 {
			break
		}
		ev := by[n]
		fmt.Fprintf(w, "  %-60s %3d sessions  %7d tokens  %5d per session  (Bash %d, reads %d)\n", truncateRunes(n, 60), ev.Sessions, ev.BigTokens, ev.perSession(ev.BigTokens), ev.BashTokens, ev.ReadTokens)
	}
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

// applyEvidence makes the Subagents and Skills gaps evidence-based. Built-in
// Task subagents need no agents/ directory, so recorded delegations count as
// Subagents in use; a remaining gap cites the user's own measured tokens per
// session. It never changes what auditHarness found on disk for other items.
func applyEvidence(items []harnessItem, ev harnessEvidence) []harnessItem {
	out := make([]harnessItem, len(items))
	copy(out, items)
	for i, it := range out {
		if it.Present() {
			continue
		}
		switch it.Component {
		case "Subagents":
			if ev.Delegations > 0 {
				out[i].Found = []string{fmt.Sprintf("%d delegations in recent sessions", ev.Delegations)}
			} else if ev.BigTokens > 0 {
				out[i].Hint = fmt.Sprintf("Bash %d and whole-file reads %d tokens per session of oversized results; define a subagent for large searches.", ev.perSession(ev.BashTokens), ev.perSession(ev.ReadTokens))
			}
		case "Skills":
			if ev.ReadTokens > 0 {
				out[i].Hint = fmt.Sprintf("Whole-file reads cost %d tokens per session; capture the repeated workflow as a skill. cully workflow creates a private dev-workflow-rules one.", ev.perSession(ev.ReadTokens))
			}
		}
	}
	return out
}

// RunHarness implements `cully harness`.
func RunHarness(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("harness", flag.ContinueOnError)
	fs.SetOutput(w)
	cwd := fs.String("cwd", "", "project directory (default: current directory)")
	days := fs.Int("days", 30, "look back this many days when measuring")
	all := fs.Bool("all", false, "measure every project's recent sessions instead of auditing one setup")
	only := fs.String("only", "", "with --all, only project folders containing this text (e.g. personal)")
	max := fs.Int("max", 40, "most sessions to measure per project")
	dir := fs.String("dir", "", "Claude projects directory (default ~/.claude/projects)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: cully harness [--cwd DIR] [--all] [--days N] [--max N]")
	}
	if *days <= 0 || *max <= 0 {
		return fmt.Errorf("--days and --max must be positive")
	}
	if *cwd == "" {
		var err error
		*cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	absolute, err := filepath.Abs(*cwd)
	if err != nil {
		return err
	}
	*cwd = absolute
	if *dir == "" {
		*dir = filepath.Join(ConfigDir(), "projects")
	}
	if *all {
		total, by := measureAllProjects(*dir, *only, *days, *max)
		fmt.Fprintf(w, "HARNESS MEASUREMENT  (all projects, last %d days)\n\n", *days)
		writeAllProjects(w, total, by, *days)
		return nil
	}
	ev := measureHarnessEvidence(*dir, projectFolderMatch(*cwd), *days, *max)
	items := applyEvidence(auditHarness(harnessDirs{Claude: ConfigDir(), Codex: CodexConfigDir(), Cursor: CursorConfigDir(), Project: *cwd}), ev)
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
	ev.write(w, *days)
	fmt.Fprintf(w, "\n%d of %d components configured. Read-only: setup checks use file names and config keys; transcript measurements retain counts only.\n", len(items)-missing, len(items))
	return nil
}
