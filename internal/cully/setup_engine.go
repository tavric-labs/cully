package cully

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The setup engine is the one place that knows how Cully is set up for a user:
// which harnesses are installed and linked, which mode was chosen, the rules
// interview and its proposal, and the agents and jobs worth suggesting. The
// Setup page (setup_handlers.go) and the terminal wizard (setup_wizard.go) are
// two thin skins over it, so both ask the same questions and write the same
// files. Nothing here calls a model unless the user asks (Analyse).

const (
	setupMinSessions = 20 // fewer analysed sessions than this and nothing is suggested
	metaSetupMode    = "setup.mode"
	metaSetupURL     = "setup.mcpUrl"
	metaSetupDone    = "setup.done"
	metaSetupSkipped = "setup.rules.skipped"
)

type setupEngine struct {
	m *uiManager
}

func newSetupEngine(m *uiManager) *setupEngine { return &setupEngine{m: m} }

// newSetupEngineLocal builds an engine for the terminal, where no UI server runs.
func newSetupEngineLocal(repos ...string) *setupEngine {
	return &setupEngine{m: newUIManager(uiConfig{Repos: repos})}
}

// ---- state

type setupStep struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Detail string `json:"detail"`
}

type setupHarness struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Linked    bool   `json:"linked"`
	SkillPath string `json:"skillPath"`
}

type setupRulesInfo struct {
	Exists  bool   `json:"exists"`
	Path    string `json:"path"`
	Updated string `json:"updated"`
	Lines   int    `json:"lines"`
}

type setupUsage struct {
	Count  int     `json:"count"`
	Hours  float64 `json:"hours"`
	Tokens int64   `json:"tokens"`
}

type setupSessions struct {
	Count         int                   `json:"count"`
	Hours         float64               `json:"hours"`
	Tokens        int64                 `json:"tokens"`
	Days          int                   `json:"days"`
	ByHarness     map[string]setupUsage `json:"byHarness"`
	TokensPartial bool                  `json:"tokensPartial,omitempty"` // some transcripts were not read, so tokens is a lower bound
}

type setupState struct {
	Done      bool              `json:"done"`
	Mode      string            `json:"mode"`
	Modes     map[string]string `json:"modes"`
	Steps     []setupStep       `json:"steps"`
	Harnesses []setupHarness    `json:"harnesses"`
	Rules     setupRulesInfo    `json:"rules"`
	Sessions  setupSessions     `json:"sessions"`
}

func setupModesText() map[string]string {
	return map[string]string{"local": SetupModeLocalText, "mcp": SetupModeMCPText}
}

func setupMode() string {
	switch v := uiDB().metaGet(metaSetupMode); v {
	case "local", "mcp":
		return v
	}
	return ""
}

// SetupDone reports whether setup is finished, for the info line and banner.
func SetupDone() bool {
	return setupIsDone(setupMode(), setupRulesExist())
}

func setupIsDone(mode string, rulesExist bool) bool {
	db := uiDB()
	if db.metaGet(metaSetupDone) == "1" {
		return true
	}
	return mode != "" && (rulesExist || db.metaGet(metaSetupSkipped) == "1")
}

func (e *setupEngine) State() setupState {
	mode := setupMode()
	harnesses := setupHarnesses()
	rules := setupRules()
	sess := e.sessionsUsage()
	st := setupState{Mode: mode, Modes: setupModesText(), Harnesses: harnesses, Rules: rules, Sessions: sess}
	st.Done = setupIsDone(mode, rules.Exists)
	installed, linked, withSkill := 0, 0, 0
	for _, h := range harnesses {
		if h.Installed {
			installed++
			if h.Linked {
				linked++
			}
			if fileExists(runtimeSkillPath(h.ID)) {
				withSkill++
			}
		}
	}
	skipped := uiDB().metaGet(metaSetupSkipped) == "1"
	st.Steps = []setupStep{
		{ID: "install", Title: "Install Cully into your coding agents", Done: installed > 0 && withSkill == installed,
			Detail: fmt.Sprintf("%d of %d installed agents have the Cully skill.", withSkill, installed)},
		{ID: "mode", Title: "Choose how Cully runs", Done: mode != "",
			Detail: map[string]string{"": "Not chosen yet: local mode for one machine, MCP mode for many.", "local": "Local mode: history and recall stay on this machine.", "mcp": "MCP mode: agents share one Cully MCP service."}[mode]},
		{ID: "rules", Title: "Write your dev-workflow rules", Done: rules.Exists || skipped,
			Detail: setupRulesDetail(rules, skipped, linked, installed)},
		{ID: "suggest", Title: "Review suggested agents and background jobs", Done: uiDB().metaGet(metaSetupDone) == "1",
			Detail: fmt.Sprintf("Based on %d sessions.", sess.Count)},
	}
	return st
}

