package cully

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Advice is the advisor's guidance on one agent's work. Cully's own advisor is
// driven by hooks inside `cully run`, so it never sees the agents this UI
// starts. This asks the same kind of question for them: given what the agent
// has done, what would you tell it? The answer is kept per agent and can be
// sent to that agent with one click.

// uiHostname is this machine's name, for the advisor and the page header.
func uiHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

const (
	uiAdviceMax    = 3
	uiAdvicePrompt = `You are Cully's advisor, reviewing the progress of a coding agent. Give at most 3 pieces of advice, each on its own line, each starting with WARN| for something that will waste time or break work, CAUT| for a risk worth checking, or ADV| for a helpful improvement. Address the agent directly, for example "Run the tests before editing more." Be concrete and short: one sentence, no markdown. Base the advice only on the signals below. Use HANDOFF| instead of ADV| when the best next step is to copy this session's handoff and continue the work from it, for example when the work should continue in another open session or agent. Do not use any tools. You are told which machine and folder the work is on and which sessions are already open there; if one of those sessions is the natural place to continue, say so, so the person can continue it. If everything looks fine, reply with exactly: ADV|Nothing to change; keep going.`
)

type uiAdvice struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"`            // warn, caution or advice
	Action    string    `json:"action,omitempty"` // handoff: the card offers to copy the session's handoff
	Text      string    `json:"text"`
	Created   time.Time `json:"created"`
	Applied   bool      `json:"applied"`
	Dismissed bool      `json:"dismissed"`
}

type uiAdviceFor struct {
	AgentID    string     `json:"agentId"`
	AgentTitle string     `json:"agentTitle"`
	AgentState string     `json:"agentState"`
	Status     string     `json:"status"` // "", asking, ok or failed
	Error      string     `json:"error,omitempty"`
	Asked      time.Time  `json:"asked"`
	Advice     []uiAdvice `json:"advice"`
}

func (a *uiAgent) adviceList() []uiAdvice {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]uiAdvice{}, a.advice...)
}

// uiAdviceSignals describes the agent's work in plain text for the advisor.
func (a *uiAgent) uiAdviceSignals() string {
	s := a.summary()
	var b strings.Builder
	fmt.Fprintf(&b, "Machine: %s\n", uiHostname())
	fmt.Fprintf(&b, "Folder: %s\n", a.repo)
	if branch := uiFolderInfo(a.repo).branch; branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n", branch)
	}
	if s.Branch != "" {
		fmt.Fprintf(&b, "Working branch: %s\n", s.Branch)
	}
	if open := a.openSessionsInFolder(); open != "" {
		fmt.Fprintf(&b, "Sessions already open in this folder: %s\n", open)
	}
	fmt.Fprintf(&b, "Task: %s\n", s.Task)
	turns := 0
	var failures []string
	for _, e := range a.allEvents() {
		switch e.Kind {
		case "result":
			turns++
		case "tool_result":
			if e.Failed && len(failures) < 3 {
				failures = append(failures, uiClip(e.Text, 160))
			}
		}
	}
	fmt.Fprintf(&b, "Turns completed: %d\n", turns)
	fmt.Fprintf(&b, "Files changed (%d): %s\n", s.FileCount, strings.Join(s.Files, ", "))
	fmt.Fprintf(&b, "Checks: %d passed, %d failed, last result: %s; edits since the last check: %d\n", s.Checks.Passed, s.Checks.Failed, s.Checks.Last, s.EditsSinceCheck)
	fmt.Fprintf(&b, "Repeated failing commands: %d\n", s.Loops)
	if len(failures) > 0 {
		fmt.Fprintf(&b, "Recent failures: %s\n", strings.Join(failures, " | "))
	}
	if s.LastReply != "" {
		fmt.Fprintf(&b, "The agent's last reply: %s\n", s.LastReply)
	}
	return b.String()
}

func uiParseAdvice(out string, existing []uiAdvice) []uiAdvice {
	seen := map[string]bool{}
	for _, e := range existing {
		seen[strings.ToLower(e.Text)] = true
	}
	var res []uiAdvice
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.ReplaceAll(raw, "**", ""))
		if line == "" || strings.HasPrefix(line, "TOOLGAP:") || strings.HasPrefix(line, "```") || strings.HasPrefix(line, "---") {
			continue
		}
		if line[0] == '{' || line[0] == '[' {
			continue // machine output, not advice
		}
		level, text, action := "advice", line, ""
		if rest, ok := strings.CutPrefix(line, "HANDOFF|"); ok {
			text, action = strings.TrimSpace(rest), "handoff"
		} else if lvl, body, ok := parseSeverityPrefix(line); ok {
			text = body
			switch lvl {
			case AlertWarn:
				level = "warn"
			case AlertCaut:
				level = "caution"
			}
		}
		text = uiClip(strings.TrimSpace(strings.TrimLeft(text, "-*• ")), 300)
		if text == "" || seen[strings.ToLower(text)] {
			continue
		}
		seen[strings.ToLower(text)] = true
		res = append(res, uiAdvice{ID: uiNewID(), Level: level, Text: text, Action: action, Created: time.Now().UTC()})
		if len(res) == uiAdviceMax {
			break
		}
	}
	return res
}

