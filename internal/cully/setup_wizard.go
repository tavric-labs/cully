package cully

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// SetupWizardOptions drive the terminal wizard. Input and output are injected
// so tests can script it. With AnswersFile set, nothing is asked: the file's
// answers are used (a JSON map of question id to answer, null to skip).
type SetupWizardOptions struct {
	In          io.Reader
	Out         io.Writer
	Mode        string // "local" or "mcp" when a flag already chose and applied it; empty asks
	MCPURL      string
	NoInterview bool     // skip the rules questions
	AnswersFile string   // answer from a file instead of asking
	Yes         bool     // approve the proposed rules without asking
	Repos       []string // projects to learn commands and conventions from, besides the folders in your sessions
	Index       bool     // read this machine's sessions first (off in tests)
	// Install puts Cully into the detected agents after the mode is chosen;
	// endpoint is the MCP URL in MCP mode. Nil uses the normal installer.
	Install func(endpoint string) error
}

// InstallAgentsOnly installs Cully's hooks and skills for detected agents
// without starting any service. endpoint registers the Cully MCP when given.
func InstallAgentsOnly(endpoint string, targets ...string) error {
	return installAgents(endpoint, false, targets...)
}

// RecordSetupMode stores a mode that setup flags have already applied.
func RecordSetupMode(mode, mcpURL string) { newSetupEngineLocal().RecordMode(mode, mcpURL) }

type wizard struct {
	o  SetupWizardOptions
	in *bufio.Reader
	e  *setupEngine
}

func (w *wizard) say(format string, a ...any) { fmt.Fprintf(w.o.Out, format+"\n", a...) }

// ask prints a prompt and reads one trimmed line. At end of input it returns "q".
func (w *wizard) ask(prompt string) string {
	fmt.Fprint(w.o.Out, prompt)
	line, err := w.in.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(w.o.Out)
		return "q"
	}
	return strings.TrimSpace(line)
}

