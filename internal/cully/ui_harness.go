package cully

import (
	"strings"
	"sync"
)

// The harness is what Cully adds around the model when an agent is started
// with "keep going until it is checked". After each turn it looks at what the
// agent did and decides what to say next, the way coding-agent harnesses do
// (see "Harness Engineering", arXiv 2609.00006): a verify-on-stop guard, a
// warn-first loop detector, an iteration budget and a judge against the
// stated checks. Every message it sends is shown in the feed as a note.

const (
	uiHarnessDefaultRounds = 3
	uiHarnessMaxRounds     = 6
	uiHarnessToolBudget    = 200 // tool calls before the harness stops the agent
	uiHarnessRepeatWarn    = 3   // identical calls in a row before it steers
	uiHarnessRepeatStop    = 8
)

type uiHarness struct {
	mu          sync.Mutex
	round       int
	max         int
	criteria    string
	status      string
	tools       int
	lastHash    string
	run         int
	steered     map[string]bool
	ids         map[string]string
	fails       map[string]int
	sinceCheck  bool
	stopWarned  bool
	finished    bool
	turnRunning bool
}

type uiHarnessView struct {
	Round  int    `json:"round"`
	Max    int    `json:"max"`
	Status string `json:"status"`
}

func newUIHarness(req uiStartRequest) *uiHarness {
	max := req.MaxRounds
	if max <= 0 {
		max = uiHarnessDefaultRounds
	}
	if max > uiHarnessMaxRounds {
		max = uiHarnessMaxRounds
	}
	return &uiHarness{max: max, criteria: uiClip(strings.TrimSpace(req.DoneWhen), 1000), status: "working",
		steered: map[string]bool{}, ids: map[string]string{}, fails: map[string]int{}}
}

func (a *uiAgent) harnessView() *uiHarnessView {
	if a.harness == nil {
		return nil
	}
	a.harness.mu.Lock()
	defer a.harness.mu.Unlock()
	return &uiHarnessView{Round: a.harness.round, Max: a.harness.max, Status: a.harness.status}
}

// harnessUse is called for every tool call the agent makes.
func (a *uiAgent) harnessUse(id string, u uiToolUse) {
	h := a.harness
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tools++
	hash := u.Name + "|" + u.Sig + "|" + u.Path + "|" + u.Command
	if hash == h.lastHash {
		h.run++
	} else {
		h.lastHash, h.run = hash, 1
	}
	if id != "" {
		h.ids[id] = hash
	}
	switch journalClassFor(u.Name, u.Command) {
	case journalEdit:
		h.sinceCheck = true
	case journalCheck:
		h.sinceCheck = false
	}
}

func (a *uiAgent) harnessResult(id string, failed bool) {
	h := a.harness
	if h == nil || !failed {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if hash, ok := h.ids[id]; ok {
		h.fails[hash]++
	}
}

func (a *uiAgent) harnessNote(text string) { a.add(uiEvent{Kind: "system", Text: "Cully: " + text}) }

func (a *uiAgent) harnessSet(status string) {
	a.harness.mu.Lock()
	a.harness.status = status
	a.harness.mu.Unlock()
}

// harnessAfterTurn decides what to do when the agent finishes a turn.
func (a *uiAgent) harnessAfterTurn() {
	h := a.harness
	if h == nil || a.isStopped() {
		return
	}
	h.mu.Lock()
	if h.finished {
		h.mu.Unlock()
		return
	}
	tools, run, last := h.tools, h.run, h.lastHash
	maxFail := 0
	failedHash := ""
	for k, n := range h.fails {
		if n > maxFail {
			maxFail, failedHash = n, k
		}
	}
	steeredRepeat := h.steered[last]
	steeredFail := h.steered[failedHash]
	needVerify := h.sinceCheck
	round, max, criteria := h.round, h.max, h.criteria
	h.mu.Unlock()

	say := func(msg string) {
		a.harness.mu.Lock()
		a.harness.round++
		a.harness.mu.Unlock()
		a.harnessNote(msg)
	}
	finish := func(status, note string) {
		a.harness.mu.Lock()
		a.harness.status, a.harness.finished = status, true
		a.harness.mu.Unlock()
		a.harnessNote(note)
	}
	send := func(text string) { _ = a.send(text) }

	switch {
	case tools >= uiHarnessToolBudget:
		finish("stopped", "Stopped after "+uiItoa(tools)+" tool calls. That is the limit for one task; take a look at where it got to.")
		a.stop()
		return
	case run >= uiHarnessRepeatStop:
		finish("stopped", "Stopped: the agent made the same call "+uiItoa(run)+" times in a row.")
		a.stop()
		return
	case round >= max:
		finish("stopped", "Stopped after "+uiItoa(max)+" rounds. Take a look at what it did.")
		return
	case run >= uiHarnessRepeatWarn && !steeredRepeat:
		h.mu.Lock()
		h.steered[last] = true
		h.mu.Unlock()
		a.harnessSet("steering")
		say("The agent repeated the same call " + uiItoa(run) + " times. Asking it to try a different approach.")
		send("You have made the same call " + uiItoa(run) + " times in a row. Stop repeating it. Say what is blocking you and try a different approach.")
		return
	case maxFail >= uiHarnessRepeatWarn && !steeredFail:
		h.mu.Lock()
		h.steered[failedHash] = true
		h.mu.Unlock()
		a.harnessSet("steering")
		say("The same call failed " + uiItoa(maxFail) + " times. Asking the agent to change course.")
		send("The same call has failed " + uiItoa(maxFail) + " times. Read the error carefully, find the cause, and try something different rather than running it again.")
		return
	case needVerify:
		h.mu.Lock()
		h.sinceCheck = false // asked once; the next edit sets it again
		h.mu.Unlock()
		a.harnessSet("verifying")
		say("The agent changed files and ran no checks. Asking it to verify.")
		send("You changed files but did not run any checks afterwards. Run the project's tests (or the most relevant check), report the result, and fix anything that fails.")
		return
	case criteria != "":
		a.harnessSet("judging")
		status, reason := a.harnessJudge(criteria)
		if status == "pass" {
			finish("done", "Checks met: "+reason)
			return
		}
		if status == "error" {
			finish("stopped", "Could not judge the work against your checks. Look at it yourself.")
			return
		}
		a.harnessSet("steering")
		say("Not there yet (" + status + "): " + reason)
		send("A reviewer compared your work with the checks and found it " + status + ": " + reason + "\nChecks: " + criteria + "\nFix what is missing, then say what you did.")
		return
	}
	finish("done", "The agent finished and ran its checks.")
}

func (a *uiAgent) harnessJudge(criteria string) (string, string) {
	last := ""
	for _, ev := range a.allEvents() {
		if ev.Kind == "text" || ev.Kind == "result" {
			last = ev.Text
		}
	}
	v := a.snapshot()
	data := "Goal: " + uiClipWords(a.req.Prompt, 400) + "\nChecks: " + criteria + "\nFiles the agent changed: " + strings.Join(v.Files, ", ") + "\nLast reply: " + uiClipWords(last, 800) + "\n"
	if a.mgr == nil {
		return "error", ""
	}
	out, err := a.mgr.cfg.Advisor("claude", a.worktree, false, uiJudgePrompt+a.tokens.redact(data))
	if err != nil {
		return "error", ""
	}
	return uiParseJudge(out)
}
