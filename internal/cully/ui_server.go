package cully

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed ui_static
var uiStaticFS embed.FS

const (
	uiCSP = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; " +
		"frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	uiBodyLimit   = 1 << 20
	uiCookieName  = "cully_ui"
	uiPanelBudget = 25 * time.Second
)

// uiBuildID identifies this build of the server and its embedded page. It is
// sent on every response so a page left open across a restart can tell it is
// stale and offer a reload.
var uiBuildID = uiComputeBuild()

func uiComputeBuild() string {
	files := map[string]string{}
	_ = fs.WalkDir(uiStaticFS, "ui_static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := uiStaticFS.ReadFile(p)
			files[p] = string(b)
		}
		return nil
	})
	if exe, err := os.Executable(); err == nil {
		if info, err := os.Stat(exe); err == nil {
			files["<executable>"] = fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
		}
	}
	return uiBuildFrom(files)
}

func uiBuildFrom(files map[string]string) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write([]byte(files[n]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

var uiPanels = []string{"advisor", "scout", "handoff", "status", "timeline", "replay", "suggestions", "impact", "jobs", "rescue"}

var uiANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]`)

func uiStripANSI(s string) string { return uiANSI.ReplaceAllString(s, "") }

type uiServer struct {
	m        *uiManager
	token    string
	loopback bool
}

// newUIServer returns the UI handler. loopback true means the listener is
// bound to a loopback address, so requests with any other Host are refused
// (DNS rebinding); the token is required either way.
func newUIServer(m *uiManager, token string, loopback bool) http.Handler {
	s := &uiServer{m: m, token: token, loopback: loopback}
	static, _ := fs.Sub(uiStaticFS, "ui_static")
	mux := http.NewServeMux()
	mux.Handle("GET /api/info", s.api(s.info))
	mux.Handle("GET /api/agents", s.api(s.listAgents))
	mux.Handle("POST /api/agents", s.api(s.createAgent))
	mux.Handle("GET /api/agents/{id}", s.api(s.getAgent))
	mux.Handle("GET /api/agents/{id}/events", s.api(s.events))
	mux.Handle("POST /api/agents/{id}/message", s.api(s.message))
	mux.Handle("POST /api/agents/{id}/stop", s.api(s.stop))
	mux.Handle("GET /api/agents/{id}/handoff", s.api(s.handoff))
	mux.Handle("GET /api/agents/{id}/diff", s.api(s.diff))
	mux.Handle("GET /api/board", s.api(s.board))
	mux.Handle("GET /api/admin/tools", s.api(s.adminToolsGet))
	mux.Handle("PUT /api/admin/tools", s.api(s.adminToolsPut))
	mux.Handle("GET /api/queue", s.api(s.queueList))
	mux.Handle("POST /api/queue", s.api(s.queueAdd))
	mux.Handle("DELETE /api/queue/{id}", s.api(s.queueRemove))
	mux.Handle("POST /api/queue/{id}/start", s.api(s.queueStart))
	mux.Handle("POST /api/fanout", s.api(s.fanout))
	mux.Handle("GET /api/fs", s.api(s.browse))
	mux.Handle("POST /api/setup/{agent}/enable", s.api(s.enableAgent))
	mux.Handle("POST /api/setup/advisor/start", s.api(s.startAdvisor))
	mux.Handle("POST /api/mcp/{agent}/connect", s.api(s.connectMCP))
	mux.Handle("GET /api/jobs", s.api(s.jobList))
	mux.Handle("POST /api/jobs/{name}/run", s.api(s.runJob))
	mux.Handle("POST /api/scout/run", s.api(s.scoutRun))
	mux.Handle("GET /api/scout/runs", s.api(s.scoutRunList))
	mux.Handle("GET /api/setup", s.api(s.setup))
	mux.Handle("GET /api/impact", s.api(s.impact))
	mux.Handle("GET /api/activity", s.api(s.activity))
	mux.Handle("GET /api/sessions", s.api(s.sessionList))
	mux.Handle("GET /api/history", s.api(s.historyList))
	mux.Handle("GET /api/sessions/library", s.api(s.libraryGet))
	mux.Handle("GET /api/sessions/evals", s.api(s.evalList))
	mux.Handle("GET /api/evals/runs", s.api(s.evalRunsGet))
	mux.Handle("GET /api/coach", s.api(s.coachGetH))
	mux.Handle("GET /api/running", s.api(s.runningGet))
	mux.Handle("GET /api/limits", s.api(s.limitsGet))
	mux.Handle("GET /api/rules", s.api(s.rulesGet))
	mux.Handle("POST /api/rules/propose", s.api(s.rulesProposeH))
	mux.Handle("POST /api/rules/{id}/approve", s.api(s.rulesApproveH))
	mux.Handle("POST /api/rules/{id}/dismiss", s.api(s.rulesDismissH))
	mux.Handle("GET /api/harness", s.api(s.harnessGet))
	mux.Handle("POST /api/harness/probe", s.api(s.harnessProbe))
	mux.Handle("GET /api/harness/server", s.api(s.harnessServerGet))
	mux.Handle("POST /api/harness/server/details", s.api(s.harnessServerLoad))
	mux.Handle("GET /api/usage", s.api(s.usageGet))
	mux.Handle("GET /api/agent-suggestions", s.api(s.agentSuggestListH))
	mux.Handle("POST /api/agent-suggestions/run", s.api(s.agentSuggestRunH))
	mux.Handle("POST /api/agent-suggestions/{id}/{state}", s.api(s.agentSuggestSetH))
	mux.Handle("GET /api/identities", s.api(s.identityListH))
	mux.Handle("POST /api/identities", s.api(s.identityCreateH))
	mux.Handle("PUT /api/identities/{id}", s.api(s.identityUpdateH))
	mux.Handle("DELETE /api/identities/{id}", s.api(s.identityDeleteH))
	mux.Handle("POST /api/identities/{id}/run", s.api(s.identityRunH))
	mux.Handle("POST /api/identities/{id}/approve", s.api(s.identityApproveH))
	mux.Handle("GET /api/identities/{id}/evals", s.api(s.agentEvalsGet))
	mux.Handle("POST /api/identities/{id}/evals/suggest", s.api(s.agentEvalsSuggest))
	mux.Handle("POST /api/identities/{id}/evals/run", s.api(s.agentEvalsRun))
	mux.Handle("POST /api/identities/{id}/evals/{cid}/approve", s.api(s.agentEvalsApprove))
	mux.Handle("DELETE /api/identities/{id}/evals/{cid}", s.api(s.agentEvalsRemove))
	mux.Handle("GET /api/research", s.api(s.researchGet))
	mux.Handle("GET /api/feed", s.api(s.feedGet))
	mux.Handle("GET /api/voice", s.api(s.voiceGet))
	mux.Handle("POST /api/voice/setup", s.api(s.voiceSetup))
	mux.Handle("POST /api/voice/transcribe", s.api(s.voiceTranscribe))
	mux.Handle("GET /api/suggestions", s.api(s.suggestionsGet))
	mux.Handle("GET /api/job-suggestions", s.api(s.jobSuggestionsGet))
	mux.Handle("POST /api/job-suggestions/run", s.api(s.jobSuggestRunH))
	mux.Handle("POST /api/job-suggestions/{id}/accept", s.api(s.jobSuggestAcceptH))
	mux.Handle("POST /api/job-suggestions/{id}/dismiss", s.api(s.jobSuggestDismissH))
	mux.Handle("POST /api/coach/run", s.api(s.coachRunH))
	mux.Handle("POST /api/evals/{id}/run", s.api(s.evalRunStart))
	mux.Handle("POST /api/sessions/{id}/summarize", s.api(s.summarize))
	mux.Handle("GET /api/sessions/{id}/handoff", s.api(s.sessionHandoffGet))
	mux.Handle("POST /api/sessions/{id}/continue", s.api(s.sessionContinue))
	mux.Handle("POST /api/sessions/{id}/eval", s.api(s.evalCreate))
	mux.Handle("GET /api/sessions/{id}/eval", s.api(s.evalGet))
	mux.Handle("POST /api/sessions/analysis", s.api(s.analysisSet))
	mux.Handle("POST /api/scout/auto", s.api(s.autoScoutSet))
	mux.Handle("POST /api/history/{id}/resume", s.api(s.historyResume))
	mux.Handle("POST /api/repos", s.api(s.addRepo))
	mux.Handle("GET /api/scout", s.api(s.scoutList))
	mux.Handle("GET /api/advice", s.api(s.adviceList))
	mux.Handle("GET /api/handoff", s.api(s.repoHandoff))
	mux.Handle("GET /api/agents/{id}/summary", s.api(s.summary))
	mux.Handle("POST /api/agents/{id}/advise", s.api(s.advise))
	mux.Handle("POST /api/agents/{id}/advice/{aid}/apply", s.api(s.adviceApply))
	mux.Handle("POST /api/agents/{id}/advice/{aid}/dismiss", s.api(s.adviceDismiss))
	mux.Handle("POST /api/agents/{id}/relay", s.api(s.relay))
	mux.Handle("POST /api/agents/{id}/review", s.api(s.review))
	mux.Handle("GET /api/mcp", s.api(s.mcpStatus))
	mux.Handle("POST /api/mcp/in-use", s.api(s.mcpInUse))
	mux.Handle("POST /api/mcp/{agent}/login", s.api(s.mcpLogin))
	mux.Handle("GET /api/mcp/{agent}/login", s.api(s.mcpLoginView))
	mux.Handle("POST /api/mcp/{agent}/login/paste", s.api(s.mcpPaste))
	mux.Handle("POST /api/mcp/{agent}/login/cancel", s.api(s.mcpCancel))
	mux.Handle("GET /api/panel/{name}", s.api(s.panel))
	mux.Handle("GET /", http.FileServerFS(static))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", uiCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Cully-Build", uiBuildID)
		if s.loopback && !uiLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" && r.URL.Query().Has("token") {
			if s.tokenOK(r.URL.Query().Get("token")) {
				http.SetCookie(w, &http.Cookie{
					Name: uiCookieName, Value: s.token, Path: "/", MaxAge: 30 * 24 * 3600,
					HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
				})
			}
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func uiLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (s *uiServer) tokenOK(got string) bool {
	return len(got) > 0 && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// api wraps a handler with authentication and the mutation header check.
func (s *uiServer) api(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := ""
		if auth, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			got = auth
		} else if c, err := r.Cookie(uiCookieName); err == nil {
			got = c.Value
		}
		if !s.tokenOK(got) {
			uiError(w, http.StatusUnauthorized, "unauthorized: open the URL printed by `cully ui`")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Cully-UI") != "1" {
			uiError(w, http.StatusForbidden, "missing X-Cully-UI header")
			return
		}
		h(w, r)
	})
}

func uiError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func uiJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func uiDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, uiBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			uiError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			uiError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}

func (s *uiServer) info(w http.ResponseWriter, r *http.Request) {
	accounts := s.m.cfg.Tokens.count()
	mode := "login" // the agents use this machine's own Claude login
	if accounts > 0 {
		mode = "subscription" // Claude subscription tokens from `claude setup-token`
	}
	uiJSON(w, http.StatusOK, map[string]any{
		"repos": repoPaths(s.m.repoList()), "repoInfo": s.m.repoListNow(), "tokens": accounts, "host": uiHostname(), "user": uiUsername(),
		"auth":      map[string]any{"mode": mode, "accounts": accounts},
		"maxAgents": s.m.cfg.MaxAgents, "panels": uiPanels, "memoryOverride": uiMemoryOverride(),
	})
}

func (s *uiServer) listAgents(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.list())
}

func (s *uiServer) createAgent(w http.ResponseWriter, r *http.Request) {
	var req uiStartRequest
	if !uiDecode(w, r, &req) {
		return
	}
	a, err := s.m.start(req)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, a.snapshot())
}

func (s *uiServer) agent(w http.ResponseWriter, r *http.Request) (*uiAgent, bool) {
	a, ok := s.m.get(r.PathValue("id"))
	if !ok {
		uiError(w, http.StatusNotFound, "no such agent")
	}
	return a, ok
}

func (s *uiServer) getAgent(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.agent(w, r); ok {
		uiJSON(w, http.StatusOK, a.snapshot())
	}
}

func (s *uiServer) message(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := a.send(body.Text); err != nil {
		uiError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) stop(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.agent(w, r); ok {
		a.stop()
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *uiServer) events(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		uiError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	if id, err := strconv.Atoi(r.Header.Get("Last-Event-ID")); err == nil && id > since {
		since = id
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	for {
		evs, wake := a.eventsSince(since)
		for _, e := range evs {
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, data)
			since = e.Seq
		}
		flusher.Flush()
		select {
		case <-wake:
		case <-r.Context().Done():
			return
		case <-time.After(20 * time.Second):
			fmt.Fprint(w, ": keepalive\n\n")
		}
	}
}

func (s *uiServer) handoff(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, uiStripANSI(uiContextPack(a.worktree)))
}

// panel renders an existing Cully command into text for the dashboard. The
// advisor, scout and handoff views are structured and have their own endpoints.
func (s *uiServer) panel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	known := false
	for _, p := range uiPanels {
		known = known || p == name
	}
	if !known {
		uiError(w, http.StatusNotFound, "unknown panel")
		return
	}
	repo, err := s.m.resolveRepo(r.URL.Query().Get("repo"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	var buf bytes.Buffer
	run := func() error {
		switch name {
		case "status":
			RunStatus(&buf, repo)
		case "timeline":
			return RunTimeline(&buf, []string{"--cwd", repo})
		case "replay":
			return RunReplay(&buf, []string{"--cwd", repo, "--instant"})
		case "handoff":
			buf.WriteString(uiContextPack(repo))
		case "suggestions":
			return RunSuggestions(&buf, nil)
		case "impact":
			return RunImpact(&buf, nil)
		case "jobs":
			return RunJobs(&buf, nil)
		case "rescue":
			return RunRescue(&buf, []string{"--cwd", repo, "--no-ai"})
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- run() }()
	var text string
	select {
	case err := <-done:
		text = buf.String()
		if err != nil {
			text += "\nerror: " + err.Error()
		}
	case <-time.After(uiPanelBudget):
		text = "timed out after " + uiPanelBudget.String()
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, uiStripANSI(text))
}

func (s *uiServer) diff(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, a.diff())
}

func (s *uiServer) board(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.board())
}

func (s *uiServer) mcpStatus(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.mcp.statusFor(r.Context(), s.m.mcp.isInUse))
}

func (s *uiServer) mcpAgent(w http.ResponseWriter, r *http.Request) (string, bool) {
	agent := r.PathValue("agent")
	if !uiKnownAgent(agent) {
		uiError(w, http.StatusNotFound, "unknown agent")
		return "", false
	}
	return agent, true
}

func (s *uiServer) mcpLogin(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.mcpAgent(w, r)
	if !ok {
		return
	}
	v, err := s.m.mcp.startLogin(agent)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, v)
}

func (s *uiServer) mcpLoginView(w http.ResponseWriter, r *http.Request) {
	if agent, ok := s.mcpAgent(w, r); ok {
		v, _ := s.m.mcp.loginView(agent)
		uiJSON(w, http.StatusOK, v)
	}
}

func (s *uiServer) mcpPaste(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.mcpAgent(w, r)
	if !ok {
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := s.m.mcp.paste(agent, body.URL); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) mcpCancel(w http.ResponseWriter, r *http.Request) {
	if agent, ok := s.mcpAgent(w, r); ok {
		s.m.mcp.cancel(agent)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *uiServer) relay(w http.ResponseWriter, r *http.Request) {
	from, ok := s.agent(w, r)
	if !ok {
		return
	}
	var body struct {
		To   string `json:"to"`
		What string `json:"what"`
		Note string `json:"note"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := s.m.relay(from.id, body.To, body.What, body.Note); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) review(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	rev, err := s.m.review(a.id)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, rev.snapshot())
}

