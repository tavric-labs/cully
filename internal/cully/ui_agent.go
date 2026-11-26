package cully

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	uiMaxPromptRunes = 20000
	uiEventCap       = 5000
	uiStderrTail     = 4096
)

// uiDeniedTools blocks the obvious ways an agent could publish its work or
// reach the network. It is defense in depth, not a sandbox: a determined
// command can still find another route, so run cully ui as a user with no
// credentials you would not give the agent.
// Agents the page runs are headless, so nothing can answer a permission prompt:
// a tool that is not listed here is refused, even when its server is connected.
// These are the Cully memory tools an agent needs to read what the project
// already knows and to save its own summary. Delete and update are left out.
const (
	uiCullyReadTools  = "ToolSearch,mcp__cully__cully_context,mcp__cully__cully_get,mcp__cully__cully_search,mcp__cully__cully_recall,mcp__cully__cully_recent,mcp__cully__cully_projects"
	uiCullyWriteTools = "mcp__cully__cully_log,mcp__cully__cully_session"
)

const uiDeniedTools = "Bash(git push:*),Bash(curl:*),Bash(wget:*),Bash(ssh:*),Bash(scp:*),Bash(nc:*),Bash(kubectl:*)"

var (
	uiModelName = regexp.MustCompile(`^[A-Za-z0-9._\[\]-]{1,64}$`)
	uiSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
)

// Agent states.
const (
	uiPreparing = "preparing"
	uiWorking   = "working"
	uiWaiting   = "waiting"
	uiDone      = "done"
	uiFailed    = "failed"
	uiStopped   = "stopped"
)

type uiConfig struct {
	// ScoutStream runs the scout with live progress; when nil the scout uses Advisor.
	ScoutStream uiScoutFunc
	Repos       []string // allow-list of git repositories agents may work in
	Tokens      *uiTokenPool
	ClaudeBin   string // default: claudeExecutable()
	MaxAgents   int    // live agents at once; default 4
	// NoConflictAlerts stops Cully telling two agents that they edited the same file.
	NoConflictAlerts bool
	// HideSessionsIn keeps sessions started in these folders (and below) out of the Sessions list.
	HideSessionsIn []string
	// Advisor runs the recall and scout stages; default runAdvisorAgent.
	Advisor func(agent, cwd string, research bool, prompt string) (string, error)
}

type uiStartRequest struct {
	Repo    string `json:"repo"`
	Title   string `json:"title"`
	Prompt  string `json:"prompt"`
	Model   string `json:"model"`
	Context bool   `json:"context"`
	Recall  bool   `json:"recall"`
	Scout   bool   `json:"scout"`
	Mode    string `json:"mode"` // "" or "edit" (accept edits) or "plan" (read-only planning)

	// Set by Cully for review agents; never read from the request body.
	addDirs      []string
	rules        string
	Loop         bool            `json:"loop"`     // keep going until the agent has checked its work
	DoneWhen     string          `json:"doneWhen"` // checks that say the task is done, one per line
	MaxRounds    int             `json:"maxRounds"`
	Helpers      []string        `json:"helpers"` // named agents the loop asks to review the finished work
	identityName string          // the named agent this run is for
	inPlaceDir   string          // a folder with no git: the task works in it directly
	helperNames  []string        // named agents the loop asks for a review
	groups       map[string]bool // tool groups of a named agent; nil means the global switches
	appendSystem string          // identity text added to the system prompt
	resumeID     string          // continue this Claude session instead of starting a new one
	resumeDir    string          // ...in this folder, run in place (no worktree)
	fork         bool            // start a new session from resumeID; the original stays as it is
	group        string          // fan-out options share a group
	option       string          // "A", "B", ...
}

type uiStage struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Detail string   `json:"detail,omitempty"`
	Log    []string `json:"log,omitempty"` // searches and page reads, as they happen
	Output string   `json:"-"`             // findings; read through /api/scout
}

