package cully

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tasks that outlast the page. A task is the unit of work you care about, and it can be more than one
// session: the one Cully started, the one you resumed later, a session from Codex or Cursor it was
// continued from. Each task is kept in the local database (state, title, what it was last doing and
// every session it has had, with the harness of each), so today's and this week's tasks are still
// there after a restart and can be continued.

const (
	uiTaskKeepDays = 60
	uiTaskPrompt   = 400
	uiTaskLast     = 300
)

type uiTaskSession struct {
	ID    string    `json:"id"`
	Agent string    `json:"agent"` // claude, codex or cursor
	At    time.Time `json:"at"`
	Role  string    `json:"role"` // origin: where the task came from; run: a run Cully made
	// Added later; records stored before them load with these empty.
	Run   string    `json:"run,omitempty"`   // the Cully run that drives this session, which has a task page
	State string    `json:"state,omitempty"` // working, waiting, done, failed or stopped
	Ended time.Time `json:"ended,omitempty"` // when it last finished; zero while it is going
	Turns int       `json:"turns,omitempty"`
	Cost  float64   `json:"cost,omitempty"` // list price in dollars, when known
}

// The three groups the overview shows.
const (
	uiLaneRunning   = "running"
	uiLaneAttention = "attention"
	uiLaneFinished  = "finished"
)

type uiTaskRecord struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Repo     string          `json:"repo"`
	Branch   string          `json:"branch,omitempty"`
	Worktree string          `json:"worktree,omitempty"`
	State    string          `json:"state"`
	Created  time.Time       `json:"created"`
	Updated  time.Time       `json:"updated"`
	Prompt   string          `json:"prompt,omitempty"`
	Last     string          `json:"last,omitempty"`
	Files    int             `json:"files"`
	Runs     int             `json:"runs"` // how many times Cully has run it
	Sessions []uiTaskSession `json:"sessions"`
	Live     string          `json:"live,omitempty"` // the id of the run that is working now, if any
	Stop     string          `json:"stop,omitempty"` // cost_limit or turn_limit when Cully stopped it at a limit
	Status   string          `json:"status"`         // running, attention or finished: worked out when listed, never stored
	Why      string          `json:"why,omitempty"`  // in words, what needs attention
	Closed   bool            `json:"closed"`         // you closed it: done with, kept for the record
	ClosedAt time.Time       `json:"closedAt,omitempty"`
	Synced   string          `json:"synced,omitempty"`   // fingerprint of what was last saved to Cully memory
	SyncedAt time.Time       `json:"syncedAt,omitempty"` // when
	Entries  int             `json:"entries,omitempty"`  // summaries of it saved to memory

	Checkpoints []uiCheckpoint `json:"checkpoints,omitempty"` // Cully's per-turn snapshots of the worktree; old records have none
}

var uiTaskMu sync.Mutex

func (a *uiAgent) taskID() string {
	if a.req.taskRef != "" {
		return a.req.taskRef
	}
	return a.id
}

