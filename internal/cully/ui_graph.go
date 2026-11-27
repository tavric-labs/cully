package cully

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The task graph is a record of what a task did, seen from outside: each visit to a phase is a node,
// each change of phase is an edge with a reason. Cully does not run this graph and cannot see inside
// the harness, so it holds only what the event stream, the permission relay, the stuck watch and
// Cully's own harness loop show.

const (
	uiGraphCap      = 400 // nodes kept per task
	uiGraphSummary  = 120 // characters of tool summary kept
	uiGraphEarlier  = "earlier"
	uiGraphMergeMax = 60 // oldest steps folded into the summary node when over the cap
)

type uiGraphNode struct {
	ID      string       `json:"id"`
	Session string       `json:"session,omitempty"` // the harness session it happened in; its lane
	Phase   string       `json:"phase"`
	Tags    []string     `json:"tags,omitempty"`
	Start   time.Time    `json:"start"`
	End     time.Time    `json:"end,omitempty"` // zero while the node is open
	Cause   string       `json:"cause,omitempty"`
	Ended   string       `json:"ended,omitempty"` // why it closed, when nothing followed (restart)
	Tool    string       `json:"tool,omitempty"`
	Summary string       `json:"summary,omitempty"` // up to 120 characters, redacted; never a tool's input in full
	Answer  string       `json:"answer,omitempty"`  // for a permission question: what you chose
	Changed string       `json:"changed,omitempty"` // what changed in the files during this node
	Cost    float64      `json:"cost,omitempty"`
	Tokens  int          `json:"tokens,omitempty"`
	Count   int          `json:"count,omitempty"`   // visits folded into this node
	Earlier int          `json:"earlier,omitempty"` // for the summary node: how many steps it stands for
	Acts    []uiGraphAct `json:"acts,omitempty"`    // what the task did while in this state: a few of the first and the latest
	ActsN   int          `json:"actsN,omitempty"`   // how many things it did in all
}

// uiGraphAct is one thing the task did or said in a state: a tool call and how it ended, a message,
// a plan update, an answer from you. Labels are short, redacted text; a tool's input is never kept in full.
type uiGraphAct struct {
	T  time.Time `json:"t"`
	K  string    `json:"k"`            // tool, said, you, plan, note, error, turn
	L  string    `json:"l"`            // what it was, in a short line
	R  string    `json:"r,omitempty"`  // for a tool: how it ended, in a short line
	Ok string    `json:"ok,omitempty"` // for a tool: ok or fail
}

const (
	uiGraphActMax   = 30  // acts kept per state
	uiGraphActHead  = 8   // of those, the first ones are kept
	uiGraphActLabel = 140 // characters of a label
)

// uiTaskGraph is what is stored with the task record.
type uiTaskGraph struct {
	Seq   int           `json:"seq"`
	Nodes []uiGraphNode `json:"nodes"`
}

type uiGraphNote struct {
	Tool    string
	Summary string
}

type uiGraph struct {
	mu        sync.Mutex
	g         uiTaskGraph
	loaded    bool
	session   string
	known     map[string]bool
	cost      float64
	tokens    int
	openCost  float64
	openToks  int
	now       func() time.Time
	firstCall string       // the reason the first node of this run gets, when the task already had a history
	runNodes  int          // nodes this run has made
	pending   []uiGraphAct // tool calls seen just before the state they belong to is opened
}

func (g *uiGraph) clock() time.Time {
	if g.now != nil {
		return g.now().UTC()
	}
	return time.Now().UTC()
}

func uiSameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func uiTerminalPhase(p string) bool {
	switch p {
	case uiPhDone, uiPhFailed, uiPhLimit, uiPhStopped, uiPhClosed:
		return true
	}
	return false
}

// visit closes the open node and opens the next one. The same phase with the same tags extends the
// open node instead.
func (g *uiGraph) visit(phase string, tags []string, cause string, n uiGraphNote, files []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.visitLocked(phase, tags, cause, n, files)
}