type uiAgentView struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Repo     string         `json:"repo"`
	Branch   string         `json:"branch"`
	Worktree string         `json:"worktree"`
	Token    int            `json:"token"` // 1-based index; the value is never exposed
	State    string         `json:"state"`
	Created  time.Time      `json:"created"`
	Stages   []uiStage      `json:"stages"`
	Events   int            `json:"events"`
	Files    []string       `json:"files"`   // project-relative files this agent has edited
	MCP      []uiMCPServer  `json:"mcp"`     // MCP servers as the agent saw them at start
	Last     string         `json:"last"`    // one line describing the latest activity
	Updated  time.Time      `json:"updated"` // when the latest activity happened
	Harness  *uiHarnessView `json:"harness,omitempty"`
	Usage    *uiUsageView   `json:"usage,omitempty"`
	Context  *uiContextView `json:"context,omitempty"` // how full the model's context is
	Cost     float64        `json:"cost,omitempty"`    // list price of the session so far, in dollars
	Limits   *uiLimits      `json:"limits,omitempty"`  // the account's five-hour and seven-day limits
	Run      *uiRunInfo     `json:"run,omitempty"`     // how the harness underneath was set up for this task
	Group    string         `json:"group,omitempty"`   // fan-out: options of one task share it
	Option   string         `json:"option,omitempty"`  // fan-out: "A", "B", ...
}

type uiManager struct {
	cfg       uiConfig
	repos     []string
	mcp       *uiMCP
	scout     uiScoutRuns
	repoMu    sync.RWMutex
	repoInfos []uiRepoInfo

	alerted map[string]struct{} // file|idA|idB pairs already warned about

	evalRuns  []*uiEvalRun
	infoMu    sync.Mutex
	infoCache map[string]struct {
		at   time.Time
		info uiHarnessInfo
	}
	lastMCP    map[string]string
	syncMu     sync.Mutex
	initNames  *uiInitNames
	idxWritten map[string]string // session documents as last written, so only changes are saved
	syncAt     time.Time
	snapMu     sync.Mutex
	snapCache  map[string]struct {
		at   time.Time
		snap uiSnapshot
	}
	limitMu       sync.Mutex
	accountLimits map[int]uiLimits
	identities    []*uiIdentity
	coachMu       sync.Mutex
	idxMu         sync.Mutex
	index         *uiIndexFile
	analyzedAt    []time.Time

	policyMu     sync.Mutex
	policy       uiPolicy
	policyLoaded bool

	qmu   sync.Mutex
	queue []*uiQueued

	mu     sync.Mutex
	agents map[string]*uiAgent
	order  []string
}

func newUIManager(cfg uiConfig) *uiManager {
	if cfg.MaxAgents <= 0 {
		cfg.MaxAgents = 4
	}
	if cfg.Tokens == nil {
		cfg.Tokens = &uiTokenPool{}
	}
	if cfg.Advisor == nil {
		cfg.Advisor = runAdvisorAgent
	}
	m := &uiManager{cfg: cfg, agents: map[string]*uiAgent{}, mcp: newUIMCP(), alerted: map[string]struct{}{}}
	for _, r := range cfg.Repos {
		_, _ = m.addRepo(r) // folders that are not git repositories are skipped
	}
	return m
}