func setupRulesDetail(r setupRulesInfo, skipped bool, linked, installed int) string {
	switch {
	case r.Exists:
		return fmt.Sprintf("Rules written (%d lines) and linked into %d of %d installed agents.", r.Lines, linked, installed)
	case skipped:
		return "You chose to skip the rules."
	}
	return "No rules yet: answer a few questions, or skip them."
}

func setupRulesExist() bool { return setupRules().Exists }

func setupRules() setupRulesInfo {
	src := workflowSkillPath()
	text, path := "", src
	if b, err := os.ReadFile(src); err == nil {
		text = string(b)
	} else if cur := uiRulesCurrent(); cur != "" {
		text = cur
		for _, t := range uiRulesTargets() {
			if t.Exists {
				path = t.Path
				break
			}
		}
	}
	info := setupRulesInfo{Path: path}
	if text == "" {
		return info
	}
	info.Exists = true
	info.Lines = strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	if st, err := os.Stat(path); err == nil {
		info.Updated = st.ModTime().UTC().Format(time.RFC3339)
	}
	return info
}

func setupHarnesses() []setupHarness {
	var out []setupHarness
	for _, h := range setupHarnessHomes() {
		sh := setupHarness{ID: h.ID, Name: h.Name, Installed: codingAgentInstalled(h.ID)}
		if sh.Installed {
			sh.Version = uiHarnessVersion(h.ID)
		}
		sh.Linked, sh.SkillPath = setupLinkState(h)
		out = append(out, sh)
	}
	return out
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// withFiles fills in each session's transcript path, which the library hides from the page.
func (m *uiManager) withFiles(in []uiIndexed) []uiIndexed {
	m.idxMu.Lock()
	defer m.idxMu.Unlock()
	m.loadIndexLocked()
	out := make([]uiIndexed, len(in))
	for i, s := range in {
		if e := m.index.Sessions[s.ID]; e != nil {
			s.File = e.File
		}
		out[i] = s
	}
	return out
}

func (e *setupEngine) sessionsUsage() setupSessions {
	lib := e.m.sessionLibrary()
	lib.Sessions = e.m.withFiles(lib.Sessions)
	out := setupSessions{ByHarness: map[string]setupUsage{"claude": {}, "codex": {}, "cursor": {}}}
	per, partial := setupTokensFor(lib.Sessions)
	out.TokensPartial = partial
	days := map[string]bool{}
	secs := map[string]int{}
	for _, s := range lib.Sessions {
		out.Count++
		u := out.ByHarness[s.Agent]
		u.Count++
		u.Tokens += per[s.ID]
		secs[s.Agent] += s.ActiveSec
		out.ByHarness[s.Agent] = u
		out.Tokens += per[s.ID]
		if !s.Updated.IsZero() {
			days[s.Updated.Local().Format("2006-01-02")] = true
		}
	}
	total := 0
	for a, u := range out.ByHarness {
		u.Hours = round1(float64(secs[a]) / 3600)
		out.ByHarness[a] = u
		total += secs[a]
	}
	out.Hours = round1(float64(total) / 3600)
	out.Days = len(days)
	return out
}

// ---- mode

// setupMCPURLOK accepts https, or http only to this machine.
func setupMCPURLOK(raw string) error {
	if err := validateMCPURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	if u.Scheme == "https" {
		return nil
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	return errors.New("the MCP URL must use https (plain http is allowed only for localhost)")
}

// SetMode records the chosen mode. MCP mode registers the URL with every
// installed harness through the same code as `cully mcp add`; no credentials are stored.
func (e *setupEngine) SetMode(mode, mcpURL string) (setupState, error) {
	mcpURL = strings.TrimSpace(mcpURL)
	switch mode {
	case "local":
		uiDB().metaSet(metaSetupURL, "")
	case "mcp":
		if err := setupMCPURLOK(mcpURL); err != nil {
			return setupState{}, err
		}
		var failed []string
		n := 0
		for _, h := range setupHarnessHomes() {
			if !codingAgentInstalled(h.ID) {
				continue
			}
			n++
			if err := AddMCP(devNull{}, h.ID, mcpURL, false); err != nil {
				failed = append(failed, h.ID+": "+err.Error())
			}
		}
		if n == 0 {
			return setupState{}, errors.New("no coding agent is installed on this machine to connect")
		}
		if len(failed) > 0 {
			return setupState{}, errors.New("could not register the MCP URL: " + strings.Join(failed, "; "))
		}
		uiDB().metaSet(metaSetupURL, mcpURL)
	default:
		return setupState{}, errors.New(`mode must be "local" or "mcp"`)
	}
	uiDB().metaSet(metaSetupMode, mode)
	return e.State(), nil
}

// RecordMode stores a mode that setup flags already applied (for example --mcp-url).
func (e *setupEngine) RecordMode(mode, mcpURL string) {
	if mode == "local" || mode == "mcp" {
		uiDB().metaSet(metaSetupMode, mode)
		uiDB().metaSet(metaSetupURL, mcpURL)
	}
}

type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

// Complete marks setup finished. skipRules records that the user chose no rules.
func (e *setupEngine) Complete(skipRules bool) setupState {
	if skipRules && !setupRulesExist() {
		uiDB().metaSet(metaSetupSkipped, "1")
	}
	uiDB().metaSet(metaSetupDone, "1")
	return e.State()
}

// ---- suggestions

type setupAgentSuggestion struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Role              string   `json:"role"`
	Why               string   `json:"why"`
	Evidence          []string `json:"evidence"`
	Count             int      `json:"count"`
	EstMinutesPerWeek int      `json:"estMinutesPerWeek"`
	EstTokensPerRun   int64    `json:"estTokensPerRun"`
	Folder            string   `json:"folder"`
	// Set when an agent you already have covers this work: the suggestion is an update to it, not a new agent.
	Replaces     string `json:"replaces,omitempty"`
	ReplacesName string `json:"replacesName,omitempty"`
}

type setupJobSuggestion struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Command           string   `json:"command"`
	Schedule          string   `json:"schedule"`
	Why               string   `json:"why"`
	Evidence          []string `json:"evidence"`
	Count             int      `json:"count"`
	EstMinutesPerWeek int      `json:"estMinutesPerWeek"`
	Folder            string   `json:"folder"`
}