func (g *uiGraph) visitLocked(phase string, tags []string, cause string, n uiGraphNote, files []string) {
	now := g.clock()
	changed := g.newFiles(files)
	pend := g.pending
	g.pending = nil
	if k := len(g.g.Nodes); k > 0 && g.g.Nodes[k-1].End.IsZero() {
		open := &g.g.Nodes[k-1]
		if open.Phase == phase && uiSameTags(open.Tags, tags) {
			open.Count++
			uiAddActs(open, pend)
			if changed != "" {
				open.Changed = uiJoinChanged(open.Changed, changed)
			}
			if open.Summary == "" && n.Summary != "" {
				open.Summary, open.Tool = n.Summary, firstNonEmpty(open.Tool, n.Tool)
			}
			return
		}
		if phase != uiPhTool { // calls seen before a state other than waiting-on-a-tool belong to the one ending
			uiAddActs(open, pend)
			pend = nil
		}
		g.closeLocked(open, now, changed)
	} else if changed != "" && len(g.g.Nodes) > 0 { // files changed after the last node closed
		last := &g.g.Nodes[len(g.g.Nodes)-1]
		last.Changed = uiJoinChanged(last.Changed, changed)
	}
	if cause == "" && len(g.g.Nodes) == 0 {
		cause = "started"
	}
	g.g.Seq++
	g.runNodes++
	node := uiGraphNode{ID: "n" + strconv.Itoa(g.g.Seq), Session: g.session, Phase: phase, Tags: append([]string(nil), tags...), Start: now, Cause: cause, Tool: n.Tool, Summary: n.Summary, Count: 1}
	g.openCost, g.openToks = g.cost, g.tokens
	uiAddActs(&node, pend)
	if uiTerminalPhase(phase) {
		node.End = now
	}
	g.g.Nodes = append(g.g.Nodes, node)
	g.capLocked()
}

func (g *uiGraph) closeLocked(n *uiGraphNode, now time.Time, changed string) {
	n.End = now
	if changed != "" {
		n.Changed = uiJoinChanged(n.Changed, changed)
	}
	if d := g.cost - g.openCost; d > 0 {
		n.Cost = d
	}
	if d := g.tokens - g.openToks; d > 0 {
		n.Tokens = d
	}
}

func uiJoinChanged(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// newFiles says in one line which files appeared since the last visit.
func (g *uiGraph) newFiles(files []string) string {
	if g.known == nil {
		g.known = map[string]bool{}
	}
	var fresh []string
	for _, f := range files {
		if !g.known[f] {
			g.known[f] = true
			fresh = append(fresh, f)
		}
	}
	if len(fresh) == 0 {
		return ""
	}
	sort.Strings(fresh)
	shown := fresh
	if len(shown) > 3 {
		shown = shown[:3]
	}
	out := "Edited " + strings.Join(shown, ", ")
	if len(fresh) > 3 {
		out += " and " + strconv.Itoa(len(fresh)-3) + " more"
	}
	return uiClip(out, 200)
}

// meter keeps the latest cost and token totals, so a node can say what it cost.
func (g *uiGraph) meter(cost float64, tokens int) {
	g.mu.Lock()
	if cost > g.cost {
		g.cost = cost
	}
	if tokens > g.tokens {
		g.tokens = tokens
	}
	g.mu.Unlock()
}

// setSession names the session this run is. Nodes made before it was known join its lane.
func (g *uiGraph) setSession(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.session == id {
		return
	}
	for i := len(g.g.Nodes) - 1; i >= 0 && g.g.Nodes[i].Session == "" && g.session == ""; i-- {
		g.g.Nodes[i].Session = id
	}
	g.session = id
}

// annotate sets the summary of the open node, for example the name of the helper being waited on.
func (g *uiGraph) annotate(summary string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if k := len(g.g.Nodes); k > 0 && g.g.Nodes[k-1].End.IsZero() {
		g.g.Nodes[k-1].Summary = uiClip(summary, uiGraphSummary)
	}
}

// answer records what you chose on the open permission node.
func (g *uiGraph) answer(status string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := len(g.g.Nodes) - 1; i >= 0 && i >= len(g.g.Nodes)-3; i-- {
		if n := &g.g.Nodes[i]; n.Phase == uiPhNeedsYou && n.Answer == "" {
			n.Answer = status
			return
		}
	}
}

// lastPhase is the phase of the newest node, or "".
func (g *uiGraph) lastPhase() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if k := len(g.g.Nodes); k > 0 {
		return g.g.Nodes[k-1].Phase
	}
	return ""
}

// load starts from a stored graph. A node that was still open belonged to a run the server lost.
func (g *uiGraph) load(st *uiTaskGraph, at time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.loaded {
		return
	}
	g.loaded = true
	if st == nil {
		return
	}
	g.g = uiTaskGraph{Seq: st.Seq, Nodes: append([]uiGraphNode(nil), st.Nodes...)}
	closeOpenAtRestart(g.g.Nodes, at)
}

func closeOpenAtRestart(nodes []uiGraphNode, at time.Time) {
	for i := range nodes {
		if nodes[i].End.IsZero() {
			end := at
			if end.Before(nodes[i].Start) {
				end = nodes[i].Start
			}
			nodes[i].End, nodes[i].Ended = end, "restart"
		}
	}
}

// copy is the graph as it is now.
func (g *uiGraph) copy() uiTaskGraph {
	g.mu.Lock()
	defer g.mu.Unlock()
	return uiTaskGraph{Seq: g.g.Seq, Nodes: append([]uiGraphNode(nil), g.g.Nodes...)}
}