func canonicalDir(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// addRepo allows agents to run in a git repository. The folder must be an
// absolute path to a directory the current user owns, inside a git repository;
// the repository root is what gets added.
func (m *uiManager) addRepo(path string) (uiRepoInfo, error) { return m.addRepoInit(path, false) }

// addRepoInit allows a folder for agents. With initGit a folder that is not a
// git repository becomes one, with an empty first commit, so agents can get
// worktrees. It refuses the home folder and the filesystem root.
func (m *uiManager) addRepoInit(path string, initGit bool) (uiRepoInfo, error) {
	path = uiExpandPath(path)
	if !filepath.IsAbs(path) {
		return uiRepoInfo{}, errors.New("give the full path of the folder")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return uiRepoInfo{}, errors.New("that folder does not exist")
	}
	// On a shared machine the folder may belong to a teammate: what matters is
	// whether this user can write in it, which the operating system decides.
	if !uiWritable(path) {
		return uiRepoInfo{}, errors.New("you do not have write access to that folder")
	}
	root, err := uiRepoRoot(path)
	if err != nil && initGit {
		if err = uiInitRepo(path); err == nil {
			root, err = uiRepoRoot(path)
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "dubious ownership") {
			return uiRepoInfo{}, errors.New("git does not trust this folder because another user owns it; ask the owner to run: git config --global --add safe.directory " + path)
		}
		return uiRepoInfo{}, err
	}
	m.repoMu.Lock()
	defer m.repoMu.Unlock()
	for _, r := range m.repoInfos {
		if r.Path == root {
			return r, nil
		}
	}
	ri := uiRepoInfo{Path: root, Name: filepath.Base(root), Remote: uiRepoRemote(root)}
	m.repos = append(m.repos, root)
	m.repoInfos = append(m.repoInfos, ri)
	return ri, nil
}

func (m *uiManager) repoList() []uiRepoInfo {
	m.repoMu.RLock()
	defer m.repoMu.RUnlock()
	return append([]uiRepoInfo{}, m.repoInfos...)
}

func (m *uiManager) resolveRepo(req string) (string, error) {
	m.repoMu.RLock()
	defer m.repoMu.RUnlock()
	if len(m.repos) == 0 {
		return "", errors.New("no folder has been added yet; add a git repository folder first")
	}
	if req == "" {
		return m.repos[0], nil
	}
	real, err := canonicalDir(req)
	if err != nil {
		return "", errors.New("repo is not an allowed repository")
	}
	for _, r := range m.repos {
		if r == real {
			return r, nil
		}
	}
	return "", errors.New("repo is not an allowed repository")
}

func uiNewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func uiSlug(s string) string {
	s = strings.Trim(uiSlugChars.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 24 {
		s = strings.Trim(s[:24], "-")
	}
	if s == "" {
		s = "task"
	}
	return s
}

func (m *uiManager) liveCount() int {
	n := 0
	for _, a := range m.agents {
		switch a.snapshot().State {
		case uiPreparing, uiWorking, uiWaiting:
			n++
		}
	}
	return n
}

// start validates the request and launches the agent pipeline in the
// background; progress is visible through the agent's state and events.
func (m *uiManager) start(req uiStartRequest) (*uiAgent, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return nil, errors.New("prompt is required")
	}
	if len([]rune(req.Prompt)) > uiMaxPromptRunes {
		return nil, errors.New("prompt is too long")
	}
	if req.Mode != "" && req.Mode != "edit" && req.Mode != "plan" {
		return nil, errors.New(`mode must be "edit" or "plan"`)
	}
	if req.Model != "" && !uiModelName.MatchString(req.Model) {
		return nil, errors.New("invalid model name")
	}
	var repo string
	if req.inPlaceDir != "" { // an agent's own folder: no git, no worktree
		repo = req.inPlaceDir
	} else {
		var err error
		repo, err = m.resolveRepo(req.Repo)
		if err != nil {
			return nil, err
		}
		if out, err := exec.Command("git", "-C", repo, "rev-parse", "--git-dir").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("repo is not a git repository: %s", strings.TrimSpace(string(out)))
		}
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = req.Prompt
	}
	if r := []rune(title); len(r) > 60 {
		title = string(r[:60]) + "…"
	}
	if req.Loop && len(req.Helpers) > 0 {
		names, err := m.resolveHelpers(req.Helpers)
		if err != nil {
			return nil, err
		}
		req.helperNames = names
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.liveCount() >= m.cfg.MaxAgents {
		return nil, fmt.Errorf("already running %d agents; stop one first", m.cfg.MaxAgents)
	}
	id := uiNewID()
	branch := "cully/" + uiSlug(req.Title+" "+boolText(req.Title == "", req.Prompt)) + "-" + id
	worktree := filepath.Join(ConfigDir(), "ui-worktrees", id)
	if req.resumeDir != "" { // a resumed session runs where it started, on the branch that is checked out
		worktree = req.resumeDir
		branch = uiCurrentBranch(req.resumeDir)
	}
	if req.inPlaceDir != "" {
		worktree, branch = req.inPlaceDir, "no git"
	}
	a := &uiAgent{
		id: id, title: title, repo: repo, req: req, created: time.Now().UTC(),
		branch:   branch,
		worktree: worktree,
		tokens:   m.cfg.Tokens, mgr: m, state: uiPreparing,
		notify: make(chan struct{}), uses: map[string]uiToolUse{}, files: map[string]struct{}{}, done: make(chan struct{}),
	}
	if req.Loop {
		a.harness = newUIHarness(req)
	}
	m.agents[id] = a
	m.order = append(m.order, id)
	_ = m.mcp.markInUse("claude") // UI-spawned agents run Claude
	go a.run(m)
	return a, nil
}

func boolText(use bool, s string) string {
	if use {
		return s
	}
	return ""
}

func (m *uiManager) get(id string) (*uiAgent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	return a, ok
}

func (m *uiManager) list() []uiAgentView {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	out := make([]uiAgentView, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- { // newest first
		if a, ok := m.get(ids[i]); ok {
			out = append(out, a.snapshot())
		}
	}
	return out
}

func (m *uiManager) stopAll() {
	m.mcp.stopAll()
	m.mu.Lock()
	var all []*uiAgent
	for _, a := range m.agents {
		all = append(all, a)
	}
	m.mu.Unlock()
	for _, a := range all {
		a.stop()
	}
	for _, a := range all {
		select {
		case <-a.done:
		case <-time.After(5 * time.Second):
		}
	}
}

type uiAgent struct {
	id, title, repo, branch, worktree string
	req                               uiStartRequest
	created                           time.Time
	tokens                            *uiTokenPool
	done                              chan struct{}

	mu      sync.Mutex
	state   string
	token   int
	stages  []uiStage
	events  []uiEvent
	nextSeq int
	notify  chan struct{}
	cmd     *exec.Cmd
	stdin   interface {
		Write([]byte) (int, error)
		Close() error
	}
	stopped       bool
	pending       int // turns sent and not yet answered by a result
	harness       *uiHarness
	runInfo       *uiRunInfo
	model         string  // reported by the agent when it starts
	window        int     // the model's context window, as reported
	compactions   int     // times the harness compacted this task's history
	cost          float64 // what the session has cost at list price, as reported
	limits        *uiLimits
	ctx           int // tokens in its context after its latest call
	usage         uiTokens
	turns         int // turns that reported usage
	usageRecorded bool
	mcpFiltered   bool // the task loaded only chosen MCP servers
	uses          map[string]uiToolUse
	files         map[string]struct{}
	base          string // commit the worktree started from
	mgr           *uiManager

	advice       []uiAdvice
	adviceStatus string
	adviceError  string
	adviceAsked  time.Time
	last         string
	updated      time.Time
	mcp          []uiMCPServer
}

func (a *uiAgent) snapshot() uiAgentView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return uiAgentView{
		ID: a.id, Title: a.title, Repo: a.repo, Branch: a.branch, Worktree: a.worktree,
		Token: a.token, State: a.state, Created: a.created,
		Stages: a.stagesCopyLocked(), Events: len(a.events), Files: a.sortedFilesLocked(), Last: a.last, Updated: a.updated, Group: a.req.group, Option: a.req.option, Harness: a.harnessView(), Usage: usageOf(a.usage), Run: a.runInfo, Context: contextOf(a.model, a.ctx, a.window, a.compactions), Cost: a.cost, Limits: a.limitsView(), MCP: append([]uiMCPServer(nil), a.mcp...),
	}
}

func (a *uiAgent) add(ev uiEvent) {
	ev.Text = a.tokens.redact(ev.Text)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextSeq++
	ev.Seq, ev.Time = a.nextSeq, time.Now().UTC()
	a.events = append(a.events, ev)
	if line := uiLastLine(ev); line != "" {
		a.last, a.updated = line, ev.Time
	}
	if len(a.events) > uiEventCap {
		a.events = append([]uiEvent(nil), a.events[len(a.events)-uiEventCap:]...)
	}
	close(a.notify)
	a.notify = make(chan struct{})
}

// eventsSince returns events after seq and a channel closed by the next append.
func (a *uiAgent) eventsSince(seq int) ([]uiEvent, <-chan struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []uiEvent
	for _, e := range a.events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, a.notify
}

func (a *uiAgent) allEvents() []uiEvent {
	evs, _ := a.eventsSince(0)
	return evs
}

func (a *uiAgent) setState(s string) {
	a.mu.Lock()
	a.state = s
	a.mu.Unlock()
	if a.mgr != nil && (s == uiDone || s == uiFailed || s == uiStopped) {
		go a.mgr.drainQueue()
		a.mgr.recordUsage(a)
	}
}

func (a *uiAgent) fail(msg string) {
	a.add(uiEvent{Kind: "error", Text: msg})
	a.setState(uiFailed)
}

func (a *uiAgent) journal(ev journalEvent) {
	appendJournal(a.worktree, "ui-"+a.id, ev)
}

func (a *uiAgent) addWorktree() error {
	if err := os.MkdirAll(filepath.Dir(a.worktree), 0o700); err != nil {
		return err
	}
	out, err := exec.Command("git", "-C", a.repo, "worktree", "add", "-q", "-b", a.branch, a.worktree).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	a.recordBase()
	return nil
}

// recordBase remembers the commit the agent started from, so its changes can be shown against it.
func (a *uiAgent) recordBase() {
	if head, err := exec.Command("git", "-C", a.worktree, "rev-parse", "HEAD").Output(); err == nil {
		a.mu.Lock()
		a.base = strings.TrimSpace(string(head))
		a.mu.Unlock()
	}
}

func uiCurrentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "current branch"
	}
	return strings.TrimSpace(string(out))
}

