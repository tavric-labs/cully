package cully

import (
	"hash/fnv"
	"strconv"
	"sync"
	"time"
)

// Noticing a task that is going in circles, while it happens. Four patterns, drawn from how OpenHands,
// Hermes and OpenCode watch their own agents: the same call giving the same answer again and again,
// the same call failing again and again, two calls taking turns, and (not detected here) a long
// monologue. A call is the tool and what it was asked to do. Calls that are meant to repeat (updating
// the agent's own todo list) are ignored. The page shows it as a badge with a Nudge button; nothing is
// stopped automatically, since a person decides what a loop is worth.

const (
	uiStuckSame   = 3 // identical calls with identical answers
	uiStuckFail   = 3 // identical calls that all failed
	uiStuckAlt    = 6 // calls that alternate between two
	uiStuckWindow = 30
)

type uiCallRec struct {
	Sig    string
	Failed bool
	Res    uint64
	Done   bool
}

type uiStuck struct {
	Kind   string `json:"kind"` // repeat-failure, repeat-result or alternating
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Count  int    `json:"count"`
	Since  int64  `json:"since"`
}

type uiStuckWatch struct {
	mu    sync.Mutex
	calls []uiCallRec
	cur   *uiStuck
}

func uiResultHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

var uiStuckExempt = map[string]bool{"TodoWrite": true}

// call records a tool call as it starts.
func (w *uiStuckWatch) call(tool, what string) {
	if uiStuckExempt[tool] {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, uiCallRec{Sig: tool + "|" + what})
	if len(w.calls) > uiStuckWindow {
		w.calls = w.calls[len(w.calls)-uiStuckWindow:]
	}
}

// result records how the most recent unanswered call ended, and returns what is stuck now, if anything.
func (w *uiStuckWatch) result(failed bool, text string) *uiStuck {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := len(w.calls) - 1; i >= 0; i-- {
		if !w.calls[i].Done {
			w.calls[i].Done, w.calls[i].Failed, w.calls[i].Res = true, failed, uiResultHash(text)
			break
		}
	}
	st := uiDetectStuck(w.calls)
	if st == nil {
		w.cur = nil
	} else if w.cur == nil || w.cur.Kind != st.Kind {
		st.Since = time.Now().Unix()
		w.cur = st
	} else {
		st.Since = w.cur.Since
		w.cur = st
	}
	if w.cur == nil {
		return nil
	}
	c := *w.cur
	return &c
}

func (w *uiStuckWatch) current() *uiStuck {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur == nil {
		return nil
	}
	c := *w.cur
	return &c
}

// uiDetectStuck looks at the end of the call list.
func uiDetectStuck(calls []uiCallRec) *uiStuck {
	done := calls
	for len(done) > 0 && !done[len(done)-1].Done {
		done = done[:len(done)-1]
	}
	n := len(done)
	if n == 0 {
		return nil
	}
	last := done[n-1]
	run, fails := 0, 0
	for i := n - 1; i >= 0 && done[i].Sig == last.Sig; i-- {
		if done[i].Res == last.Res && done[i].Failed == last.Failed {
			run++
		} else {
			break
		}
	}
	for i := n - 1; i >= 0 && done[i].Sig == last.Sig && done[i].Failed; i-- {
		fails++
	}
	if fails >= uiStuckFail {
		return &uiStuck{Kind: "repeat-failure", Label: "Retrying a failing call", Count: fails, Detail: "The same call has failed " + strconv.Itoa(fails) + " times in a row."}
	}
	if run >= uiStuckSame {
		return &uiStuck{Kind: "repeat-result", Label: "Repeating the same call", Count: run, Detail: "The same call has been made " + strconv.Itoa(run) + " times and gave the same answer each time."}
	}
	if n >= uiStuckAlt {
		a, b := done[n-1].Sig, done[n-2].Sig
		if a != b {
			alt := true
			for i := 1; i <= uiStuckAlt; i++ {
				want := a
				if i%2 == 0 {
					want = b
				}
				if done[n-i].Sig != want {
					alt = false
					break
				}
			}
			if alt {
				return &uiStuck{Kind: "alternating", Label: "Going back and forth", Count: uiStuckAlt, Detail: "The last " + strconv.Itoa(uiStuckAlt) + " calls alternate between the same two."}
			}
		}
	}
	return nil
}