// saveTask writes the task a run belongs to. It reads only the run itself, so it is safe to call
// while the manager's lock is held.
func (m *uiManager) saveTask(a *uiAgent) {
	if a.req.identityName != "" && a.req.Title == "" { // a named agent's own run is kept with the agent, not as a task
		return
	}
	v := a.snapshot()
	a.mu.Lock()
	sid, turns, cost := a.sessionID, a.turns, a.cost
	a.mu.Unlock()
	uiTaskMu.Lock()
	defer uiTaskMu.Unlock()
	id := a.taskID()
	var rec uiTaskRecord
	if !uiDB().get("task", id, &rec) {
		rec = uiTaskRecord{ID: id, Title: v.Title, Repo: v.Repo, Created: v.Created, Prompt: uiClip(m.cfg.Tokens.redact(a.req.Prompt), uiTaskPrompt), Sessions: []uiTaskSession{}}
		if a.req.originID != "" {
			rec.Sessions = append(rec.Sessions, uiTaskSession{ID: a.req.originID, Agent: a.req.originAgent, At: v.Created, Role: "origin"})
		}
	}
	if a.req.taskRef != "" && rec.Closed {
		rec.Closed, rec.ClosedAt = false, time.Time{} // continuing a closed task reopens it
	}
	rec.State, rec.Branch, rec.Worktree, rec.Updated, rec.Files = v.State, v.Branch, v.Worktree, time.Now().UTC(), len(v.Files)
	if v.Last != "" {
		rec.Last = uiClip(m.cfg.Tokens.redact(v.Last), uiTaskLast)
	}
	rec.Stop = v.StopReason
	now := time.Now().UTC()
	idx := -1
	for i, s := range rec.Sessions {
		if s.Run == a.id || (sid != "" && s.ID == sid) {
			idx = i
			break
		}
	}
	if idx < 0 && sid != "" { // a new session: a run, a resume, or a handoff in another harness
		rec.Sessions = append(rec.Sessions, uiTaskSession{ID: sid, Agent: uiHarnessID(a.req.Harness), At: now, Role: "run"})
		idx = len(rec.Sessions) - 1
	}
	if idx >= 0 {
		s := &rec.Sessions[idx]
		s.Run, s.State, s.Turns, s.Cost = a.id, v.State, turns, cost
		if uiLive(v.State) {
			s.Ended = time.Time{}
		} else {
			s.Ended = now
		}
	}
	a.mu.Lock()
	if !a.counted { // each run counts once
		a.counted = true
		rec.Runs++
	}
	a.mu.Unlock()
	uiDB().put("task", id, rec)
}

