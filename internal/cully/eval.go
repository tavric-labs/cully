package cully

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mcp-runtime/cully/internal/evals"
)

// `cully eval` scores Cully as one agent: the hooks and skill it installs, the
// harness audit that reports them, the advisor that watches a session, and the
// memory it recalls. Every check compares real Cully code with ground truth
// that is planted in a fixture or computed by hand. No check asks a model to
// grade another model. docs/evals.md explains the method and its limits.

// EvalCheck is one pass/fail comparison against known ground truth.
type EvalCheck struct {
	Name   string
	Pass   bool
	Detail string `json:",omitempty"`
}

// EvalSuite is the result of one suite. Notes are measurements without a
// threshold; they are reported but never fail a run.
type EvalSuite struct {
	Name    string
	Skipped string `json:",omitempty"`
	Checks  []EvalCheck
	Notes   []string `json:",omitempty"`
}

func (s *EvalSuite) check(name string, pass bool, format string, args ...any) {
	s.Checks = append(s.Checks, EvalCheck{Name: name, Pass: pass, Detail: fmt.Sprintf(format, args...)})
}

func (s *EvalSuite) note(format string, args ...any) {
	s.Notes = append(s.Notes, fmt.Sprintf(format, args...))
}

// Failed counts failing checks.
func (s EvalSuite) Failed() int {
	n := 0
	for _, c := range s.Checks {
		if !c.Pass {
			n++
		}
	}
	return n
}

var evalSuites = []string{"setup", "harness", "improve", "tasks", "advisor", "memory"}

type evalOptions struct {
	days, max, live     int
	repeat              int
	project, agent, dir string
	mcpURL              string
	semanticMin         float64
	asJSON              bool
	suites              []string
	explicit            bool
}

func parseEval(args []string) (evalOptions, error) {
	o := evalOptions{}
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.suites, args = append(o.suites, args[0]), args[1:]
	}
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.IntVar(&o.repeat, "repeat", 1, "setup, harness and improve: run N times and fail if the results differ")
	fs.IntVar(&o.days, "days", 30, "advisor: look back this many days")
	fs.StringVar(&o.project, "project", "personal", "advisor: only transcript folders containing this text")
	fs.IntVar(&o.max, "max", 40, "advisor: most sessions to replay")
	fs.IntVar(&o.live, "live", 0, "advisor: also ask the real advisor about this many checkpoints (uses model tokens and web search)")
	fs.StringVar(&o.agent, "agent", "", "advisor: history and live-pass agent (default: cully run agent, otherwise detected)")
	fs.StringVar(&o.dir, "dir", "", "advisor: override the selected agent's transcript directory")
	fs.StringVar(&o.mcpURL, "mcp-url", "", "memory: MCP endpoint that needs no sign-in, such as the local stack")
	fs.Float64Var(&o.semanticMin, "semantic-min", 0, "memory: fail when semantic recall is below this (0 reports only)")
	fs.BoolVar(&o.asJSON, "json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.suites = append(o.suites, fs.Args()...)
	if o.repeat < 1 || o.repeat > 10 {
		return o, fmt.Errorf("--repeat must be between 1 and 10")
	}
	o.explicit = len(o.suites) > 0
	if !o.explicit {
		o.suites = evalSuites
	}
	for _, name := range o.suites {
		known := false
		for _, s := range evalSuites {
			known = known || s == name
		}
		if !known {
			return o, fmt.Errorf("unknown suite %q; choose from %s", name, strings.Join(evalSuites, ", "))
		}
	}
	if o.explicit && o.mcpURL == "" {
		for _, name := range o.suites {
			if name == "memory" {
				return o, fmt.Errorf("the memory suite needs --mcp-url, for example http://127.0.0.1:3393/mcp")
			}
		}
	}
	return o, nil
}

