package cully

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiFakeProcs(t *testing.T, procs []uiProc) {
	t.Helper()
	old := uiListProcs
	uiListProcs = func() []uiProc { return procs }
	t.Cleanup(func() { uiListProcs = old })
}

func uiNewBranchRepo(t *testing.T, branch string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "proj-"+branch)
	os.MkdirAll(filepath.Join(repo, "pkg"), 0o755)
	for _, args := range [][]string{{"init", "-q", "-b", branch}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		if o, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, o)
		}
	}
	real, _ := canonicalDir(repo)
	return real
}

func TestSessionsFindsEveryAgentAcrossFolders(t *testing.T) {
	repoA := uiNewTestRepo(t) // main
	repoB := uiNewBranchRepo(t, "dev")
	plain := t.TempDir()
	now := time.Now()
	uiFakeProcs(t, []uiProc{
		{Interactive: true, PID: 10, PPID: 1, Cmd: []string{"/home/u/.local/bin/claude"}, Cwd: repoA, Started: now.Add(-10 * time.Minute), InCully: true},
		{Interactive: true, PID: 20, PPID: 1, Cmd: []string{"codex"}, Cwd: filepath.Join(repoB, "pkg"), Started: now.Add(-20 * time.Minute)},
		{Interactive: true, PID: 30, PPID: 1, Cmd: []string{"node", "/opt/cursor-agent/index.js"}, Cwd: repoA, Started: now.Add(-5 * time.Minute)},
		{PID: 40, PPID: 1, Cmd: []string{"claude", "-p", "hello"}, Cwd: repoA, Started: now},
		{PID: 41, PPID: 1, Cmd: []string{"claude", "mcp", "list"}, Cwd: repoA, Started: now},
		{PID: 42, PPID: 1, Cmd: []string{"bash"}, Cwd: repoA, Started: now},
		{PID: 43, PPID: 1, Cmd: []string{"/usr/bin/cully", "ui"}, Cwd: repoA, Started: now},
		{PID: 44, PPID: 10, Cmd: []string{"claude"}, Cwd: repoA, Started: now}, // a helper of session 10
		{Interactive: true, PID: 50, PPID: 1, Cmd: []string{"claude"}, Cwd: plain, Started: now.Add(-1 * time.Minute)},
	})
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	got := m.sessions()
	var pids []int
	byPID := map[int]uiSession{}
	for _, s := range got {
		pids = append(pids, s.PID)
		byPID[s.PID] = s
	}
	if len(got) != 4 || pids[0] != 50 || pids[1] != 30 || pids[2] != 10 || pids[3] != 20 {
		t.Fatalf("sessions (newest first) = %v", pids)
	}
	a, _ := canonicalDir(repoA)
	if s := byPID[10]; s.Agent != "claude" || s.Via != "cully" || s.Repo != a || s.Branch != "main" || s.Name != filepath.Base(a) {
		t.Fatalf("session 10 = %+v", s)
	}
	if s := byPID[20]; s.Agent != "codex" || s.Via != "terminal" || s.Repo != repoB || s.Branch != "dev" || s.Name != filepath.Base(repoB) {
		t.Fatalf("session 20 = %+v (a folder inside the repo should resolve to the repo root)", s)
	}
	if byPID[30].Agent != "cursor" {
		t.Fatalf("session 30 = %+v", byPID[30])
	}
	if s := byPID[50]; s.Repo != "" || s.Branch != "" || s.Name != filepath.Base(plain) {
		t.Fatalf("a session in a folder that is not a git repository = %+v", s)
	}
}

func TestSessionsDoNotListTheAgentsTheUIStarted(t *testing.T) {
	uiFakeProcs(t, nil)
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	if got := m.sessions(); len(got) != 0 {
		t.Fatalf("sessions = %+v; UI-started agents belong to the Agents tab", got)
	}
}