func uiAgentEnv(token string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "ANTHROPIC_API_KEY", k == "ANTHROPIC_AUTH_TOKEN", k == "CLAUDE_CODE_OAUTH_TOKEN",
			strings.HasPrefix(k, "CULLY_PANE"), k == "CULLY_SESSION", k == "CULLY_AGENT", k == "MODEL_HINT_GUARD":
			continue
		}
		env = append(env, kv)
	}
	if token != "" {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+token)
	}
	return env
}

type uiTail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *uiTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > uiStderrTail {
		t.buf = t.buf[len(t.buf)-uiStderrTail:]
	}
	return len(p), nil
}

func (t *uiTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

func (a *uiAgent) isStopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopped
}

func (a *uiAgent) run(m *uiManager) {
	defer close(a.done)
	var prompt string
	if a.req.resumeID != "" || a.req.inPlaceDir != "" {
		prompt = a.req.Prompt
		for _, st := range []string{"context", "recall", "scout"} {
			a.setStage(st, "skipped", "")
		}
		a.recordBase()
	} else {
		prompt = a.prepare(m)
		if err := a.addWorktree(); err != nil {
			a.fail("could not create worktree: " + err.Error())
			return
		}
	}
	if a.isStopped() {
		a.setState(uiStopped)
		return
	}
	bin := m.cfg.ClaudeBin
	if bin == "" {
		bin = claudeExecutable()
	}
	idx, tok := m.cfg.Tokens.take()
	permission := "acceptEdits"
	if a.req.Mode == "plan" {
		permission = "plan"
	}
	allowed, denied := m.toolPolicyFor(a.req.Mode == "plan", a.req.groups)
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--permission-mode", permission, "--allowedTools", allowed,
		"--disallowedTools", denied}
	mcpArgs := m.mcpFilterArgs()
	args = append(args, mcpArgs...)
	if len(mcpArgs) > 0 {
		a.mu.Lock()
		a.mcpFiltered = true
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.runInfo = m.describeRun(a, permission, allowed, denied, mcpArgs)
	a.mu.Unlock()
	if a.req.appendSystem != "" {
		args = append(args, "--append-system-prompt", a.req.appendSystem)
	}
	if a.req.resumeID != "" {
		args = append(args, "--resume", a.req.resumeID)
		if a.req.fork {
			args = append(args, "--fork-session")
		}
	}
	if a.req.Model != "" {
		args = append(args, "--model", a.req.Model)
	}
	for _, dir := range a.req.addDirs {
		args = append(args, "--add-dir", dir)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = a.worktree
	cmd.Env = uiAgentEnv(tok)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		a.fail("stdin: " + err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.fail("stdout: " + err.Error())
		return
	}
	stderr := &uiTail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		a.fail("cannot start claude: " + err.Error())
		return
	}
	a.mu.Lock()
	a.cmd, a.stdin, a.token = cmd, stdin, idx
	stopped := a.stopped
	a.mu.Unlock()
	if stopped {
		a.kill()
	}
	a.journal(journalEvent{Agent: "claude", Class: journalStart})
	a.add(uiEvent{Kind: "user", Text: a.req.Prompt})
	if err := a.write(prompt); err != nil {
		a.add(uiEvent{Kind: "error", Text: "could not send the task: " + err.Error()})
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		a.handleLine(sc.Bytes())
	}
	waitErr := cmd.Wait()
	a.journal(journalEvent{Agent: "claude", Class: journalEnd})

	a.mu.Lock()
	stopped = a.stopped
	a.mu.Unlock()
	switch {
	case stopped:
		a.setState(uiStopped)
	case waitErr == nil:
		a.setState(uiDone)
	default:
		msg := "claude exited: " + waitErr.Error()
		if tail := stderr.String(); tail != "" {
			msg += "\n" + tail
		}
		a.fail(msg)
	}
}