// RunEval implements `cully eval [setup|harness|advisor|memory] [flags]`. It
// returns an error when any check fails so scripts and CI can gate on it.
func RunEval(w io.Writer, args []string) error {
	o, err := parseEval(args)
	if err != nil {
		return err
	}
	for _, suite := range o.suites {
		if suite == "advisor" {
			src, err := measurementSource(o.agent, o.dir)
			if err != nil {
				return err
			}
			o.agent = src.Agent
			if o.dir == "" {
				o.dir = transcriptRoot(src.Agent)
			}
		}
	}
	var results []EvalSuite
	for _, name := range o.suites {
		var s EvalSuite
		switch name {
		case "setup":
			s = repeatSuite(evalSetup, o.repeat)
		case "harness":
			s = repeatSuite(evalHarness, o.repeat)
		case "improve":
			s = repeatSuite(evalImprove, o.repeat)
		case "tasks":
			s = repeatSuite(func() EvalSuite { return evalTasks(o) }, o.repeat)
		case "advisor":
			s = evalAdvisor(o)
		case "memory":
			s = evalMemory(o)
		}
		results = append(results, s)
	}
	failed := 0
	for _, s := range results {
		failed += s.Failed()
	}
	if o.asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return err
		}
	} else {
		writeEval(w, results)
	}
	if failed > 0 {
		return fmt.Errorf("%d eval check(s) failed", failed)
	}
	return nil
}

func writeEval(w io.Writer, results []EvalSuite) {
	for _, s := range results {
		fmt.Fprintf(w, "%s\n", strings.ToUpper(s.Name))
		if s.Skipped != "" {
			fmt.Fprintf(w, "  skipped: %s\n\n", s.Skipped)
			continue
		}
		for _, c := range s.Checks {
			mark := "pass"
			if !c.Pass {
				mark = "FAIL"
			}
			fmt.Fprintf(w, "  %s  %s", mark, c.Name)
			if c.Detail != "" {
				fmt.Fprintf(w, " — %s", c.Detail)
			}
			fmt.Fprintln(w)
		}
		for _, n := range s.Notes {
			fmt.Fprintf(w, "  note  %s\n", n)
		}
		fmt.Fprintln(w)
	}
	passed, failed, skipped := 0, 0, 0
	for _, s := range results {
		if s.Skipped != "" {
			skipped++
		}
		for _, c := range s.Checks {
			if c.Pass {
				passed++
			} else {
				failed++
			}
		}
	}
	fmt.Fprintf(w, "%d passed, %d failed, %d suite(s) skipped\n", passed, failed, skipped)
}

// isolated runs fn with HOME and every agent's config directory pointing at
// fresh temporary directories, then restores the environment. It also discards
// what fn prints, so setup code can run without touching the user's files.
func isolated(fn func(root string)) {
	root, err := os.MkdirTemp("", "cully-eval-home-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	vars := map[string]string{
		"HOME": root, "CLAUDE_CONFIG_DIR": filepath.Join(root, ".claude"),
		"CODEX_HOME": filepath.Join(root, ".codex"), "CURSOR_CONFIG_DIR": filepath.Join(root, ".cursor"),
	}
	saved := map[string]*string{}
	for k, v := range vars {
		if old, ok := os.LookupEnv(k); ok {
			saved[k] = &old
		} else {
			saved[k] = nil
		}
		os.Setenv(k, v)
	}
	defer func() {
		for k, old := range saved {
			if old == nil {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, *old)
			}
		}
	}()
	stdout := os.Stdout
	if null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		os.Stdout = null
		defer func() { os.Stdout = stdout; null.Close() }()
	}
	fn(root)
}

func fileHas(path, text string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), text)
}

func dirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// evalSetup installs Cully for every agent into an empty home and checks the
// result from the outside: the files exist, the harness audit independently
// reports them, nothing leaks into the project, rerunning changes nothing, and
// uninstall removes only what Cully owns.
func evalSetup() EvalSuite {
	return evalSetupWithInstaller(installAgents)
}

