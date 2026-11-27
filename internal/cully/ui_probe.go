package cully

import (
	"context"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Capability probing. The matrix in ui_adapter.go says what each harness lets Cully do. The probe asks
// each installed program (--version and --help only, never a model, never the network) whether the
// flags and commands Cully relies on are still there. When a declared capability's flag is missing,
// the result says DRIFT for that harness, so a release that renames a flag is noticed before a task
// fails. The result is kept in the database with a time and refreshed at most once a day, or on demand.

const uiProbeEvery = 24 * time.Hour

// uiProbeNeed is one thing Cully relies on. Sub is the help command: nil for `--help`, or a
// subcommand such as exec for `exec --help`.
type uiProbeNeed struct {
	Token string   `json:"token"`
	Cap   string   `json:"cap"` // the capability that stops working without it
	For   string   `json:"for"`
	Sub   []string `json:"-"`
}

type uiProbeDrift struct {
	Flag string `json:"flag"`
	Cap  string `json:"cap"`
	For  string `json:"for"`
}

type uiProbeHarness struct {
	Agent     string         `json:"agent"`
	Installed bool           `json:"installed"`
	Version   string         `json:"version,omitempty"`
	Read      bool           `json:"read"` // the help text was read, so the comparison means something
	Drift     []uiProbeDrift `json:"drift"`
}

type uiProbeResult struct {
	At        int64            `json:"at"`
	Harnesses []uiProbeHarness `json:"harnesses"`
}

// uiClaudeCap says which matrix row each Claude Code flag belongs to.
var uiClaudeCap = map[string]string{
	"--input-format": "Run a task from Cully", "--output-format": "Run a task from Cully",
	"--allowedTools": "Run a task from Cully", "--disallowedTools": "Run a task from Cully",
	"--append-system-prompt": "Run a task from Cully", "--add-dir": "Run a task from Cully",
	"--permission-prompt-tool": "Ask you before a risky call", "--settings": "Ask you before a risky call",
	"--strict-mcp-config": "Choose which servers a task loads",
	"--resume":            "Resume a session", "--fork-session": "Fork a session",
}

// uiProbeNeeds is what Cully relies on in each harness.
func uiProbeNeeds(agent string) []uiProbeNeed {
	switch agent {
	case "claude":
		out := make([]uiProbeNeed, 0, len(uiClaudeNeeds))
		for _, n := range uiClaudeNeeds {
			out = append(out, uiProbeNeed{Token: n.Flag, Cap: uiClaudeCap[n.Flag], For: n.For})
		}
		return out
	case "codex":
		exec := []string{"exec"}
		return []uiProbeNeed{
			{Token: "exec", Cap: "Advisor and scout runs", For: "run a read-only question"},
			{Token: "resume", Cap: "Resume a session", For: "continue a session"},
			{Token: "fork", Cap: "Fork a session", For: "branch from a session"},
			{Token: "--sandbox", Cap: "Advisor and scout runs", For: "keep advisor runs read-only"},
			{Token: "--sandbox", Cap: "Advisor and scout runs", For: "keep advisor runs read-only", Sub: exec},
			{Token: "--ephemeral", Cap: "Advisor and scout runs", For: "leave no session behind", Sub: exec},
			{Token: "--skip-git-repo-check", Cap: "Advisor and scout runs", For: "run outside a repository", Sub: exec},
		}
	case "cursor":
		return []uiProbeNeed{
			{Token: "--print", Cap: "Advisor and scout runs", For: "run without the interface"},
			{Token: "--output-format", Cap: "Advisor and scout runs", For: "read the answer as text"},
			{Token: "--mode", Cap: "Advisor and scout runs", For: "keep advisor runs read-only"},
			{Token: "--resume", Cap: "Resume a session", For: "continue a session"},
		}
	}
	return nil
}

// uiHelpHas reports whether help text lists a flag (a whole token) or a command (a word that starts a line).
func uiHelpHas(help, token string) bool {
	if strings.HasPrefix(token, "-") {
		for i := 0; ; {
			j := strings.Index(help[i:], token)
			if j < 0 {
				return false
			}
			end := i + j + len(token)
			if end >= len(help) || !(help[end] == '-' || help[end] == '_' || (help[end] >= 'a' && help[end] <= 'z') || (help[end] >= 'A' && help[end] <= 'Z') || (help[end] >= '0' && help[end] <= '9')) {
				return true
			}
			i = end
		}
	}
	return regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(token) + `(\s|,|$)`).MatchString(help)
}

// uiProbeCompare checks the declared needs against help text, keyed by the help command ("" for
// --help, or "exec"). Help that could not be read proves nothing, so it never counts as drift.
func uiProbeCompare(needs []uiProbeNeed, helps map[string]string) (drift []uiProbeDrift, read bool) {
	drift = []uiProbeDrift{}
	for _, n := range needs {
		help := helps[strings.Join(n.Sub, " ")]
		if help == "" {
			continue
		}
		read = true
		if !uiHelpHas(help, n.Token) {
			drift = append(drift, uiProbeDrift{Flag: n.Token, Cap: n.Cap, For: n.For})
		}
	}
	return drift, read
}

var (
	probeMu sync.Mutex
	// uiProbeHelp runs a harness binary with --help (and optional subcommand). A variable so tests need no real programs.
	uiProbeHelp = func(agent string, sub []string) (string, bool) {
		bin := map[string]string{"claude": claudeExecutable(), "codex": "codex", "cursor": "cursor-agent"}[agent]
		path, err := exec.LookPath(bin)
		if err != nil {
			return "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, path, append(append([]string{}, sub...), "--help")...).CombinedOutput()
		return string(out), true
	}
)

// runProbe asks every harness and stores the answer.
func (m *uiManager) runProbe() uiProbeResult {
	probeMu.Lock()
	defer probeMu.Unlock()
	res := uiProbeResult{At: time.Now().Unix()}
	for _, agent := range []string{"claude", "codex", "cursor"} {
		ph := uiProbeHarness{Agent: agent, Drift: []uiProbeDrift{}}
		needs := uiProbeNeeds(agent)
		helps := map[string]string{}
		for _, n := range needs {
			key := strings.Join(n.Sub, " ")
			if _, done := helps[key]; done {
				continue
			}
			text, installed := uiProbeHelp(agent, n.Sub)
			ph.Installed = ph.Installed || installed
			helps[key] = text
		}
		if ph.Installed {
			ph.Version = uiHarnessVersion(agent)
			ph.Drift, ph.Read = uiProbeCompare(needs, helps)
		}
		res.Harnesses = append(res.Harnesses, ph)
	}
	uiDB().put("probe", "last", res)
	return res
}

// probeResult returns the stored probe, running a new one when there is none or it is over a day old.
func (m *uiManager) probeResult() uiProbeResult {
	var res uiProbeResult
	if uiDB().get("probe", "last", &res) && time.Since(time.Unix(res.At, 0)) < uiProbeEvery {
		return res
	}
	return m.runProbe()
}

func (s *uiServer) probeGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.probeResult())
}

func (s *uiServer) probeRefresh(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.runProbe())
}
