package cully

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Messages to a task that is busy. While a task is working you can send a message two ways:
// "steer" delivers it as soon as it is safe, and "followup" holds it until the turn ends. A harness
// that cannot take input mid-turn gets steer as follow-up, and the page says so. The queue belongs to
// one task and is separate from the manager's queue of tasks waiting for a free slot. A task that is
// stopped or has finished drops its queue: nothing is sent to it afterwards.

const (
	uiModeSteer    = "steer"
	uiModeFollowup = "followup"
	uiMaxQueued    = 20
)

// uiMsg is one queued message.
type uiMsg struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	Mode       string `json:"mode"` // steer or followup
	At         int64  `json:"at"`   // unix seconds
	Sent       bool   `json:"sent"` // a steer already written to the harness, applied at its next step
	Downgraded bool   `json:"downgraded,omitempty"`
	Note       string `json:"note,omitempty"`
}

type uiMsgQueue struct {
	mu    sync.Mutex
	items []uiMsg
}

func (q *uiMsgQueue) list() []uiMsg {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]uiMsg(nil), q.items...)
}

// canSteerMidTurn reports whether this task's harness takes a message while a turn is running. Claude
// Code in stream-json mode queues one itself; the capability table says the same for the others.
func (a *uiAgent) canSteerMidTurn() bool {
	return uiClaudeHas("--input-format") // tasks run in Claude today
}

// publishQueue tells the page the queue changed, as an event the feed does not draw.
func (a *uiAgent) publishQueue() {
	data, _ := json.Marshal(a.queueView())
	a.add(uiEvent{Kind: "queue_update", Text: string(data)})
}

func (a *uiAgent) queueView() []uiMsg {
	return a.mq.list()
}

// dropQueue empties the queue when the task ends.
func (a *uiAgent) dropQueue() {
	a.mq.mu.Lock()
	n := len(a.mq.items)
	a.mq.items = nil
	a.mq.mu.Unlock()
	if n > 0 {
		a.publishQueue()
	}
}

func (a *uiAgent) busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state == uiWorking || a.state == uiPreparing
}

// uiSubmit is the result of handing a message to a task.
type uiSubmit struct {
	Status string `json:"status"` // sent, sent_midturn or queued
	Item   *uiMsg `json:"item,omitempty"`
	Note   string `json:"note,omitempty"`
}

// submit sends a message the way the mode asks. mode is steer, followup or empty (steer when busy).
func (a *uiAgent) submit(text, mode string) (uiSubmit, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return uiSubmit{}, errors.New("message is empty")
	}
	if len([]rune(text)) > uiMaxPromptRunes {
		return uiSubmit{}, errors.New("message is too long")
	}
	if mode != "" && mode != uiModeSteer && mode != uiModeFollowup {
		return uiSubmit{}, errors.New("mode must be steer or followup")
	}
	switch a.snapshot().State {
	case uiDone, uiFailed, uiStopped:
		return uiSubmit{}, errors.New("agent has finished")
	}
	if !a.busy() {
		return uiSubmit{Status: "sent"}, a.send(text)
	}
	if mode == "" {
		mode = uiModeSteer
	}
	msg := uiMsg{ID: uiNewID(), Text: text, Mode: mode, At: time.Now().Unix()}
	// A task still starting has no input to write to yet, so even a steer waits for its first turn.
	a.mu.Lock()
	preparing := a.state == uiPreparing
	a.mu.Unlock()
	if mode == uiModeSteer && (preparing || !a.canSteerMidTurn()) {
		msg.Mode, msg.Downgraded = uiModeFollowup, true
		if preparing {
			msg.Note = "The task is still starting: it will be sent when its first turn ends."
		} else {
			msg.Note = "This harness cannot take a message mid-turn: it will be sent when the turn ends."
		}
	}
	if msg.Mode == uiModeSteer {
		if err := a.send(text); err != nil {
			return uiSubmit{}, err
		}
		msg.Sent, msg.Note = true, "Sent. The task applies it at its next step."
		if !a.enqueue(msg) {
			return uiSubmit{Status: "sent_midturn", Note: msg.Note}, nil
		}
		return uiSubmit{Status: "sent_midturn", Item: &msg, Note: msg.Note}, nil
	}
	if !a.enqueue(msg) {
		return uiSubmit{}, errors.New("too many messages are waiting")
	}
	// The turn may have ended while this was being queued; nothing else would deliver it then.
	a.flushFollowups()
	return uiSubmit{Status: "queued", Item: &msg, Note: msg.Note}, nil
}