func evalSetupWithInstaller(install func(string, bool, ...string) error) EvalSuite {
	s := EvalSuite{Name: "setup"}
	isolated(func(root string) {
		project := filepath.Join(root, "project")
		_ = os.MkdirAll(project, 0o755)
		cwd, _ := os.Getwd()
		_ = os.Chdir(project)
		defer os.Chdir(cwd)

		claude, codex, cursor := ConfigDir(), CodexConfigDir(), CursorConfigDir()
		// User-owned content that setup and uninstall must never remove.
		_ = os.MkdirAll(claude, 0o755)
		_ = os.MkdirAll(codex, 0o755)
		userSettings := `{"permissions":{"allow":["Bash(ls:*)"]}}`
		userAgents := "# My Codex rules\n\nAlways run tests.\n"
		_ = os.WriteFile(filepath.Join(claude, "settings.json"), []byte(userSettings), 0o600)
		_ = os.WriteFile(filepath.Join(codex, "AGENTS.md"), []byte(userAgents), 0o600)

		if err := install("", false, "claude", "codex", "cursor"); err != nil {
			s.check("install for every agent", false, "%v", err)
			return
		}
		s.check("install for every agent", true, "")
		for _, agent := range []struct{ name, dir string }{{"claude", claude}, {"codex", codex}, {"cursor", cursor}} {
			skill := filepath.Join(agent.dir, ".cully-runtime", "skills", "cully", "SKILL.md")
			b, err := os.ReadFile(skill)
			s.check(agent.name+" skill is the canonical one", err == nil && strings.Contains(string(b), strings.TrimSpace(cullySkill())), "%s", skill)
		}
		s.check("claude hooks reference cully", fileHas(filepath.Join(claude, "settings.json"), "cully") && fileHas(filepath.Join(claude, "settings.json"), `"hooks"`), "")
		s.check("codex hooks and prompt installed", fileHas(filepath.Join(codex, "hooks.json"), "cully") || fileHas(filepath.Join(codex, "config.toml"), "[hooks"), "")
		s.check("cursor hooks installed", fileHas(filepath.Join(cursor, "hooks.json"), "cully"), "")

		// The harness audit is a second, independent reader of what setup wrote.
		found := map[string]bool{}
		for _, item := range auditHarness(harnessDirs{Claude: claude, Codex: codex, Cursor: cursor}) {
			found[item.Component] = item.Present()
		}
		for _, component := range []string{"Instructions", "Hooks and scripts"} {
			s.check("harness audit finds "+component+" after setup", found[component], "")
		}
		s.check("setup left the project untouched", dirEmpty(project), "")

		before, _ := os.ReadFile(filepath.Join(claude, "settings.json"))
		if err := install("", false, "claude", "codex", "cursor"); err != nil {
			s.check("setup is repeatable", false, "%v", err)
		} else {
			after, _ := os.ReadFile(filepath.Join(claude, "settings.json"))
			s.check("rerunning setup changes nothing", bytes.Equal(before, after), "")
		}

		if err := Uninstall("claude", "codex", "cursor"); err != nil {
			s.check("uninstall every agent", false, "%v", err)
			return
		}
		s.check("uninstall removes the skills", !fileHas(filepath.Join(claude, ".cully-runtime", "skills", "cully", "SKILL.md"), "") && !fileHas(filepath.Join(codex, ".cully-runtime", "skills", "cully", "SKILL.md"), "") && !fileHas(filepath.Join(cursor, ".cully-runtime", "skills", "cully", "SKILL.md"), ""), "")
		s.check("uninstall removes the hooks", !fileHas(filepath.Join(claude, "settings.json"), `"hooks"`) && !fileHas(filepath.Join(cursor, "hooks.json"), "cully"), "")
		s.check("uninstall keeps the user's Claude permissions", fileHas(filepath.Join(claude, "settings.json"), "Bash(ls:*)"), "")
		s.check("uninstall keeps the user's Codex instructions", fileHas(filepath.Join(codex, "AGENTS.md"), "Always run tests."), "")
		s.check("uninstall removes Cully's Codex section", !fileHas(filepath.Join(codex, "AGENTS.md"), "cully:codex"), "")
	})
	return s
}

// harnessScenario is a directory layout with a known answer: which of the
// audit's components genuinely exist in it.
type harnessScenario struct {
	name  string
	build func(dirs harnessDirs)
	want  map[string]bool
}

func write(path, text string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(text), 0o600)
}