// uiGraphKeep says whether a node must survive merging. Only plain working and tool steps are folded away;
// a question to you, a stuck spell, a helper, a failure or a stop always stays, and so does a step that changed files.
func uiGraphKeep(n uiGraphNode) bool {
	switch n.Phase {
	case uiPhWorking, uiPhTool:
		return n.Cause == "nudge" || n.Changed != ""
	}
	return true
}

// capLocked folds the oldest finished working and tool nodes into one summary node when the graph is over its cap.
func (g *uiGraph) capLocked() {
	if len(g.g.Nodes) <= uiGraphCap {
		return
	}
	over := len(g.g.Nodes) - uiGraphCap
	if over < uiGraphMergeMax {
		over = uiGraphMergeMax
	}
	g.g.Nodes = uiMergeOldest(g.g.Nodes, over, &g.g.Seq)
}

func uiMergeOldest(nodes []uiGraphNode, want int, seq *int) []uiGraphNode {
	sumIdx := -1
	for i, n := range nodes {
		if n.Phase == uiGraphEarlier {
			sumIdx = i
			break
		}
	}
	var drop []int
	for i := 0; i < len(nodes)-1 && len(drop) < want; i++ { // never the open, newest node
		n := nodes[i]
		if i == sumIdx || n.End.IsZero() || uiGraphKeep(n) {
			continue
		}
		drop = append(drop, i)
	}
	if len(drop) == 0 {
		return nodes
	}
	if sumIdx < 0 {
		*seq++
		first := nodes[drop[0]]
		nodes = append(nodes, uiGraphNode{})
		copy(nodes[drop[0]+1:], nodes[drop[0]:])
		nodes[drop[0]] = uiGraphNode{ID: "n" + strconv.Itoa(*seq), Session: first.Session, Phase: uiGraphEarlier, Start: first.Start, End: first.End, Cause: first.Cause}
		sumIdx = drop[0]
		for i := range drop {
			drop[i]++
		}
	}
	sum := &nodes[sumIdx]
	gone := map[int]bool{}
	for _, i := range drop {
		n := nodes[i]
		gone[i] = true
		c := n.Count
		if c < 1 {
			c = 1
		}
		if n.Earlier > 0 {
			c = n.Earlier
		}
		sum.Earlier += c
		sum.Cost += n.Cost
		sum.Tokens += n.Tokens
		if n.End.After(sum.End) {
			sum.End = n.End
		}
		if n.Start.Before(sum.Start) {
			sum.Start = n.Start
		}
	}
	sum.Summary = fmt.Sprintf("%d earlier steps", sum.Earlier)
	out := nodes[:0:0]
	for i, n := range nodes {
		if !gone[i] {
			out = append(out, n)
		}
	}
	return out
}

// ---- the agent's side ----

// currentPhase works out the phase now, without holding the agent's lock for the caller.
func (a *uiAgent) currentPhase() (phase string, tags []string, files []string) {
	a.mu.Lock()
	in := uiPhaseIn{State: a.state, StopReason: a.stopReason, OpenTool: a.openToolLocked()}
	files = a.sortedFilesLocked()
	a.mu.Unlock()
	in.Permission = len(a.pendingPermissions()) > 0
	in.Stuck = a.watch.current() != nil
	if v := a.harnessView(); v != nil {
		in.Harness = v.Status
	}
	phase, _, tags = uiPhase(in)
	return
}

// graphLoad brings in the task's stored graph the first time this run records anything.
func (a *uiAgent) graphLoad() {
	a.graph.mu.Lock()
	done := a.graph.loaded
	a.graph.mu.Unlock()
	if done {
		return
	}
	var st *uiTaskGraph
	var at time.Time
	if a.mgr != nil && a.req.taskRef != "" {
		var rec uiTaskRecord
		if uiDB().get("task", a.req.taskRef, &rec) {
			st, at = rec.Graph, rec.Updated
		}
	}
	a.graph.load(st, at)
	a.graph.mu.Lock()
	if len(a.graph.g.Nodes) > 0 && a.graph.firstCall == "" {
		a.graph.firstCall = "resumed"
		if a.req.originID != "" && a.req.originAgent != "" && a.req.originAgent != uiHarnessID(a.req.Harness) {
			a.graph.firstCall = "resumed-on-other-harness"
		}
	}
	a.graph.mu.Unlock()
}