func TestAddRepoAcceptsGitFoldersOnly(t *testing.T) {
	m := newUIManager(uiConfig{})
	repo := uiNewTestRepo(t)
	info, err := m.addRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := canonicalDir(repo)
	if info.Path != real || info.Name != filepath.Base(real) {
		t.Fatalf("info = %+v", info)
	}
	if _, err := m.addRepo(repo); err != nil || len(m.repoList()) != 1 {
		t.Fatalf("adding the same folder twice: err=%v repos=%d", err, len(m.repoList()))
	}
	os.MkdirAll(filepath.Join(repo, "deep"), 0o755)
	if in, err := m.addRepo(filepath.Join(repo, "deep")); err != nil || in.Path != real || len(m.repoList()) != 1 {
		t.Fatalf("a folder inside a repo should add the repo root: %+v %v", in, err)
	}
	file := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	for _, bad := range []string{t.TempDir(), file, "/nonexistent/path", "", "relative/path"} {
		if _, err := m.addRepo(bad); err == nil {
			t.Fatalf("addRepo(%q) accepted", bad)
		}
	}
	if got, err := m.resolveRepo(""); err != nil || got != real {
		t.Fatalf("resolveRepo = %q, %v", got, err)
	}
}

func TestAgentsCanStartInAFolderAddedLater(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	other := uiNewBranchRepo(t, "dev")
	if _, err := m.start(uiStartRequest{Repo: other, Prompt: "go"}); err == nil {
		t.Fatal("started in a folder that was never added")
	}
	if _, err := m.addRepo(other); err != nil {
		t.Fatal(err)
	}
	a, err := m.start(uiStartRequest{Repo: other, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
}

func TestSessionAndRepoEndpoints(t *testing.T) {
	repoB := uiNewBranchRepo(t, "dev")
	uiFakeProcs(t, []uiProc{{Interactive: true, PID: 7, PPID: 1, Cmd: []string{"codex"}, Cwd: repoB, Started: time.Now()}})
	srv, m, _ := uiTestServer(t, false)
	m.mcp.markInUse("codex") // the page was opened through Codex
	if r := uiDo(t, "GET", srv.URL+"/api/sessions", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var list []uiSession
	rr := uiDo(t, "GET", srv.URL+"/api/sessions", nil, uiAuthed)
	json.NewDecoder(rr.Body).Decode(&list)
	if len(list) != 1 || list[0].Agent != "codex" || list[0].Branch != "dev" {
		t.Fatalf("sessions = %+v", list)
	}
	noHeader := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+uiTestToken) }
	if r := uiDo(t, "POST", srv.URL+"/api/repos", map[string]string{"path": repoB}, noHeader); r.StatusCode != 403 {
		t.Fatalf("repos without X-Cully-UI: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/repos", map[string]string{"path": t.TempDir()}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("non-git folder: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/repos", map[string]string{"path": repoB}, uiAuthed); r.StatusCode != 201 {
		t.Fatalf("add repo: %d", r.StatusCode)
	}
	var info struct{ RepoInfo []uiRepoInfo }
	json.NewDecoder(uiDo(t, "GET", srv.URL+"/api/info", nil, uiAuthed).Body).Decode(&info)
	found := false
	for _, r := range info.RepoInfo {
		found = found || r.Path == repoB
	}
	if !found {
		t.Fatalf("added repo missing from info: %+v", info.RepoInfo)
	}
}

func TestAppListsSessionsAcrossTheMachine(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"/api/sessions", "/api/repos"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js does not use %s", want)
		}
	}
	html, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(html), "Sessions") {
		t.Fatal("index.html has no Sessions section")
	}
}

func TestSessionsRecognizeCursorsAgentBinaryAndMarkDetachedOnes(t *testing.T) {
	repo := uiNewTestRepo(t)
	now := time.Now()
	cursorCmd := []string{"/home/u/.local/bin/agent", "--disable-sigusr1", "--use-system-ca", "/home/u/.local/share/cursor-agent/versions/2026/index.js", "resume"}
	uiFakeProcs(t, []uiProc{
		{PID: 1, PPID: 9, Cmd: cursorCmd, Cwd: repo, Started: now, Interactive: true},
		{PID: 2, PPID: 9, Cmd: cursorCmd, Cwd: repo, Started: now.Add(-time.Hour), Detached: true},
		{PID: 3, PPID: 1, Cmd: []string{"codex", "app-server", "--listen", "unix://"}, Cwd: repo, Started: now}, // background daemon
		{PID: 4, PPID: 1, Cmd: []string{"claude"}, Cwd: repo, Started: now},                                     // no terminal at all
	})
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	got := m.sessions()
	if len(got) != 2 {
		t.Fatalf("sessions = %+v, want the live and the detached Cursor session only", got)
	}
	if got[0].PID != 1 || got[0].Agent != "cursor" || got[0].Detached {
		t.Fatalf("first = %+v, want the live Cursor session first", got[0])
	}
	if got[1].PID != 2 || !got[1].Detached {
		t.Fatalf("second = %+v, want the detached session marked", got[1])
	}
}