func harnessScenarios() []harnessScenario {
	all := func(v bool) map[string]bool {
		return map[string]bool{"Instructions": v, "Guardrails": v, "Memory": v, "Skills": v, "Subagents": v, "Hooks and scripts": v, "Plugins": v}
	}
	with := func(base map[string]bool, keys ...string) map[string]bool {
		for _, k := range keys {
			base[k] = true
		}
		return base
	}
	return []harnessScenario{
		{"empty setup", func(harnessDirs) {}, all(false)},
		{"every component", func(d harnessDirs) {
			write(filepath.Join(d.Claude, "CLAUDE.md"), "rules")
			write(filepath.Join(d.Claude, "settings.json"), `{"permissions":{},"hooks":{},"enabledPlugins":{},"mcp":"cully"}`)
			write(filepath.Join(d.Claude, "skills", "s", "SKILL.md"), "skill")
			write(filepath.Join(d.Claude, "agents", "reviewer.md"), "agent")
		}, all(true)},
		{"permissions only", func(d harnessDirs) {
			write(filepath.Join(d.Claude, "settings.json"), `{"permissions":{"allow":[]}}`)
		}, with(all(false), "Guardrails")},
		{"empty folders are not skills or subagents", func(d harnessDirs) {
			_ = os.MkdirAll(filepath.Join(d.Claude, "skills"), 0o755)
			_ = os.MkdirAll(filepath.Join(d.Claude, "agents"), 0o755)
		}, all(false)},
		{"project instructions and memory", func(d harnessDirs) {
			write(filepath.Join(d.Project, "AGENTS.md"), "rules")
			write(filepath.Join(d.Project, ".mcp.json"), `{"mcpServers":{"cully":{}}}`)
		}, with(all(false), "Instructions", "Memory")},
		{"codex guardrails and hooks", func(d harnessDirs) {
			write(filepath.Join(d.Codex, "config.toml"), "approval_policy = \"on-request\"\n[hooks.SessionStart]\n")
		}, with(all(false), "Guardrails", "Hooks and scripts")},
	}
}

// evalHarness plants known setups and checks the audit reports exactly those
// components, with neither misses nor false alarms.
func evalHarness() EvalSuite { return evalHarnessScenarios(harnessScenarios()) }

func evalHarnessScenarios(scenarios []harnessScenario) EvalSuite {
	return evalHarnessWithAudit(scenarios, auditHarness)
}

func evalHarnessWithAudit(scenarios []harnessScenario, audit func(harnessDirs) []harnessItem) EvalSuite {
	s := EvalSuite{Name: "harness"}
	tp, fp, fn := 0, 0, 0
	for _, sc := range scenarios {
		root, err := os.MkdirTemp("", "cully-eval-harness-")
		if err != nil {
			s.check(sc.name, false, "%v", err)
			continue
		}
		dirs := harnessDirs{Claude: filepath.Join(root, "claude"), Codex: filepath.Join(root, "codex"), Cursor: filepath.Join(root, "cursor"), Project: filepath.Join(root, "project")}
		for _, d := range []string{dirs.Claude, dirs.Codex, dirs.Cursor, dirs.Project} {
			_ = os.MkdirAll(d, 0o755)
		}
		sc.build(dirs)
		var wrong []string
		seen := map[string]bool{}
		for _, item := range audit(dirs) {
			if seen[item.Component] {
				wrong = append(wrong, "duplicate component: "+item.Component)
			}
			seen[item.Component] = true
			got, want := item.Present(), sc.want[item.Component]
			switch {
			case got && want:
				tp++
			case got:
				fp++
				wrong = append(wrong, "false alarm: "+item.Component)
			case want:
				fn++
				wrong = append(wrong, "missed: "+item.Component)
			}
		}
		for component := range sc.want {
			if !seen[component] {
				wrong = append(wrong, "missing audit result: "+component)
			}
		}
		_ = os.RemoveAll(root)
		s.check(sc.name, len(wrong) == 0, "%s", strings.Join(wrong, ", "))
	}
	if tp+fn > 0 && tp+fp > 0 {
		s.note("components: %d found correctly, %d false alarms, %d missed", tp, fp, fn)
	}
	return s
}

// plantedTranscript builds a session whose tool output is fully known: sixty
// small searches, then reads of bytes characters each. Twelve 12,000 character
// reads are 36,000 tokens of oversized output; twelve 400 character reads are
// ordinary.
func plantedTranscript(readBytes int) [][]byte {
	body := strings.Repeat("x", readBytes)
	var lines [][]byte
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("g%d", i)
		lines = append(lines, []byte(fmt.Sprintf(`{"message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"Grep","input":{}}]}}`, id)),
			[]byte(fmt.Sprintf(`{"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"hit"}]}}`, id)))
	}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("r%d", i)
		lines = append(lines, []byte(fmt.Sprintf(`{"message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"Read","input":{"file_path":"/p/f%d.go"}}]}}`, id, i)),
			[]byte(fmt.Sprintf(`{"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}}`, id, body)))
	}
	return lines
}

func writeLines(path string, lines [][]byte) error {
	return os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600)
}

