package cully

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// MCP sign-in for the three agents Cully wraps. Status reuses the same probes
// as the terminal harness (agentMCPProbe), so the browser and `cully status`
// agree. Sign-in runs the agent's own login command in a pseudo-terminal,
// captures the authorization URL it prints, and, for agents that support it,
// feeds a pasted redirect URL back. That paste step is what makes sign-in work
// when the browser is on a different machine from the one running the agent:
// the redirect points at localhost on the browser's machine, so it cannot
// reach the agent by itself.

var uiMCPAgents = []string{"claude", "codex", "cursor"}

const (
	uiMCPCacheTTL    = 30 * time.Second
	uiMCPProbeBudget = 20 * time.Second
	uiLoginBudget    = 10 * time.Minute
	uiPasteMax       = 4096
)

var uiURLRe = regexp.MustCompile(`https?://[^\s]+`)

type uiMCPAgent struct {
	Agent    string `json:"agent"`
	State    string `json:"state"` // connected, needs-auth, unhealthy, not-configured, not-installed, unknown
	CanPaste bool   `json:"canPaste"`
	// InUse marks the agents you actually launch Cully with; the UI focuses on
	// those and tucks the others away. With none registered, Claude is in use,
	// because the agents the UI spawns run Claude.
	InUse bool         `json:"inUse"`
	Login *uiLoginView `json:"login,omitempty"`
}

type uiLoginView struct {
	Agent    string    `json:"agent"`
	State    string    `json:"state"` // idle, starting, awaiting, done, failed, cancelled
	URL      string    `json:"url,omitempty"`
	CanPaste bool      `json:"canPaste"`
	Message  string    `json:"message,omitempty"`
	Started  time.Time `json:"started"`
}

// uiAgentInstalled and uiLoginCommand are variables so tests need no real CLI.
var uiAgentInstalled = func(agent string) bool {
	name := map[string]string{"claude": claudeExecutable(), "codex": "codex", "cursor": "cursor-agent"}[agent]
	_, err := exec.LookPath(name)
	return err == nil
}

// uiLoginCommand returns the agent's login command and whether it can take a
// pasted redirect URL. Claude and Codex have --no-browser; Cursor's login has
// no such option, so it can only finish where its own browser callback lands.
var uiLoginCommand = func(agent string) (string, []string, bool, error) {
	switch agent {
	case "claude":
		return claudeExecutable(), []string{"mcp", "login", "cully", "--no-browser"}, true, nil
	case "codex":
		return "codex", []string{"mcp", "login", "cully", "--no-browser"}, true, nil
	case "cursor":
		return "cursor-agent", []string{"mcp", "login", "cully"}, false, nil
	}
	return "", nil, false, fmt.Errorf("unknown agent %q", agent)
}

type uiMCP struct {
	mu     sync.Mutex
	cache  map[string]uiMCPCached
	logins map[string]*uiLogin
	inUse  map[string]struct{}
}

type uiMCPCached struct {
	state string
	at    time.Time
}

func newUIMCP() *uiMCP {
	return &uiMCP{cache: map[string]uiMCPCached{}, logins: map[string]*uiLogin{}, inUse: map[string]struct{}{}}
}

func uiKnownAgent(agent string) bool {
	for _, a := range uiMCPAgents {
		if a == agent {
			return true
		}
	}
	return false
}

func uiMCPState(health, auth string, found bool) string {
	switch {
	case !found:
		return "not-configured"
	case auth == "unauthenticated":
		return "needs-auth"
	case health == "healthy":
		return "connected"
	case health == "unhealthy":
		return "unhealthy"
	}
	return "unknown"
}

// status reports each wrapped agent's Cully MCP connection, probing in
// parallel and caching briefly because the CLIs health-check every server.
func (m *uiMCP) status(ctx context.Context) []uiMCPAgent {
	m.mu.Lock()
	used := len(m.inUse) > 0
	inUse := map[string]bool{}
	for a := range m.inUse {
		inUse[a] = true
	}
	m.mu.Unlock()
	out := make([]uiMCPAgent, len(uiMCPAgents))
	var wg sync.WaitGroup
	for i, agent := range uiMCPAgents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = uiMCPAgent{Agent: agent, State: m.agentState(ctx, agent), InUse: inUse[agent] || (!used && agent == "claude")}
			if _, _, canPaste, err := uiLoginCommand(agent); err == nil {
				out[i].CanPaste = canPaste
			}
			if v, ok := m.loginView(agent); ok {
				out[i].Login = &v
			}
		}()
	}
	wg.Wait()
	return out
}