func (s *uiServer) mcpInUse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := s.m.mcp.markInUse(body.Agent); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// uiMemoryOverride lets a user pin how Cully keeps memory: "local" for files
// on this machine, "mcp" for the Cully MCP. Anything else means detect.
func uiMemoryOverride() string {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("CULLY_MEMORY"))); v {
	case "local", "mcp":
		return v
	}
	return ""
}

func (s *uiServer) scoutList(w http.ResponseWriter, r *http.Request) {
	list := s.m.scouts()
	if list == nil {
		list = []uiScoutView{}
	}
	uiJSON(w, http.StatusOK, list)
}

func (s *uiServer) adviceList(w http.ResponseWriter, r *http.Request) {
	list := s.m.adviceAll()
	if list == nil {
		list = []uiAdviceFor{}
	}
	uiJSON(w, http.StatusOK, list)
}

func (s *uiServer) summary(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.agent(w, r); ok {
		uiJSON(w, http.StatusOK, a.summary())
	}
}

func (s *uiServer) repoHandoff(w http.ResponseWriter, r *http.Request) {
	repo, err := s.m.resolveRepo(r.URL.Query().Get("repo"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, uiRepoSummary(repo))
}

func (s *uiServer) advise(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	if err := s.m.advise(a.id); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *uiServer) adviceApply(w http.ResponseWriter, r *http.Request) {
	if err := s.m.applyAdvice(r.PathValue("id"), r.PathValue("aid")); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) adviceDismiss(w http.ResponseWriter, r *http.Request) {
	if err := s.m.dismissAdvice(r.PathValue("id"), r.PathValue("aid")); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func repoPaths(list []uiRepoInfo) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.Path)
	}
	return out
}