func (a *uiAgent) enqueue(m uiMsg) bool {
	a.mq.mu.Lock()
	if len(a.mq.items) >= uiMaxQueued {
		a.mq.mu.Unlock()
		return false
	}
	a.mq.items = append(a.mq.items, m)
	a.mq.mu.Unlock()
	a.publishQueue()
	return true
}

// turnEnded clears steers the harness has now taken and delivers the next follow-up.
func (a *uiAgent) turnEnded() {
	a.mq.mu.Lock()
	kept := a.mq.items[:0]
	changed := false
	for _, m := range a.mq.items {
		if m.Sent {
			changed = true
			continue
		}
		kept = append(kept, m)
	}
	a.mq.items = kept
	a.mq.mu.Unlock()
	if changed {
		a.publishQueue()
	}
	a.flushFollowups()
}

// flushFollowups delivers the oldest follow-up, only when the task is waiting with nothing pending
// and no question is open. One goes per turn, so the rest keep their order.
func (a *uiAgent) flushFollowups() {
	a.mu.Lock()
	idle := a.state == uiWaiting && a.pending == 0 && !a.stopped
	a.mu.Unlock()
	if !idle || len(a.pendingPermissions()) > 0 {
		return
	}
	a.mq.mu.Lock()
	idx := -1
	for i, m := range a.mq.items {
		if m.Mode == uiModeFollowup {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mq.mu.Unlock()
		return
	}
	m := a.mq.items[idx]
	a.mq.items = append(a.mq.items[:idx:idx], a.mq.items[idx+1:]...)
	a.mq.mu.Unlock()
	a.publishQueue()
	if err := a.send(m.Text); err != nil {
		a.add(uiEvent{Kind: "error", Text: "A queued message could not be sent: " + err.Error()})
	}
}

// cancelQueued removes a message that has not been sent.
func (a *uiAgent) cancelQueued(id string) error {
	a.mq.mu.Lock()
	for i, m := range a.mq.items {
		if m.ID != id {
			continue
		}
		if m.Sent {
			a.mq.mu.Unlock()
			return errors.New("that message was already sent")
		}
		a.mq.items = append(a.mq.items[:i:i], a.mq.items[i+1:]...)
		a.mq.mu.Unlock()
		a.publishQueue()
		return nil
	}
	a.mq.mu.Unlock()
	return errNoQueued
}

var errNoQueued = errors.New("no such queued message")

// promoteQueued turns a follow-up into a steer: it is sent now.
func (a *uiAgent) promoteQueued(id string) error {
	if !a.canSteerMidTurn() && a.busy() {
		return errors.New("this harness cannot take a message mid-turn")
	}
	a.mu.Lock()
	preparing := a.state == uiPreparing
	a.mu.Unlock()
	if preparing {
		return errors.New("the task is still starting")
	}
	a.mq.mu.Lock()
	idx := -1
	for i, m := range a.mq.items {
		if m.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mq.mu.Unlock()
		return errNoQueued
	}
	m := a.mq.items[idx]
	if m.Sent || m.Mode == uiModeSteer {
		a.mq.mu.Unlock()
		return errors.New("that message is already a steer")
	}
	a.mq.items = append(a.mq.items[:idx:idx], a.mq.items[idx+1:]...)
	a.mq.mu.Unlock()
	a.publishQueue()
	if err := a.send(m.Text); err != nil {
		return err
	}
	if a.busy() {
		m.Mode, m.Sent, m.Downgraded, m.Note = uiModeSteer, true, false, "Sent. The task applies it at its next step."
		a.enqueue(m)
	}
	return nil
}

func (s *uiServer) queueOfAgent(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.agent(w, r); ok {
		uiJSON(w, http.StatusOK, map[string]any{"queue": a.queueView()})
	}
}

func (s *uiServer) queueCancel(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	switch err := a.cancelQueued(r.PathValue("qid")); {
	case errors.Is(err, errNoQueued):
		uiError(w, http.StatusNotFound, err.Error())
	case err != nil:
		uiError(w, http.StatusConflict, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *uiServer) queuePromote(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	switch err := a.promoteQueued(r.PathValue("qid")); {
	case errors.Is(err, errNoQueued):
		uiError(w, http.StatusNotFound, err.Error())
	case err != nil:
		uiError(w, http.StatusConflict, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
