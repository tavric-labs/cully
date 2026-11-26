package cully

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// The scout is the web-research step of a task. It used to run unseen and its
// findings went straight into the agent's prompt. Now each stage keeps its
// progress and findings so the page can show what was searched and found.

// uiScoutFunc runs a research query, reporting each search or page read as it
// happens, and returns the findings.
type uiScoutFunc func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error)

const (
	uiScoutBudget  = 3 * time.Minute
	uiStageLogMax  = 30
	uiStageOutShow = 8000
)

// uiScoutBinary is a variable so tests need no real CLI.
var uiScoutBinary = func() string { return claudeExecutable() }

// uiStreamScout runs the advisor's research command in streaming mode, so the
// searches it makes can be shown live, and returns the full findings.
func uiStreamScout(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
	_, args, err := advisorCommand("claude", true)
	if err != nil {
		return "", err
	}
	config, cleanup, err := claudeAdvisorMCP(cwd, true)
	if err != nil {
		return "", err
	}
	defer cleanup()
	args = append(args, "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--mcp-config", config)
	ctx, cancel := context.WithTimeout(ctx, uiScoutBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, uiScoutBinary(), args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(prompt)
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "CULLY_PANE_SESSION", "CULLY_SESSION", "CULLY_AGENT", "MODEL_HINT_GUARD":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "MODEL_HINT_GUARD=1", "CULLY_AGENT=claude")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	final := ""
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		p := parseStreamLine(sc.Bytes())
		for _, ev := range p.Events {
			if ev.Kind != "tool" || strings.TrimSpace(ev.Text) == "" {
				continue
			}
			switch ev.Tool {
			case "WebSearch":
				progress("Searching: " + ev.Text)
			case "WebFetch":
				progress("Reading: " + ev.Text)
			}
		}
		if p.Final != "" {
			final = p.Final
		}
	}
	err = cmd.Wait()
	if strings.TrimSpace(final) == "" {
		if err != nil {
			return "", err
		}
		return "", errors.New("the scout returned nothing")
	}
	return final, nil
}

type uiStageDetail struct {
	Status string   `json:"status"`
	Detail string   `json:"detail,omitempty"`
	Log    []string `json:"log,omitempty"`
	Output string   `json:"output,omitempty"`
}

type uiScoutView struct {
	AgentID string        `json:"agentId"`
	Title   string        `json:"title"`
	Task    string        `json:"task"`
	State   string        `json:"state"`
	Recall  uiStageDetail `json:"recall"`
	Scout   uiStageDetail `json:"scout"`
}

func (a *uiAgent) stageDetail(name string) uiStageDetail {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.stages {
		if s.Name == name {
			return uiStageDetail{Status: s.Status, Detail: s.Detail, Log: append([]string(nil), s.Log...), Output: uiClip(s.Output, uiStageOutShow)}
		}
	}
	return uiStageDetail{Status: "pending"}
}

func (a *uiAgent) setStageOutput(name, output string) {
	a.mu.Lock()
	for i := range a.stages {
		if a.stages[i].Name == name {
			a.stages[i].Output = output
		}
	}
	a.mu.Unlock()
}

func (a *uiAgent) appendStageLog(name, line string) {
	a.mu.Lock()
	for i := range a.stages {
		if a.stages[i].Name == name && len(a.stages[i].Log) < uiStageLogMax {
			a.stages[i].Log = append(a.stages[i].Log, uiClip(line, 300))
		}
	}
	a.mu.Unlock()
}

// stagesCopyLocked copies the stages for a snapshot, without their findings.
func (a *uiAgent) stagesCopyLocked() []uiStage {
	out := make([]uiStage, len(a.stages))
	for i, s := range a.stages {
		out[i] = uiStage{Name: s.Name, Status: s.Status, Detail: s.Detail, Log: append([]string(nil), s.Log...)}
	}
	return out
}

// scouts lists every agent's recall and scout stages, newest agent first.
func (m *uiManager) scouts() []uiScoutView {
	var out []uiScoutView
	for _, v := range m.list() {
		a, ok := m.get(v.ID)
		if !ok {
			continue
		}
		out = append(out, uiScoutView{
			AgentID: v.ID, Title: v.Title, Task: uiClip(a.req.Prompt, 400), State: v.State,
			Recall: a.stageDetail("recall"), Scout: a.stageDetail("scout"),
		})
	}
	return out
}