func (a *uiAgent) handleLine(line []byte) {
	p := parseStreamLine(line)
	a.mu.Lock()
	for id, u := range p.Uses {
		a.uses[id] = u
	}
	uses := p.Uses
	if len(p.MCP) > 0 {
		a.mcp = p.MCP
		if a.mgr != nil {
			a.mgr.rememberMCP(p.MCP)
		}
	}
	a.mu.Unlock()
	for id, u := range uses {
		a.harnessUse(id, u)
	}
	for _, ev := range p.Events {
		if ev.Kind == "tool" || ev.Kind == "tool_result" {
			// Show paths as the agent's own project sees them.
			ev.Text = strings.ReplaceAll(ev.Text, a.worktree+string(os.PathSeparator), "")
		}
		a.add(ev)
	}
	for _, r := range p.Results {
		a.mu.Lock()
		use, ok := a.uses[r.ID]
		a.mu.Unlock()
		if ok {
			a.journalToolResult(use, r.Failed)
		}
		a.harnessResult(r.ID, r.Failed)
	}
	if len(p.InitTools) > 0 && a.mgr != nil {
		a.mgr.rememberInit(p.InitTools, p.InitCommands)
	}
	if p.Compacted {
		a.mu.Lock()
		a.compactions++
		a.mu.Unlock()
	}
	if p.Window > 0 || p.Cost > 0 || p.Limits != nil {
		a.mu.Lock()
		if p.Window > 0 {
			a.window = p.Window
		}
		if p.Cost > 0 {
			a.cost = p.Cost
		}
		if p.Limits != nil {
			p.Limits.Seen = time.Now().Unix()
			a.limits = p.Limits
			if a.mgr != nil && a.token > 0 {
				a.mgr.setLimits(a.token, *p.Limits)
			}
		}
		a.mu.Unlock()
	}
	if p.Model != "" || p.Context > 0 {
		a.mu.Lock()
		if p.Model != "" {
			a.model = p.Model
		}
		if p.Context > 0 {
			a.ctx = p.Context
		}
		a.mu.Unlock()
	}
	if p.Usage != nil {
		a.mu.Lock()
		a.usage = a.usage.add(*p.Usage)
		a.turns++
		a.mu.Unlock()
	}
	if p.TurnDone {
		a.mu.Lock()
		if a.pending > 0 {
			a.pending--
		}
		turnOver := a.pending == 0
		if a.pending == 0 && (a.state == uiWorking || a.state == uiPreparing) {
			a.state = uiWaiting
		}
		a.mu.Unlock()
		if turnOver && a.harness != nil {
			go a.harnessAfterTurn()
		}
	}
}

