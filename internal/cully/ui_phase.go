package cully

import "strings"

// A phase says in plain words what a task is doing right now. It is worked out from signals Cully
// already has, each time it is asked; the stored state string stays as it was.
const (
	uiPhQueued    = "queued"
	uiPhPreparing = "preparing"
	uiPhWorking   = "working"
	uiPhTool      = "tool"
	uiPhNeedsYou  = "needs-you"
	uiPhHelper    = "helper"
	uiPhChecking  = "checking"
	uiPhStuck     = "stuck"
	uiPhDone      = "done"
	uiPhFailed    = "failed"
	uiPhLimit     = "limit"
	uiPhStopped   = "stopped"
	uiPhClosed    = "closed"
)

// uiPhaseIn is every signal a phase is worked out from.
type uiPhaseIn struct {
	State      string // the stored state: preparing, working, waiting, done, failed or stopped
	StopReason string // cost_limit, turn_limit, ...; set when Cully stopped the task at a limit
	Permission bool   // a tool is waiting for your answer
	Stuck      bool   // the task is going in circles
	Harness    string // steering, verifying, judging or reviewing
	OpenTool   string // a tool call that has started and not yet returned
	Closed     bool   // you closed the task
	Queued     bool
}

var uiPhaseLabels = map[string]string{
	uiPhQueued: "Queued", uiPhPreparing: "Getting ready", uiPhWorking: "Working", uiPhTool: "Waiting on a tool",
	uiPhNeedsYou: "Needs you", uiPhHelper: "Waiting on a helper", uiPhChecking: "Checking", uiPhStuck: "Stuck",
	uiPhDone: "Done", uiPhFailed: "Failed", uiPhLimit: "Stopped by a limit", uiPhStopped: "Stopped", uiPhClosed: "Closed",
}

// uiPhase returns the phase, its label and a small set of tags. The first rule that matches wins.
func uiPhase(in uiPhaseIn) (phase, label string, tags []string) {
	phase = uiPhaseOf(in, &tags)
	label = uiPhaseLabels[phase]
	if phase == uiPhTool && in.OpenTool != "" {
		label += ": " + in.OpenTool
	}
	return phase, label, tags
}

func uiPhaseOf(in uiPhaseIn, tags *[]string) string {
	switch {
	case in.Closed:
		return uiPhClosed
	case in.Queued:
		return uiPhQueued
	case in.State == uiDone:
		return uiPhDone
	case in.State == uiFailed:
		return uiPhFailed
	case in.State == uiStopped:
		if r := strings.TrimSuffix(in.StopReason, "_limit"); in.StopReason != "" && in.StopReason != "restart" {
			*tags = []string{"limit:" + r}
			return uiPhLimit
		}
		if in.StopReason == "restart" {
			*tags = []string{"restart"}
		} else {
			*tags = []string{"user"}
		}
		return uiPhStopped
	case in.State == uiPreparing:
		*tags = []string{"agent"}
		return uiPhPreparing
	case in.Permission:
		*tags = []string{"user", "permission"}
		return uiPhNeedsYou
	case in.Stuck:
		*tags = []string{"agent"}
		return uiPhStuck
	case in.Harness == "reviewing":
		*tags = []string{"helper"}
		return uiPhHelper
	case in.Harness == "verifying":
		*tags = []string{"tests"}
		return uiPhChecking
	case in.Harness == "judging":
		*tags = []string{"judge"}
		return uiPhChecking
	case in.OpenTool != "":
		*tags = []string{"tool:" + in.OpenTool}
		return uiPhTool
	case in.State == uiWaiting:
		*tags = []string{"user"}
		return uiPhNeedsYou
	}
	*tags = []string{"agent"}
	if in.Harness == "steering" {
		*tags = []string{"agent", "nudge"}
	}
	return uiPhWorking
}