// RunSetupWizard walks the user through harnesses, mode, the rules interview,
// the approval and the suggestions.
func RunSetupWizard(o SetupWizardOptions) error {
	if o.In == nil {
		o.In = strings.NewReader("")
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	w := &wizard{o: o, in: bufio.NewReader(o.In), e: newSetupEngineLocal(o.Repos...)}
	if o.Index {
		w.say("Reading your recent sessions on this machine (no model is used)...")
		w.e.m.indexSessions(500)
	}
	w.harnesses()
	if err := w.chooseMode(); err != nil {
		return err
	}
	skipped, err := w.rules()
	if err != nil {
		return err
	}
	w.suggestions()
	w.e.Complete(skipped)
	w.say("\nSetup is finished.")
	return nil
}

func (w *wizard) harnesses() {
	w.say("\nStep 1. Your coding agents")
	any := false
	for _, h := range w.e.State().Harnesses {
		if !h.Installed {
			w.say("  %-12s not found", h.Name)
			continue
		}
		any = true
		link := "rules not linked yet"
		if h.Linked {
			link = "rules linked"
		}
		ver := ""
		if h.Version != "" {
			ver = " " + h.Version
		}
		w.say("  %-12s installed%s, %s", h.Name, ver, link)
	}
	if !any {
		w.say("  No coding agent was found. Install Claude Code, Codex or Cursor, then run cully setup again.")
	}
}

func (w *wizard) chooseMode() error {
	w.say("\nStep 2. How should Cully run?")
	mode, url := w.o.Mode, w.o.MCPURL
	if mode != "" {
		w.e.RecordMode(mode, url)
		w.say("  Mode: %s (chosen with a setup option).", mode)
		return nil
	}
	w.say("  local: %s", SetupModeLocalText)
	w.say("  mcp:   %s", SetupModeMCPText)
	for mode == "" {
		switch strings.ToLower(w.ask("Mode [local/mcp] (local): ")) {
		case "", "l", "local":
			mode = "local"
		case "m", "mcp":
			mode = "mcp"
		case "q":
			return errors.New("setup stopped before a mode was chosen")
		default:
			w.say("  Please type local or mcp.")
		}
	}
	if mode == "mcp" {
		for tries := 0; url == "" && tries < 3; tries++ {
			v := w.ask("Cully MCP URL (https): ")
			if v == "q" {
				return errors.New("setup stopped before the MCP URL was given")
			}
			if err := setupMCPURLOK(v); err != nil {
				w.say("  %v", err)
				continue
			}
			url = v
		}
		if url == "" {
			return errors.New("no valid MCP URL was given; deploy the Cully MCP service first, or choose local mode")
		}
	}
	install := w.o.Install
	if install == nil {
		install = func(endpoint string) error { return InstallAgentsOnly(endpoint) }
	}
	if err := install(url); err != nil {
		return err
	}
	w.e.RecordMode(mode, url)
	w.say("  Mode set to %s.", mode)
	return nil
}

func (w *wizard) rules() (skipped bool, err error) {
	w.say("\nStep 3. Your dev-workflow rules")
	if w.o.NoInterview {
		w.say("  Skipped (--no-interview).")
		return !setupRulesExist(), nil
	}
	answers, err := w.collect()
	if err != nil {
		return false, err
	}
	if len(answers) == 0 {
		w.say("  No answers were given, so no rules were written.")
		return !setupRulesExist(), nil
	}
	p, err := w.e.Propose(answers)
	if err != nil {
		return false, err
	}
	w.say("\n%s", p.Summary)
	changed := 0
	for _, d := range p.Diff {
		switch d.T {
		case "add":
			w.say("+ %s", d.L)
			changed++
		case "del":
			w.say("- %s", d.L)
			changed++
		}
	}
	if changed == 0 {
		_ = w.e.Discard(p.ID)
		return false, nil
	}
	ok := w.o.Yes
	if !ok {
		ok = strings.HasPrefix(strings.ToLower(w.ask("Approve these changes? [y/N] ")), "y")
	}
	if !ok {
		_ = w.e.Discard(p.ID)
		w.say("  Nothing was written.")
		return !setupRulesExist(), nil
	}
	res, err := w.e.Approve(p.ID)
	if err != nil {
		return false, err
	}
	w.say("  Wrote %s", res.Path)
	if len(res.Linked) > 0 {
		w.say("  Linked into: %s", strings.Join(res.Linked, ", "))
	}
	for _, n := range res.Notes {
		w.say("  Note: %s", n)
	}
	w.say("  Restart your coding agents so they read the rules.")
	return false, nil
}

// collect asks each question, or reads the answers file.
func (w *wizard) collect() (map[string]*string, error) {
	if w.o.AnswersFile != "" {
		b, err := os.ReadFile(w.o.AnswersFile)
		if err != nil {
			return nil, fmt.Errorf("read answers: %w", err)
		}
		var raw map[string]*string
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("the answers file must be a JSON object of question id to answer or null: %w", err)
		}
		return raw, nil
	}
	w.say("  For each question press Enter to accept the suggestion in brackets, type your own answer,")
	w.say("  type s to skip it, or q to stop asking.")
	out := map[string]*string{}
	iv := w.e.Interview()
	for i, q := range iv.Questions {
		w.say("\n[%d/%d] %s: %s", i+1, len(iv.Questions), q.Group, q.Text)
		if q.Hint != "" {
			w.say("  %s", q.Hint)
		}
		if q.Evidence != "" {
			w.say("  Based on: %s", q.Evidence)
		}
		for n, c := range q.Choices {
			w.say("    %d) %s", n+1, c)
		}
		def := q.Suggested
		if v, ok := iv.Answers[q.ID]; ok {
			def = v
		}
		prompt := "  Answer"
		if def != "" {
			prompt += " [" + def + "]"
		}
		in := w.ask(prompt + ": ")
		switch strings.ToLower(in) {
		case "q":
			return out, nil
		case "s":
			out[q.ID] = nil
			continue
		case "":
			if def == "" {
				out[q.ID] = nil
				continue
			}
			in = def
		}
		if n, err := strconv.Atoi(in); err == nil && n >= 1 && n <= len(q.Choices) {
			in = q.Choices[n-1]
		}
		v := in
		out[q.ID] = &v
	}
	return out, nil
}

func (w *wizard) suggestions() {
	w.say("\nStep 4. What Cully can do for you")
	st := w.e.State().Sessions
	w.say("  Sessions on this machine: %d over %d days, about %.1f hours of work, about %s tokens.", st.Count, st.Days, st.Hours, setupHumanTokens(st.Tokens))
	for _, a := range []string{"claude", "codex", "cursor"} {
		if u := st.ByHarness[a]; u.Count > 0 {
			w.say("    %-7s %d sessions, %.1f h, %s tokens", a, u.Count, u.Hours, setupHumanTokens(u.Tokens))
		}
	}
	if st.TokensPartial {
		w.say("  Some transcripts carry no token counts (Cursor keeps none), so tokens are a lower bound.")
	}
	sg := w.e.Suggestions()
	for _, a := range sg.Agents {
		w.say("  Agent: %s - %s", a.Name, a.Why)
		w.say("    saves about %d min a week; a run costs about %s tokens", a.EstMinutesPerWeek, setupHumanTokens(a.EstTokensPerRun))
	}
	for _, j := range sg.Jobs {
		w.say("  Background job: %s (%s, daily at %s) - %s", j.Name, j.Command, j.Schedule, j.Why)
		w.say("    saves about %d min a week", j.EstMinutesPerWeek)
	}
	if sg.Note != "" {
		w.say("  %s", sg.Note)
	}
	w.say("  Open them with: cully ui, then the Setup page")
}

func setupHumanTokens(n int64) string {
	switch {
	case n <= 0:
		return "unknown"
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return strconv.FormatInt(n, 10)
}
