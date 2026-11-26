package cully

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const uiTestToken = "test-ui-token-0123456789abcdef"

func uiTestServer(t *testing.T, loopback bool) (*httptest.Server, *uiManager, string) {
	t.Helper()
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	srv := httptest.NewServer(newUIServer(m, uiTestToken, loopback))
	t.Cleanup(srv.Close)
	return srv, m, repo
}

func uiDo(t *testing.T, method, url string, body any, mutate func(*http.Request)) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	if mutate != nil {
		mutate(req)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func uiAuthed(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+uiTestToken)
	req.Header.Set("X-Cully-UI", "1")
}

func TestAPIRequiresToken(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	if res := uiDo(t, "GET", srv.URL+"/api/info", nil, nil); res.StatusCode != 401 {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	if res := uiDo(t, "GET", srv.URL+"/api/info", nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }); res.StatusCode != 401 {
		t.Fatalf("wrong token: %d", res.StatusCode)
	}
	if res := uiDo(t, "GET", srv.URL+"/api/info", nil, uiAuthed); res.StatusCode != 200 {
		t.Fatalf("bearer: %d", res.StatusCode)
	}
	cookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "cully_ui", Value: uiTestToken}) }
	if res := uiDo(t, "GET", srv.URL+"/api/info", nil, cookie); res.StatusCode != 200 {
		t.Fatalf("cookie: %d", res.StatusCode)
	}
}

func TestMutationRequiresHeader(t *testing.T) {
	srv, _, repo := uiTestServer(t, false)
	noHeader := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+uiTestToken) }
	res := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": "x"}, noHeader)
	if res.StatusCode != 403 {
		t.Fatalf("status %d, want 403", res.StatusCode)
	}
}

func TestLoopbackRejectsForeignHost(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	evil := func(r *http.Request) { uiAuthed(r); r.Host = "evil.example.com" }
	if res := uiDo(t, "GET", srv.URL+"/api/info", nil, evil); res.StatusCode != 403 {
		t.Fatalf("foreign host: %d", res.StatusCode)
	}
	ok := func(r *http.Request) { uiAuthed(r); r.Host = "localhost:7777" }
	if res := uiDo(t, "GET", srv.URL+"/api/info", nil, ok); res.StatusCode != 200 {
		t.Fatalf("localhost host: %d", res.StatusCode)
	}
}

func TestTokenQueryLoginSetsCookie(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	res := uiDo(t, "GET", srv.URL+"/?token="+uiTestToken, nil, nil)
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("status %d location %q", res.StatusCode, res.Header.Get("Location"))
	}
	var c *http.Cookie
	for _, ck := range res.Cookies() {
		if ck.Name == "cully_ui" {
			c = ck
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Value != uiTestToken {
		t.Fatalf("cookie = %+v", c)
	}
	bad := uiDo(t, "GET", srv.URL+"/?token=nope", nil, nil)
	if len(bad.Cookies()) != 0 {
		t.Fatal("cookie set for a wrong token")
	}
}

func TestServerStartRejectsRepoOutsideAllowList(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	res := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": "/etc", "prompt": "x"}, uiAuthed)
	if res.StatusCode != 400 {
		t.Fatalf("status %d, want 400", res.StatusCode)
	}
}

func TestInfoNeverContainsTokenValue(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	res := uiDo(t, "GET", srv.URL+"/api/info", nil, uiAuthed)
	var buf bytes.Buffer
	buf.ReadFrom(res.Body)
	if strings.Contains(buf.String(), "tok-aaa1") || strings.Contains(buf.String(), uiTestToken) {
		t.Fatalf("info leaked a token: %s", buf.String())
	}
	var info struct {
		Tokens int      `json:"tokens"`
		Repos  []string `json:"repos"`
	}
	json.Unmarshal(buf.Bytes(), &info)
	if info.Tokens != 1 || len(info.Repos) != 1 {
		t.Fatalf("info = %+v", info)
	}
}

func TestAgentLifecycleOverHTTPAndSSE(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	res := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": "hello"}, uiAuthed)
	if res.StatusCode != 201 {
		t.Fatalf("create: %d", res.StatusCode)
	}
	var view uiAgentView
	json.NewDecoder(res.Body).Decode(&view)
	a, _ := m.get(view.ID)
	uiWaitState(t, a, "waiting")

	if res := uiDo(t, "POST", srv.URL+"/api/agents/"+view.ID+"/message", map[string]any{"text": "more"}, uiAuthed); res.StatusCode != 204 {
		t.Fatalf("message: %d", res.StatusCode)
	}
	sse := uiDo(t, "GET", srv.URL+"/api/agents/"+view.ID+"/events?since=0", nil, uiAuthed)
	if ct := sse.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q", ct)
	}
	got := make(chan uiEvent, 64)
	go func() {
		sc := bufio.NewScanner(sse.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var ev uiEvent
				if json.Unmarshal([]byte(data), &ev) == nil {
					got <- ev
				}
			}
		}
	}()
	deadline := time.After(5 * time.Second)
	seenResult := false
	for !seenResult {
		select {
		case ev := <-got:
			seenResult = ev.Kind == "result"
		case <-deadline:
			t.Fatal("no result event over SSE")
		}
	}
	if res := uiDo(t, "POST", srv.URL+"/api/agents/"+view.ID+"/stop", nil, uiAuthed); res.StatusCode != 204 {
		t.Fatalf("stop: %d", res.StatusCode)
	}
	uiWaitState(t, a, "stopped")
}

