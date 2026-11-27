package cully

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGraphMermaidCollapsesRepeats(t *testing.T) {
	g, _ := testGraph()
	g.visit("preparing", []string{"agent"}, "started", uiGraphNote{}, nil)
	for i := 0; i < 3; i++ {
		g.visit("working", []string{"agent"}, "tool-finished", uiGraphNote{}, nil)
		g.visit("tool", []string{"tool:Bash"}, "tool-started", uiGraphNote{}, nil)
	}
	g.visit("needs-you", []string{"user"}, "turn-ended", uiGraphNote{}, nil)
	got := uiGraphMermaid(g.copy().Nodes)
	want := `stateDiagram-v2
    state "Needs you" as needs_you
    state "Getting ready" as preparing
    state "Waiting on a tool" as tool
    state "Working" as working
    [*] --> preparing
    preparing --> working: tool-finished
    working --> tool: tool-started x3
    tool --> working: tool-finished x2
    tool --> needs_you: turn-ended
`
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestGraphViewLanesAndCrossEdges(t *testing.T) {
	g, _ := testGraph()
	g.session = "aaaaaaaaaaaa"
	g.visit("working", nil, "started", uiGraphNote{}, nil)
	g.session = "bbbbbbbbbbbb"
	g.visit("working", []string{"x"}, "resumed-on-other-harness", uiGraphNote{}, nil)
	v := uiBuildGraph("t", g.copy(), []uiTaskSession{{ID: "bbbbbbbbbbbb", Agent: "codex"}}, false)
	if len(v.Lanes) != 2 || v.Lanes[1].Agent != "codex" || len(v.Edges) != 1 || !v.Edges[0].Cross || v.Edges[0].Reason != "resumed-on-other-harness" {
		t.Fatalf("%+v", v)
	}
}

func TestGraphEndpointForStoredTask(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok")
	st := uiTaskGraph{Seq: 1, Nodes: []uiGraphNode{{ID: "n1", Phase: "working", Start: time.Now().Add(-time.Minute), Count: 1}}}
	uiDB().put("task", "gtask", uiTaskRecord{ID: "gtask", Title: "x", State: "stopped", Updated: time.Now(), Graph: &st})
	s := &uiServer{m: m}
	req := httptest.NewRequest("GET", "/api/tasks/gtask/graph", nil)
	req.SetPathValue("id", "gtask")
	rec := httptest.NewRecorder()
	s.taskGraph(rec, req)
	var v uiGraphView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil || len(v.Nodes) != 1 || v.Nodes[0].Ended != "restart" || !strings.Contains(v.Note, "not a graph Cully runs") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
