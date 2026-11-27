package cully

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testGraph() (*uiGraph, *time.Time) {
	t0 := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	g := &uiGraph{}
	g.now = func() time.Time { t0 = t0.Add(time.Second); return t0 }
	return g, &t0
}

func TestGraphCoalesceAndEdges(t *testing.T) {
	g, _ := testGraph()
	g.visit("preparing", []string{"agent"}, "started", uiGraphNote{}, nil)
	g.visit("working", []string{"agent"}, "state", uiGraphNote{}, nil)
	g.visit("working", []string{"agent"}, "state", uiGraphNote{}, nil)
	g.visit("tool", []string{"tool:Bash"}, "tool-started", uiGraphNote{Tool: "Bash", Summary: "ls"}, nil)
	g.visit("tool", []string{"tool:Bash"}, "tool-started", uiGraphNote{Tool: "Bash"}, nil)
	g.visit("working", []string{"agent"}, "tool-finished", uiGraphNote{}, []string{"a.go"})
	c := g.copy()
	if len(c.Nodes) != 4 {
		t.Fatalf("nodes %d", len(c.Nodes))
	}
	if c.Nodes[1].Count != 2 || c.Nodes[2].Count != 2 || c.Nodes[2].Tool != "Bash" || c.Nodes[2].Summary != "ls" {
		t.Fatalf("%+v", c.Nodes)
	}
	if c.Nodes[2].Changed != "Edited a.go" && c.Nodes[3].Changed != "Edited a.go" {
		t.Fatalf("changed not noted: %+v", c.Nodes)
	}
	if c.Nodes[3].Cause != "tool-finished" || c.Nodes[0].Cause != "started" || c.Nodes[0].End.IsZero() {
		t.Fatalf("%+v", c.Nodes)
	}
	if !c.Nodes[3].End.IsZero() {
		t.Fatal("newest node should be open")
	}
}

func TestGraphPermissionPair(t *testing.T) {
	g, _ := testGraph()
	g.visit("tool", []string{"tool:Bash"}, "tool-started", uiGraphNote{Tool: "Bash"}, nil)
	g.visit("needs-you", []string{"user", "permission"}, "permission-asked", uiGraphNote{Tool: "Bash", Summary: "rm -rf x"}, nil)
	g.answer("denied")
	g.visit("tool", []string{"tool:Bash"}, "answered-deny", uiGraphNote{Tool: "Bash"}, nil)
	n := g.copy().Nodes
	if len(n) != 3 || n[1].Answer != "denied" || n[2].Cause != "answered-deny" || n[1].Cause != "permission-asked" || n[1].End.IsZero() {
		t.Fatalf("%+v", n)
	}
}

func TestGraphCapMergesOldestKeepsImportant(t *testing.T) {
	g, _ := testGraph()
	g.visit("needs-you", []string{"user", "permission"}, "permission-asked", uiGraphNote{}, nil)
	for i := 0; i < uiGraphCap+30; i++ {
		ph, tg := "working", []string{"agent"}
		if i%2 == 1 {
			ph, tg = "tool", []string{"tool:Bash"}
		}
		g.visit(ph, tg, "x", uiGraphNote{}, nil)
	}
	n := g.copy().Nodes
	if len(n) > uiGraphCap {
		t.Fatalf("over cap: %d", len(n))
	}
	if n[0].Phase != "needs-you" {
		t.Fatalf("permission node lost: %+v", n[0])
	}
	var sum *uiGraphNode
	for i := range n {
		if n[i].Phase == uiGraphEarlier {
			sum = &n[i]
		}
	}
	if sum == nil || sum.Earlier < 30 || !strings.Contains(sum.Summary, "earlier steps") {
		t.Fatalf("no summary: %+v", sum)
	}
	if !n[len(n)-1].End.IsZero() {
		t.Fatal("open node was merged")
	}
}