func (a *uiAgent) journalToolResult(use uiToolUse, failed bool) {
	class := journalClassFor(use.Name, use.Command)
	ev := journalEvent{Agent: "claude", Class: class, Failed: failed, Tool: use.Name}
	if class == journalCheck || use.Name == "Bash" {
		ev.Sig = use.Sig
	}
	rel := ""
	if use.Path != "" {
		rel = relProjectPath(a.worktree, use.Path)
	}
	if class == journalEdit && !failed && rel != "" {
		a.mu.Lock()
		_, seen := a.files[rel]
		a.files[rel] = struct{}{} // board only; held in memory
		a.mu.Unlock()
		if !seen && a.mgr != nil && !a.mgr.cfg.NoConflictAlerts {
			go a.mgr.conflictAlerts(a, rel)
		}
	}
	if journalPathsEnabled() && rel != "" {
		ev.Path = rel
		switch use.Name {
		case "Read":
			ev.Op = opRead
		case "Write":
			ev.Op = opWrite
		case "Edit", "MultiEdit":
			ev.Op = opEdit
		}
	}
	a.journal(ev)
}

func (a *uiAgent) write(text string) error {
	line, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	stdin := a.stdin
	if stdin != nil {
		a.pending++
		if a.state == uiPreparing || a.state == uiWaiting {
			a.state = uiWorking
		}
	}
	a.mu.Unlock()
	if stdin == nil {
		return errors.New("agent is not running")
	}
	_, err = stdin.Write(append(line, '\n'))
	return err
}

