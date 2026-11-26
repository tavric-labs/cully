package cully

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Things you can do from the terminal that the page can do too: set up an
// agent, start the advisor, connect the Cully MCP, run a job, run a scout.
// Each calls the same code the terminal command calls. The functions are
// variables so tests never start real processes or change real settings.

var uiStartAdvisor = StartDaemonDetached

var uiAddMCP = func(agent, endpoint string) error { return AddMCP(io.Discard, agent, endpoint, true) }

var uiStartJob = func(name string) error {
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Name != name {
			continue
		}
		done, err := startJob(j, jobsNow(), true, io.Discard)
		if err != nil {
			return err
		}
		go func() { <-done }() // the job's own record keeps the result
		return nil
	}
	return errors.New("no such job")
}

// uiEnableAgent sets up an agent's session tracking and Cully skill, the same
// steps `cully setup --agent X` takes without an MCP. Other settings stay as
// they are.
func uiEnableAgent(agent string) (string, error) {
	if !uiKnownAgent(agent) {
		return "", errors.New("unknown agent")
	}
	if err := installAgents("", false, agent); err != nil {
		return "", err
	}
	return AGENTNAME(agent) + " is set up. Restart any open " + AGENTNAME(agent) + " sessions so they pick it up.", nil
}

// AGENTNAME is the display name used in messages.
func AGENTNAME(agent string) string {
	switch agent {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "cursor":
		return "Cursor"
	}
	return agent
}

// ---- jobs ----

type uiJobView struct {
	Name    string `json:"name"`
	After   string `json:"after"`
	Program string `json:"program"` // the program only; arguments can hold secrets
	Running bool   `json:"running"`
}

func uiJobs() []uiJobView {
	jobs, err := loadJobs(jobsConfigFile())
	out := []uiJobView{}
	if err != nil {
		return out
	}
	states := readJobStates()
	for _, j := range jobs {
		v := uiJobView{Name: j.Name, After: j.After}
		if len(j.Command) > 0 {
			v.Program = filepath.Base(j.Command[0])
		}
		if st := states[j.Name]; st != nil && st.PID > 0 {
			v.Running = processAlive(st.PID)
		}
		out = append(out, v)
	}
	return out
}

// ---- standalone scouts ----

const (
	uiScoutQueryMax = 500
	uiScoutRunsMax  = 2  // running at once
	uiScoutKeep     = 10 // runs kept in memory
)

type uiScoutRunView struct {
	ID      string    `json:"id"`
	Query   string    `json:"query"`
	Status  string    `json:"status"` // running, ok or failed
	Detail  string    `json:"detail,omitempty"`
	Log     []string  `json:"log"`
	Output  string    `json:"output,omitempty"`
	Started time.Time `json:"started"`
}

type uiScoutRuns struct {
	mu   sync.Mutex
	runs []*uiScoutRunView
}

func (m *uiManager) scoutRuns() []uiScoutRunView {
	m.scout.mu.Lock()
	defer m.scout.mu.Unlock()
	out := make([]uiScoutRunView, 0, len(m.scout.runs))
	for i := len(m.scout.runs) - 1; i >= 0; i-- {
		r := *m.scout.runs[i]
		r.Log = append([]string{}, r.Log...)
		out = append(out, r)
	}
	return out
}

// startScout researches a question on the web without starting an agent. The
// findings can be copied or used in a new task.
func (m *uiManager) startScout(query string) (uiScoutRunView, error) {
	return m.startScoutFrom(query, "Started from the page")
}

// startScoutFrom starts a standalone scout; source says who asked, for the history.
func (m *uiManager) startScoutFrom(query, source string) (uiScoutRunView, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return uiScoutRunView{}, errors.New("say what the scout should look into")
	}
	if len([]rune(query)) > uiScoutQueryMax {
		return uiScoutRunView{}, errors.New("that question is too long")
	}
	m.scout.mu.Lock()
	running := 0
	for _, r := range m.scout.runs {
		if r.Status == "running" {
			running++
		}
	}
	if running >= uiScoutRunsMax {
		m.scout.mu.Unlock()
		return uiScoutRunView{}, errors.New("Two scouts are already running. Wait for one to finish.")
	}
	run := &uiScoutRunView{ID: uiNewID(), Query: query, Status: "running", Log: []string{}, Started: time.Now().UTC()}
	m.scout.runs = append(m.scout.runs, run)
	if len(m.scout.runs) > uiScoutKeep {
		m.scout.runs = m.scout.runs[len(m.scout.runs)-uiScoutKeep:]
	}
	view := *run
	m.scout.mu.Unlock()

	cwd := ""
	if repos := m.repoList(); len(repos) > 0 {
		cwd = repos[0].Path
	}
	if cwd == "" {
		cwd = filepath.Dir(jobsConfigFile())
	}
	prompt := "Scout the web for current best practice and known pitfalls on the topic below. Use at most 5 searches. Reply with at most 10 short lines, each with a source URL.\n\nTopic: " + query
	go func() {
		var text string
		var err error
		progress := func(line string) {
			m.scout.mu.Lock()
			if len(run.Log) < uiStageLogMax {
				run.Log = append(run.Log, uiClip(line, 300))
			}
			m.scout.mu.Unlock()
		}
		if m.cfg.ScoutStream != nil {
			ctx, cancel := contextWithScoutBudget()
			defer cancel()
			text, err = m.cfg.ScoutStream(ctx, cwd, prompt, progress)
		} else {
			text, err = m.cfg.Advisor("claude", cwd, true, prompt)
		}
		m.scout.mu.Lock()
		defer m.scout.mu.Unlock()
		if err != nil || strings.TrimSpace(text) == "" {
			run.Status, run.Detail = "failed", "The scout could not finish. Try again in a few minutes."
			return
		}
		run.Status, run.Output = "ok", text
		m.recordResearch(uiResearchRec{Kind: "scout", Source: source, Repo: cwd, Query: query, Text: text})
	}()
	return view, nil
}
