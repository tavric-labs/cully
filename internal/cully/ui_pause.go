package cully

import (
	"net/http"
	"time"
)

// Pause new work. One setting in the database that stops Cully from starting anything on its own:
// queued tasks, scheduled agent runs, scheduled jobs, automatic scouts and advisor analysis. It never
// stops or interrupts anything already running. Starting a task by hand from the page asks first.

const uiPausedMsg = "New work is paused. Start anyway?"

// uiPaused reads the switch from the database, so the page's server and the daemon agree.
func uiPaused() bool { return uiDB().metaGet("paused") == "1" }

// paused is the one check every starter calls.
func (m *uiManager) paused() bool { return uiPaused() }

func uiSetPaused(on bool) {
	v := "0"
	if on {
		v = "1"
		uiDB().metaSet("paused_at", time.Now().UTC().Format(time.RFC3339))
	}
	uiDB().metaSet("paused", v)
}

// askIfPaused answers a manual start with a question while paused; the page repeats the call with
// anyway=1 once the user agrees. It reports whether the caller should stop.
func (s *uiServer) askIfPaused(w http.ResponseWriter, r *http.Request) bool {
	if !s.m.paused() || r.URL.Query().Get("anyway") == "1" {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_, _ = w.Write([]byte(`{"error":"` + uiPausedMsg + `","paused":true}` + "\n"))
	return true
}

func (s *uiServer) pauseGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, map[string]any{"paused": s.m.paused(), "since": uiDB().metaGet("paused_at")})
}

func (s *uiServer) pauseSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paused bool `json:"paused"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	uiSetPaused(body.Paused)
	if !body.Paused {
		go s.m.drainQueue() // work that waited can start now
	}
	s.pauseGet(w, r)
}