type setupSuggestions struct {
	Basis  setupBasis             `json:"basis"`
	Agents []setupAgentSuggestion `json:"agents"`
	Jobs   []setupJobSuggestion   `json:"jobs"`
	Note   string                 `json:"note"`
}

type setupBasis struct {
	Sessions int     `json:"sessions"`
	Hours    float64 `json:"hours"`
	Tokens   int64   `json:"tokens"`
	Days     int     `json:"days"`
}

func medianInt64(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func evidenceTitles(r uiRepeat) []string {
	out := []string{}
	for _, ev := range r.Evidence {
		if len(out) < 5 {
			out = append(out, fmt.Sprintf("%s (%s, %s)", ev.Title, ev.Agent, ev.At.Local().Format("2 Jan")))
		}
	}
	return out
}

func (e *setupEngine) Suggestions() setupSuggestions {
	use := e.sessionsUsage()
	lib := e.m.sessionLibrary()
	out := setupSuggestions{Basis: setupBasis{Sessions: use.Count, Hours: use.Hours, Tokens: use.Tokens, Days: use.Days}, Agents: []setupAgentSuggestion{}, Jobs: []setupJobSuggestion{}}
	if lib.Totals.Analyzed < setupMinSessions {
		out.Note = fmt.Sprintf("Cully has analysed %d sessions. It needs at least %d to suggest agents or background jobs without guessing; keep working and check back.", lib.Totals.Analyzed, setupMinSessions)
		if !lib.Analysis.On && lib.Totals.Sessions >= setupMinSessions {
			out.Note = fmt.Sprintf("You have %d sessions but session analysis is off, so none are analysed. Turn it on in the Sessions page to get suggestions.", lib.Totals.Sessions)
		}
		return out
	}
	lib.Sessions = e.m.withFiles(lib.Sessions)
	byID := map[string]uiIndexed{}
	for _, s := range lib.Sessions {
		byID[s.ID] = s
	}
	var evSessions []uiIndexed
	repeats := uiRepeatedWork(lib.Sessions)
	for _, r := range repeats {
		for _, ev := range r.Evidence {
			if s, ok := byID[ev.ID]; ok {
				evSessions = append(evSessions, s)
			}
		}
	}
	tok, _ := setupTokensFor(evSessions)
	repeats = setupMergeGoalRepeats(repeats)
	existing := e.m.identityList()
	for i, r := range repeats {
		var mins, toks []int64
		for _, ev := range r.Evidence {
			s, ok := byID[ev.ID]
			if !ok {
				continue
			}
			mins = append(mins, int64(s.ActiveSec))
			if t, ok := tok[s.ID]; ok && t > 0 {
				toks = append(toks, t)
			}
		}
		typical := float64(medianInt64(mins)) / 60
		weeks := math.Max(1, float64(uiRepeatWindow/(24*time.Hour))/7)
		perWeek := int(math.Round(float64(r.Count) * typical / weeks))
		id := fmt.Sprintf("%s-%d", r.Kind, i+1)
		switch r.Kind {
		case "goal":
			why := fmt.Sprintf("In %s you started the same kind of task %d times over %d days (about %d minutes each).", r.Folder, r.Count, r.Days, int(math.Round(typical)))
			sg := setupAgentSuggestion{ID: id, Name: r.Folder + " helper", Role: "Takes over the repeated task in " + r.Folder + ": " + firstOr(r.Examples, "the work you keep repeating") + ".",
				Why: why, Evidence: evidenceTitles(r), Count: r.Count, EstMinutesPerWeek: perWeek, EstTokensPerRun: medianInt64(toks), Folder: r.Path}
			if cur := setupCoveringAgent(existing, sg); cur != nil {
				if uiSimilar(uiGoalWords(cur.Role), uiGoalWords(sg.Role)) {
					continue // the agent you have already does this
				}
				sg.Replaces, sg.ReplacesName, sg.Name = cur.ID, cur.Name, cur.Name
				sg.Why += " Your agent " + cur.Name + " already works in this folder, so this updates it instead of adding another."
			}
			out.Agents = append(out.Agents, sg)
		case "untested":
			cmd := setupJobCommandFor(r.Path)
			if cmd == nil {
				continue
			}
			out.Jobs = append(out.Jobs, setupJobSuggestion{ID: id, Name: r.Folder + " checks", Command: strings.Join(cmd, " "), Schedule: "02:30",
				Why:      fmt.Sprintf("%d sessions in %s changed files and never ran the tests. A nightly check is a command, so it costs no tokens.", r.Count, r.Folder),
				Evidence: evidenceTitles(r), Count: r.Count, EstMinutesPerWeek: perWeek, Folder: r.Path})
		}
	}
	if len(out.Agents) == 0 && len(out.Jobs) == 0 {
		out.Note = "Cully found nothing you repeat often enough to suggest an agent or job. Estimates, when shown, are about the time of those sessions and are not a promise."
	} else {
		out.Note = "Estimates say about: time is how often the work repeated times the usual length of those sessions, tokens are the median of the evidence sessions."
	}
	return out
}

func firstOr(l []string, def string) string {
	if len(l) > 0 {
		return l[0]
	}
	return def
}

// setupMergeGoalRepeats folds the goal patterns of one folder into a single one, so a folder gets one
// suggestion however many separate clusters its sessions fall into. Other kinds pass through unchanged.
func setupMergeGoalRepeats(in []uiRepeat) []uiRepeat {
	var out []uiRepeat
	at := map[string]int{}
	for _, r := range in {
		if r.Kind != "goal" {
			out = append(out, r)
			continue
		}
		i, ok := at[r.Path]
		if !ok {
			at[r.Path] = len(out)
			out = append(out, r)
			continue
		}
		m := &out[i]
		m.Count += r.Count
		if r.Days > m.Days {
			m.Days = r.Days
		}
		for _, x := range r.Examples {
			if len(m.Examples) < 3 {
				m.Examples = append(m.Examples, x)
			}
		}
		for _, ev := range r.Evidence {
			if len(m.Evidence) < 12 {
				m.Evidence = append(m.Evidence, ev)
			}
		}
	}
	return out
}

// setupCoveringAgent returns an existing agent that already does this job: the same name or job, or any
// project agent tied to the same folder.
func setupCoveringAgent(existing []uiIdentity, sg setupAgentSuggestion) *uiIdentity {
	for i := range existing {
		e := &existing[i]
		if uiAgentSame(sg.Name, sg.Role, sg.Folder, e.Name, e.Role, e.Folder) || (e.Scope == "project" && e.Folder != "" && filepath.Clean(e.Folder) == filepath.Clean(sg.Folder)) {
			return e
		}
	}
	return nil
}