// graphSync records where the task is now. Call it after something that may have changed the phase,
// with no lock of the agent's held. cause is the reason for the step if the phase did change.
func (a *uiAgent) graphSync(cause string, note ...string) {
	a.graphLoad()
	phase, tags, files := a.currentPhase()
	switch phase {
	case uiPhStuck:
		cause = "stuck-detected"
	case uiPhLimit:
		cause = "limit-hit"
	case uiPhHelper:
		cause = "helper-handed"
	case uiPhDone:
		cause = "finished"
	case uiPhFailed:
		cause = "failed"
	case uiPhStopped:
		cause = "stopped"
	}
	if a.graph.lastPhase() == uiPhHelper && phase != uiPhHelper {
		cause = "helper-returned"
	}
	n := uiGraphNote{}
	if len(note) > 0 {
		n.Summary = uiClip(a.tokens.redact(note[0]), uiGraphSummary)
	}
	for _, t := range tags {
		if strings.HasPrefix(t, "tool:") {
			n.Tool = strings.TrimPrefix(t, "tool:")
		}
	}
	if phase == uiPhNeedsYou && len(note) > 1 {
		n.Tool = note[1]
	}
	a.graph.mu.Lock()
	if a.graph.runNodes == 0 && a.graph.firstCall != "" {
		cause = a.graph.firstCall
	}
	a.graph.visitLocked(phase, tags, cause, n, files)
	a.graph.mu.Unlock()
}

// uiAddActs appends acts to a node, keeping the first few and the latest ones when there are too many.
func uiAddActs(n *uiGraphNode, acts []uiGraphAct) {
	for _, a := range acts {
		n.ActsN++
		n.Acts = append(n.Acts, a)
		if len(n.Acts) > uiGraphActMax {
			n.Acts = append(n.Acts[:uiGraphActHead], n.Acts[uiGraphActHead+1:]...)
		}
	}
}

// act records one thing the task did or said in its present state. A tool call waits for the state it
// belongs to (waiting on a tool) to open; how it ended is added to the state still open.
func (g *uiGraph) act(a uiGraphAct, toolEnd bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := len(g.g.Nodes)
	if k == 0 {
		return
	}
	if a.K == "tool" {
		g.pending = append(g.pending, a)
		if len(g.pending) > uiGraphActMax {
			g.pending = g.pending[len(g.pending)-uiGraphActMax:]
		}
		return
	}
	n := &g.g.Nodes[k-1]
	if toolEnd {
		for i := len(n.Acts) - 1; i >= 0; i-- {
			if n.Acts[i].K == "tool" && n.Acts[i].Ok == "" {
				n.Acts[i].Ok, n.Acts[i].R = a.Ok, a.R
				return
			}
		}
		return
	}
	uiAddActs(n, []uiGraphAct{a})
}

func uiToolShort(name string) string {
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		if server, tool, ok := strings.Cut(rest, "__"); ok {
			return tool + " on " + server
		}
	}
	return name
}

// graphAct turns an event the task produced into an act of the state it happened in.
func (a *uiAgent) graphAct(ev uiEvent) {
	a.graph.mu.Lock()
	ready := a.graph.loaded
	a.graph.mu.Unlock()
	if !ready {
		return
	}
	one := func(s string, n int) string {
		for _, line := range strings.Split(s, "\n") {
			if line = strings.Join(strings.Fields(line), " "); line != "" {
				return uiClip(line, n)
			}
		}
		return ""
	}
	act := uiGraphAct{T: ev.Time, K: ev.Kind}
	switch ev.Kind {
	case "tool":
		act.L = uiToolShort(ev.Tool)
		if t := one(ev.Text, 110); t != "" {
			act.L += ": " + t
		}
	case "tool_result":
		ok := "ok"
		if ev.Failed {
			ok = "fail"
		}
		a.graph.act(uiGraphAct{Ok: ok, R: one(ev.Text, 100)}, true)
		return
	case "text":
		act.K, act.L = "said", one(ev.Text, uiGraphActLabel)
	case "user":
		act.K, act.L = "you", one(ev.Text, uiGraphActLabel)
	case "plan":
		var items []struct {
			S string `json:"s"`
		}
		if json.Unmarshal([]byte(ev.Text), &items) != nil || len(items) == 0 {
			return
		}
		done := 0
		for _, it := range items {
			if it.S == "completed" {
				done++
			}
		}
		act.L = "Plan: " + strconv.Itoa(done) + " of " + strconv.Itoa(len(items)) + " done"
	case "error":
		act.L = one(ev.Text, uiGraphActLabel)
	case "note":
		if strings.HasPrefix(ev.Text, "Waiting for you to allow") {
			return // the state itself says so
		}
		act.L = one(ev.Text, uiGraphActLabel)
	case "result":
		act.K, act.L = "turn", "The turn finished"
		if ev.Failed {
			act.L = "The turn ended with an error"
		}
	default:
		return
	}
	if act.L == "" {
		return
	}
	a.graph.act(act, false)
}
