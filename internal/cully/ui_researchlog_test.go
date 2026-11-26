package cully

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchIsKeptOnDiskPrivatelyAndFilteredByFolder(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-secret-9z")
	m.recordResearch(uiResearchRec{Kind: "scout", Source: "a", Repo: repo, Query: "q1", Text: "found https://x with tok-secret-9z"})
	m.recordResearch(uiResearchRec{Kind: "advice", Source: "a", Repo: repo, Text: "add tests", Level: "advice"})
	hidden := t.TempDir()
	m.recordResearch(uiResearchRec{Kind: "scout", Source: "b", Repo: hidden, Text: "company"})
	m.cfg.HideSessionsIn = []string{hidden}
	all := m.researchList("", 10)
	if len(all) != 2 || all[0].Kind != "advice" {
		t.Fatalf("all = %+v", all)
	}
	if s := m.researchList("scout", 10); len(s) != 1 || strings.Contains(s[0].Text, "tok-secret-9z") {
		t.Fatalf("scouts = %+v", s)
	}
	if st, err := os.Stat(filepath.Join(cullyDir(), "research-log.jsonl")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", st, err)
	}
	m2 := newUIManager(m.cfg)
	if len(m2.researchList("", 10)) != 2 {
		t.Fatal("research did not survive a restart")
	}
}

func TestResearchLogKeepsOnlyTheNewest(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for i := 0; i < uiResearchKeep+120; i++ {
		m.recordResearch(uiResearchRec{Kind: "scout", Repo: repo, Text: "x"})
	}
	if n := len(uiReadResearch()); n > uiResearchKeep+100 || n < uiResearchKeep {
		t.Fatalf("kept %d", n)
	}
}

func TestStandaloneScoutIsRecorded(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.ScoutStream = func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
		return "finding https://example.com", nil
	}
	if _, err := m.startScout("go modules"); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the scout to be recorded", func() bool { return len(m.researchList("scout", 5)) == 1 })
	if r := m.researchList("scout", 5)[0]; r.Query != "go modules" || !strings.Contains(r.Text, "example.com") {
		t.Fatalf("record = %+v", r)
	}
}

func TestResearchEndpoint(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	m.recordResearch(uiResearchRec{Kind: "scout", Repo: repo, Text: "t"})
	if r := uiDo(t, "GET", srv.URL+"/api/research", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var l []uiResearchRec
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/research?kind=scout", nil, uiAuthed), &l)
	if len(l) != 1 {
		t.Fatalf("list = %+v", l)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/research?kind=bogus", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bogus kind: %d", r.StatusCode)
	}
}
