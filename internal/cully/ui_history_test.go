package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	uiTestSessA = "11111111-1111-4111-8111-111111111111"
	uiTestSessB = "22222222-2222-4222-8222-222222222222"
	uiTestSessC = "33333333-3333-4333-8333-333333333333"
)

func uiWriteTranscript(t *testing.T, dir, id, cwd, title string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	recs := []map[string]any{
		{"type": "permission-mode", "sessionId": id},
		{"type": "user", "sessionId": id, "cwd": cwd, "gitBranch": "feat/x", "timestamp": time.Now().Add(-age).UTC().Format(time.RFC3339Nano), "message": map[string]any{"content": "SECRET PROMPT"}},
	}
	if title != "" {
		recs = append(recs, map[string]any{"type": "ai-title", "sessionId": id, "aiTitle": title})
	}
	var b strings.Builder
	for _, r := range recs {
		line, _ := json.Marshal(r)
		b.Write(line)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	_ = os.Chtimes(p, mt, mt)
}

func TestHistoryListsRecentClaudeSessionsAndOffersOlder(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	uiListProcsSaved := uiListProcs
	uiListProcs = func() []uiProc { return nil }
	t.Cleanup(func() { uiListProcs = uiListProcsSaved })
	proj := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteTranscript(t, proj, uiTestSessA, repo, "Fix the gateway", 2*time.Hour)
	uiWriteTranscript(t, proj, uiTestSessB, repo, "", 30*24*time.Hour)
	h := m.history("claude", 7)
	if len(h.Sessions) != 1 || h.Sessions[0].ID != uiTestSessA || h.Sessions[0].Title != "Fix the gateway" || h.Sessions[0].Branch != "feat/x" {
		t.Fatalf("history = %+v", h)
	}
	if !h.Older {
		t.Fatal("the older session was not offered")
	}
	s := h.Sessions[0]
	if !s.Exists || s.Open || !strings.Contains(s.Resume, "claude --resume "+uiTestSessA) || !strings.HasSuffix(s.Fork, "--fork-session") {
		t.Fatalf("session = %+v", s)
	}
	if all := m.history("claude", 0); len(all.Sessions) != 2 || all.Older {
		t.Fatalf("all = %+v", all)
	}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), "SECRET PROMPT") {
		t.Fatal("conversation text reached the listing")
	}
}

func TestHistorySkipsHiddenFoldersAndCullyWorktreesAndMarksOpen(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	proj := filepath.Join(ConfigDir(), "projects", "enc")
	hiddenDir := t.TempDir()
	m.cfg.HideSessionsIn = []string{hiddenDir}
	uiWriteTranscript(t, proj, uiTestSessA, hiddenDir, "private", time.Hour)
	uiWriteTranscript(t, proj, uiTestSessB, filepath.Join(ConfigDir(), "ui-worktrees", "abc"), "cully run", time.Hour)
	uiWriteTranscript(t, proj, uiTestSessC, repo, "mine", time.Minute)
	saved := uiListProcs
	uiListProcs = func() []uiProc {
		return []uiProc{{PID: 9, Cmd: []string{"claude"}, Cwd: repo, Interactive: true}}
	}
	t.Cleanup(func() { uiListProcs = saved })
	h := m.history("claude", 7)
	if len(h.Sessions) != 1 || h.Sessions[0].ID != uiTestSessC || !h.Sessions[0].Open {
		t.Fatalf("history = %+v", h)
	}
}

func TestHistoryListsCodexSessions(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "11", "26")
	_ = os.MkdirAll(dir, 0o755)
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": uiTestSessA, "cwd": repo, "timestamp": time.Now().UTC().Format(time.RFC3339Nano)}})
	_ = os.WriteFile(filepath.Join(dir, "rollout-2026-11-26T01-00-00-"+uiTestSessA+".jsonl"), append(meta, '\n'), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "rollout-bad.jsonl"), []byte("not json\n"), 0o600)
	h := m.history("codex", 7)
	if len(h.Sessions) != 1 || h.Sessions[0].Resume != "cd '"+repo+"' && codex resume "+uiTestSessA {
		t.Fatalf("history = %+v", h)
	}
	if c := m.history("cursor", 7); len(c.Sessions) != 0 || c.Note == "" {
		t.Fatalf("cursor = %+v", c)
	}
}

func TestHistoryEndpointCoversEveryHarness(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/history", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/history", nil, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("default: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/history?agent=codex", nil, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("codex: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/history?agent=bogus", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/history?days=abc", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bad days: %d", r.StatusCode)
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	if got := uiShellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("quote = %s", got)
	}
}

func uiResumeFixture(t *testing.T) (*uiManager, string, string) {
	t.Helper()
	body := strings.Replace(fakeClaude, `echo "$@"`, `echo "$PWD" >> "$FAKE_OUT/pwd.log"`+"\n"+`echo "$@"`, 1)
	m, repo, out := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1")
	saved := uiListProcs
	uiListProcs = func() []uiProc { return nil }
	t.Cleanup(func() { uiListProcs = saved })
	uiWriteTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo, "Fix the gateway", 2*time.Hour)
	return m, repo, out
}

func TestResumeRunsClaudeInTheOriginalFolderWithTheSameSession(t *testing.T) {
	m, repo, out := uiResumeFixture(t)
	a, err := m.resumeSession(uiTestSessA, "carry on", false)
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(args), "--resume "+uiTestSessA) || strings.Contains(string(args), "--fork-session") {
		t.Fatalf("args = %s", args)
	}
	pwd, _ := os.ReadFile(filepath.Join(out, "pwd.log"))
	real, _ := canonicalDir(repo)
	if got, _ := canonicalDir(strings.TrimSpace(string(pwd))); got != real {
		t.Fatalf("ran in %q, want %q", got, real)
	}
	if v := a.snapshot(); v.Worktree != repo || !strings.Contains(v.Title, "Fix the gateway") {
		t.Fatalf("view = %+v", v)
	}
	if _, err := os.Stat(filepath.Join(ConfigDir(), "ui-worktrees", a.id)); err == nil {
		t.Fatal("a worktree was made for a resumed session")
	}
	if _, err := m.resumeSession(uiTestSessA, "again", false); err == nil {
		t.Fatal("the same session was resumed twice at once")
	}
}