// taskList is the tasks of a period, newest first. A task whose run is live shows that run's state.
func (m *uiManager) taskList(period string) []uiTaskRecord {
	var cut time.Time
	now := time.Now()
	switch period {
	case "today":
		y, mo, d := now.Date()
		cut = time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	case "week":
		cut = now.AddDate(0, 0, -7)
	default:
		cut = now.AddDate(0, 0, -uiTaskKeepDays)
	}
	live := map[string]uiAgentView{}
	m.mu.Lock()
	for _, a := range m.agents {
		if v := a.snapshot(); uiLive(v.State) {
			live[a.taskID()] = v
		}
	}
	m.mu.Unlock()
	out := []uiTaskRecord{}
	for _, raw := range uiDB().all("task") {
		var r uiTaskRecord
		if json.Unmarshal(raw, &r) != nil || r.ID == "" || r.Updated.Before(cut) || uiHidden(r.Repo, m.cfg.HideSessionsIn) {
			continue
		}
		v, isLive := live[r.ID]
		r.Live = v.ID
		if isLive && !uiLive(r.State) {
			r.State = uiWorking
		}
		if !isLive && uiLive(r.State) { // it was going when the server stopped, and nothing is running it now
			r.State, r.Stop = uiStopped, "restart"
			for i := range r.Sessions {
				if uiLive(r.Sessions[i].State) {
					r.Sessions[i].State, r.Sessions[i].Ended = uiStopped, r.Updated
				}
			}
		}
		r.Status, r.Why = uiTaskStatus(r, v, isLive)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// uiTaskStatus puts a task in one of three groups: running, needs attention or finished.
func uiTaskStatus(r uiTaskRecord, v uiAgentView, isLive bool) (status, why string) {
	if r.Closed {
		return uiLaneFinished, ""
	}
	if isLive {
		switch {
		case len(v.Permissions) > 0:
			return uiLaneAttention, "A tool is waiting for your approval"
		case v.Stuck != nil:
			return uiLaneAttention, "It looks stuck, going in circles"
		case v.State == uiWaiting:
			return uiLaneAttention, "Waiting for your reply"
		}
		return uiLaneRunning, ""
	}
	switch r.State {
	case uiFailed:
		return uiLaneAttention, "It failed"
	case uiWaiting:
		return uiLaneAttention, "Waiting for your reply"
	case uiStopped:
		switch r.Stop {
		case "cost_limit":
			return uiLaneAttention, "Stopped at the cost limit"
		case "turn_limit":
			return uiLaneAttention, "Stopped at the turn limit"
		case "restart":
			return uiLaneAttention, "Stopped when the server restarted"
		}
	}
	return uiLaneFinished, ""
}

// taskContinue runs the next step of a task. When its last Claude session can be resumed, that session
// carries on, so the conversation continues. Otherwise (the work began in another harness, or the
// session is gone) a new run starts from a handoff of the task, in the folder it worked in.
func (m *uiManager) taskContinue(id, next string) (*uiAgent, error) {
	var rec uiTaskRecord
	if !uiDB().get("task", id, &rec) {
		return nil, errors.New("no such task")
	}
	m.mu.Lock()
	for _, a := range m.agents {
		if a.taskID() == id && uiLive(a.snapshot().State) {
			m.mu.Unlock()
			return nil, errors.New("this task is already running")
		}
	}
	m.mu.Unlock()
	next = strings.TrimSpace(next)
	for i := len(rec.Sessions) - 1; i >= 0; i-- {
		if s := rec.Sessions[i]; s.Agent == "claude" && s.Role == "run" {
			if a, err := m.resumeSessionFor(s.ID, next, false, id); err == nil {
				return a, nil
			}
			break
		}
	}
	var b strings.Builder
	b.WriteString("Continue the task \"" + rec.Title + "\".\n")
	if rec.Prompt != "" {
		b.WriteString("What it was asked: " + rec.Prompt + "\n")
	}
	if rec.Last != "" {
		b.WriteString("Where it stood: " + rec.Last + "\n")
	}
	if rec.Files > 0 {
		b.WriteString("It had changed " + uiItoa(rec.Files) + " files.\n")
	}
	for _, s := range rec.Sessions {
		if s.Agent != "" && s.ID != "" {
			b.WriteString("An earlier " + uiHarnessName(s.Agent) + " session worked on it: " + s.ID + ".\n")
		}
	}
	b.WriteString("\nLook at the current state of the folder first. Then finish what is left, and run the checks before you say you are done.")
	if next != "" {
		b.WriteString("\n\nWhat the user wants next: " + uiClip(next, 2000))
	}
	req := uiStartRequest{Title: rec.Title, Prompt: b.String(), Context: true, taskRef: id}
	dir := rec.Worktree
	if dir == "" {
		dir = rec.Repo
	}
	if ok, _ := osStatDir(dir); !ok {
		dir = rec.Repo
	}
	if root, err := uiRepoRoot(dir); err == nil {
		info, err := m.addRepo(root)
		if err != nil {
			return nil, err
		}
		req.Repo = info.Path
	} else if ok, _ := osStatDir(dir); ok {
		req.inPlaceDir = dir
	} else {
		return nil, errors.New("the folder this task worked in is gone")
	}
	return m.start(req)
}

func (s *uiServer) tasksGet(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("range")
	if period != "" && period != "today" && period != "week" && period != "all" {
		uiError(w, http.StatusBadRequest, "range must be today, week or all")
		return
	}
	uiJSON(w, http.StatusOK, s.m.taskList(period))
}

func (s *uiServer) taskContinueH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if r.ContentLength > 0 && !uiDecode(w, r, &body) {
		return
	}
	a, err := s.m.taskContinue(r.PathValue("id"), body.Prompt)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, a.snapshot())
}

// taskClose closes a task, or reopens it. Closing keeps everything (sessions, summary, what was saved to
// memory) and takes it off the open list; a run that is still going is stopped first.
func (m *uiManager) taskClose(id string, closed bool) error {
	uiTaskMu.Lock()
	var rec uiTaskRecord
	ok := uiDB().get("task", id, &rec)
	uiTaskMu.Unlock()
	if !ok {
		return errors.New("no such task")
	}
	if closed {
		m.mu.Lock()
		var live []*uiAgent
		for _, a := range m.agents {
			if a.taskID() == id && uiLive(a.snapshot().State) {
				live = append(live, a)
			}
		}
		m.mu.Unlock()
		for _, a := range live {
			a.stop()
		}
	}
	uiTaskMu.Lock()
	defer uiTaskMu.Unlock()
	if !uiDB().get("task", id, &rec) {
		return errors.New("no such task")
	}
	rec.Closed = closed
	if closed {
		rec.ClosedAt = time.Now().UTC()
	} else {
		rec.ClosedAt = time.Time{}
	}
	rec.Synced = "" // what memory holds is out of date: the task changed
	uiDB().put("task", id, rec)
	return nil
}

func (s *uiServer) taskCloseH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Closed bool `json:"closed"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := s.m.taskClose(r.PathValue("id"), body.Closed); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