func uiAdviceWaitMessage(reason string) string {
	switch {
	case strings.Contains(reason, "cooldown"):
		return "The advisor ran a moment ago. Try again in a couple of minutes."
	case strings.Contains(reason, "limit"):
		return "The advisor has reached its hourly limit. Try again later."
	case strings.Contains(reason, "unchanged"):
		return "Nothing has changed since the last advice."
	}
	return "The advisor is unavailable right now."
}

// advise asks the advisor about one agent. It returns at once; the answer
// arrives on the agent and is read with adviceList.
func (m *uiManager) advise(id string) error {
	a, ok := m.get(id)
	if !ok {
		return errors.New("no such agent")
	}
	a.mu.Lock()
	if a.adviceStatus == "asking" {
		a.mu.Unlock()
		return errors.New("The advisor is already looking at this agent.")
	}
	a.mu.Unlock()
	signals := a.uiAdviceSignals()
	overlap := a.openSessionsInFolder()
	if reason := admitAdvisorRequest("ui-"+a.id, signals, time.Now()); reason != "" {
		return errors.New(uiAdviceWaitMessage(reason))
	}
	a.mu.Lock()
	a.adviceStatus, a.adviceError, a.adviceAsked = "asking", "", time.Now().UTC()
	a.mu.Unlock()
	go func() {
		out, err := m.cfg.Advisor("claude", a.worktree, false, uiAdvicePrompt+"\n\nSIGNALS:\n"+signals)
		a.mu.Lock()
		defer a.mu.Unlock()
		if err != nil || strings.TrimSpace(out) == "" {
			a.adviceStatus, a.adviceError = "failed", "The advisor could not answer. Try again in a few minutes."
			return
		}
		fresh := uiParseAdvice(out, a.advice)
		if overlap != "" && !uiHasAction(a.advice, fresh, "handoff") {
			fresh = append(fresh, uiAdvice{ID: uiNewID(), Level: "advice", Action: "handoff", Created: time.Now().UTC(),
				Text: "Another session is already open in this folder (" + overlap + "). Copy this session's handoff and continue the work there."})
		}
		a.advice = append(a.advice, fresh...)
		for _, f := range fresh {
			m.recordResearch(uiResearchRec{Kind: "advice", Source: a.title, Repo: a.repo, Query: a.req.Prompt, Text: f.Text, Level: f.Level, Action: f.Action})
		}
		a.adviceStatus = "ok"
	}()
	return nil
}

func (a *uiAgent) findAdviceLocked(id string) *uiAdvice {
	for i := range a.advice {
		if a.advice[i].ID == id {
			return &a.advice[i]
		}
	}
	return nil
}

// applyAdvice sends a piece of advice to the agent it was written for.
func (m *uiManager) applyAdvice(agentID, adviceID string) error {
	a, ok := m.get(agentID)
	if !ok {
		return errors.New("no such agent")
	}
	if !uiLive(a.snapshot().State) {
		return errors.New("that agent has finished")
	}
	a.mu.Lock()
	adv := a.findAdviceLocked(adviceID)
	if adv == nil {
		a.mu.Unlock()
		return errors.New("no such advice")
	}
	text := adv.Text
	a.mu.Unlock()
	a.deliver("Advice from Cully's advisor: "+text+"\n\nApply it if it fits what you are doing; if it does not, say why.",
		"Advisor advice sent to this agent: "+uiClip(text, 200))
	a.mu.Lock()
	if adv := a.findAdviceLocked(adviceID); adv != nil {
		adv.Applied = true
	}
	a.mu.Unlock()
	return nil
}

func (m *uiManager) dismissAdvice(agentID, adviceID string) error {
	a, ok := m.get(agentID)
	if !ok {
		return errors.New("no such agent")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	adv := a.findAdviceLocked(adviceID)
	if adv == nil {
		return errors.New("no such advice")
	}
	adv.Dismissed = true
	return nil
}

// adviceAll lists every agent with its advice, newest agent first.
func (m *uiManager) adviceAll() []uiAdviceFor {
	var out []uiAdviceFor
	for _, v := range m.list() {
		a, ok := m.get(v.ID)
		if !ok {
			continue
		}
		a.mu.Lock()
		out = append(out, uiAdviceFor{
			AgentID: v.ID, AgentTitle: v.Title, AgentState: v.State, Status: a.adviceStatus, Error: a.adviceError,
			Asked: a.adviceAsked, Advice: append([]uiAdvice{}, a.advice...),
		})
		a.mu.Unlock()
	}
	return out
}

func uiAgoWords(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// openSessionsInFolder names the sessions already open in this agent's folder,
// such as "claude (open 2h)". Empty when there are none, which is also the
// signal that the work does not overlap with anything.
func (a *uiAgent) openSessionsInFolder() string {
	if a.mgr == nil {
		return ""
	}
	var open []string
	for _, x := range a.mgr.sessions() {
		if x.Repo == a.repo && !x.Detached {
			open = append(open, x.Agent+" (open "+strings.TrimSuffix(uiAgoWords(time.Since(x.Started)), " ago")+")")
		}
	}
	return strings.Join(open, ", ")
}

func uiHasAction(existing, fresh []uiAdvice, action string) bool {
	for _, list := range [][]uiAdvice{existing, fresh} {
		for _, x := range list {
			if x.Action == action {
				return true
			}
		}
	}
	return false
}
