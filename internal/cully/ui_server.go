package cully

import (
	"bytes"
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
	mux.Handle("GET /api/sessions", s.api(s.sessionList))
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
		"repos": repoPaths(s.m.repoList()), "repoInfo": s.m.repoList(), "tokens": accounts,
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
	uiJSON(w, http.StatusOK, s.m.mcp.status(r.Context()))
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
	uiJSON(w, http.StatusOK, s.m.sessions())
}

func (s *uiServer) addRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	info, err := s.m.addRepo(body.Path)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusCreated, info)
}