func TestPanelStripsANSI(t *testing.T) {
	if got := uiStripANSI("\x1b[31mred\x1b[0m plain \x1b[1;32mgreen\x1b[0m"); got != "red plain green" {
		t.Fatalf("uiStripANSI = %q", got)
	}
	srv, _, _ := uiTestServer(t, false)
	if res := uiDo(t, "GET", srv.URL+"/api/panel/nonsense", nil, uiAuthed); res.StatusCode != 404 {
		t.Fatalf("unknown panel: %d", res.StatusCode)
	}
	res := uiDo(t, "GET", srv.URL+"/api/panel/jobs", nil, uiAuthed)
	if res.StatusCode != 200 {
		t.Fatalf("jobs panel: %d", res.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	res := uiDo(t, "GET", srv.URL+"/", nil, nil)
	if got := res.Header.Get("Content-Security-Policy"); got != uiCSP {
		t.Fatalf("CSP = %q", got)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

func TestBodyLimit(t *testing.T) {
	srv, _, repo := uiTestServer(t, false)
	big := strings.Repeat("a", 2<<20)
	res := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": big}, uiAuthed)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", res.StatusCode)
	}
}

func TestBoardAndDiffEndpoints(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	res := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": "go", "mode": "plan"}, uiAuthed)
	var view uiAgentView
	json.NewDecoder(res.Body).Decode(&view)
	a, _ := m.get(view.ID)
	uiWaitState(t, a, "waiting")

	var board uiBoard
	r := uiDo(t, "GET", srv.URL+"/api/board", nil, uiAuthed)
	if r.StatusCode != 200 {
		t.Fatalf("board: %d", r.StatusCode)
	}
	json.NewDecoder(r.Body).Decode(&board)
	if len(board.Agents) != 1 || board.Agents[0].ID != view.ID || board.Overlaps == nil {
		t.Fatalf("board = %+v", board)
	}
	if d := uiDo(t, "GET", srv.URL+"/api/agents/"+view.ID+"/diff", nil, uiAuthed); d.StatusCode != 200 {
		t.Fatalf("diff: %d", d.StatusCode)
	}
	if d := uiDo(t, "GET", srv.URL+"/api/agents/nope/diff", nil, uiAuthed); d.StatusCode != 404 {
		t.Fatalf("unknown agent diff: %d", d.StatusCode)
	}
}

func TestMCPEndpointsAuthAndValidation(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	if r := uiDo(t, "GET", srv.URL+"/api/mcp", nil, nil); r.StatusCode != 401 {
		t.Fatalf("status without token: %d", r.StatusCode)
	}
	noHeader := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+uiTestToken) }
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/login", nil, noHeader); r.StatusCode != 403 {
		t.Fatalf("login without X-Cully-UI: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/emacs/login", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/login/paste", map[string]string{"url": "http://localhost:1/cb?code=1"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("paste with no login running: %d", r.StatusCode)
	}
}