// plantedJournal builds one session journal, a minute per event, from a compact
// spec: E edit, F failed check, G failed check of another command, P passing
// check of F's command.
func plantedJournal(spec string) []journalEvent {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var out []journalEvent
	for i, c := range spec {
		ev := journalEvent{Time: start.Add(time.Duration(i) * time.Minute)}
		switch c {
		case 'E':
			ev.Class = journalEdit
		case 'F':
			ev.Class, ev.Failed, ev.Sig = journalCheck, true, "a"
		case 'G':
			ev.Class, ev.Failed, ev.Sig = journalCheck, true, "b"
		case 'P':
			ev.Class, ev.Sig = journalCheck, "a"
		}
		out = append(out, ev)
	}
	return out
}

// loopScenarios have answers fixed by the rules documented in loops.go: three
// repeats of one failing command, or three edits each followed by a failed
// check, with a passing check resetting both.
var loopScenarios = []struct {
	name, spec string
	want       string // loop kind, or "" when the session is not stuck
}{
	{"the same command fails three times", "FFF", loopRepeatFailure},
	{"fails again after each edit", "FEFEF", loopRepeatFailure},
	{"three edits each followed by a failed check", "EGEFEG", loopEditCycle},
	{"cycles resume after a passing check", "EFPEGEFEG", loopEditCycle},
	{"a passing check resets the count", "FFPF", ""},
	{"two failures are not a loop", "FEFE", ""},
	{"different commands failing once each", "FGF", ""},
	{"edits that were never checked", "EEEEE", ""},
}

// guardScenarios plant a command's history, F for a failed run and P for a
// passing one, and say whether the PreToolUse guard should speak before the next.
var guardScenarios = []struct {
	name, history string
	speak         bool
}{
	{"fails twice in a row", "FF", true},
	{"fails again after a pass", "PFF", true},
	{"one failure is not enough", "F", false},
	{"a pass resets the count", "FFP", false},
	{"no history", "", false},
}

func evalGuard(s *EvalSuite, dir string) {
	for _, tc := range []struct {
		name  string
		class string
		count int
		want  string
	}{
		{"ordinary exploration", journalSearch, 19, ""},
		{"prolonged exploration", journalSearch, 20, "exploration"},
		{"unchecked edits", journalEdit, 12, "unchecked"},
	} {
		var events []journalEvent
		for i := 0; i < tc.count; i++ {
			events = append(events, journalEvent{Class: tc.class})
		}
		got := guardCheckpoint(events)
		s.check("guard checkpoint: "+tc.name, got.reason == tc.want, "found %q, want %q", got.reason, tc.want)
	}
	const command = "go test ./... -run TestWidget"
	for _, sc := range guardScenarios {
		var events []journalEvent
		for _, c := range sc.history {
			events = append(events, journalEvent{Class: journalCheck, Failed: c == 'F', Sig: commandSig(command)})
		}
		// A different command's failures must never count against this one.
		events = append(events, journalEvent{Class: journalCheck, Failed: true, Sig: commandSig("make lint")})
		advice, _ := guardBashAdvice(events, command)
		s.check("guard: command that "+sc.name, (advice != "") == sc.speak, "history %q: spoke=%v, want %v", sc.history, advice != "", sc.speak)
	}
	big, small := filepath.Join(dir, "big.txt"), filepath.Join(dir, "small.txt")
	_ = os.WriteFile(big, []byte(strings.Repeat("x", 3*guardBigFileBytes/2)), 0o600)
	_ = os.WriteFile(small, []byte(strings.Repeat("x", guardBigFileBytes/8)), 0o600)
	for _, sc := range []struct {
		name, path    string
		offset, limit int
		speak         bool
	}{
		{"whole read of a large file", big, 0, 0, true},
		{"large file read with a limit", big, 0, 200, false},
		{"large file read from an offset", big, 100, 0, false},
		{"whole read of a small file", small, 0, 0, false},
		{"file that does not exist", filepath.Join(dir, "missing"), 0, 0, false},
	} {
		advice := guardReadAdvice(sc.path, sc.offset, sc.limit)
		s.check("guard: "+sc.name, (advice != "") == sc.speak, "spoke=%v, want %v", advice != "", sc.speak)
	}
}

// evalAdvisor has three parts. First, planted sessions with known waste: the
// advisor's signals must count it exactly, speak when it is significant and
// stay quiet when it is not. Second, the metric code must reproduce
// hand-computed ContextBench values. Third, when real transcripts exist, they
// are replayed and measured, and --live asks the real advisor about some of
// them and checks that it cites no figure it was not given.
func evalAdvisor(o evalOptions) EvalSuite {
	return evalAdvisorWithDetectors(o, scanWaste, detectLoops)
}

