package cully

import (
	"strings"
)

// uiRunInfo says, in plain terms, how the harness underneath a task was set
// up: which program runs the model, what it may use, which connected servers it
// loads, where it works, and what Cully adds around it. It is recorded when the
// task starts so the page can always answer "what is running this?".
type uiRunInfo struct {
	Harness     string   `json:"harness"`    // the program that runs the model
	Interface   string   `json:"interface"`  // how Cully talks to it
	Permission  string   `json:"permission"` // its permission mode
	Model       string   `json:"model"`
	Groups      []string `json:"groups"`  // tool groups allowed, in words
	Allowed     []string `json:"allowed"` // tool names allowed
	Blocked     []string `json:"blocked"` // tool names always refused
	Asks        []string `json:"asks"`    // calls that stop and ask you first (a card in the task window)
	McpMode     string   `json:"mcpMode"` // all or chosen
	McpChosen   []string `json:"mcpChosen,omitempty"`
	Isolation   string   `json:"isolation"` // worktree or in place
	Resume      string   `json:"resume,omitempty"`
	Identity    string   `json:"identity,omitempty"` // the named agent this run is for
	Brief       []string `json:"brief"`              // what Cully put in front of the first turn
	Loop        bool     `json:"loop"`
	LoopChecks  []string `json:"loopChecks,omitempty"` // what Cully does after each turn
	DoneWhen    bool     `json:"doneWhen"`
	MaxRounds   int      `json:"maxRounds,omitempty"`
	ToolBudget  int      `json:"toolBudget,omitempty"`
	HelperNames []string `json:"helpers,omitempty"`
}

func (m *uiManager) describeRun(a *uiAgent, permission, allowed, denied string, mcpArgs []string, asks []string) *uiRunInfo {
	r := &uiRunInfo{Harness: "Claude Code", Interface: "Headless, talking over a JSON stream", Permission: permission, Model: a.req.Model,
		Allowed: uiSplitTools(allowed), Blocked: uiSplitTools(denied), Asks: append([]string{}, asks...), McpMode: "all", Isolation: "worktree", Identity: a.req.identityName, Loop: a.harness != nil}
	if r.Model == "" {
		r.Model = "its default"
	}
	groups := a.req.groups
	m.policyMu.Lock()
	m.loadPolicyLocked()
	for _, pr := range uiToolPresets {
		on := m.policy.presetOn(pr)
		if groups != nil {
			on = groups[pr.ID]
		}
		if on && !(a.req.Mode == "plan" && pr.NotInPlan) {
			r.Groups = append(r.Groups, pr.Label)
		}
	}
	chosen := append([]string{}, m.policy.McpOnly...)
	m.policyMu.Unlock()
	if len(mcpArgs) > 0 {
		r.McpMode = "chosen"
		defs := uiMCPDefs()
		for _, n := range chosen {
			if _, ok := defs[n]; ok {
				r.McpChosen = append(r.McpChosen, n)
			}
		}
	}
	if a.req.resumeDir != "" || a.req.inPlaceDir != "" {
		r.Isolation = "in place"
	}
	switch {
	case a.req.resumeID != "" && a.req.fork:
		r.Resume = "Forked from an earlier session"
	case a.req.resumeID != "":
		r.Resume = "Continues an earlier session"
	}
	switch {
	case a.req.resumeID != "":
		r.Brief = []string{"Nothing added: the session already holds its own history"}
	default:
		if a.req.Context {
			r.Brief = append(r.Brief, "The project's journal and git state")
		}
		if a.req.Recall {
			r.Brief = append(r.Brief, "What Cully memory recalls")
		}
		if a.req.Scout {
			r.Brief = append(r.Brief, "Web research from the scout")
		}
		if len(r.Brief) == 0 {
			r.Brief = []string{"Only your task"}
		}
	}
	if a.req.appendSystem != "" {
		r.Brief = append(r.Brief, "The agent's identity and its notes")
	}
	if a.harness != nil {
		r.MaxRounds = a.harness.max
		r.ToolBudget = uiHarnessToolBudget
		r.DoneWhen = a.harness.criteria != ""
		r.LoopChecks = []string{"Asks it to run tests if it changed files and ran none", "Steers it when it repeats a call (" + uiItoa(uiHarnessRepeatWarn) + " times) and stops it at " + uiItoa(uiHarnessRepeatStop), "Stops after " + uiItoa(uiHarnessToolBudget) + " tool calls or " + uiItoa(a.harness.max) + " rounds"}
		if r.DoneWhen {
			r.LoopChecks = append(r.LoopChecks, "Judges its work against your checks")
		}
		if len(a.req.helperNames) > 0 {
			r.HelperNames = append([]string{}, a.req.helperNames...)
			r.LoopChecks = append(r.LoopChecks, "Asks "+strings.Join(a.req.helperNames, ", ")+" to review the finished work")
		}
	}
	return r
}
