package cully

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// applyPlan is the structured fix the apply worker returns.
type applyPlan struct {
	Summary         string         `json:"summary"`
	ClaudeMDSection string         `json:"claude_md_section"`
	MCPServers      map[string]any `json:"mcp_servers"`
	ShellCommands   []string       `json:"shell_commands"`
	SkillName       string         `json:"skill_name"`
	SkillContent    string         `json:"skill_content"`
	Notes           string         `json:"notes"`
}

const applyInstr = `You are the cully fix applier. The user accepted a cully suggestion and wants it applied.
Read the SUGGESTION and optional SIGNALS, then reply with ONLY a single JSON object (no markdown fences) shaped like:
{
  "summary": "one-line description of what will change",
  "claude_md_section": "markdown rules saved to Cully's private notes for this project, outside the repository (empty string if not needed)",
  "mcp_servers": { "server-name": { "command": "npx", "args": ["-y", "package"] } } or {},
  "shell_commands": ["safe non-destructive install commands"] or [],
  "skill_name": "kebab-case skill folder name or empty",
  "skill_content": "full SKILL.md markdown or empty",
  "notes": "manual follow-ups the user must do, or empty"
}
Rules:
- Cully writes only to its own private notes for this project, never to the repository. claude_md_section and skill_content are saved there; mcp_servers and shell_commands are shown to the user as manual steps and are never executed.
- Prefer project rules for workflow levers: graphify query, /loop polling, Explore subagent, model tiering, skills by name.
- Only add mcp_servers when the suggestion clearly needs a real integration; use well-known packages only.
- shell_commands: safe installs only (brew, npm -g, npx -y, curl to official repos). No secrets, no rm, no force flags.
- skill_content: only when a concrete project skill should exist; keep it short and actionable.
- Never invent API keys. If auth is required, put it in notes.
- Be minimal: one focused change that matches the suggestion.`

// RunList prints numbered applyable fixes and unnumbered notes for the session
// resolved from the current directory, and names that session so the isolation
// boundary is visible.
func RunList(w io.Writer) {
	cwd, _ := os.Getwd()
	session := resolveSession(cwd)
	if sess, evs, ok := latestJournal(cwd); ok && sess.Session == session {
		ensureTaskSuggestion(session, cwd, evs)
	}
	lines := readSuggestionsLimit(session, maxReportLines)
	if len(lines) == 0 {
		fmt.Fprintln(w, "No cully suggestions for this session.")
		writeSelfImproveHint(w)
		return
	}
	fmt.Fprintf(w, "Session %s\n\n", shortSession(session))
	snap := readSnapshot(session)
	st, _ := readState()
	classified := parseSuggestionStore(lines, reportAge(session), snap, st)
	notes, fixes := partitionSuggestions(classified)
	for i, c := range fixes {
		fmt.Fprintf(w, "[%d] %s %s\n", i+1, c.Level.Display(), c.Text)
	}
	for _, c := range notes {
		fmt.Fprintf(w, "    %s %s\n", c.Level.Display(), c.Text)
	}
	if len(fixes) > 0 {
		fmt.Fprintln(w, "\nWire up a fix: cully apply <n>  ·  ask your agent to do it: cully suggestions --request <n>")
	}
	writeSelfImproveHint(w)
}

// writeSelfImproveHint points at proposals from Cully's own scheduled eval,
// which are separate from this session's suggestions.
func writeSelfImproveHint(w io.Writer) {
	if n := len(pendingImprovements()); n > 0 {
		fmt.Fprintf(w, "\nCully's own eval proposes %d improvement(s) to Cully itself: cully improve\n", n)
	}
}

// RunSuggestions implements `cully suggestions [--request N]`. Without a flag it
// lists the session's suggestions. With --request it prints the guarded request
// for numbered fix N, so an agent running outside the Cully terminal, such as in
// T3 Code, can offer to set it up. It changes nothing.
func RunSuggestions(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("suggestions", flag.ContinueOnError)
	request := fs.Int("request", 0, "print the setup request for numbered fix N")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: cully suggestions [--request N]")
	}
	if *request == 0 {
		RunList(w)
		return nil
	}
	cwd, _ := os.Getwd()
	session := resolveSession(cwd)
	if _, err := applyableReportIndex(session, *request); err != nil {
		return err
	}
	suggestion := applyableReportLines(session)[*request-1]
	if _, ok := taskNameFromSuggestion(stripSeverityPrefix(suggestion)); ok {
		return fmt.Errorf("fix #%d sets the session task; run: cully apply %d", *request, *request)
	}
	fmt.Fprintln(w, advisorSetupRequest(suggestion, readSessionSignals(session)))
	return nil
}

// shortSession renders a session id compactly for display.
func shortSession(session string) string {
	if len(session) > 8 {
		return session[:8]
	}
	if session == "" {
		return "(unknown)"
	}
	return session
}

// applyableReportLines returns suggestions cully apply can act on, in report order.
func applyableReportLines(session string) []string {
	lines := readSuggestionsLimit(session, maxReportLines)
	snap := readSnapshot(session)
	st, _ := readState()
	var out []string
	for _, ln := range lines {
		if isApplyable(classifySuggestion(ln, snap, st)) {
			out = append(out, ln)
		}
	}
	return out
}

