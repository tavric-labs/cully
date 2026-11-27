package cully

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A task runs on a harness: Claude Code through ui_agent.go, or Codex through ui_codex_runner.go. A
// runner is the small surface the rest of the page needs from a harness that has its own protocol:
// start a turn, send a follow-up, interrupt, stop. What it reports goes back through the task as the
// same uiParsed values Claude's stream produces (text, tool, tool_result, plan, result, usage, error),
// and a question about a tool call goes through askPermission, the same card Claude uses. Claude Code
// itself keeps its own run loop; uiClaudeRunner is the thin adapter that lets it be driven the same way.

// uiRunSpec is what a runner is started with.
type uiRunSpec struct {
	Cwd      string
	Prompt   string
	Mode     string // "edit" or "plan"
	ResumeID string // the harness's own session or thread id to continue
	Model    string
	System   string // extra instructions, such as a named agent's role
	MaxCost  float64
	MaxTurns int
}

type uiRunner interface {
	// Start launches the harness and sends the first turn. It returns when the harness has exited.
	Start(spec uiRunSpec) error
	// Send delivers a follow-up message: it starts the next turn when idle, or after the current one.
	Send(text string) error
	// Interrupt ends the turn in progress and leaves the session open.
	Interrupt() error
	// Stop ends the harness and everything it started.
	Stop()
}

// uiClaudeRunner adapts a Claude task to the interface. Claude's run loop is unchanged: Start is run(),
// Send is the stream-json write, and Stop is the process group kill.
type uiClaudeRunner struct {
	a *uiAgent
	m *uiManager
}

func (r uiClaudeRunner) Start(uiRunSpec) error  { r.a.run(r.m); return nil }
func (r uiClaudeRunner) Send(text string) error { return r.a.write(text) }
func (r uiClaudeRunner) Interrupt() error {
	return errors.New("Claude Code tasks cannot be interrupted without stopping them")
}
func (r uiClaudeRunner) Stop() { r.a.stop() }

// uiHarnessID is the harness a request names, with the default filled in.
func uiHarnessID(h string) string {
	if h == "" {
		return "claude"
	}
	return h
}

// uiRunCapability is the matrix's answer for one harness and one capability: yes, partial or no.
func uiRunCapability(id, harness string) string {
	for _, c := range (&uiManager{}).capabilities().List {
		if c.ID != id {
			continue
		}
		switch harness {
		case "codex":
			return c.Codex
		case "cursor":
			return c.Cursor
		}
		return c.Claude
	}
	return "no"
}

// prepareHarness checks the harness a request names against the capability matrix and the machine.
func (m *uiManager) prepareHarness(req *uiStartRequest) error {
	req.Harness = strings.ToLower(strings.TrimSpace(req.Harness))
	if req.Harness == "claude" {
		req.Harness = ""
	}
	switch req.Harness {
	case "":
		req.ResumeThread = ""
		return nil
	case "codex":
	case "cursor":
		return m.prepareCursor(req)
	default:
		return errors.New("tasks cannot run on " + req.Harness + " yet")
	}
	if uiRunCapability("run", "codex") != "yes" {
		return errors.New("Codex tasks are not supported by this version of Cully")
	}
	if m.codexBin() == "" {
		return errors.New("Codex is not installed on this machine")
	}
	if req.ResumeThread != "" {
		if !uiSessionID.MatchString(req.ResumeThread) {
			return errors.New("invalid thread id")
		}
	}
	return nil
}

// codexBin finds the Codex program: the configured one, the PATH, or the usual per-user folder.
func (m *uiManager) codexBin() string {
	if m.cfg.CodexBin != "" {
		return m.cfg.CodexBin
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "codex")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

type uiHarnessChoice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Available bool   `json:"available"`        // a task can be started on it
	Reason    string `json:"reason,omitempty"` // why not
	Steering  string `json:"steering"`         // follow-up messages: yes, partial or no, from the matrix
	Resume    string `json:"resume"`
}

// harnessAvailable lists the harnesses the new-task page can offer. Signed-in state is not known.
func (s *uiServer) harnessAvailable(w http.ResponseWriter, r *http.Request) {
	m := s.m
	out := []uiHarnessChoice{}
	cl := uiHarnessChoice{ID: "claude", Name: "Claude Code", Steering: uiRunCapability("run", "claude"), Resume: uiRunCapability("resume", "claude")}
	if m.cfg.ClaudeBin != "" {
		cl.Installed = true
	} else if p := claudeExecutable(); p != "" {
		if _, err := exec.LookPath(p); err == nil {
			cl.Installed = true
		}
	}
	if cl.Installed {
		cl.Version = uiVersionOf(uiHarnessVersion, "claude")
	}
	cl.Available = cl.Installed
	if !cl.Installed {
		cl.Reason = "not installed"
	}
	out = append(out, cl)
	cx := uiHarnessChoice{ID: "codex", Name: "Codex", Steering: uiRunCapability("run", "codex"), Resume: uiRunCapability("resume", "codex")}
	cx.Installed = m.codexBin() != ""
	if cx.Installed {
		cx.Version = uiVersionOf(uiHarnessVersion, "codex")
	}
	switch {
	case !cx.Installed:
		cx.Reason = "not installed"
	case cx.Steering != "yes":
		cx.Reason = "not supported by this version of Cully"
	default:
		cx.Available = true
	}
	out = append(out, cx)
	out = append(out, s.cursorAvailable())
	uiJSON(w, http.StatusOK, out)
}

func uiVersionOf(f func(string) string, agent string) string {
	done := make(chan string, 1)
	go func() { done <- f(agent) }()
	select {
	case v := <-done:
		return v
	case <-time.After(4 * time.Second):
		return ""
	}
}

var _ uiRunner = uiClaudeRunner{}
var _ uiRunner = (*uiCodexRunner)(nil)
