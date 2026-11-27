package cully

import (
	"errors"
	"strings"
	"time"
)

// The queue holds tasks that wait for a free agent slot. It lives in memory:
// restarting the page's server clears it.

const (
	uiQueueMax  = 20
	uiFanoutMin = 2
	uiFanoutMax = 4
)

type uiQueued struct {
	id, title, repo string
	added           time.Time
	req             uiStartRequest
	err             string
}

type uiQueuedView struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Repo   string    `json:"repo"`
	Prompt string    `json:"prompt"` // clipped
	Added  time.Time `json:"added"`
	Group  string    `json:"group,omitempty"`
	Option string    `json:"option,omitempty"`
	Error  string    `json:"error,omitempty"`
}

func (q *uiQueued) view() uiQueuedView {
	return uiQueuedView{ID: q.id, Title: q.title, Repo: q.repo, Prompt: uiClip(q.req.Prompt, 300), Added: q.added, Group: q.req.group, Option: q.req.option, Error: q.err}
}

func (m *uiManager) hasRoom() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveCount() < m.cfg.MaxAgents
}

// validateQueued applies the checks start would, so a task that can never run
// is refused when it is queued rather than failing later.
func (m *uiManager) validateQueued(req *uiStartRequest) error {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return errors.New("prompt is required")
	}
	if len([]rune(req.Prompt)) > uiMaxPromptRunes {
		return errors.New("prompt is too long")
	}
	if req.Mode != "" && req.Mode != "edit" && req.Mode != "plan" {
		return errors.New(`mode must be "edit" or "plan"`)
	}
	if req.Model != "" && !uiModelName.MatchString(req.Model) {
		return errors.New("invalid model name")
	}
	repo, err := m.resolveRepo(req.Repo)
	if err != nil {
		return err
	}
	req.Repo = repo
	return nil
}

func uiQueueTitle(req uiStartRequest) string {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = req.Prompt
	}
	if r := []rune(title); len(r) > 60 {
		title = string(r[:60]) + "…"
	}
	return title
}

func (m *uiManager) enqueue(req uiStartRequest) (uiQueuedView, error) {
	if err := m.validateQueued(&req); err != nil {
		return uiQueuedView{}, err
	}
	m.qmu.Lock()
	if len(m.queue) >= uiQueueMax {
		m.qmu.Unlock()
		return uiQueuedView{}, errors.New("the queue is full")
	}
	item := &uiQueued{id: uiNewID(), title: uiQueueTitle(req), repo: req.Repo, added: time.Now().UTC(), req: req}
	m.queue = append(m.queue, item)
	m.qmu.Unlock()
	v := item.view()
	m.drainQueue()
	return v, nil
}

func (m *uiManager) queueList() []uiQueuedView {
	m.qmu.Lock()
	defer m.qmu.Unlock()
	out := make([]uiQueuedView, 0, len(m.queue))
	for _, q := range m.queue {
		out = append(out, q.view())
	}
	return out
}

func (m *uiManager) dequeue(id string) error {
	m.qmu.Lock()
	defer m.qmu.Unlock()
	for i, q := range m.queue {
		if q.id == id {
			m.queue = append(m.queue[:i:i], m.queue[i+1:]...)
			return nil
		}
	}
	return errors.New("no such queued task")
}

// startQueued starts one waiting task now. It fails when there is no room.
func (m *uiManager) startQueued(id string) (*uiAgent, error) {
	m.qmu.Lock()
	var item *uiQueued
	for _, q := range m.queue {
		if q.id == id {
			item = q
		}
	}
	m.qmu.Unlock()
	if item == nil {
		return nil, errors.New("no such queued task")
	}
	a, err := m.start(item.req)
	if err != nil {
		return nil, err
	}
	_ = m.dequeue(id)
	return a, nil
}

// drainQueue starts waiting tasks, oldest first, while there is room.
func (m *uiManager) drainQueue() {
	if m.paused() {
		return
	}
	for m.hasRoom() {
		m.qmu.Lock()
		var item *uiQueued
		for _, q := range m.queue {
			if q.err == "" {
				item = q
				break
			}
		}
		m.qmu.Unlock()
		if item == nil {
			return
		}
		if _, err := m.start(item.req); err != nil {
			if !m.hasRoom() {
				return
			}
			m.qmu.Lock()
			item.err = err.Error()
			m.qmu.Unlock()
			continue
		}
		_ = m.dequeue(item.id)
	}
}

type uiFanoutResult struct {
	Group   string `json:"group"`
	Started int    `json:"started"`
	Queued  int    `json:"queued"`
}

// fanout runs the same task as count options, each on its own branch, so the
// results can be compared. Options that do not fit wait in the queue.
func (m *uiManager) fanout(req uiStartRequest, count int) (uiFanoutResult, error) {
	if count < uiFanoutMin || count > uiFanoutMax {
		return uiFanoutResult{}, errors.New("choose between 2 and 4 options")
	}
	if err := m.validateQueued(&req); err != nil {
		return uiFanoutResult{}, err
	}
	base := uiQueueTitle(req)
	res := uiFanoutResult{Group: uiNewID()}
	for i := 0; i < count; i++ {
		opt := string(rune('A' + i))
		r := req
		r.Title, r.group, r.option = base+" (option "+opt+")", res.Group, opt
		if m.hasRoom() {
			if _, err := m.start(r); err == nil {
				res.Started++
				continue
			}
		}
		m.qmu.Lock()
		if len(m.queue) >= uiQueueMax {
			m.qmu.Unlock()
			return res, errors.New("the queue is full")
		}
		m.queue = append(m.queue, &uiQueued{id: uiNewID(), title: r.Title, repo: r.Repo, added: time.Now().UTC(), req: r})
		m.qmu.Unlock()
		res.Queued++
	}
	return res, nil
}
