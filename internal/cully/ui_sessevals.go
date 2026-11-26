package cully

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// An eval case is made from a session judged complete: the goal in one plain
// sentence, how to tell it was met, and what the session actually did. It holds
// no prompts or replies. Cases live in the user's Cully folder, one file each.

const uiEvalSchema = "cully.session-eval/v1"

type uiEvalObserved struct {
	Completed string   `json:"completed"`
	Files     []string `json:"files,omitempty"`
	Edits     int      `json:"edits"`
	Checks    int      `json:"checks"`
	Errors    int      `json:"errors"`
	Tools     int      `json:"tools"`
	ActiveSec int      `json:"activeSec"`
}

type uiEvalCase struct {
	Schema   string         `json:"schema"`
	ID       string         `json:"id"` // the session it came from
	Created  time.Time      `json:"created"`
	Folder   string         `json:"folder"`
	Cwd      string         `json:"cwd"`
	Branch   string         `json:"branch,omitempty"`
	Task     string         `json:"task"`
	Criteria []string       `json:"criteria"`
	Notes    string         `json:"notes,omitempty"`
	Observed uiEvalObserved `json:"observed"`
}

func (m *uiManager) createEval(id string) (uiEvalCase, error) {
	if !uiSessionID.MatchString(id) {
		return uiEvalCase{}, errors.New("no such session")
	}
	m.idxMu.Lock()
	m.loadIndexLocked()
	e := m.index.Sessions[id]
	var cp uiIndexed
	if e != nil {
		cp = *e
	}
	m.idxMu.Unlock()
	if e == nil || uiHidden(cp.Cwd, m.cfg.HideSessionsIn) {
		return uiEvalCase{}, errors.New("no such session")
	}
	if cp.Source != "ai" || cp.Completed != "yes" || cp.Task == "" {
		return uiEvalCase{}, errors.New("only a session judged complete, and named, can become an eval")
	}
	c := uiEvalCase{Schema: uiEvalSchema, ID: id, Created: time.Now().UTC(), Folder: cp.Name, Cwd: cp.Cwd, Branch: cp.Branch, Task: cp.Task,
		Criteria: append([]string{}, cp.Criteria...), Notes: cp.Description,
		Observed: uiEvalObserved{Completed: cp.Completed, Edits: cp.Edits, Checks: cp.Checks, Errors: cp.Errors, Tools: cp.Tools, ActiveSec: cp.ActiveSec}}
	if cp.File != "" {
		if _, dg, ok := uiScanAgent(cp.Agent, cp.File, true); ok {
			for _, p := range dg.Files {
				if r, err := filepath.Rel(cp.Cwd, p); err == nil && !strings.HasPrefix(r, "..") {
					p = r
				}
				c.Observed.Files = append(c.Observed.Files, p)
			}
		}
	}
	uiDB().put("eval", id, c)
	m.idxMu.Lock()
	if e := m.index.Sessions[id]; e != nil {
		e.EvalAt = c.Created
		m.saveIndexLocked()
	}
	m.idxMu.Unlock()
	return c, nil
}

func (m *uiManager) evalCase(id string) (uiEvalCase, bool) {
	if !uiSessionID.MatchString(id) {
		return uiEvalCase{}, false
	}
	var c uiEvalCase
	if !uiDB().get("eval", id, &c) || uiHidden(c.Cwd, m.cfg.HideSessionsIn) {
		return uiEvalCase{}, false
	}
	return c, true
}

func (m *uiManager) listEvals() []uiEvalCase {
	out := []uiEvalCase{}
	for _, raw := range uiDB().all("eval") {
		var c uiEvalCase
		if json.Unmarshal(raw, &c) == nil && !uiHidden(c.Cwd, m.cfg.HideSessionsIn) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}