func TestMCPLoginFlowOverHTTP(t *testing.T) {
	bin := uiWriteScript(t, fakeLogin)
	old := uiLoginCommand
	uiLoginCommand = func(string) (string, []string, bool, error) { return bin, nil, true, nil }
	t.Cleanup(func() { uiLoginCommand = old })
	srv, m, _ := uiTestServer(t, false)
	_ = m
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/login", nil, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("start: %d", r.StatusCode)
	}
	var v uiLoginView
	uiWaitFor(t, "awaiting", func() bool {
		r := uiDo(t, "GET", srv.URL+"/api/mcp/claude/login", nil, uiAuthed)
		v = uiLoginView{}
		json.NewDecoder(r.Body).Decode(&v)
		return v.State == "awaiting"
	})
	if v.URL == "" {
		t.Fatalf("no URL in %+v", v)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/login/paste", map[string]string{"url": "https://evil.example.com/?code=1"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("evil paste: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/claude/login/paste", map[string]string{"url": "http://localhost:4321/callback?code=abc&state=s1"}, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("good paste: %d", r.StatusCode)
	}
	uiWaitFor(t, "done", func() bool {
		r := uiDo(t, "GET", srv.URL+"/api/mcp/claude/login", nil, uiAuthed)
		v = uiLoginView{}
		json.NewDecoder(r.Body).Decode(&v)
		return v.State == "done"
	})
}

func TestRelayAndReviewEndpoints(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	mk := func(prompt string) uiAgentView {
		r := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": prompt}, uiAuthed)
		var v uiAgentView
		json.NewDecoder(r.Body).Decode(&v)
		return v
	}
	v1 := mk("one")
	a1, _ := m.get(v1.ID)
	uiWaitState(t, a1, "waiting")
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+v1.ID+"/relay", map[string]string{"to": v1.ID, "what": "reply"}, nil); r.StatusCode != 401 {
		t.Fatalf("relay without token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agents/nope/relay", map[string]string{"to": v1.ID, "what": "reply"}, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("relay from unknown agent: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+v1.ID+"/relay", map[string]string{"to": v1.ID, "what": "reply"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("relay to itself: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+v1.ID+"/review", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("review with no changes: %d", r.StatusCode)
	}
	uiCommitFile(t, a1.snapshot().Worktree, "x.go", "package x\n")
	r := uiDo(t, "POST", srv.URL+"/api/agents/"+v1.ID+"/review", nil, uiAuthed)
	if r.StatusCode != 201 {
		t.Fatalf("review: %d", r.StatusCode)
	}
	var rv uiAgentView
	json.NewDecoder(r.Body).Decode(&rv)
	if !strings.HasPrefix(rv.Title, "Review:") {
		t.Fatalf("review title = %q", rv.Title)
	}
	rev, _ := m.get(rv.ID)
	uiWaitState(t, rev, "waiting")
	if r := uiDo(t, "POST", srv.URL+"/api/agents/"+rv.ID+"/relay", map[string]string{"to": v1.ID, "what": "reply", "note": "fix these"}, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("relay review back to the author: %d", r.StatusCode)
	}
}

func TestBuildHeaderIsStableAndPresentOnEveryResponse(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	first := uiDo(t, "GET", srv.URL+"/api/info", nil, uiAuthed).Header.Get("X-Cully-Build")
	if len(first) < 8 {
		t.Fatalf("X-Cully-Build = %q", first)
	}
	if second := uiDo(t, "GET", srv.URL+"/api/agents", nil, uiAuthed).Header.Get("X-Cully-Build"); second != first {
		t.Fatalf("build id changed between requests: %q then %q", first, second)
	}
	if unauth := uiDo(t, "GET", srv.URL+"/api/info", nil, nil).Header.Get("X-Cully-Build"); unauth != first {
		t.Fatalf("a 401 response lacks the build id: %q", unauth)
	}
	if js := uiDo(t, "GET", srv.URL+"/app.js", nil, nil).Header.Get("X-Cully-Build"); js != first {
		t.Fatalf("static assets lack the build id: %q", js)
	}
}

func TestBuildIDChangesWithTheAssets(t *testing.T) {
	a := uiBuildFrom(map[string]string{"app.js": "one", "style.css": "x"})
	b := uiBuildFrom(map[string]string{"app.js": "two", "style.css": "x"})
	if a == b || len(a) != 12 {
		t.Fatalf("ids = %q, %q", a, b)
	}
	if uiBuildFrom(map[string]string{"app.js": "one", "style.css": "x"}) != a {
		t.Fatal("id is not deterministic")
	}
}

func TestAppNoticesANewServerBuild(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"X-Cully-Build", "location.reload"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js lacks %q", want)
		}
	}
}