// markInUse records that an agent is one you launch Cully with.
func (m *uiMCP) markInUse(agent string) error {
	if !uiKnownAgent(agent) {
		return fmt.Errorf("unknown agent %q", agent)
	}
	m.mu.Lock()
	m.inUse[agent] = struct{}{}
	m.mu.Unlock()
	return nil
}

func (m *uiMCP) agentState(ctx context.Context, agent string) string {
	if !uiAgentInstalled(agent) {
		return "not-installed"
	}
	m.mu.Lock()
	c, ok := m.cache[agent]
	m.mu.Unlock()
	if ok && time.Since(c.at) < uiMCPCacheTTL {
		return c.state
	}
	probeCtx, cancel := context.WithTimeout(ctx, uiMCPProbeBudget)
	defer cancel()
	state := uiMCPState(agentMCPProbe(probeCtx, agent))
	m.mu.Lock()
	m.cache[agent] = uiMCPCached{state, time.Now()}
	m.mu.Unlock()
	return state
}

type uiLogin struct {
	agent    string
	canPaste bool
	started  time.Time

	mu        sync.Mutex
	state     string
	url       string
	message   string
	cmd       *exec.Cmd
	ptmx      *os.File
	cancelled bool
}

func (l *uiLogin) view() uiLoginView {
	l.mu.Lock()
	defer l.mu.Unlock()
	return uiLoginView{Agent: l.agent, State: l.state, URL: l.url, CanPaste: l.canPaste, Message: l.message, Started: l.started}
}

func (m *uiMCP) loginView(agent string) (uiLoginView, bool) {
	m.mu.Lock()
	l, ok := m.logins[agent]
	m.mu.Unlock()
	if !ok {
		return uiLoginView{Agent: agent, State: "idle"}, false
	}
	return l.view(), true
}

// startLogin runs the agent's login command. A login already in progress is
// returned as is, so a double click never starts two OAuth flows.
func (m *uiMCP) startLogin(agent string) (uiLoginView, error) {
	if !uiKnownAgent(agent) {
		return uiLoginView{}, fmt.Errorf("unknown agent %q", agent)
	}
	m.mu.Lock()
	if cur, ok := m.logins[agent]; ok {
		if v := cur.view(); v.State == "starting" || v.State == "awaiting" {
			m.mu.Unlock()
			return v, nil
		}
	}
	m.mu.Unlock()
	name, args, canPaste, err := uiLoginCommand(agent)
	if err != nil {
		return uiLoginView{}, err
	}
	cmd := exec.Command(name, args...)
	cmd.Env = uiLoginEnv()
	// Wide enough that the terminal never wraps the long authorization URL.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 10000})
	if err != nil {
		return uiLoginView{}, fmt.Errorf("cannot start %s login: %w", agent, err)
	}
	l := &uiLogin{agent: agent, canPaste: canPaste, started: time.Now().UTC(), state: "starting", cmd: cmd, ptmx: ptmx}
	m.mu.Lock()
	m.logins[agent] = l
	m.mu.Unlock()
	go m.watchLogin(l)
	time.AfterFunc(uiLoginBudget, func() { l.kill(false) })
	return l.view(), nil
}

func uiLoginEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "CULLY_PANE_SESSION", "CULLY_SESSION", "CULLY_AGENT", "MODEL_HINT_GUARD", "BROWSER", "TERM":
			continue
		}
		env = append(env, kv)
	}
	// BROWSER=true stops a login without --no-browser from trying to open a
	// browser on a machine that has none; the printed URL is what we use.
	return append(env, "TERM=xterm", "BROWSER=true", "MODEL_HINT_GUARD=1")
}

