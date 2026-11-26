package cully

import "time"

// Everything Cully is doing for the user right now, in one place, so the Board
// can show it all: tasks, named agents, eval runs, scouts, advisor reviews,
// session naming, and the sessions open in terminals of any harness.

type uiRunningAgent struct {
	Name    string    `json:"name"`
	Scope   string    `json:"scope"`
	TaskID  string    `json:"taskId"`
	Started time.Time `json:"started"`
}

type uiRunningScout struct {
	Query   string    `json:"query"`
	Started time.Time `json:"started"`
	Steps   int       `json:"steps"`
}

type uiRunningAdvice struct {
	TaskID string `json:"taskId"`
	Title  string `json:"title"`
}

type uiRunningNaming struct {
	On      bool `json:"on"`
	Done    int  `json:"done"`
	Pending int  `json:"pending"`
}

type uiRunningNow struct {
	Agents   []uiRunningAgent  `json:"agents"`
	Evals    []uiEvalRun       `json:"evals"`
	Scouts   []uiRunningScout  `json:"scouts"`
	Advisor  []uiRunningAdvice `json:"advisor"`
	Naming   uiRunningNaming   `json:"naming"`
	Sessions []uiSession       `json:"sessions"`
}

func (m *uiManager) running() uiRunningNow {
	out := uiRunningNow{Agents: []uiRunningAgent{}, Evals: []uiEvalRun{}, Scouts: []uiRunningScout{}, Advisor: []uiRunningAdvice{}, Sessions: []uiSession{}}
	for _, a := range m.identityList() {
		if len(a.Runs) > 0 && a.Runs[0].Status == "running" {
			out.Agents = append(out.Agents, uiRunningAgent{Name: a.Name, Scope: a.Scope, TaskID: a.Runs[0].TaskID, Started: a.Runs[0].Started})
		}
	}
	for _, r := range m.evalRunList() {
		if r.Status == "running" || r.Status == "judging" {
			out.Evals = append(out.Evals, r)
		}
	}
	for _, r := range m.scoutRuns() {
		if r.Status == "running" {
			out.Scouts = append(out.Scouts, uiRunningScout{Query: r.Query, Started: r.Started, Steps: len(r.Log)})
		}
	}
	for _, v := range m.list() {
		if a, ok := m.get(v.ID); ok {
			a.mu.Lock()
			asking := a.adviceStatus == "asking"
			a.mu.Unlock()
			if asking {
				out.Advisor = append(out.Advisor, uiRunningAdvice{TaskID: v.ID, Title: v.Title})
			}
		}
	}
	lib := m.sessionLibrary()
	out.Naming = uiRunningNaming{On: lib.Analysis.On, Done: lib.Totals.Analyzed, Pending: lib.Totals.Pending}
	out.Sessions = m.sessions()
	if out.Sessions == nil {
		out.Sessions = []uiSession{}
	}
	return out
}
