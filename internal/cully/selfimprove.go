package cully

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// The self-improvement loop scores Cully with its own `cully eval` on a
// schedule, keeps the history, and turns failing checks into proposals. A
// proposal changes nothing until the user approves it with `cully improve
// --apply N`; an approved proposal is then written by Cully itself, limited to
// instruction rules and a skill under ~/.cully. It never adds MCP servers or
// runs commands, and it cannot change Cully's own code.

const (
	selfEvalDefaultInterval = 5 * time.Hour
	selfEvalMinInterval     = 30 * time.Minute
	selfEvalTimeout         = 15 * time.Minute
	selfEvalHistoryMax      = 1 << 20
	improvementsMax         = 50
	improveContentMax       = 12 << 10
)

var selfEvalSuites = []string{"setup", "harness", "improve", "tasks", "advisor"}

func evalHistoryFile() string  { return filepath.Join(cullyDir(), "eval-history.jsonl") }
func improvementsFile() string { return filepath.Join(cullyDir(), "improvements.json") }

type evalFailure struct {
	Check  string `json:"check"`
	Detail string `json:"detail,omitempty"`
}

type evalSuiteRecord struct {
	Name    string        `json:"name"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Skipped string        `json:"skipped,omitempty"`
	Failing []evalFailure `json:"failing,omitempty"`
}

type evalRun struct {
	At     time.Time         `json:"at"`
	Suites []evalSuiteRecord `json:"suites"`
	Failed int               `json:"failed"`
}

// improvement is one proposal. Status is pending, applied, resolved or
// dismissed; only a pending proposal can be applied.
type improvement struct {
	ID       string        `json:"id"`
	Suite    string        `json:"suite"`
	Evidence []evalFailure `json:"evidence"`
	Status   string        `json:"status"`
	Created  time.Time     `json:"created"`
	Updated  time.Time     `json:"updated"`
	Summary  string        `json:"summary,omitempty"`
}

// terminalSafe removes escape sequences and control characters from text that a
// model or an evaluation produced before it reaches a terminal, so it cannot
// move the cursor, rewrite the screen or set the clipboard. Line breaks and
// tabs become spaces.
func terminalSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, ansi.Strip(s))
}

// stripControls cleans text written to a file: escape sequences and control
// characters go, line breaks and tabs stay.
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r == '\r' || r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(redactSecrets(terminalSafe(s))), " ")
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func summarizeEvalRun(at time.Time, suites []EvalSuite) evalRun {
	run := evalRun{At: at.UTC()}
	for _, s := range suites {
		rec := evalSuiteRecord{Name: s.Name, Skipped: s.Skipped}
		for _, c := range s.Checks {
			if c.Pass {
				rec.Passed++
				continue
			}
			rec.Failed++
			rec.Failing = append(rec.Failing, evalFailure{Check: clipText(c.Name, 120), Detail: clipText(c.Detail, 200)})
		}
		run.Failed += rec.Failed
		run.Suites = append(run.Suites, rec)
	}
	return run
}

func appendEvalHistory(run evalRun) {
	line, err := json.Marshal(run)
	if err != nil {
		return
	}
	path := evalHistoryFile()
	if info, err := os.Stat(path); err == nil && info.Size() > selfEvalHistoryMax {
		trimImpactLedger(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func readEvalHistory(limit int) []evalRun {
	f, err := os.Open(evalHistoryFile())
	if err != nil {
		return nil
	}
	defer f.Close()
	var runs []evalRun
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r evalRun
		if json.Unmarshal(sc.Bytes(), &r) == nil && !r.At.IsZero() {
			runs = append(runs, r)
		}
	}
	if limit > 0 && len(runs) > limit {
		runs = runs[len(runs)-limit:]
	}
	return runs
}

func improvementID(suite string, failing []evalFailure) string {
	names := make([]string, 0, len(failing))
	for _, f := range failing {
		names = append(names, f.Check)
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(suite + "\x00" + strings.Join(names, "\x00")))
	return suite + "-" + hex.EncodeToString(sum[:4])
}

// updateImprovements folds a run into the proposals. A failure set that was
// already proposed refreshes its evidence, and reopens a proposal whose fix
// did not hold. Proposals whose failures have gone become resolved, so the
// history shows what an applied change fixed. Dismissed proposals stay dismissed.
func updateImprovements(existing []improvement, run evalRun) []improvement {
	byID := map[string]*improvement{}
	out := make([]improvement, len(existing))
	copy(out, existing)
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	current := map[string]bool{}
	for _, s := range run.Suites {
		if len(s.Failing) == 0 {
			continue
		}
		id := improvementID(s.Name, s.Failing)
		current[id] = true
		if p, ok := byID[id]; ok {
			p.Evidence, p.Updated = s.Failing, run.At
			if p.Status == "applied" || p.Status == "resolved" {
				p.Status = "pending"
			}
			continue
		}
		out = append(out, improvement{ID: id, Suite: s.Name, Evidence: s.Failing, Status: "pending", Created: run.At, Updated: run.At})
	}
	ranSuite := map[string]bool{}
	for _, s := range run.Suites {
		ranSuite[s.Name] = s.Skipped == ""
	}
	for i := range out {
		p := &out[i]
		if !current[p.ID] && ranSuite[p.Suite] && (p.Status == "pending" || p.Status == "applied") {
			p.Status, p.Updated = "resolved", run.At
		}
	}
	if len(out) > improvementsMax {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
		out = out[:improvementsMax]
	}
	return out
}

func readImprovements() []improvement {
	data, err := os.ReadFile(improvementsFile())
	if err != nil {
		return nil
	}
	var list []improvement
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

func writeImprovements(list []improvement) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(improvementsFile()), ".improvements-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	_, writeErr := tmp.Write(append(data, '\n'))
	if err := tmp.Close(); err != nil || writeErr != nil {
		return fmt.Errorf("write improvements: %v %v", err, writeErr)
	}
	return os.Rename(tmp.Name(), improvementsFile())
}

// pendingImprovements lists proposals waiting for approval, in apply order.
func pendingImprovements() []improvement {
	var out []improvement
	for _, p := range readImprovements() {
		if p.Status == "pending" {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// selfEvalInterval is how often the daemon re-evaluates Cully. CULLY_EVAL_INTERVAL
// takes a duration such as 5h; "off" disables the schedule.
func selfEvalInterval() (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv("CULLY_EVAL_INTERVAL"))
	if raw == "" {
		return selfEvalDefaultInterval, true
	}
	if strings.EqualFold(raw, "off") {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return selfEvalDefaultInterval, true
	}
	return max(d, selfEvalMinInterval), true
}

func selfEvalDue(now time.Time, interval time.Duration) bool {
	runs := readEvalHistory(1)
	return len(runs) == 0 || now.Sub(runs[0].At) >= interval
}

// evalRunner produces the suites to record. It is a variable so tests can
// supply fixtures instead of running the real evaluation.
var evalRunner = runEvalSubprocess

// runEvalSubprocess runs `cully eval` in its own process: the setup suite
// redirects configuration directories through the environment, which must not
// leak into the daemon's other work. Failing checks exit non-zero but still
// print their JSON.
func runEvalSubprocess(ctx context.Context) ([]EvalSuite, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, selfEvalTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, append(append([]string{"eval"}, selfEvalSuites...), "--repeat", "2", "--json")...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	runErr := cmd.Run()
	var suites []EvalSuite
	if err := json.Unmarshal(stdout.Bytes(), &suites); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("eval: %w", runErr)
		}
		return nil, fmt.Errorf("eval: unreadable output: %w", err)
	}
	return suites, nil
}

// runSelfEval evaluates Cully now, records the run and refreshes the proposals.
func runSelfEval(ctx context.Context, now time.Time) (evalRun, error) {
	suites, err := evalRunner(ctx)
	if err != nil {
		return evalRun{}, err
	}
	run := summarizeEvalRun(now, suites)
	appendEvalHistory(run)
	if err := writeImprovements(updateImprovements(readImprovements(), run)); err != nil {
		return run, err
	}
	return run, nil
}

// selfImproveLoop re-evaluates Cully every interval, remembering the last run
// across daemon restarts so a restart does not trigger an immediate re-run.
func selfImproveLoop(ctx context.Context) {
	interval, on := selfEvalInterval()
	if !on {
		return
	}
	check := func() {
		if !selfEvalDue(time.Now(), interval) {
			return
		}
		run, err := runSelfEval(ctx, time.Now())
		if err != nil {
			daemonLog("self-eval: %v", err)
			return
		}
		daemonLog("self-eval: %d failing check(s); proposals refreshed", run.Failed)
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	check()
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			check()
		}
	}
}

const improveInstr = `You are Cully's self-improvement planner. Cully scored itself with its own evaluations and some checks failed. The user approved a change that helps Cully and the coding agents that use it avoid or report the failing behavior.
Read the EVIDENCE, then reply with ONLY a single JSON object (no markdown fences) shaped like:
{
  "summary": "one-line description of what will change",
  "claude_md_section": "short markdown rules for coding agents that avoid or report the failing behavior, or an empty string",
  "skill_name": "kebab-case skill folder name or empty",
  "skill_content": "full SKILL.md markdown for a short, concrete workflow skill, or empty",
  "notes": "follow-ups that need a change to Cully's own code or a person, or empty"
}
Rules:
- Only write instruction rules and one small skill. Never include MCP servers, shell commands, credentials or file paths outside a skill.
- You cannot change Cully's code. If the failure needs a code change, say so in notes and keep the rules minimal.
- Be concrete and brief; reference the failing check names.`

// improvePlanner turns evidence into a plan. It is a variable so tests do not
// call a model.
var improvePlanner = func(prompt string) (string, error) {
	agent := plannerAgent()
	switch agent {
	case "":
		return "", errors.New("no coding agent CLI found: install claude, codex or cursor-agent")
	case "claude":
		return runClaude("", prompt)
	}
	home, _ := os.UserHomeDir()
	return runAdvisorAgentContext(context.Background(), agent, home, false, prompt)
}

// plannerAgent picks the coding agent that plans an improvement: the one
// running Cully (CULLY_AGENT) when its CLI is installed, otherwise the first
// installed of Claude, Codex and Cursor. It returns "" when none is installed.
func plannerAgent() string {
	installed := func(id string) bool {
		spec, ok := lookupAgentSpec(id)
		if !ok || spec.Binary == "" {
			return false
		}
		binary := spec.Binary
		if id == "claude" {
			binary = claudeExecutable()
		}
		_, err := exec.LookPath(binary)
		return err == nil
	}
	if id := normalizeAgentID(os.Getenv("CULLY_AGENT")); id != "" && installed(id) {
		return id
	}
	for _, id := range []string{"claude", "codex", "cursor"} {
		if installed(id) {
			return id
		}
	}
	return ""
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func improvePrompt(p improvement) string {
	var b strings.Builder
	b.WriteString(improveInstr)
	fmt.Fprintf(&b, "\n\nEVIDENCE (suite %s):\n", p.Suite)
	for _, f := range p.Evidence {
		fmt.Fprintf(&b, "- %s", f.Check)
		if f.Detail != "" {
			fmt.Fprintf(&b, ": %s", f.Detail)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// sanitizeImprovePlan keeps only what a self-improvement may write, and fails
// on anything that looks like a credential or an unsafe skill name.
func sanitizeImprovePlan(plan applyPlan) (applyPlan, []string, error) {
	var dropped []string
	if len(plan.MCPServers) > 0 {
		dropped = append(dropped, "MCP servers")
	}
	if len(plan.ShellCommands) > 0 {
		dropped = append(dropped, "shell commands")
	}
	plan.MCPServers, plan.ShellCommands = nil, nil
	// The summary and notes are printed and the rules and skill are written;
	// none may carry escape sequences.
	plan.Summary, plan.Notes = terminalSafe(plan.Summary), terminalSafe(plan.Notes)
	plan.ClaudeMDSection, plan.SkillContent = stripControls(plan.ClaudeMDSection), stripControls(plan.SkillContent)
	if plan.SkillName != "" && !skillNamePattern.MatchString(plan.SkillName) {
		return plan, dropped, fmt.Errorf("refusing skill name %q", plan.SkillName)
	}
	for _, text := range []string{plan.ClaudeMDSection, plan.SkillContent} {
		if len(text) > improveContentMax {
			return plan, dropped, fmt.Errorf("plan is too long; nothing written")
		}
		if redactSecrets(text) != text {
			return plan, dropped, fmt.Errorf("plan contains something that looks like a credential; nothing written")
		}
	}
	return plan, dropped, nil
}

func writeRunSummary(w io.Writer, run evalRun) {
	fmt.Fprintf(w, "Last eval %s: ", run.At.Local().Format("Jan 02 15:04"))
	var parts []string
	for _, s := range run.Suites {
		switch {
		case s.Skipped != "":
			parts = append(parts, s.Name+" skipped")
		default:
			parts = append(parts, fmt.Sprintf("%s %d/%d", s.Name, s.Passed, s.Passed+s.Failed))
		}
	}
	fmt.Fprintf(w, "%s\n", strings.Join(parts, " · "))
}

// RunImprove implements `cully improve [--run] [--apply N] [--dismiss N] [--dry-run] [--yes]`.
func RunImprove(w io.Writer, in io.Reader, args []string) error {
	fs := flag.NewFlagSet("improve", flag.ContinueOnError)
	fs.SetOutput(w)
	runNow := fs.Bool("run", false, "evaluate Cully now instead of waiting for the schedule")
	apply := fs.Int("apply", 0, "apply pending proposal N after approval")
	dismiss := fs.Int("dismiss", 0, "dismiss pending proposal N")
	dryRun := fs.Bool("dry-run", false, "with --apply, show the plan and write nothing")
	yes := fs.Bool("yes", false, "with --apply, skip the confirmation prompt")
	list := fs.Bool("list", false, "print the proposals instead of opening the window")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: cully improve [--run] [--list] [--apply N [--dry-run] [--yes]] [--dismiss N]")
	}
	if *runNow {
		fmt.Fprintln(w, "Evaluating Cully (setup, harness, improve, tasks, advisor)…")
		run, err := runSelfEval(context.Background(), time.Now())
		if err != nil {
			return err
		}
		writeRunSummary(w, run)
		fmt.Fprintln(w)
	}
	pending := pendingImprovements()
	pick := func(n int) (improvement, error) {
		if n < 1 || n > len(pending) {
			return improvement{}, fmt.Errorf("proposal #%d not found: %d pending; run: cully improve", n, len(pending))
		}
		return pending[n-1], nil
	}
	switch {
	case *dismiss > 0:
		p, err := pick(*dismiss)
		if err != nil {
			return err
		}
		return setImprovementStatus(p.ID, "dismissed", "", w, "Dismissed "+p.ID+".")
	case *apply > 0:
		p, err := pick(*apply)
		if err != nil {
			return err
		}
		return applyImprovement(w, in, p, *dryRun, *yes)
	}
	if outFile, ok := w.(*os.File); ok && !*list && len(pending) > 0 {
		if inFile, ok := in.(*os.File); ok && term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd())) {
			home, _ := os.UserHomeDir()
			return runImproveTUI(outFile, inFile, pending, improveActions{
				Plan:  planImprovement,
				Apply: commitImprovement,
				Drop: func(p improvement) error {
					return setImprovementStatus(p.ID, "dismissed", "", io.Discard, "")
				},
				Home: home,
			})
		}
	}
	if !*runNow {
		if runs := readEvalHistory(1); len(runs) > 0 {
			writeRunSummary(w, runs[0])
			fmt.Fprintln(w)
		}
	}
	history := readEvalHistory(5)
	if len(history) > 1 {
		var trend []string
		for _, r := range history {
			trend = append(trend, fmt.Sprint(r.Failed))
		}
		fmt.Fprintf(w, "Failing checks over the last %d runs: %s\n\n", len(history), strings.Join(trend, " → "))
	}
	if len(history) == 0 {
		fmt.Fprintln(w, "No eval has run yet. The daemon evaluates Cully every 5 hours (CULLY_EVAL_INTERVAL changes it, off disables it); run cully improve --run to evaluate now.")
		return nil
	}
	if len(pending) == 0 {
		fmt.Fprintln(w, "No pending proposals: the last eval found nothing to improve.")
		return nil
	}
	fmt.Fprintln(w, "Proposed improvements (nothing changes until you approve one):")
	for i, p := range pending {
		fmt.Fprintf(w, "\n[%d] %s — %d failing check(s) in %s\n", i+1, p.ID, len(p.Evidence), p.Suite)
		for _, f := range p.Evidence {
			fmt.Fprintf(w, "      - %s\n", f.Check)
		}
	}
	fmt.Fprintln(w, "\nPreview: cully improve --apply <n> --dry-run  ·  Approve: cully improve --apply <n>  ·  Skip: cully improve --dismiss <n>")
	return nil
}

func setImprovementStatus(id, status, summary string, w io.Writer, message string) error {
	list := readImprovements()
	for i := range list {
		if list[i].ID == id {
			list[i].Status, list[i].Updated = status, time.Now().UTC()
			if summary != "" {
				list[i].Summary = summary
			}
		}
	}
	if err := writeImprovements(list); err != nil {
		return err
	}
	fmt.Fprintln(w, message)
	return nil
}

// planImprovement asks the planner for a change and keeps only what a
// self-improvement may write.
func planImprovement(p improvement) (applyPlan, []string, error) {
	out, err := improvePlanner(improvePrompt(p))
	if err != nil {
		return applyPlan{}, nil, fmt.Errorf("plan improvement: %w", err)
	}
	plan, err := parseApplyPlan(out)
	if err != nil {
		return applyPlan{}, nil, fmt.Errorf("parse plan: %w", err)
	}
	return sanitizeImprovePlan(plan)
}

func planWritesAnything(plan applyPlan) bool {
	return plan.ClaudeMDSection != "" || (plan.SkillName != "" && plan.SkillContent != "")
}

// planBullets describes what an approved plan would write, one line each.
func planBullets(home string, plan applyPlan, dropped []string) []string {
	var out []string
	if plan.ClaudeMDSection != "" {
		out = append(out, fmt.Sprintf("Add rules to %s", sharedSkillPath(home, "cully")))
	}
	if plan.SkillName != "" && plan.SkillContent != "" {
		out = append(out, fmt.Sprintf("Install skill %s", sharedSkillPath(home, plan.SkillName)))
	}
	if plan.Notes != "" {
		out = append(out, "Note: "+plan.Notes)
	}
	if len(dropped) > 0 {
		out = append(out, "Ignored (self-improvement never adds these): "+strings.Join(dropped, ", "))
	}
	return out
}

// commitImprovement writes an approved plan and marks the proposal applied.
func commitImprovement(p improvement, plan applyPlan) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := executePlan(home, "cully-self:"+p.ID, plan); err != nil {
		return err
	}
	recordAppliedChange("self", "", plan.Summary)
	return setImprovementStatus(p.ID, "applied", plan.Summary, io.Discard, "")
}

func applyImprovement(w io.Writer, in io.Reader, p improvement, dryRun, yes bool) error {
	plan, dropped, err := planImprovement(p)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Proposal %s (%s)\n\nPlan: %s\n", p.ID, p.Suite, plan.Summary)
	for _, line := range planBullets(home, plan, dropped) {
		fmt.Fprintf(w, "  • %s\n", line)
	}
	fmt.Fprintln(w)
	if !planWritesAnything(plan) {
		fmt.Fprintln(w, "Nothing to write: this needs a change to Cully's code or a person. The proposal stays pending.")
		return nil
	}
	if dryRun {
		fmt.Fprintln(w, "Dry run — no changes made.")
		return nil
	}
	if !yes {
		fmt.Fprint(w, "Apply this improvement? [y/N]: ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(w, "Cancelled; the proposal stays pending.")
			return nil
		}
	}
	if err := commitImprovement(p, plan); err != nil {
		return err
	}
	fmt.Fprintln(w, "\033[32mApplied.\033[0m Cully re-evaluates on its schedule; run cully improve --run to check the effect now.")
	return nil
}

// improvementTitle is the one-line name of a proposal: the suite and what failed.
func improvementTitle(p improvement) string {
	suite := p.Suite
	if suite != "" {
		suite = strings.ToUpper(suite[:1]) + suite[1:]
	}
	if len(p.Evidence) == 0 {
		return suite
	}
	title := fmt.Sprintf("%s: %s", terminalSafe(suite), terminalSafe(p.Evidence[0].Check))
	if n := len(p.Evidence) - 1; n > 0 {
		title += fmt.Sprintf(" (+%d more)", n)
	}
	return title
}
