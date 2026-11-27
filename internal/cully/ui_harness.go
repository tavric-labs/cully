package cully

import (
	"errors"
	"strings"
	"sync"
	"time"
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
	mu             sync.Mutex
	round          int
	max            int
	criteria       string
	status         string
	tools          int
	lastHash       string
	run            int
	steered        map[string]bool
	ids            map[string]string
	fails          map[string]int
	sinceCheck     bool
	stopWarned     bool
	finished       bool
	turnRunning    bool
	helpers        []string // named agents asked to review the finished work, in order
	helpersDone    map[string]bool
	criteriaPassed bool
	holdout        bool // the checks are held back: the agent hears the gap, never the check text
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
	return &uiHarness{max: max, criteria: uiClip(strings.TrimSpace(req.DoneWhen), 1000), holdout: req.Holdout, status: "working",
		steered: map[string]bool{}, ids: map[string]string{}, fails: map[string]int{}, helpersDone: map[string]bool{}, helpers: append([]string{}, req.Helpers...)}
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
	cause := map[string]string{"steering": "nudge", "reviewing": "helper-handed"}[status]
	if cause == "" {
		cause = "checks-started"
	}
	a.graphSync(cause)
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
	passed := h.criteriaPassed
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
		a.graphSync("checks-finished")
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
	case criteria != "" && !passed:
		a.harnessSet("judging")
		status, reason := a.harnessJudge(criteria)
		if status == "pass" {
			h.mu.Lock()
			h.criteriaPassed = true
			h.mu.Unlock()
			a.harnessNote("Checks met: " + reason)
			break // on to the reviewers, if there are any
		}
		if status == "error" {
			finish("stopped", "Could not judge the work against your checks. Look at it yourself.")
			return
		}
		a.harnessSet("steering")
		say("Not there yet (" + status + "): " + reason)
		if h.holdout {
			// Holdout: the agent cannot see the checks, so it cannot write to them. It hears only the gap.
			send("A reviewer who holds checks you cannot see found your work " + status + ": " + reason + "\nFix what is missing, then say what you did.")
		} else {
			send("A reviewer compared your work with the checks and found it " + status + ": " + reason + "\nChecks: " + criteria + "\nFix what is missing, then say what you did.")
		}
		return
	}
	// Reviewers: named agents asked to look at the finished work, one after another.
	for {
		h.mu.Lock()
		next := ""
		for _, id := range h.helpers {
			if !h.helpersDone[id] {
				next = id
				break
			}
		}
		if next != "" {
			h.helpersDone[next] = true
		}
		rounds := h.round
		h.mu.Unlock()
		if next == "" {
			break
		}
		a.harnessSet("reviewing")
		name, report, concerns, err := a.mgr.runHelper(next, a)
		a.graph.annotate("Review by " + name)
		switch {
		case err != nil:
			a.harnessNote("The reviewer could not look at the work: " + err.Error())
		case concerns && rounds < max:
			say(name + " reviewed the work and raised concerns. Passing them to the agent.")
			send("A reviewer (" + name + ") checked your work and raised concerns:\n" + uiClip(report, 3000) + "\nAddress them, then say what you changed.")
			return
		case concerns:
			a.harnessNote(name + " raised concerns but there are no rounds left: " + uiClip(report, 400))
		default:
			a.harnessNote(name + " reviewed the work and found nothing to fix.")
		}
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
	prompt := uiJudgePrompt
	if a.harness != nil && a.harness.holdout {
		prompt += "The agent must not learn the checks. In your reason, describe what is missing as behaviour a person could observe, and do not quote or list the checks.\n\n"
	}
	out, err := a.mgr.cfg.Advisor("claude", a.worktree, false, prompt+a.tokens.redact(data))
	if err != nil {
		return "error", ""
	}
	return uiParseJudge(out)
}

const uiHelperPrompt = `You are reviewing another agent's finished work. You are read only: do not edit files. Look at the goal, the files changed and the change below, and read the files in its worktree where you need to. Report concrete problems, each with the file and what is wrong: bugs, missing or weak tests, risky or unrelated changes, and security issues. If the work is sound, say so briefly. End with exactly one line: "VERDICT: ok" if there is nothing to fix, or "VERDICT: concerns" if something should be fixed.

`

// runHelper asks a named agent to review a task's work and waits for its report.
func (m *uiManager) runHelper(identID string, a *uiAgent) (name, report string, concerns bool, err error) {
	var ident uiIdentity
	found := false
	for _, x := range m.identityList() {
		if x.ID == identID {
			ident, found = x, true
		}
	}
	if !found || !ident.Approved {
		return "", "", false, errors.New("that agent is not available")
	}
	diff := a.diff()
	v := a.snapshot()
	prompt := uiHelperPrompt + "Goal the author was given:\n" + uiClip(a.req.Prompt, 2000) + "\n\nFiles changed: " + strings.Join(v.Files, ", ") + "\nThe author's worktree is " + a.worktree + ".\n\nChange:\n" + uiClip(diff, uiReviewDiff)
	run, _, err := m.identityRunWith(identID, uiRunOpts{NoMemory: true, Prompt: prompt, ReadOnly: true, AddDirs: []string{a.worktree}, Rules: uiReviewRules, Title: "Review of " + uiClipWords(a.title, 40)})
	if err != nil {
		return ident.Name, "", false, err
	}
	deadline := time.Now().Add(uiIdentityRunLimit)
	for time.Now().Before(deadline) {
		for _, x := range m.identityList() {
			if x.ID != identID {
				continue
			}
			for _, r := range x.Runs {
				if r.ID == run.ID && r.Status != "running" {
					if r.Status == "failed" || strings.TrimSpace(r.Summary) == "" {
						return ident.Name, "", false, errors.New("it did not produce a report")
					}
					low := strings.ToLower(r.Summary)
					return ident.Name, strings.TrimSpace(r.Summary), strings.Contains(low, "verdict: concerns"), nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ident.Name, "", false, errors.New("it took too long")
}
