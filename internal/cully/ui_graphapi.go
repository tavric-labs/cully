package cully

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type uiGraphLane struct {
	ID    string `json:"id"`              // the session, or "run" before one is known
	Agent string `json:"agent,omitempty"` // claude, codex or cursor
	Label string `json:"label"`
}

type uiGraphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
	Cross  bool   `json:"cross,omitempty"` // between two lanes: a handoff to another session
}

type uiGraphView struct {
	Task    string        `json:"task"`
	Live    bool          `json:"live"`
	Lanes   []uiGraphLane `json:"lanes"`
	Nodes   []uiGraphNode `json:"nodes"`
	Edges   []uiGraphEdge `json:"edges"`
	Mermaid string        `json:"mermaid"` // a plain text view of the same trace
	Note    string        `json:"note"`    // what this graph is and is not
}

const uiGraphNoteText = "This is a trace of what Cully observed, not a graph Cully runs. It cannot see inside the harness, so steps between tool calls are inferred."

// uiBuildGraph makes the API view from a stored graph and the task's sessions.
func uiBuildGraph(task string, g uiTaskGraph, sessions []uiTaskSession, live bool) uiGraphView {
	v := uiGraphView{Task: task, Live: live, Nodes: g.Nodes, Edges: []uiGraphEdge{}, Lanes: []uiGraphLane{}, Note: uiGraphNoteText}
	if v.Nodes == nil {
		v.Nodes = []uiGraphNode{}
	}
	agent := map[string]string{}
	for _, s := range sessions {
		agent[s.ID] = s.Agent
	}
	seen := map[string]bool{}
	lane := func(n uiGraphNode) string {
		if n.Session == "" {
			return "run"
		}
		return n.Session
	}
	for _, n := range v.Nodes {
		id := lane(n)
		if seen[id] {
			continue
		}
		seen[id] = true
		l := uiGraphLane{ID: id, Agent: agent[id]}
		l.Label = firstNonEmpty(l.Agent, "claude")
		if id != "run" && len(id) > 8 {
			l.Label += " " + id[:8]
		}
		v.Lanes = append(v.Lanes, l)
	}
	for i := 1; i < len(v.Nodes); i++ {
		p, n := v.Nodes[i-1], v.Nodes[i]
		v.Edges = append(v.Edges, uiGraphEdge{From: p.ID, To: n.ID, Reason: firstNonEmpty(n.Cause, "next"), Cross: lane(p) != lane(n)})
	}
	v.Mermaid = uiGraphMermaid(v.Nodes)
	return v
}

// uiGraphMermaid draws the phases as a Mermaid state diagram. Steps that repeat are one line with a count.
func uiGraphMermaid(nodes []uiGraphNode) string {
	if len(nodes) == 0 {
		return ""
	}
	id := func(p string) string { return strings.ReplaceAll(p, "-", "_") }
	type key struct{ from, to, reason string }
	counts := map[key]int{}
	var order []key
	used := map[string]bool{}
	for i, n := range nodes {
		used[n.Phase] = true
		if i == 0 {
			continue
		}
		k := key{nodes[i-1].Phase, n.Phase, firstNonEmpty(n.Cause, "next")}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")
	var phases []string
	for p := range used {
		phases = append(phases, p)
	}
	sort.Strings(phases)
	for _, p := range phases {
		label := uiPhaseLabels[p]
		if p == uiGraphEarlier {
			label = "Earlier steps"
		}
		b.WriteString("    state \"" + label + "\" as " + id(p) + "\n")
	}
	b.WriteString("    [*] --> " + id(nodes[0].Phase) + "\n")
	for _, k := range order {
		line := "    " + id(k.from) + " --> " + id(k.to) + ": " + k.reason
		if c := counts[k]; c > 1 {
			line += " x" + strconv.Itoa(c)
		}
		b.WriteString(line + "\n")
	}
	if last := nodes[len(nodes)-1]; uiTerminalPhase(last.Phase) {
		b.WriteString("    " + id(last.Phase) + " --> [*]\n")
	}
	return b.String()
}

func (m *uiManager) graphForAgent(a *uiAgent) uiGraphView {
	a.graphLoad()
	g := a.graph.copy()
	var rec uiTaskRecord
	var sessions []uiTaskSession
	if uiDB().get("task", a.taskID(), &rec) {
		sessions = rec.Sessions
	}
	return uiBuildGraph(a.taskID(), g, sessions, uiLive(a.snapshot().State))
}

func (s *uiServer) agentGraph(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.agent(w, r); ok {
		uiJSON(w, http.StatusOK, s.m.graphForAgent(a))
	}
}

// taskGraph serves the live graph of a task that is running, otherwise the one stored with its record.
func (s *uiServer) taskGraph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.m.mu.Lock()
	var live *uiAgent
	for _, a := range s.m.agents {
		if a.taskID() == id && (live == nil || a.created.After(live.created)) {
			live = a
		}
	}
	s.m.mu.Unlock()
	if live != nil && uiLive(live.snapshot().State) {
		uiJSON(w, http.StatusOK, s.m.graphForAgent(live))
		return
	}
	var rec uiTaskRecord
	if !uiDB().get("task", id, &rec) {
		uiError(w, http.StatusNotFound, "no such task")
		return
	}
	var g uiTaskGraph
	if rec.Graph != nil {
		g = uiTaskGraph{Seq: rec.Graph.Seq, Nodes: append([]uiGraphNode(nil), rec.Graph.Nodes...)}
		closeOpenAtRestart(g.Nodes, firstTime(rec.Updated, time.Now()))
	}
	uiJSON(w, http.StatusOK, uiBuildGraph(id, g, rec.Sessions, false))
}

func firstTime(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}
