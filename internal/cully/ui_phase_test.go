package cully

import (
	"reflect"
	"testing"
)

func TestUIPhase(t *testing.T) {
	cases := []struct {
		name  string
		in    uiPhaseIn
		phase string
		label string
		tags  []string
	}{
		{"queued", uiPhaseIn{Queued: true}, "queued", "Queued", nil},
		{"preparing", uiPhaseIn{State: uiPreparing}, "preparing", "Getting ready", []string{"agent"}},
		{"working", uiPhaseIn{State: uiWorking}, "working", "Working", []string{"agent"}},
		{"tool", uiPhaseIn{State: uiWorking, OpenTool: "Bash"}, "tool", "Waiting on a tool: Bash", []string{"tool:Bash"}},
		{"permission beats tool", uiPhaseIn{State: uiWorking, OpenTool: "Bash", Permission: true}, "needs-you", "Needs you", []string{"user", "permission"}},
		{"turn over", uiPhaseIn{State: uiWaiting}, "needs-you", "Needs you", []string{"user"}},
		{"helper", uiPhaseIn{State: uiWaiting, Harness: "reviewing"}, "helper", "Waiting on a helper", []string{"helper"}},
		{"verifying", uiPhaseIn{State: uiWaiting, Harness: "verifying"}, "checking", "Checking", []string{"tests"}},
		{"judging", uiPhaseIn{State: uiWorking, Harness: "judging"}, "checking", "Checking", []string{"judge"}},
		{"steering is working", uiPhaseIn{State: uiWorking, Harness: "steering"}, "working", "Working", []string{"agent"}},
		{"stuck", uiPhaseIn{State: uiWorking, Stuck: true, OpenTool: "Bash"}, "stuck", "Stuck", []string{"agent"}},
		{"done", uiPhaseIn{State: uiDone, Permission: true}, "done", "Done", nil},
		{"failed", uiPhaseIn{State: uiFailed}, "failed", "Failed", nil},
		{"limit", uiPhaseIn{State: uiStopped, StopReason: "cost_limit"}, "limit", "Stopped by a limit", []string{"limit:cost"}},
		{"turn limit", uiPhaseIn{State: uiStopped, StopReason: "turn_limit"}, "limit", "Stopped by a limit", []string{"limit:turn"}},
		{"stopped", uiPhaseIn{State: uiStopped}, "stopped", "Stopped", []string{"user"}},
		{"restart", uiPhaseIn{State: uiStopped, StopReason: "restart"}, "stopped", "Stopped", []string{"restart"}},
		{"closed", uiPhaseIn{State: uiDone, Closed: true}, "closed", "Closed", nil},
	}
	for _, c := range cases {
		p, l, tg := uiPhase(c.in)
		if p != c.phase || l != c.label || !reflect.DeepEqual(tg, c.tags) {
			t.Errorf("%s: got %q %q %v", c.name, p, l, tg)
		}
	}
}

func TestSnapshotPhaseOpenTool(t *testing.T) {
	a := &uiAgent{state: uiWorking, uses: map[string]uiToolUse{}, files: map[string]struct{}{}, tokens: &uiTokenPool{}, notify: make(chan struct{})}
	a.handleLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`))
	if v := a.snapshot(); v.Phase != "tool" || v.PhaseLabel != "Waiting on a tool: Bash" {
		t.Fatalf("got %q %q", v.Phase, v.PhaseLabel)
	}
	a.handleLine([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`))
	if v := a.snapshot(); v.Phase != "working" {
		t.Fatalf("got %q", v.Phase)
	}
}