// send delivers a message to a running agent.
func (a *uiAgent) send(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("message is empty")
	}
	if len([]rune(text)) > uiMaxPromptRunes {
		return errors.New("message is too long")
	}
	switch a.snapshot().State {
	case uiDone, uiFailed, uiStopped:
		return errors.New("agent has finished")
	}
	if err := a.write(text); err != nil {
		return err
	}
	a.add(uiEvent{Kind: "user", Text: text})
	return nil
}

func (a *uiAgent) kill() {
	a.mu.Lock()
	cmd := a.cmd
	a.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(2*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
}

// stop ends the agent and everything it started.
func (a *uiAgent) stop() {
	a.mu.Lock()
	a.stopped = true
	stdin := a.stdin
	a.mu.Unlock()
	a.kill()
	if stdin != nil {
		_ = stdin.Close()
	}
}

const uiLastMax = 90

// uiLastLine summarises an event for the agent list. Stage and system events
// are bookkeeping and do not count as activity.
func uiLastLine(ev uiEvent) string {
	var line string
	switch ev.Kind {
	case "text":
		line = ev.Text
	case "tool":
		line = strings.TrimSpace(ev.Tool + " " + ev.Text)
	case "user":
		line = "you: " + ev.Text
	case "error":
		line = "error: " + ev.Text
	case "result":
		line = "turn done"
	default:
		return ""
	}
	line = strings.Join(strings.Fields(line), " ")
	if r := []rune(line); len(r) > uiLastMax {
		line = string(r[:uiLastMax]) + "…"
	}
	return line
}

type uiRepoInfo struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Remote string `json:"remote,omitempty"` // host/owner/name, never with credentials
	Branch string `json:"branch,omitempty"` // the branch checked out now
}

func uiRepoRemote(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", path, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return uiCleanRemote(string(out))
}

// uiCleanRemote turns a git remote into host/owner/name for display, dropping
// the scheme, any user:password, the port and a trailing .git.
func uiCleanRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		colon := strings.Index(raw, ":")
		if colon <= 0 || strings.ContainsAny(raw, " \t") {
			return ""
		}
		host := raw[:colon]
		if at := strings.Index(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		path := strings.TrimSuffix(strings.Trim(raw[colon+1:], "/"), ".git")
		if host == "" || path == "" {
			return ""
		}
		return host + "/" + path
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname() + "/" + strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
}

