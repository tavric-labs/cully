package cully

import (
	"errors"
	"sort"
	"strings"
)

// Agent-to-agent features. Agents never talk to each other directly: Cully
// relays, so every message is visible in both feeds and the user stays in the
// loop. Three things use it: conflict alerts (two agents edited one file),
// relay (send one agent's reply or diff to another) and review (a read-only
// agent that critiques another agent's change).

const (
	uiRelayBody  = 15000 // runes of a reply or diff sent to another agent
	uiReviewDiff = 12000 // runes of diff put in a review prompt
	uiPreview    = 300
)

const uiReviewRules = `Rules for this run:
- You are read-only. Do not edit files, commit or push.
- Be specific and brief: name the file and say what is wrong.
- End with a numbered list of the fixes to ask the author for, most important first.`

func uiClip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// deliver sends a message from Cully to this agent and records it in the
// feed. A finished agent simply ignores it.
func (a *uiAgent) deliver(msg, shown string) {
	if err := a.write(msg); err != nil {
		return
	}
	a.add(uiEvent{Kind: "note", Text: shown})
}

// lastReply is what the agent said since the user last spoke to it.
func (a *uiAgent) lastReply() string {
	evs := a.allEvents()
	start := 0
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == "user" {
			start = i + 1
			break
		}
	}
	var parts []string
	for _, e := range evs[start:] {
		if e.Kind == "text" && strings.TrimSpace(e.Text) != "" {
			parts = append(parts, e.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// conflictAlerts tells both agents when a second agent edits a file the first
// already edited. Each pair is told once per file.
func (m *uiManager) conflictAlerts(a *uiAgent, file string) {
	if a.req.Mode == "plan" {
		return // read-only agents cannot conflict
	}
	for _, v := range m.list() {
		if v.ID == a.id || v.Repo != a.repo || !uiLive(v.State) {
			continue
		}
		if other, ok := m.get(v.ID); !ok || other.req.Mode == "plan" {
			continue
		}
		has := false
		for _, f := range v.Files {
			has = has || f == file
		}
		if !has {
			continue
		}
		ids := []string{a.id, v.ID}
		sort.Strings(ids)
		key := file + "|" + ids[0] + "|" + ids[1]
		m.mu.Lock()
		_, done := m.alerted[key]
		m.alerted[key] = struct{}{}
		m.mu.Unlock()
		if done {
			continue
		}
		peer, ok := m.get(v.ID)
		if !ok {
			continue
		}
		tell := func(to *uiAgent, other uiAgentView) {
			text := "Heads up from Cully: " + other.Title + " (branch " + other.Branch + ") has also edited " + file +
				". Keep your changes to that file minimal, avoid reformatting it, and expect to reconcile at merge."
			to.deliver(text, other.Title+" also edited "+file+"; Cully warned this agent.")
		}
		tell(a, v)
		tell(peer, a.snapshot())
	}
}

// relay sends one agent's latest reply, or its diff, to another live agent.
func (m *uiManager) relay(fromID, toID, what, note string) error {
	if what != "reply" && what != "diff" {
		return errors.New(`what must be "reply" or "diff"`)
	}
	if fromID == toID {
		return errors.New("choose a different agent to send to")
	}
	if len([]rune(note)) > uiMaxPromptRunes {
		return errors.New("note is too long")
	}
	from, ok := m.get(fromID)
	if !ok {
		return errors.New("no such agent to send from")
	}
	to, ok := m.get(toID)
	if !ok {
		return errors.New("no such agent to send to")
	}
	if !uiLive(to.snapshot().State) {
		return errors.New("that agent has finished")
	}
	var body, label string
	if what == "reply" {
		body, label = from.lastReply(), "its latest reply"
		if strings.TrimSpace(body) == "" {
			return errors.New("that agent has not said anything yet")
		}
	} else {
		body, label = from.diff(), "its change against the commit it started from"
		if body == "No changes yet." || body == "No worktree yet." {
			return errors.New("that agent has no changes yet")
		}
	}
	fv := from.snapshot()
	msg := "Message from another agent (" + fv.Title + ", branch " + fv.Branch + "), " + label + ":\n\n" + uiClip(body, uiRelayBody)
	if n := strings.TrimSpace(note); n != "" {
		msg += "\n\nNote from the user: " + n
	}
	to.deliver(msg, "From "+fv.Title+": "+uiClip(strings.Join(strings.Fields(body), " "), uiPreview))
	from.add(uiEvent{Kind: "note", Text: "Sent " + what + " to " + to.snapshot().Title})
	return nil
}

// review starts a read-only agent that critiques another agent's change.
func (m *uiManager) review(id string) (*uiAgent, error) {
	a, ok := m.get(id)
	if !ok {
		return nil, errors.New("no such agent")
	}
	diff := a.diff()
	if diff == "No changes yet." || diff == "No worktree yet." {
		return nil, errors.New("this agent has no changes to review yet")
	}
	v := a.snapshot()
	prompt := "Review the following change made by another agent. You are read-only: do not edit files. " +
		"Report concrete problems, each with the file and what is wrong: bugs, missing or weak tests, risky or unrelated changes, and security issues. " +
		"Then say what you would ask the author to fix first. If the change looks sound, say so briefly.\n\n" +
		"Task the author was given:\n" + uiClip(a.req.Prompt, 3000) + "\n\n" +
		"The author's worktree is " + v.Worktree + "; you may read files there. Change against the commit it started from:\n" + uiClip(diff, uiReviewDiff)
	return m.start(uiStartRequest{
		Repo: v.Repo, Title: "Review: " + v.Title, Prompt: prompt, Mode: "plan",
		addDirs: []string{v.Worktree}, rules: uiReviewRules,
	})
}
