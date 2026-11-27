package cully

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Switching harness in the middle of a task. A task is many sessions across harnesses: when the one
// running it is out of tokens, or another suits the next step better, Cully waits for the current turn
// to end, saves where the work stands, and starts a new run on the chosen harness in the same worktree
// and branch with a handoff as its first prompt. The old run stays visible and read only.

var (
	uiSwitchTurnWait = 60 * time.Second // how long the current turn may take to end before it is stopped
	uiSwitchPoll     = 100 * time.Millisecond
	uiSwitchStopWait = 10 * time.Second // how long a stopped run may take to exit
)

// uiSwitchInput is what the handoff to the next harness is made of.
type uiSwitchInput struct {
	From, To string // harness ids
	Title    string
	Prompt   string // what the task was first asked
	Last     string // where it stood
	Files    []string
	Pack     string // the existing handoff: journal and live git state
	Note     string // what the user wants next
	Earlier  []uiTaskSession
}

// uiSwitchHandoff is the first prompt of the run that takes over. It is redacted by the caller.
func uiSwitchHandoff(in uiSwitchInput) string {
	var b strings.Builder
	b.WriteString("You are continuing a task another agent started. It was running in " + uiHarnessName(in.From) + " and has been handed to you in " + uiHarnessName(in.To) + ", in the same folder and on the same branch. Nothing has been undone.\n\n")
	b.WriteString("Task: " + in.Title + "\n")
	if in.Prompt != "" {
		b.WriteString("What it was asked: " + in.Prompt + "\n")
	}
	if in.Last != "" {
		b.WriteString("Where it stood: " + in.Last + "\n")
	}
	if len(in.Files) > 0 {
		shown := in.Files
		if len(shown) > 30 {
			shown = shown[:30]
		}
		b.WriteString("Files changed so far (" + uiItoa(len(in.Files)) + "): " + strings.Join(shown, ", ") + "\n")
	}
	for _, s := range in.Earlier {
		if s.Agent != "" && s.ID != "" {
			b.WriteString("An earlier " + uiHarnessName(s.Agent) + " session worked on it: " + s.ID + ".\n")
		}
	}
	if p := strings.TrimSpace(in.Pack); p != "" {
		b.WriteString("\n--- Where the work stands (from Cully) ---\n" + p + "\n")
	}
	b.WriteString("\nLook at the current state of the folder first. Then finish what is left, and run the checks before you say you are done.")
	if n := strings.TrimSpace(in.Note); n != "" {
		b.WriteString("\n\nWhat the user wants next: " + uiClip(n, 2000))
	}
	return b.String()
}

// taskLatestRun is the newest run of a task. id may be the task's id or the id of one of its runs.
func (m *uiManager) taskLatestRun(id string) (*uiAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[id]; ok {
		id = a.taskID()
	}
	var best *uiAgent
	for _, a := range m.agents {
		if a.taskID() == id && (best == nil || a.created.After(best.created) || (a.created.Equal(best.created) && a.id > best.id)) {
			best = a
		}
	}
	if best == nil {
		return nil, errors.New("this task has no run in this server session to switch; continue it instead")
	}
	return best, nil
}

// uiHarnessStartable reports why a task cannot start on a harness, or nil when it can.
func (m *uiManager) uiHarnessStartable(id string) error {
	req := uiStartRequest{Harness: id}
	if err := m.prepareHarness(&req); err != nil {
		return err
	}
	if uiHarnessID(id) == "claude" {
		if m.cfg.ClaudeBin == "" && claudeExecutable() == "" {
			return errors.New("Claude Code is not installed on this machine")
		}
	}
	return nil
}

