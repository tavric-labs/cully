package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGroupFilesByFolder(t *testing.T) {
	got := uiGroupFiles([]string{
		"internal/cully/a.go", "internal/cully/b.go", "internal/cully/ui_static/app.js",
		"docs/ui.md", "docs/plan.md", "CHANGELOG.md", ".claude/settings.json", "env",
		"cmd/cully/main.go",
	})
	want := []uiFileGroup{
		{"internal/cully", 3}, {"docs", 2}, {"Local settings", 2}, {"cmd/cully", 1}, {"Top level", 1},
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("group %d = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
	many := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		many = append(many, "d"+itoa(i)+"/sub/f.go")
	}
	if g := uiGroupFiles(many); len(g) != uiGroupsMax+1 || g[len(g)-1].Name != "Other" {
		t.Fatalf("too many groups were not folded into Other: %+v", g)
	}
}

func TestSummariesCarryFileGroups(t *testing.T) {
	repo := uiNewTestRepo(t)
	os.MkdirAll(filepath.Join(repo, "pkg", "x"), 0o755)
	for _, f := range []string{"pkg/x/a.go", "pkg/x/b.go", "README.md"} {
		os.WriteFile(filepath.Join(repo, f), []byte("x\n"), 0o644)
	}
	s := uiRepoSummary(repo)
	if s.FileCount != 3 && s.FileCount != 2 {
		t.Fatalf("file count = %d", s.FileCount)
	}
	found := false
	for _, g := range s.Groups {
		found = found || (strings.HasPrefix(g.Name, "pkg") && g.Count >= 1)
	}
	if !found {
		t.Fatalf("groups = %+v", s.Groups)
	}
}

func TestSetupDescribesEachAgentWithoutInternals(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	v := uiSetup()
	if len(v.Agents) != 3 {
		t.Fatalf("agents = %+v", v.Agents)
	}
	for _, a := range v.Agents {
		if len(a.Items) == 0 {
			t.Fatalf("%s has no checks", a.Agent)
		}
		if a.Ready {
			t.Fatalf("%s reported ready on a machine with nothing installed", a.Agent)
		}
		for _, it := range a.Items {
			if it.Label == "" || (!it.OK && it.Hint == "") {
				t.Fatalf("check %+v needs a label and, when it fails, a hint in plain words", it)
			}
		}
	}
	raw, _ := json.Marshal(v)
	for _, internal := range []string{"Stop/analyze", "SessionEnd", "pane-signal", "continuity", "PostToolUse", "hook", "daemon", "cully_"} {
		if strings.Contains(string(raw), internal) {
			t.Fatalf("setup view exposes %q: %s", internal, raw)
		}
	}
	if v.AllReady {
		t.Fatal("AllReady with nothing installed")
	}
}

func TestImpactViewSummarisesTheLedger(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	recordImpact("s1", impactAnalysisSkipped, "", 1000)
	recordImpact("s1", impactAnalysisSkipped, "", 1700)
	v := uiImpact(14)
	if v.Days != 14 || v.Saved != 2700 || v.Net != 2700 || v.AnalysesSkipped != 2 || v.Sessions != 1 {
		t.Fatalf("view = %+v", v)
	}
	if len(v.Daily) != 14 || v.Daily[13].Saved != 2700 || v.Daily[0].Saved != 0 {
		t.Fatalf("daily = %+v", v.Daily)
	}
	if uiImpact(0).Days != 14 || uiImpact(500).Days != 90 {
		t.Fatal("days not defaulted and clamped")
	}
}

func TestActivityMapsTheJournalToPlainEvents(t *testing.T) {
	repo := uiNewTestRepo(t)
	base := time.Now().Add(-10 * time.Minute)
	appendJournal(repo, "s1", journalEvent{Time: base, Class: journalStart, Agent: "claude"})
	appendJournal(repo, "s1", journalEvent{Time: base.Add(1 * time.Minute), Class: journalEdit, Tool: "Edit", Path: "a.go", Op: opEdit})
	appendJournal(repo, "s1", journalEvent{Time: base.Add(2 * time.Minute), Class: journalSearch, Path: "b.go", Op: opRead})
	appendJournal(repo, "s1", journalEvent{Time: base.Add(3 * time.Minute), Class: journalCheck, Cmd: "go test", Failed: true})
	appendJournal(repo, "s1", journalEvent{Time: base.Add(4 * time.Minute), Class: journalOther, Cmd: "git status"})
	v := uiActivity(repo)
	if v.Sessions != 1 || !v.Recorded {
		t.Fatalf("sessions=%d recorded=%v", v.Sessions, v.Recorded)
	}
	kinds := []string{}
	for _, e := range v.Events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "start,edit,read,check,command" {
		t.Fatalf("kinds = %v", kinds)
	}
	if e := v.Events[1]; e.Path != "a.go" || e.Label != "a.go" {
		t.Fatalf("edit = %+v", e)
	}
	if e := v.Events[3]; e.Label != "go test" || !e.Failed {
		t.Fatalf("check = %+v", e)
	}
	if v.Facts.Edits != 1 || v.Facts.Checks.Failed != 1 {
		t.Fatalf("facts = %+v", v.Facts)
	}
	for i := 1; i < len(v.Events); i++ {
		if v.Events[i].Time.Before(v.Events[i-1].Time) {
			t.Fatal("events are not in time order")
		}
	}
}

func TestActivityWithoutAJournalIsEmptyNotNull(t *testing.T) {
	repo := uiNewTestRepo(t)
	v := uiActivity(repo)
	raw, _ := json.Marshal(v)
	if v.Recorded || v.Sessions != 0 || !strings.Contains(string(raw), `"events":[]`) {
		t.Fatalf("view = %s", raw)
	}
}

func TestDashboardDataEndpoints(t *testing.T) {
	srv, _, repo := uiTestServer(t, false)
	for _, path := range []string{"/api/setup", "/api/impact?days=7", "/api/activity?repo=" + repo} {
		if r := uiDo(t, "GET", srv.URL+path, nil, nil); r.StatusCode != 401 {
			t.Fatalf("%s without a token: %d", path, r.StatusCode)
		}
		if r := uiDo(t, "GET", srv.URL+path, nil, uiAuthed); r.StatusCode != 200 {
			t.Fatalf("%s: %d", path, r.StatusCode)
		}
	}
	if r := uiDo(t, "GET", srv.URL+"/api/activity?repo=/etc", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("activity for a folder that was never added: %d", r.StatusCode)
	}
}

func TestActivityIncludesAgentsStartedFromThePage(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.NoConflictAlerts = true
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	uiWaitState(t, a, "waiting")
	v := m.activity(repo)
	if !v.Recorded || v.Sessions < 1 {
		t.Fatalf("recorded=%v sessions=%d: the page's own agents are missing from Activity", v.Recorded, v.Sessions)
	}
	edit := false
	for _, e := range v.Events {
		edit = edit || (e.Kind == "edit" && e.Path == "a.txt")
	}
	if !edit || v.Facts.Edits < 1 {
		t.Fatalf("events = %+v facts = %+v", v.Events, v.Facts)
	}
}