func evalAdvisorWithDetectors(o evalOptions, wasteScan func([][]byte) *TokenWaste, loops func([]journalEvent) loopReport) EvalSuite {
	s := EvalSuite{Name: "advisor"}
	var points []evalCheckpoint
	var sessions []SessionContext
	isolated(func(root string) {
		scratch := filepath.Join(root, "scratch")
		_ = os.MkdirAll(scratch, 0o755)
		wasteful, healthy := filepath.Join(scratch, "wasteful.jsonl"), filepath.Join(scratch, "healthy.jsonl")
		wastefulLines, healthyLines := plantedTranscript(12000), plantedTranscript(400)
		if writeLines(wasteful, wastefulLines) != nil || writeLines(healthy, healthyLines) != nil {
			s.check("write planted sessions", false, "")
			return
		}
		waste := wasteScan(wastefulLines)
		s.check("counts the planted oversized output exactly", waste.BigResults == 12 && waste.FullReads == 12 && waste.BigTokens == 36000,
			"found %d big results, %d full reads, %d tokens; planted 12, 12 and 36000", waste.BigResults, waste.FullReads, waste.BigTokens)
		calm := wasteScan(healthyLines)
		s.check("counts no oversized output in an ordinary session", calm.BigResults == 0, "found %d", calm.BigResults)

		cwd := filepath.Join(root, "project")
		_ = os.MkdirAll(cwd, 0o755)
		wastePoints, calmPoints := evalSession(wasteful, cwd, scratch), evalSession(healthy, cwd, scratch)
		if len(wastePoints) != 3 || len(calmPoints) != 3 {
			s.check("replays planted sessions at three checkpoints", false, "got %d and %d", len(wastePoints), len(calmPoints))
			return
		}
		s.check("quiet before the waste appears", !wastePoints[0].Advised, "advised at %d%%", wastePoints[0].Percent)
		s.check("speaks once the waste is significant", wastePoints[2].Advised && strings.Contains(wastePoints[2].Signals, "token_waste_significant=yes"), "")
		falseAlarms := 0
		for _, p := range calmPoints {
			if p.Advised {
				falseAlarms++
			}
		}
		s.check("never speaks in the ordinary session", falseAlarms == 0, "%d false alarm(s)", falseAlarms)

		// Loop detection is the advisor's deterministic monitor: LivePlan (Liu et
		// al., 2026, arXiv:2608.06701) flags repeated failed actions the same way
		// before any model is consulted. Check it against planted sessions, and
		// count a spurious alarm as a regression, as that paper does.
		for _, sc := range loopScenarios {
			report := loops(plantedJournal(sc.spec))
			var got []string
			for _, l := range report.Loops {
				got = append(got, l.Kind)
			}
			want := []string{}
			if sc.want != "" {
				want = []string{sc.want}
			}
			s.check("loops: "+sc.name, strings.Join(got, ",") == strings.Join(want, ","), "journal %q found %v, want %v", sc.spec, got, want)
		}

		// The guard acts on the same journal and on file sizes, so it gets the same
		// treatment: planted answers, with a spurious nudge counted as a failure.
		evalGuard(&s, scratch)

		// The scorer must reproduce values worked out by hand from the paper.
		early := scoreSessionContext([]string{"/a", "/b", "/c", "/a"}, map[string]bool{"/a": true})
		late := scoreSessionContext([]string{"/x", "/y", "/z"}, map[string]bool{"/z": true})
		s.check("reproduces ContextBench redundancy and AUC-Cov by hand",
			near(early.Redundant, 1.0/3) && near(early.AUCCov, 1) && near(late.AUCCov, 1.0/3) && near(late.Precision, 1.0/3),
			"redundancy %.3f (want 0.333), AUC-Cov %.3f and %.3f (want 1 and 0.333)", early.Redundant, early.AUCCov, late.AUCCov)

		g := "full_file_reads=12(36000 tok) big_bash=0(0 tok)"
		cited, bad := groundFigures([]string{"ADV|12 full reads cost 36,000 tokens; about 36k"}, g)
		s.check("accepts figures taken from its signals", cited == 3 && len(bad) == 0, "grounded figures: cited %d, ungrounded %v", cited, bad)
		_, bad = groundFigures([]string{"ADV|this wasted 90,000 tokens"}, g)
		s.check("catches an invented figure", len(bad) == 1, "ungrounded %v", bad)
	})
	// Real transcripts are read outside the isolated home: the user's own files.
	src := transcriptSources[o.agent]
	if o.agent == "" {
		src = transcriptSources["claude"]
	}
	if src.Agent == "" {
		s.note("replay unavailable for %s", agentDisplayName(o.agent))
		return s
	}
	if _, err := os.Stat(o.dir); err != nil {
		s.note("replay skipped: no %s transcripts at %s", src.Name, o.dir)
		if o.live > 0 {
			s.check("live advisor ran", false, "requested --live but transcript source is unavailable")
		}
		return s
	}
	isolated(func(string) {
		var err error
		points, sessions, err = replayAgentTranscripts(src, o.dir, o.project, o.days, o.max)
		if err != nil {
			s.note("replay failed: %v", err)
		}
	})
	if len(points) == 0 {
		s.note("replay skipped: no transcripts of %d+ lines for project %q in the last %d days", 40, o.project, o.days)
		if o.live > 0 {
			s.check("live advisor ran", false, "requested --live but no eligible checkpoints were available")
		}
		return s
	}
	ev := summarizeEval(points, len(sessions))
	ev.Context = summarizeContext(sessions)
	s.note("replayed %d sessions: advice precision %.0f%%, recall %.0f%% (hindsight proxies: waste followed, not that the advice was followed)", ev.Sessions, ev.Precision*100, ev.Recall*100)
	c := ev.Context
	if src.Agent == "claude" {
		s.note("file-level context (ContextBench definitions; the session's edited files stand in for gold): redundancy %.2f over all sessions; recall %.2f, precision %.2f, AUC-Cov %.2f over %d sessions that edited files",
			c.Redundancy, c.Recall, c.Precision, c.AUCCov, c.WithEdits)
	} else {
		s.note("file-level context is unavailable for %s shell history", src.Name)
	}
	if o.live > 0 {
		isolated(func(string) { ev.runLive(o.agent, points, o.live) })
		if len(ev.Live) == 0 {
			s.check("live advisor ran", false, "no checkpoints")
			return s
		}
		s.check("live advisor answered", ev.LiveOK == len(ev.Live), "%d of %d", ev.LiveOK, len(ev.Live))
		s.check("live advice cites only figures it was given", ev.LiveUngrounded == 0, "%d of %d cited figures not in the signals", ev.LiveUngrounded, ev.LiveCited)
		spoke := 0
		quiet := 0
		for _, r := range ev.Live {
			if r.Quiet {
				quiet++
				if len(r.Lines) > 0 {
					spoke++
				}
			}
		}
		if quiet > 0 {
			s.note("on %d checkpoints with no waste signal the advisor spoke %d time(s)", quiet, spoke)
		}
	}
	return s
}