func (s *uiServer) sessionList(w http.ResponseWriter, r *http.Request) {
	// Every harness on the machine, each marked with which one it is. Only a task
	// started from this page is tied to the harness the page was opened through.
	all := s.m.sessions()
	if all == nil {
		all = []uiSession{}
	}
	for i := range all {
		all[i].Snapshot = s.m.snapshotFor(all[i].Agent, all[i].Cwd)
	}
	uiJSON(w, http.StatusOK, all)
}

func (s *uiServer) addRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Init bool   `json:"init"` // make the folder a git repository first
	}
	if !uiDecode(w, r, &body) {
		return
	}
	info, err := s.m.addRepoInit(body.Path, body.Init)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, info)
}

func (s *uiServer) setup(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, uiSetupFor(s.m.mcp.isInUse))
}

func (s *uiServer) impact(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	uiJSON(w, http.StatusOK, uiImpact(days))
}

func (s *uiServer) activity(w http.ResponseWriter, r *http.Request) {
	repo, err := s.m.resolveRepo(r.URL.Query().Get("repo"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, s.m.activity(repo))
}

func (s *uiServer) enableAgent(w http.ResponseWriter, r *http.Request) {
	agent := r.PathValue("agent")
	if !uiKnownAgent(agent) {
		uiError(w, http.StatusNotFound, "unknown agent")
		return
	}
	msg, err := uiEnableAgent(agent)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.m.mcp.forget(agent)
	uiJSON(w, http.StatusOK, map[string]string{"message": msg})
}

func (s *uiServer) startAdvisor(w http.ResponseWriter, r *http.Request) {
	if err := uiStartAdvisor(); err != nil {
		uiError(w, http.StatusBadRequest, "The advisor could not start: "+err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]string{"message": "The advisor is running."})
}

func (s *uiServer) connectMCP(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.mcpAgent(w, r)
	if !ok {
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	endpoint := strings.TrimSpace(body.URL)
	if err := validateMCPURL(endpoint); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := uiAddMCP(agent, endpoint); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.m.mcp.forget(agent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) jobList(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, uiJobs())
}

func (s *uiServer) runJob(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	known := false
	for _, j := range uiJobs() {
		known = known || j.Name == name
	}
	if !known {
		uiError(w, http.StatusNotFound, "no such job")
		return
	}
	if err := uiStartJob(name); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *uiServer) scoutRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	v, err := s.m.startScout(body.Query)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusAccepted, v)
}

