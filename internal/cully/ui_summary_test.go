package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSummaryDescribesTheWorkInPlainTerms(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.NoConflictAlerts = true
	a, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix login", Prompt: "make login stricter"})
	uiWaitState(t, a, "waiting")
	uiCommitFile(t, a.snapshot().Worktree, "login.go", "line one\nline two\n")
	s := a.summary()
	if s.Task != "make login stricter" || s.Branch != a.snapshot().Branch {
		t.Fatalf("task/branch = %q / %q", s.Task, s.Branch)
	}
	found := false
	for _, f := range s.Files {
		found = found || f == "login.go"
	}
	if !found || s.FileCount < 1 || s.Added < 2 {
		t.Fatalf("files=%v count=%d added=%d", s.Files, s.FileCount, s.Added)
	}
	if s.LastReply != "working" {
		t.Fatalf("lastReply = %q", s.LastReply)
	}
	if !s.Activity || s.Edits < 1 {
		t.Fatalf("activity=%v edits=%d", s.Activity, s.Edits)
	}
	raw, _ := json.Marshal(s)
	for _, internal := range []string{"cully_context", "cully_log", "session_ref"} {
		if strings.Contains(string(raw), internal) {
			t.Fatalf("summary exposes the internal name %q: %s", internal, raw)
		}
	}
}

func TestSummaryCapsTheFileList(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	wt := a.snapshot().Worktree
	for i := 0; i < uiSummaryFiles+15; i++ {
		os.WriteFile(filepath.Join(wt, "f"+itoa(i)+".txt"), []byte("x\n"), 0o644)
	}
	s := a.summary()
	if len(s.Files) != uiSummaryFiles || s.FileCount < uiSummaryFiles+15 {
		t.Fatalf("shown=%d total=%d", len(s.Files), s.FileCount)
	}
}

func TestRepoHandoffSummaryWithoutHistory(t *testing.T) {
	repo := uiNewTestRepo(t)
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("hi\n"), 0o644)
	s := uiRepoSummary(repo)
	if s.Activity {
		t.Fatal("claims activity with no recorded session")
	}
	if s.Branch != "main" || s.FileCount != 1 || len(s.Files) != 1 || s.Files[0] != "new.txt" {
		t.Fatalf("summary = %+v", s)
	}
}

func TestSummaryEndpoints(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	m.cfg.NoConflictAlerts = true
	r := uiDo(t, "POST", srv.URL+"/api/agents", map[string]any{"repo": repo, "prompt": "go"}, uiAuthed)
	var v uiAgentView
	json.NewDecoder(r.Body).Decode(&v)
	a, _ := m.get(v.ID)
	uiWaitState(t, a, "waiting")
	if r := uiDo(t, "GET", srv.URL+"/api/agents/"+v.ID+"/summary", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/agents/nope/summary", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("unknown agent: %d", r.StatusCode)
	}
	var s uiHandoffView
	rr := uiDo(t, "GET", srv.URL+"/api/agents/"+v.ID+"/summary", nil, uiAuthed)
	if rr.StatusCode != 200 {
		t.Fatalf("summary: %d", rr.StatusCode)
	}
	json.NewDecoder(rr.Body).Decode(&s)
	if s.Task != "go" || s.Branch == "" {
		t.Fatalf("summary = %+v", s)
	}
	rr = uiDo(t, "GET", srv.URL+"/api/handoff?repo="+repo, nil, uiAuthed)
	if rr.StatusCode != 200 {
		t.Fatalf("repo handoff: %d", rr.StatusCode)
	}
	if bad := uiDo(t, "GET", srv.URL+"/api/handoff?repo=/etc", nil, uiAuthed); bad.StatusCode != 400 {
		t.Fatalf("repo outside the allow-list: %d", bad.StatusCode)
	}
}

func TestAppNeverShowsAgentFacingHandoffText(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, internal := range []string{"cully_context", "cully_log", "cully_get"} {
		if strings.Contains(string(js), internal) {
			t.Fatalf("app.js mentions the internal tool %q", internal)
		}
	}
	for _, want := range []string{"/summary", "/api/handoff"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("app.js does not use %s", want)
		}
	}
}