func TestAgentOfHandlesInterpretersAndTheAgentBinary(t *testing.T) {
	for _, c := range []struct {
		cmd   []string
		agent string
	}{
		{[]string{"claude"}, "claude"},
		{[]string{"/x/bin/codex", "--model", "o3"}, "codex"},
		{[]string{"node", "/x/cursor-agent/index.js"}, "cursor"},
		{[]string{"/x/bin/agent", "--disable-sigusr1", "/x/share/cursor-agent/v/index.js"}, "cursor"},
		{[]string{"/x/bin/agent", "--something"}, ""},
		{[]string{"bash", "-lc", "echo claude"}, ""},
		{[]string{"cully", "run", "claude"}, ""},
		{nil, ""},
	} {
		if got, _ := uiAgentOf(c.cmd); got != c.agent {
			t.Fatalf("uiAgentOf(%v) = %q, want %q", c.cmd, got, c.agent)
		}
	}
}

func TestSessionsCanHideFolders(t *testing.T) {
	repoA := uiNewTestRepo(t)
	work := filepath.Join(t.TempDir(), "work")
	os.MkdirAll(filepath.Join(work, "team", "proj"), 0o755)
	now := time.Now()
	uiFakeProcs(t, []uiProc{
		{Interactive: true, PID: 1, PPID: 9, Cmd: []string{"claude"}, Cwd: repoA, Started: now},
		{Interactive: true, PID: 2, PPID: 9, Cmd: []string{"claude"}, Cwd: filepath.Join(work, "team", "proj"), Started: now},
		{Interactive: true, PID: 3, PPID: 9, Cmd: []string{"claude"}, Cwd: work + "-other", Started: now},
	})
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.HideSessionsIn = []string{work}
	var pids []int
	for _, s := range m.sessions() {
		pids = append(pids, s.PID)
	}
	if len(pids) != 2 {
		t.Fatalf("pids = %v, want the folder under %s hidden and its look-alike sibling kept", pids, work)
	}
	for _, p := range pids {
		if p == 2 {
			t.Fatal("a session inside a hidden folder was listed")
		}
	}
}

func TestParseUIFlagsHideSessions(t *testing.T) {
	o, err := parseUIFlags([]string{"--hide-sessions-in", "/a/b", "--hide-sessions-in", "/c"})
	if err != nil || len(o.HideSessionsIn) != 2 {
		t.Fatalf("options = %+v, err = %v", o, err)
	}
	if _, err := parseUIFlags([]string{"--hide-sessions-in", "relative"}); err == nil {
		t.Fatal("a relative path was accepted")
	}
	t.Setenv("CULLY_UI_HIDE_SESSIONS_IN", "/x/y:/z")
	o, _ = parseUIFlags(nil)
	if len(o.HideSessionsIn) != 2 || o.HideSessionsIn[0] != "/x/y" {
		t.Fatalf("environment folders = %v", o.HideSessionsIn)
	}
}

func TestSessionsSayWhichAgentsYouLaunchedCullyWith(t *testing.T) {
	repo := uiNewTestRepo(t)
	now := time.Now()
	uiFakeProcs(t, []uiProc{
		{Interactive: true, PID: 1, PPID: 9, Cmd: []string{"claude"}, Cwd: repo, Started: now},
		{Interactive: true, PID: 2, PPID: 9, Cmd: []string{"codex"}, Cwd: repo, Started: now.Add(-time.Minute)},
		{Detached: true, PID: 3, PPID: 9, Cmd: []string{"/x/bin/agent", "/x/cursor-agent/index.js"}, Cwd: repo, Started: now.Add(-time.Hour)},
	})
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	inUse := func() map[string]bool {
		got := map[string]bool{}
		for _, s := range m.sessions() {
			got[s.Agent] = s.InUse
		}
		return got
	}
	if got := inUse(); !got["claude"] || got["codex"] || got["cursor"] {
		t.Fatalf("by default only Claude is in use (UI-started agents run it): %v", got)
	}
	m.mcp.inUse = map[string]struct{}{}
	m.mcp.markInUse("codex") // cully run --ui codex
	if got := inUse(); got["claude"] || !got["codex"] || got["cursor"] {
		t.Fatalf("after launching with Codex: %v", got)
	}
}

func TestAppHidesClosedTerminalsByDefault(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"terminal was closed", "/api/sessions"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js lacks %q", want)
		}
	}
}