// switchHarness moves a task to another harness and returns the run that now carries it.
func (m *uiManager) switchHarness(id, target, note string) (*uiAgent, error) {
	target = uiHarnessID(strings.ToLower(strings.TrimSpace(target)))
	a, err := m.taskLatestRun(id)
	if err != nil {
		return nil, err
	}
	from := uiHarnessID(a.req.Harness)
	if target == from {
		return nil, errors.New("this task already runs on " + uiHarnessName(from))
	}
	if err := m.uiHarnessStartable(target); err != nil {
		return nil, err
	}
	if until, ok := m.harnessExhaustion(target, time.Now()); ok {
		return nil, errors.New(uiHarnessName(target) + " is out of tokens until " + until.Local().Format("3:04 PM"))
	}
	if a.req.identityName != "" {
		return nil, errors.New("a named agent's run cannot change harness")
	}

	// Let the current turn end. A question about a tool would hold it forever, so it is refused first.
	a.denyPendingPermissions()
	deadline := time.Now().Add(uiSwitchTurnWait)
	for {
		st := a.snapshot().State
		if st != uiWorking && st != uiPreparing {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(uiSwitchPoll)
		a.denyPendingPermissions()
	}
	v := a.snapshot()
	a.stop() // the worktree must be quiet; a turn that did not end is cut off here
	select {
	case <-a.done:
	case <-time.After(uiSwitchStopWait):
		return nil, errors.New("the current run did not stop; try again")
	}

	var rec uiTaskRecord
	uiDB().get("task", a.taskID(), &rec)
	pack := uiStripANSI(uiContextPack(a.worktree))
	text := m.cfg.Tokens.redact(uiSwitchHandoff(uiSwitchInput{
		From: from, To: target, Title: a.title,
		Prompt: firstNonEmpty(rec.Prompt, uiClip(a.req.Prompt, uiTaskPrompt)),
		Last:   firstNonEmpty(rec.Last, v.Last), Files: v.Files, Pack: pack, Note: note, Earlier: rec.Sessions,
	}))

	a.mu.Lock()
	oldSession := a.sessionID
	a.mu.Unlock()
	req := uiStartRequest{
		Title: a.title, Prompt: text, Mode: a.req.Mode, Harness: target,
		MaxCost: a.req.MaxCost, MaxTurns: a.req.MaxTurns,
		taskRef: a.taskID(), originID: firstNonEmpty(oldSession, a.id), originAgent: from,
	}
	if a.req.inPlaceDir != "" || a.branch == "no git" {
		req.inPlaceDir = a.worktree
	} else {
		req.Repo, req.resumeDir = a.repo, a.worktree
	}
	n, err := m.start(req)
	if err != nil {
		return nil, err
	}
	m.recordSwitch(a, n, from, target)
	return n, nil
}

// recordSwitch writes the switch into the task: the old session ends with a reason and the new run's
// session is added with the role handoff, so the task shows its sessions across harnesses.
func (m *uiManager) recordSwitch(old, nw *uiAgent, from, to string) {
	uiTaskMu.Lock()
	defer uiTaskMu.Unlock()
	var rec uiTaskRecord
	if !uiDB().get("task", nw.taskID(), &rec) {
		return
	}
	now := time.Now().UTC()
	hadOld := false
	for i := range rec.Sessions {
		if rec.Sessions[i].Run == old.id {
			hadOld = true
			s := &rec.Sessions[i]
			s.Reason = "switched to " + uiHarnessName(to)
			if s.Ended.IsZero() {
				s.Ended = now
			}
			if uiLive(s.State) {
				s.State = uiStopped
			}
		}
	}
	if !hadOld { // the old run never named its session, but it is still one of the task's sessions
		old.mu.Lock()
		sid := old.sessionID
		old.mu.Unlock()
		rec.Sessions = append(rec.Sessions, uiTaskSession{ID: sid, Agent: from, At: old.created, Role: "run", Run: old.id, State: uiStopped, Ended: now, Reason: "switched to " + uiHarnessName(to)})
	}
	found := false
	for i := range rec.Sessions {
		if rec.Sessions[i].Run == nw.id {
			rec.Sessions[i].Role, found = "handoff", true
		}
	}
	if !found {
		rec.Sessions = append(rec.Sessions, uiTaskSession{Agent: to, At: now, Role: "handoff", Run: nw.id, State: uiPreparing})
	}
	sort.SliceStable(rec.Sessions, func(i, j int) bool { return rec.Sessions[i].At.Before(rec.Sessions[j].At) })
	rec.Synced = "" // what memory holds is out of date: the task moved to another harness
	uiDB().put("task", rec.ID, rec)
}

func (s *uiServer) taskSwitchH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Harness string `json:"harness"`
		Note    string `json:"note"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Harness) == "" {
		uiError(w, http.StatusBadRequest, "harness is required")
		return
	}
	a, err := s.m.switchHarness(r.PathValue("id"), body.Harness, body.Note)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, a.snapshot())
}