func TestForkStartsFromTheSessionAndMayRunBesideTheOriginal(t *testing.T) {
	m, _, out := uiResumeFixture(t)
	a, err := m.resumeSession(uiTestSessA, "", true)
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(args), "--resume "+uiTestSessA+" --fork-session") {
		t.Fatalf("args = %s", args)
	}
}

func TestResumeRefusesOpenHiddenUnknownAndBadSessions(t *testing.T) {
	m, repo, _ := uiResumeFixture(t)
	for _, id := range []string{"", "../../etc/passwd", "not-a-uuid", uiTestSessB} {
		if _, err := m.resumeSession(id, "x", false); err == nil {
			t.Fatalf("resumeSession(%q) accepted", id)
		}
	}
	uiListProcs = func() []uiProc {
		return []uiProc{{PID: 9, Cmd: []string{"claude"}, Cwd: repo, Interactive: true}}
	}
	uiWriteTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo, "Fix the gateway", time.Minute)
	if _, err := m.resumeSession(uiTestSessA, "x", false); err == nil {
		t.Fatal("a session open in a terminal was resumed")
	}
	if _, err := m.resumeSession(uiTestSessA, "x", true); err != nil {
		t.Fatalf("fork of an open session refused: %v", err)
	}
	m.cfg.HideSessionsIn = []string{repo}
	if _, err := m.resumeSession(uiTestSessA, "x", true); err == nil {
		t.Fatal("a session in a hidden folder was resumed")
	}
}

func TestResumeEndpoint(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	saved := uiListProcs
	uiListProcs = func() []uiProc { return nil }
	t.Cleanup(func() { uiListProcs = saved })
	uiWriteTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo, "t", time.Hour)
	_ = m
	if r := uiDo(t, "POST", srv.URL+"/api/history/"+uiTestSessA+"/resume", map[string]any{"prompt": "go"}, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/history/"+uiTestSessB+"/resume", map[string]any{"prompt": "go"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("unknown: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/history/"+uiTestSessA+"/resume", map[string]any{"prompt": "go", "fork": true}, uiAuthed); r.StatusCode != 201 {
		t.Fatalf("fork: %d", r.StatusCode)
	}
}

func TestSessionsSayWhereTheyWereStarted(t *testing.T) {
	for _, c := range []struct{ agent, raw, want string }{
		{"claude", "cli", "Terminal"}, {"claude", "sdk-cli", "Started by a tool"}, {"claude", "sdk-py", "Started by an app (SDK)"},
		{"claude", "claude-desktop", "Claude desktop"}, {"claude", "claude-vscode", "VS Code"},
		{"codex", "cli", "Terminal"}, {"codex", "vscode", "Editor or desktop app"}, {"codex", "exec", "Started by a tool"}, {"codex", "subagent", "Helper of another session"},
		{"claude", "", ""}, {"claude", "weird-thing", "weird-thing"},
	} {
		if got := uiOriginLabel(c.agent, c.raw); got != c.want {
			t.Errorf("%s %q = %q, want %q", c.agent, c.raw, got, c.want)
		}
	}
	m, repo, _ := uiResumeFixture(t)
	p := filepath.Join(ConfigDir(), "projects", "enc", uiTestSessA+".jsonl")
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), `"type":"user"`, `"entrypoint":"cli","type":"user"`, 1)), 0o600)
	_ = repo
	h := m.history("claude", 7)
	if len(h.Sessions) != 1 || h.Sessions[0].Origin != "Terminal" {
		t.Fatalf("history = %+v", h.Sessions)
	}
}

func TestHistoryAllMergesHarnessesNewestFirst(t *testing.T) {
	m, repo, _ := uiResumeFixture(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	uiWriteCodexRollout(t, uiTestSessB, repo)
	all := m.history("all", 7)
	if len(all.Sessions) != 2 {
		t.Fatalf("all = %+v", all.Sessions)
	}
	agents := all.Sessions[0].Agent + "," + all.Sessions[1].Agent
	if agents != "claude,codex" && agents != "codex,claude" {
		t.Fatalf("agents = %s", agents)
	}
	if all.Sessions[0].Updated.Before(all.Sessions[1].Updated) {
		t.Fatal("not newest first")
	}
	if all.Note == "" {
		t.Fatal("the Cursor note is missing")
	}
}

func TestRunningNowGathersEverythingInOnePlace(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/running", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	in := uiIdentityInputFor(repo)
	a, _ := m.identityCreate(in)
	if _, err := m.identityRun(a.ID); err != nil {
		t.Fatal(err)
	}
	var v uiRunningNow
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/running", nil, uiAuthed), &v)
	if len(v.Agents) != 1 || v.Agents[0].Name != "Release watcher" || v.Agents[0].TaskID == "" || v.Scouts == nil || v.Evals == nil || v.Sessions == nil {
		t.Fatalf("running = %+v", v)
	}
}

func TestSessionsPageHasTimeTabsHarnessBadgesAndTheRunningPanel(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"'Today'", "'Last 7 days'", "'Last 15 days'", "'Last 30 days'", "'Older'", "agent=all", "harnessChip", "Running now", "/api/running", "Agents at work", "Sessions open in terminals"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