func (s *uiServer) scoutRunList(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.scoutRuns())
}

// repoListNow is repoList with each repository's current branch.
func (m *uiManager) repoListNow() []uiRepoInfo {
	list := m.repoList()
	for i := range list {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if out, err := exec.CommandContext(ctx, "git", "-C", list[i].Path, "branch", "--show-current").Output(); err == nil {
			list[i].Branch = strings.TrimSpace(string(out))
		}
		cancel()
	}
	return list
}

func uiUsername() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

func (s *uiServer) browse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = uiHomeMarker
	}
	v, err := uiBrowse(path, r.URL.Query().Get("hidden") == "1")
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, v)
}

func (s *uiServer) queueList(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.queueList())
}

func (s *uiServer) queueAdd(w http.ResponseWriter, r *http.Request) {
	var req uiStartRequest
	if !uiDecode(w, r, &req) {
		return
	}
	q, err := s.m.enqueue(req)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, q)
}

func (s *uiServer) queueRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.m.dequeue(r.PathValue("id")); err != nil {
		uiError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) queueStart(w http.ResponseWriter, r *http.Request) {
	a, err := s.m.startQueued(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusConflict, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, a.snapshot())
}

func (s *uiServer) fanout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		uiStartRequest
		Count int `json:"count"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	res, err := s.m.fanout(body.uiStartRequest, body.Count)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, res)
}

func (s *uiServer) historyList(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		agent = "all" // every harness on the machine
	}
	if agent != "all" && !uiKnownAgent(agent) {
		uiError(w, http.StatusBadRequest, "unknown agent")
		return
	}
	days := 7
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 || n > uiHistoryAllMax {
			uiError(w, http.StatusBadRequest, "days must be a number from 0 to 3650")
			return
		}
		days = n
	}
	uiJSON(w, http.StatusOK, s.m.history(agent, days))
}

func (s *uiServer) historyResume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
		Fork   bool   `json:"fork"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if len([]rune(body.Prompt)) > uiMaxPromptRunes {
		uiError(w, http.StatusBadRequest, "prompt is too long")
		return
	}
	a, err := s.m.resumeSession(r.PathValue("id"), body.Prompt, body.Fork)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, a.snapshot())
}