func TestInUseEndpoint(t *testing.T) {
	uiStubProbes(t)
	srv, _, _ := uiTestServer(t, false)
	body := map[string]string{"agent": "codex"}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/in-use", body, nil); r.StatusCode != 401 {
		t.Fatalf("without token: %d", r.StatusCode)
	}
	noHeader := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+uiTestToken) }
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/in-use", body, noHeader); r.StatusCode != 403 {
		t.Fatalf("without X-Cully-UI: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/in-use", map[string]string{"agent": "emacs"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/mcp/in-use", body, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("mark: %d", r.StatusCode)
	}
	r := uiDo(t, "GET", srv.URL+"/api/mcp", nil, uiAuthed)
	var agents []uiMCPAgent
	json.NewDecoder(r.Body).Decode(&agents)
	for _, a := range agents {
		if (a.Agent == "codex") != a.InUse {
			t.Fatalf("agent %s inUse=%v", a.Agent, a.InUse)
		}
	}
}

func TestInfoReportsAuthModeRepoRemoteAndMemoryOverride(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	exec.Command("git", "-C", repo, "remote", "add", "origin", "https://user:s3cret@github.com/acme/widgets.git").Run()
	m = newUIManager(uiConfig{Repos: []string{repo}, Tokens: m.cfg.Tokens, ClaudeBin: m.cfg.ClaudeBin})
	srv := httptest.NewServer(newUIServer(m, uiTestToken, false))
	t.Cleanup(srv.Close)
	get := func() map[string]any {
		r := uiDo(t, "GET", srv.URL+"/api/info", nil, uiAuthed)
		var info map[string]any
		json.NewDecoder(r.Body).Decode(&info)
		return info
	}
	info := get()
	auth := info["auth"].(map[string]any)
	if auth["mode"] != "subscription" || auth["accounts"].(float64) != 2 {
		t.Fatalf("auth = %v", auth)
	}
	repos := info["repoInfo"].([]any)
	ri := repos[0].(map[string]any)
	if ri["name"] != "repo" || ri["remote"] != "github.com/acme/widgets" {
		t.Fatalf("repoInfo = %v", ri)
	}
	raw, _ := json.Marshal(info)
	if strings.Contains(string(raw), "s3cret") || strings.Contains(string(raw), "tok-aaa1") {
		t.Fatalf("info leaks a secret: %s", raw)
	}
	if info["memoryOverride"] != "" {
		t.Fatalf("memoryOverride = %v", info["memoryOverride"])
	}
	t.Setenv("CULLY_MEMORY", "local")
	if get()["memoryOverride"] != "local" {
		t.Fatal("CULLY_MEMORY=local not reported")
	}
	t.Setenv("CULLY_MEMORY", "bogus")
	if get()["memoryOverride"] != "" {
		t.Fatal("an unknown CULLY_MEMORY value was reported")
	}
}

func TestInfoWithoutTokensReportsMachineLogin(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude))
	m = newUIManager(uiConfig{Repos: []string{repo}, ClaudeBin: m.cfg.ClaudeBin})
	srv := httptest.NewServer(newUIServer(m, uiTestToken, false))
	t.Cleanup(srv.Close)
	r := uiDo(t, "GET", srv.URL+"/api/info", nil, uiAuthed)
	var info struct {
		Auth struct {
			Mode     string
			Accounts int
		}
	}
	json.NewDecoder(r.Body).Decode(&info)
	if info.Auth.Mode != "login" || info.Auth.Accounts != 0 {
		t.Fatalf("auth = %+v", info.Auth)
	}
}

func TestCleanRemoteStripsCredentialsAndNoise(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:tok@github.com/acme/widgets.git": "github.com/acme/widgets",
		"https://github.com/acme/widgets":              "github.com/acme/widgets",
		"git@github.com:acme/widgets.git":              "github.com/acme/widgets",
		"ssh://git@gitlab.example.com:2222/a/b.git":    "gitlab.example.com/a/b",
		"":                 "",
		"not a url at all": "",
	} {
		if got := uiCleanRemote(in); got != want {
			t.Fatalf("uiCleanRemote(%q) = %q, want %q", in, got, want)
		}
	}
}