func uiInitRepo(path string) error {
	real, err := canonicalDir(path)
	if err != nil {
		return errors.New("that folder does not exist")
	}
	if home, _ := os.UserHomeDir(); real == "/" || (home != "" && real == filepath.Clean(home)) {
		return errors.New("choose a project folder, not your home folder or the filesystem root")
	}
	if !uiWritable(real) {
		return errors.New("you cannot write in that folder")
	}
	if out, err := exec.Command("git", "-C", real, "init", "-q").CombinedOutput(); err != nil {
		return fmt.Errorf("git init failed: %s", strings.TrimSpace(string(out)))
	}
	cmd := exec.Command("git", "-C", real, "commit", "-q", "--allow-empty", "-m", "Start with Cully")
	if out, err := cmd.CombinedOutput(); err != nil {
		retry := exec.Command("git", "-C", real, "-c", "user.name=Cully", "-c", "user.email=cully@localhost", "commit", "-q", "--allow-empty", "-m", "Start with Cully")
		if out2, err2 := retry.CombinedOutput(); err2 != nil {
			return fmt.Errorf("could not make the first commit: %s %s", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return nil
}

type uiUsageView struct {
	Tokens      int `json:"tokens"` // everything the task handled
	Fresh       int `json:"fresh"`  // input read at full price
	Cached      int `json:"cached"` // input served from the prompt cache
	Output      int `json:"output"`
	CachedShare int `json:"cachedShare"` // percent of input that came from the cache
	// Saved is the cost the cache saved, in tokens at full price: a cache read costs about a tenth of fresh input.
	Saved int `json:"saved"`
}

// usageOf turns a task's token counts into what the page shows. The caller holds the agent's lock.
func usageOf(u uiTokens) *uiUsageView {
	if u.Total() == 0 {
		return nil
	}
	return &uiUsageView{Tokens: u.Total(), Fresh: u.Input + u.CacheWrite, Cached: u.CacheRead, Output: u.Output, CachedShare: u.CachedShare(), Saved: u.CacheRead * 9 / 10}
}

// uiContextView is how full the model's context window is. The window size is
// worked out from the model's name, so it is shown as an estimate.
type uiContextView struct {
	Model   string `json:"model"`
	Used    int    `json:"used"`
	Window  int    `json:"window"`
	Left    int    `json:"left"`
	Percent int    `json:"percent"`
	Near    bool   `json:"near"` // the window is nearly full; the harness is about to compact it
	// Compactions is how many times the harness has summarised this task's history to make room.
	Compactions int `json:"compactions"`
}

func uiContextWindow(model string, reported int) int {
	if reported > 0 {
		return reported
	}
	m := strings.ToLower(model)
	if strings.Contains(m, "1m") {
		return 1_000_000
	}
	return 200_000
}

func contextOf(model string, used, reported, compactions int) *uiContextView {
	if used <= 0 {
		return nil
	}
	w := uiContextWindow(model, reported)
	left := w - used
	if left < 0 {
		left = 0
	}
	p := used * 100 / w
	if p > 100 {
		p = 100
	}
	return &uiContextView{Model: model, Used: used, Window: w, Left: left, Percent: p, Near: p >= uiNearFull, Compactions: compactions}
}

// limitsView is the latest limits heard on this task, or on any task that used the same account.
// The caller holds the agent's lock.
func (a *uiAgent) limitsView() *uiLimits {
	if a.limits != nil {
		l := *a.limits
		return &l
	}
	if a.mgr != nil && a.token > 0 {
		if l, ok := a.mgr.limitsFor(a.token); ok {
			return &l
		}
	}
	return nil
}

func (m *uiManager) setLimits(token int, l uiLimits) {
	m.limitMu.Lock()
	defer m.limitMu.Unlock()
	if m.accountLimits == nil {
		m.accountLimits = map[int]uiLimits{}
	}
	m.accountLimits[token] = l
}

func (m *uiManager) limitsFor(token int) (uiLimits, bool) {
	m.limitMu.Lock()
	defer m.limitMu.Unlock()
	l, ok := m.accountLimits[token]
	return l, ok
}

const uiHelpersMax = 2

// resolveHelpers checks that every helper is one of the user's approved agents.
func (m *uiManager) resolveHelpers(ids []string) ([]string, error) {
	if len(ids) > uiHelpersMax {
		return nil, errors.New("ask at most two agents to review a task")
	}
	have := map[string]uiIdentity{}
	for _, x := range m.identityList() {
		have[x.ID] = x
	}
	var names []string
	for _, id := range ids {
		x, ok := have[id]
		if !ok || !x.Approved {
			return nil, errors.New("a reviewer is not one of your approved agents")
		}
		names = append(names, x.Name)
	}
	return names, nil
}

// uiNearFull is where a context window counts as nearly full. Claude Code
// compacts its own history before the window fills, so this is only the point
// where that is about to happen, not a reason to start over.
const uiNearFull = 92

// uiCompactedOften is how many compactions make a task worth restarting from a handoff:
// each one loses detail from early on.
const uiCompactedOften = 3