func (s *uiServer) adminToolsGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.toolsView())
}

func (s *uiServer) adminToolsPut(w http.ResponseWriter, r *http.Request) {
	var in uiPolicyInput
	if !uiDecode(w, r, &in) {
		return
	}
	if err := s.m.setToolPolicy(in); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, s.m.toolsView())
}

func (s *uiServer) libraryGet(w http.ResponseWriter, r *http.Request) {
	s.m.indexSessions(200)
	uiJSON(w, http.StatusOK, s.m.sessionLibrary())
}

func (s *uiServer) analysisSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	s.m.setAnalysis(body.On)
	uiJSON(w, http.StatusOK, s.m.sessionLibrary())
}

func (s *uiServer) evalList(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.listEvals())
}

func (s *uiServer) evalCreate(w http.ResponseWriter, r *http.Request) {
	c, err := s.m.createEval(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, c)
}

func (s *uiServer) evalGet(w http.ResponseWriter, r *http.Request) {
	c, ok := s.m.evalCase(r.PathValue("id"))
	if !ok {
		uiError(w, http.StatusNotFound, "no eval for this session")
		return
	}
	uiJSON(w, http.StatusOK, c)
}

func (s *uiServer) summarize(w http.ResponseWriter, r *http.Request) {
	e, err := s.m.summarizeOne(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, e)
}