// applyableReportIndex maps apply number (1-based) to the line index in the full report.
func applyableReportIndex(session string, n int) (int, error) {
	if n < 1 {
		return 0, fmt.Errorf("suggestion number must be >= 1")
	}
	lines := readSuggestionsLimit(session, maxReportLines)
	snap := readSnapshot(session)
	st, _ := readState()
	applyN := 0
	for i, ln := range lines {
		if isApplyable(classifySuggestion(ln, snap, st)) {
			applyN++
			if applyN == n {
				return i + 1, nil
			}
		}
	}
	if applyN == 0 {
		return 0, fmt.Errorf("no applyable fixes available; run: cully suggestions")
	}
	return 0, fmt.Errorf("fix #%d not found: only %d available; run: cully suggestions", n, applyN)
}

// RunApply applies applyable fix n (1-based) from the session resolved for cwd.
// When yes is false, prompts on stdin.
func RunApply(n int, cwd string, yes, dryRun bool) error {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	session := resolveSession(cwd)
	applyable := applyableReportLines(session)
	if n > len(applyable) {
		if len(applyable) == 0 {
			return fmt.Errorf("no applyable fixes for this session (notes and slash-command reminders are not wired via apply)")
		}
		return fmt.Errorf("fix #%d not found: only %d available; run: cully suggestions", n, len(applyable))
	}
	reportIdx, err := applyableReportIndex(session, n)
	if err != nil {
		return err
	}
	suggestion := stripSeverityPrefix(applyable[n-1])
	if name, ok := taskNameFromSuggestion(suggestion); ok {
		if err := writeTask(session, name); err != nil {
			return err
		}
		if err := removeSuggestion(session, reportIdx); err != nil {
			debugLog("apply: remove suggestion %d: %v", reportIdx, err)
		}
		fmt.Println("Task set:", name)
		fmt.Println("Agent: record this task with cully_session (task name, current session_ref).")
		return nil
	}

	signals := readSessionSignals(session)
	prompt := applyInstr + "\n\nSUGGESTION:\n" + suggestion
	if signals != "" {
		prompt += "\n\nSIGNALS:\n" + signals
	}

	out, err := runClaude("", prompt)
	if err != nil {
		return fmt.Errorf("plan suggestion: %w", err)
	}
	plan, err := parseApplyPlan(out)
	if err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}

	fmt.Println("Suggestion:", suggestion)
	fmt.Println()
	fmt.Println("Plan:", plan.Summary)
	if plan.ClaudeMDSection != "" {
		fmt.Println("  • Save rules to Cully's notes for this project (outside the repository)")
	}
	if plan.SkillName != "" && plan.SkillContent != "" {
		fmt.Printf("  • Save skill %s to Cully's notes for this project (outside the repository)\n", clipText(plan.SkillName, 60))
	}
	for _, step := range manualSteps(plan) {
		fmt.Printf("  • Manual step, not applied: %s\n", step)
	}
	if plan.Notes != "" {
		fmt.Printf("  • Note: %s\n", plan.Notes)
	}
	fmt.Println()

	if dryRun {
		fmt.Println("Dry run — no changes made.")
		return nil
	}
	// The /cully slash command has no interactive stdin: the y/N prompt
	// would read EOF and silently cancel every apply. The command template
	// declares that explicitly via CULLY_ASSUME_YES=1 (env, not a TTY
	// heuristic — /dev/null is a char device and fools isatty-style checks).
	// The plan was just printed. Applying it only writes Cully's own project notes.
	if !yes && os.Getenv("CULLY_ASSUME_YES") == "1" {
		fmt.Println("Non-interactive session — applying without prompt (use --dry-run to preview).")
		yes = true
	}
	if !yes {
		fmt.Print("Apply this fix? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans != "y" && ans != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	res, err := executePlan(cwd, suggestion, plan)
	if err != nil {
		return err
	}
	recordAppliedChange("project", projectKey(cwd), plan.Summary)
	if err := removeSuggestion(session, reportIdx); err != nil {
		debugLog("apply: remove suggestion %d: %v", reportIdx, err)
	}
	fmt.Println("\033[32mApplied.\033[0m The repository was not changed.")
	if res.ProjectDir != "" {
		fmt.Println("Saved in Cully's notes for this project:", res.ProjectDir)
		fmt.Println("To keep it in the repository, review it and copy it where your skills live; Cully will not.")
	}
	for _, step := range res.Manual {
		fmt.Println("Manual step, not applied:", step)
	}
	return nil
}

func parseApplyPlan(out string) (applyPlan, error) {
	out = strings.TrimSpace(out)
	// tolerate a prose preamble: grab the first { ... } block.
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end <= start {
		return applyPlan{}, fmt.Errorf("no JSON object in model output")
	}
	var plan applyPlan
	if err := json.Unmarshal([]byte(out[start:end+1]), &plan); err != nil {
		return applyPlan{}, err
	}
	if plan.Summary == "" {
		plan.Summary = "Apply cully suggestion"
	}
	return plan, nil
}

// applyResult reports what an applied plan did: the private project directory
// Cully wrote to, if any, and the steps it left for the user.
type applyResult struct {
	ProjectDir string
	Manual     []string
}

// manualSteps lists the plan's MCP servers and commands. Cully never runs them or
// writes repository configuration for them; the user decides whether to.
func manualSteps(plan applyPlan) []string {
	var steps []string
	names := make([]string, 0, len(plan.MCPServers))
	for name := range plan.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec, _ := json.Marshal(plan.MCPServers[name])
		steps = append(steps, fmt.Sprintf("add MCP server %s: %s", clipText(name, 60), clipText(string(spec), 200)))
	}
	for _, cmd := range plan.ShellCommands {
		if cmd = strings.TrimSpace(cmd); cmd != "" {
			steps = append(steps, "run "+clipText(cmd, 200))
		}
	}
	return steps
}