func TestGraphRestartRoundTrip(t *testing.T) {
	g, _ := testGraph()
	g.visit("working", []string{"agent"}, "started", uiGraphNote{}, nil)
	st := g.copy()
	raw, _ := json.Marshal(&uiTaskRecord{ID: "x", Graph: &st})
	var rec uiTaskRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	var g2 uiGraph
	g2.load(rec.Graph, time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC))
	n := g2.copy().Nodes
	if len(n) != 1 || n[0].End.IsZero() || n[0].Ended != "restart" {
		t.Fatalf("%+v", n)
	}
	var old uiTaskRecord // a record saved before graphs existed
	if err := json.Unmarshal([]byte(`{"id":"o","title":"t","state":"done"}`), &old); err != nil || old.Graph != nil {
		t.Fatal("old record")
	}
	var g3 uiGraph
	g3.load(old.Graph, time.Now())
	if len(g3.copy().Nodes) != 0 {
		t.Fatal("expected empty")
	}
}

func TestGraphSessionLane(t *testing.T) {
	g, _ := testGraph()
	g.visit("preparing", []string{"agent"}, "started", uiGraphNote{}, nil)
	g.setSession("s1")
	g.visit("working", []string{"agent"}, "state", uiGraphNote{}, nil)
	n := g.copy().Nodes
	if n[0].Session != "s1" || n[1].Session != "s1" {
		t.Fatalf("%+v", n)
	}
}

func TestAgentGraphHooks(t *testing.T) {
	a := &uiAgent{state: uiPreparing, uses: map[string]uiToolUse{}, files: map[string]struct{}{}, tokens: &uiTokenPool{}, notify: make(chan struct{})}
	a.graphSync("started")
	a.setState(uiWorking)
	a.handleLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`))
	a.handleLine([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`))
	a.setState(uiDone)
	var got []string
	for _, n := range a.graph.copy().Nodes {
		got = append(got, n.Phase+":"+n.Cause)
	}
	want := "preparing:started working:state tool:tool-started working:tool-finished done:finished"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
}

func TestActsLandInTheStateTheyBelongTo(t *testing.T) {
	g := &uiGraph{}
	g.loaded = true
	g.visit(uiPhWorking, nil, "started", uiGraphNote{}, nil)
	g.act(uiGraphAct{K: "said", L: "Looking at the tests"}, false)
	g.act(uiGraphAct{K: "tool", L: "Bash: go test ./..."}, false) // seen just before its state opens
	g.visit(uiPhTool, []string{"tool:Bash"}, "tool-started", uiGraphNote{Tool: "Bash"}, nil)
	g.act(uiGraphAct{Ok: "fail", R: "2 tests failed"}, true)
	g.visit(uiPhWorking, nil, "tool-finished", uiGraphNote{}, nil)
	g.act(uiGraphAct{K: "said", L: "Two tests fail, fixing"}, false)
	n := g.g.Nodes
	if len(n) != 3 {
		t.Fatalf("nodes = %d", len(n))
	}
	if len(n[0].Acts) != 1 || n[0].Acts[0].L != "Looking at the tests" {
		t.Errorf("first state: %+v", n[0].Acts)
	}
	if len(n[1].Acts) != 1 || n[1].Acts[0].K != "tool" || n[1].Acts[0].Ok != "fail" || n[1].Acts[0].R != "2 tests failed" {
		t.Errorf("the tool state should hold the call and how it ended: %+v", n[1].Acts)
	}
	if len(n[2].Acts) != 1 || n[2].Acts[0].L != "Two tests fail, fixing" {
		t.Errorf("last state: %+v", n[2].Acts)
	}
}

func TestActsPerStateAreCappedKeepingTheFirstAndTheLatest(t *testing.T) {
	n := &uiGraphNode{}
	for i := 0; i < 100; i++ {
		uiAddActs(n, []uiGraphAct{{K: "said", L: strconv.Itoa(i)}})
	}
	if len(n.Acts) != uiGraphActMax || n.ActsN != 100 {
		t.Fatalf("kept %d of %d", len(n.Acts), n.ActsN)
	}
	if n.Acts[0].L != "0" || n.Acts[len(n.Acts)-1].L != "99" || n.Acts[uiGraphActHead-1].L != strconv.Itoa(uiGraphActHead-1) {
		t.Errorf("first and latest must survive: %v ... %v", n.Acts[0].L, n.Acts[len(n.Acts)-1].L)
	}
}