// evalMemory runs the retrieval eval in internal/evals against a live server.
func evalMemory(o evalOptions) EvalSuite {
	s := EvalSuite{Name: "memory"}
	if o.mcpURL == "" {
		s.Skipped = "pass --mcp-url (for the local stack: http://127.0.0.1:3393/mcp)"
		return s
	}
	ds, err := evals.Load()
	if err != nil {
		s.check("load dataset", false, "%v", err)
		return s
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	store, err := evals.Connect(ctx, o.mcpURL)
	if err != nil {
		s.check("connect to "+o.mcpURL, false, "%v", err)
		return s
	}
	defer store.Close()
	run := strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	report, err := evals.Run(ctx, store, ds, "eval-"+run, "eval-other-"+run, "eval-"+run, 5)
	if err != nil {
		s.check("run retrieval eval", false, "%v", err)
		return s
	}
	err = report.Check(evals.ModeText, evals.RetrievalThresholds)
	s.check("text search meets the retrieval thresholds", err == nil, "%v", err)
	if o.semanticMin > 0 {
		err = report.Check(evals.ModeSemantic, evals.Thresholds{Recall: o.semanticMin})
		s.check(fmt.Sprintf("semantic recall at least %.2f", o.semanticMin), err == nil, "%v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(report.Format()), "\n") {
		s.note("%s", strings.TrimSpace(line))
	}
	return s
}