// executePlan saves the plan's rules and skill in Cully's private directory for
// the project. It writes nothing inside the repository.
func executePlan(cwd, suggestion string, plan applyPlan) (applyResult, error) {
	res := applyResult{Manual: manualSteps(plan)}
	if plan.ClaudeMDSection == "" && (plan.SkillName == "" || plan.SkillContent == "") {
		return res, nil
	}
	dir, err := projectSkillRoot(cwd)
	if err != nil {
		return res, fmt.Errorf("project notes: %w", err)
	}
	res.ProjectDir = dir
	if plan.ClaudeMDSection != "" {
		if err := appendProjectRules(dir, plan.ClaudeMDSection, suggestionMarker(suggestion)); err != nil {
			return res, fmt.Errorf("save project rules: %w", err)
		}
	}
	if plan.SkillName != "" && plan.SkillContent != "" {
		if err := writeSkillIn(dir, plan.SkillName, plan.SkillContent); err != nil {
			return res, fmt.Errorf("write skill: %w", err)
		}
	}
	return res, nil
}

// executeSelfPlan writes an approved `cully improve` plan: rules and a skill
// under ~/.cully, Cully's own directory. It ignores MCP servers and commands.
func executeSelfPlan(home, suggestion string, plan applyPlan) error {
	if plan.ClaudeMDSection != "" {
		if err := appendInstructionFile(sharedSkillPath(home, "cully"), plan.ClaudeMDSection, suggestionMarker(suggestion)); err != nil {
			return fmt.Errorf("update agent instructions: %w", err)
		}
	}
	if plan.SkillName != "" && plan.SkillContent != "" {
		if err := writeSkill(home, plan.SkillName, plan.SkillContent); err != nil {
			return fmt.Errorf("write skill: %w", err)
		}
	}
	return nil
}

func suggestionMarker(suggestion string) string {
	s := strings.TrimSpace(suggestion)
	if len(s) > 48 {
		s = s[:48]
	}
	return "cully:" + strings.ReplaceAll(s, "\n", " ")
}

// appendProjectRules appends a marked section to the project's skill in Cully's
// private directory, creating it with a valid skill header on first use.
func appendProjectRules(dir, section, marker string) error {
	path := filepath.Join(dir, projectSkillFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := "---\nname: " + filepath.Base(dir) + "\ndescription: Cully's notes for this project, kept outside the repository.\n---\n\n# Project notes (Cully)\n"
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
			return err
		}
	}
	return appendInstructionFile(path, section, marker)
}

func appendInstructionFile(path, section, marker string) error {
	markerLine := "<!-- " + marker + " -->"
	existing, _ := os.ReadFile(path)
	if strings.Contains(string(existing), markerLine) {
		fmt.Printf("%s already has this cully rule (%s)\n", path, marker)
		return nil
	}
	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("\n")
	}
	b.WriteString(markerLine)
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(section))
	b.WriteString("\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeSkill(cwd, name, content string) error {
	return writeSkillIn(sharedSkillRoot(cwd), name, content)
}

// writeSkillIn writes <root>/<name>/SKILL.md. The name must be a single path
// element so a skill cannot be written outside root.
func writeSkillIn(root, name, content string) error {
	name = strings.Trim(name, "/")
	if name == "" {
		return fmt.Errorf("empty skill name")
	}
	if name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, projectSkillFile), []byte(strings.TrimSpace(content)+"\n"), 0o644)
}

// readSessionSignals returns the resolved session's own signals — never another
// session's, which is what the old newest-file-wins scan did.
func readSessionSignals(session string) string {
	if session == "" {
		return ""
	}
	b, err := os.ReadFile(sessionSignalsFile(session))
	if err != nil {
		return ""
	}
	return string(b)
}

// removeSuggestion drops line n (1-based) from the session's report.
func removeSuggestion(session string, n int) error {
	lines := readSuggestionsLimit(session, maxReportLines)
	if n < 1 || n > len(lines) {
		return nil
	}
	lines = append(lines[:n-1], lines[n:]...)
	if len(lines) == 0 {
		_ = os.Remove(sessionReportFile(session))
		return nil
	}
	st, _ := readSessionStamp(sessionReportFile(session))
	return writeReportLines(session, st.Cwd, lines)
}