func (s *uiServer) evalRunsGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.evalRunList())
}

func (s *uiServer) evalRunStart(w http.ResponseWriter, r *http.Request) {
	run, err := s.m.runEval(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, run)
}

func (s *uiServer) coachGetH(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.coachGet())
}

func (s *uiServer) coachRunH(w http.ResponseWriter, r *http.Request) {
	c, err := s.m.coachRun()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, c)
}

func (s *uiServer) researchGet(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "scout" && kind != "recall" && kind != "advice" && kind != "digest" {
		uiError(w, http.StatusBadRequest, "kind must be scout, recall, advice or digest")
		return
	}
	uiJSON(w, http.StatusOK, s.m.researchList(kind, 200))
}

func (s *uiServer) autoScoutSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	s.m.setAutoScout(body.On)
	uiJSON(w, http.StatusOK, s.m.sessionLibrary().Analysis)
}

func (s *uiServer) identityListH(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.identityList())
}

func (s *uiServer) identityCreateH(w http.ResponseWriter, r *http.Request) {
	var in uiIdentityInput
	if !uiDecode(w, r, &in) {
		return
	}
	id, err := s.m.identityCreate(in)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, id)
}

func (s *uiServer) identityUpdateH(w http.ResponseWriter, r *http.Request) {
	var in uiIdentityInput
	if !uiDecode(w, r, &in) {
		return
	}
	id, err := s.m.identityUpdate(r.PathValue("id"), in)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, id)
}

