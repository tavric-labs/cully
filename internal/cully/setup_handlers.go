package cully

import (
	"net/http"
)

// HTTP skin over the setup engine. Every route goes through s.api, so each one
// needs the UI token and, when it changes anything, the X-Cully-UI header.

func (s *uiServer) setupRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/setup/state", s.api(s.setupStateH))
	mux.Handle("POST /api/setup/mode", s.api(s.setupModeH))
	mux.Handle("GET /api/setup/interview", s.api(s.setupInterviewGet))
	mux.Handle("POST /api/setup/interview", s.api(s.setupInterviewPost))
	mux.Handle("POST /api/setup/interview/analyse", s.api(s.setupInterviewAnalyse))
	mux.Handle("POST /api/setup/rules/approve", s.api(s.setupRulesApprove))
	mux.Handle("POST /api/setup/rules/discard", s.api(s.setupRulesDiscard))
	mux.Handle("GET /api/setup/suggestions", s.api(s.setupSuggestionsH))
	mux.Handle("POST /api/setup/complete", s.api(s.setupCompleteH))
}

func (s *uiServer) setupStateH(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, newSetupEngine(s.m).State())
}

func (s *uiServer) setupModeH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode   string `json:"mode"`
		McpURL string `json:"mcpUrl"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	st, err := newSetupEngine(s.m).SetMode(body.Mode, body.McpURL)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, st)
}

func (s *uiServer) setupInterviewGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, newSetupEngine(s.m).Interview())
}

func (s *uiServer) setupInterviewPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Answers map[string]*string `json:"answers"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	p, err := newSetupEngine(s.m).Propose(body.Answers)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]any{"proposal": p})
}

func (s *uiServer) setupInterviewAnalyse(w http.ResponseWriter, r *http.Request) {
	if s.askIfPaused(w, r) {
		return
	}
	iv, err := newSetupEngine(s.m).Analyse()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, iv)
}

func (s *uiServer) setupRulesApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	res, err := newSetupEngine(s.m).Approve(body.ID)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, res)
}

func (s *uiServer) setupRulesDiscard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := newSetupEngine(s.m).Discard(body.ID); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *uiServer) setupSuggestionsH(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, newSetupEngine(s.m).Suggestions())
}

func (s *uiServer) setupCompleteH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SkipRules bool `json:"skipRules"`
	}
	if r.ContentLength != 0 && !uiDecode(w, r, &body) {
		return
	}
	uiJSON(w, http.StatusOK, newSetupEngine(s.m).Complete(body.SkipRules))
}