func (l *uiLogin) kill(userCancel bool) {
	l.mu.Lock()
	if l.state == "done" || l.state == "failed" || l.state == "cancelled" {
		l.mu.Unlock()
		return
	}
	l.cancelled = true
	cmd := l.cmd
	l.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(2*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
}

func (m *uiMCP) watchLogin(l *uiLogin) {
	buf := make([]byte, 4096)
	var acc strings.Builder
	for {
		n, err := l.ptmx.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			l.scan(acc.String())
		}
		if err != nil {
			break
		}
	}
	waitErr := l.cmd.Wait()
	_ = l.ptmx.Close()
	tail := uiLoginTail(acc.String())
	l.mu.Lock()
	switch {
	case l.cancelled:
		l.state, l.message = "cancelled", "Sign-in cancelled."
	case waitErr == nil:
		l.state, l.message = "done", "Signed in."
	default:
		l.state, l.message = "failed", tail
	}
	l.mu.Unlock()
	m.mu.Lock()
	delete(m.cache, l.agent) // re-probe now that the credentials changed
	m.mu.Unlock()
}

// scan picks the authorization URL out of the output as soon as its line is
// complete, and moves the login to "awaiting".
func (l *uiLogin) scan(raw string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.url != "" {
		return
	}
	plain := uiStripANSI(raw)
	loc := uiURLRe.FindStringIndex(plain)
	if loc == nil || loc[1] >= len(plain) || (plain[loc[1]] != '\n' && plain[loc[1]] != '\r') {
		return // the line is not complete yet
	}
	l.url = plain[loc[0]:loc[1]]
	l.state = "awaiting"
}

// uiLoginTail keeps the last few lines of output for an error message, with
// every URL (the authorization link and any echoed redirect) replaced.
func uiLoginTail(raw string) string {
	plain := uiURLRe.ReplaceAllString(uiStripANSI(raw), "[link]")
	var lines []string
	for _, ln := range strings.FieldsFunc(plain, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	out := strings.Join(lines, " · ")
	if r := []rune(out); len(r) > 300 {
		out = string(r[:300]) + "…"
	}
	return out
}

// validateRedirect accepts only what a browser shows after sign-in: a
// localhost URL carrying an authorization code or an error. It refuses control
// characters, so a paste can never add a second line of input to the CLI.
func validateRedirect(raw string) (string, error) {
	raw = strings.Trim(raw, " ")
	if raw == "" {
		return "", errors.New("paste the full address from the browser's address bar")
	}
	if len(raw) > uiPasteMax {
		return "", errors.New("that address is too long")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("the address contains control characters")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("that is not a web address")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return "", errors.New("expected the localhost address the browser was sent to after signing in")
	}
	q := u.Query()
	if q.Get("code") == "" && q.Get("error") == "" {
		return "", errors.New("that address has no authorization code; copy it after the sign-in page finishes")
	}
	return raw, nil
}

// paste hands a redirect URL to a login that asked for one.
func (m *uiMCP) paste(agent, raw string) error {
	if !uiKnownAgent(agent) {
		return fmt.Errorf("unknown agent %q", agent)
	}
	m.mu.Lock()
	l, ok := m.logins[agent]
	m.mu.Unlock()
	if !ok {
		return errors.New("no sign-in is running; start one first")
	}
	if !l.canPaste {
		return errors.New(agent + " sign-in cannot take a pasted address; finish it where that agent's browser is, or run the login command in a terminal there")
	}
	checked, err := validateRedirect(raw)
	if err != nil {
		return err
	}
	l.mu.Lock()
	state, ptmx := l.state, l.ptmx
	l.mu.Unlock()
	if state != "awaiting" {
		return errors.New("the sign-in is not waiting for an address")
	}
	_, err = ptmx.WriteString(checked + "\r")
	return err
}

func (m *uiMCP) cancel(agent string) {
	m.mu.Lock()
	l, ok := m.logins[agent]
	m.mu.Unlock()
	if ok {
		l.kill(true)
	}
}

func (m *uiMCP) stopAll() {
	m.mu.Lock()
	all := make([]*uiLogin, 0, len(m.logins))
	for _, l := range m.logins {
		all = append(all, l)
	}
	m.mu.Unlock()
	for _, l := range all {
		l.kill(false)
	}
}