func (s *uiServer) identityDeleteH(w http.ResponseWriter, r *http.Request) {
	if err := s.m.identityDelete(r.PathValue("id")); err != nil {
		uiError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) identityRunH(w http.ResponseWriter, r *http.Request) {
	run, err := s.m.identityRun(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, run)
}

func (s *uiServer) agentSuggestListH(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.agentSuggestionsList())
}

func (s *uiServer) agentSuggestRunH(w http.ResponseWriter, r *http.Request) {
	n, err := s.m.agentSuggestRun()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]int{"added": n})
}

func (s *uiServer) agentSuggestSetH(w http.ResponseWriter, r *http.Request) {
	state := r.PathValue("state")
	if state != "accepted" && state != "dismissed" {
		uiError(w, http.StatusBadRequest, "state must be accepted or dismissed")
		return
	}
	if err := s.m.agentSuggestionSet(r.PathValue("id"), state); err != nil {
		uiError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) usageGet(w http.ResponseWriter, r *http.Request) {
	days := 14
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 365 {
			uiError(w, http.StatusBadRequest, "days must be a number from 1 to 365")
			return
		}
		days = n
	}
	uiJSON(w, http.StatusOK, s.m.usageSummary(days))
}

func (s *uiServer) identityApproveH(w http.ResponseWriter, r *http.Request) {
	id, err := s.m.identityApprove(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, id)
}

func (s *uiServer) runningGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.running())
}

func (s *uiServer) sessionHandoffGet(w http.ResponseWriter, r *http.Request) {
	e, err := s.m.sessionEntry(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, s.m.cfg.Tokens.redact(uiSessionHandoff(e)))
}

func (s *uiServer) sessionContinue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if r.ContentLength > 0 && !uiDecode(w, r, &body) {
		return
	}
	a, err := s.m.continueFromSession(r.PathValue("id"), body.Prompt)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, a.snapshot())
}

func (s *uiServer) limitsGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.limitsAll())
}

func (s *uiServer) harnessGet(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		agent = "claude"
		for _, a := range []string{"claude", "codex", "cursor"} {
			if s.m.mcp.isInUse(a) {
				agent = a
				break
			}
		}
	}
	if !uiKnownAgent(agent) {
		uiError(w, http.StatusBadRequest, "unknown harness")
		return
	}
	uiJSON(w, http.StatusOK, s.m.harnessInfo(agent))
}

func (s *uiServer) rulesGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.rulesView())
}

func (s *uiServer) rulesProposeH(w http.ResponseWriter, r *http.Request) {
	added, err := s.m.rulesPropose()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]bool{"added": added})
}

func (s *uiServer) rulesApproveH(w http.ResponseWriter, r *http.Request) {
	t, err := s.m.rulesApprove(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, t)
}

func (s *uiServer) rulesDismissH(w http.ResponseWriter, r *http.Request) {
	if err := s.m.rulesDismiss(r.PathValue("id")); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) harnessServerGet(w http.ResponseWriter, r *http.Request) {
	d, err := s.m.serverDetail(r.URL.Query().Get("agent"), r.URL.Query().Get("name"))
	if err != nil {
		uiError(w, http.StatusNotFound, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, d)
}

func (s *uiServer) harnessServerLoad(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
		Name  string `json:"name"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	d, err := s.m.loadServerDetails(body.Agent, body.Name)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, d)
}

func (s *uiServer) agentEvalsGet(w http.ResponseWriter, r *http.Request) {
	e, err := s.m.agentEvalsView(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusNotFound, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, e)
}

func (s *uiServer) agentEvalsSuggest(w http.ResponseWriter, r *http.Request) {
	n, err := s.m.agentEvalSuggest(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]int{"added": n})
}

func (s *uiServer) agentEvalsRun(w http.ResponseWriter, r *http.Request) {
	id, err := s.m.agentEvalRun(r.PathValue("id"))
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, map[string]string{"run": id})
}

func (s *uiServer) agentEvalsApprove(w http.ResponseWriter, r *http.Request) {
	if err := s.m.agentEvalSetCase(r.PathValue("id"), r.PathValue("cid"), true, false); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) agentEvalsRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.m.agentEvalSetCase(r.PathValue("id"), r.PathValue("cid"), false, true); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *uiServer) harnessProbe(w http.ResponseWriter, r *http.Request) {
	n, err := s.m.probeHarness()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]int{"servers": n})
}
