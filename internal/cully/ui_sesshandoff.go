package cully

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Continuing the work of an earlier session in a new task. The earlier one may
// be out of room, closed, or from another harness, so Cully hands the new task
// a short account of it (what it was for, what it did, where it ended) and
// the path of the full record, rather than the whole conversation.

func (m *uiManager) sessionEntry(id string) (uiIndexed, error) {
	if !uiSessionID.MatchString(id) {
		return uiIndexed{}, errors.New("no such session")
	}
	m.indexSessions(500)
	m.idxMu.Lock()
	m.loadIndexLocked()
	e := m.index.Sessions[id]
	var cp uiIndexed
	if e != nil {
		cp = *e
	}
	m.idxMu.Unlock()
	if e == nil || uiHidden(cp.Cwd, m.cfg.HideSessionsIn) {
		return uiIndexed{}, errors.New("no such session")
	}
	return cp, nil
}

func uiHarnessName(agent string) string {
	switch agent {
	case "codex":
		return "Codex"
	case "cursor":
		return "Cursor"
	}
	return "Claude"
}

func uiSessionHandoff(e uiIndexed) string {
	var b strings.Builder
	b.WriteString("Continue the work of an earlier " + uiHarnessName(e.Agent) + " session in " + e.Cwd + ".\n\n")
	title := e.Title
	if title == "" {
		title = "(not named)"
	}
	b.WriteString("What it was: " + title + "\n")
	if e.Task != "" {
		b.WriteString("Its goal: " + e.Task + "\n")
	}
	if e.Description != "" {
		b.WriteString("What it did: " + e.Description + "\n")
	}
	switch e.Completed {
	case "yes":
		b.WriteString("Where it ended: it finished the job.\n")
	case "partly":
		b.WriteString("Where it ended: only partly finished.\n")
	case "no":
		b.WriteString("Where it ended: it did not finish.\n")
	}
	if e.Drift != "" {
		b.WriteString("It drifted: " + e.Drift + "\n")
	}
	if e.Different != "" && e.Different != "Nothing notable." {
		b.WriteString("What would have gone better: " + e.Different + "\n")
	}
	b.WriteString("Its size: " + uiItoa(e.Turns) + " requests, " + uiItoa(e.Tools) + " tool calls, " + uiItoa(e.Files) + " files changed, " + uiItoa(e.Checks) + " test runs, about " + uiItoa(e.ActiveSec/60) + " minutes.\n")
	if e.Unverified {
		b.WriteString("It changed files and never ran the tests afterwards.\n")
	}
	if e.File != "" {
		b.WriteString("\nThe full record of that session is at " + e.File + ". Read it only if you need exact details.\n")
	}
	b.WriteString("\nLook at the current state of the folder first. Then finish what is left, and run the checks before you say you are done.")
	return b.String()
}

// continueFromSession starts a new task in the session's folder with its handoff.
func (m *uiManager) continueFromSession(id string) (*uiAgent, error) {
	e, err := m.sessionEntry(id)
	if err != nil {
		return nil, err
	}
	st, err := osStatDir(e.Cwd)
	if err != nil || !st {
		return nil, errors.New("the folder this session worked in is gone")
	}
	req := uiStartRequest{Title: "Continue: " + uiClipWords(firstNonEmpty(e.Title, e.Name), 50), Prompt: uiSessionHandoff(e), Context: true}
	if e.File != "" {
		req.addDirs = []string{filepath.Dir(e.File)}
	}
	if root, err := uiRepoRoot(e.Cwd); err == nil {
		info, err := m.addRepo(root)
		if err != nil {
			return nil, err
		}
		req.Repo = info.Path
	} else {
		req.inPlaceDir = e.Cwd
	}
	return m.start(req)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func osStatDir(p string) (bool, error) {
	info, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
