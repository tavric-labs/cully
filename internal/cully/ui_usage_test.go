package cully

import (
	"os"
	"strings"
	"testing"
	"time"
)

func uiFinishedTaskWithUsage(t *testing.T, m *uiManager, repo, in string) *uiAgent {
	t.Helper()
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	a.stop()
	uiWaitFor(t, "the task's usage to be recorded", func() bool { return len(uiReadUsage()) > 0 && uiReadUsage()[len(uiReadUsage())-1].TaskID == a.id })
	return a
}

const uiUsageFake = uiFakeHead + `  echo '{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"input_tokens":100,"cache_read_input_tokens":900,"cache_creation_input_tokens":0,"output_tokens":10}}'
done
`

func TestFinishedTasksLeaveACountOnlyUsageRecord(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, uiUsageFake), "tok-aaa1")
	a := uiFinishedTaskWithUsage(t, m, repo, "")
	recs := uiReadUsage()
	if len(recs) != 1 || recs[0].Fresh != 100 || recs[0].Cached != 900 || recs[0].Output != 10 || recs[0].Turns != 1 || recs[0].TaskID != a.id {
		t.Fatalf("records = %+v", recs)
	}
	for _, raw := range uiDB().logAll("usage") {
		if strings.Contains(string(raw), "\"prompt\"") {
			t.Fatal("a prompt reached the usage log")
		}
	}
	if st, err := os.Stat(uiStorePath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("database file: %v %v", st, err)
	}
	m.recordUsage(a) // a second record for the same task is not written
	if len(uiReadUsage()) != 1 {
		t.Fatal("the same task was recorded twice")
	}
}

func TestSavingsSummaryShowsCacheAndWhatLimitingServersSaved(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, uiUsageFake), "tok-aaa1")
	hidden := t.TempDir()
	now := time.Now().UTC()
	for _, r := range []uiUsageRec{
		{Time: now, TaskID: "a", Title: "plain 1", Repo: repo, Turns: 2, Fresh: 4000, Cached: 6000, Output: 100},
		{Time: now, TaskID: "b", Title: "plain 2", Repo: repo, Turns: 2, Fresh: 4000, Cached: 6000, Output: 100},
		{Time: now, TaskID: "c", Title: "lean", Repo: repo, Turns: 2, Fresh: 1000, Cached: 3000, Output: 100, McpFiltered: true},
		{Time: now.AddDate(0, 0, -40), TaskID: "old", Title: "old", Repo: repo, Turns: 1, Fresh: 1, Cached: 1, Output: 1},
		{Time: now, TaskID: "h", Title: "hidden", Repo: hidden, Turns: 1, Fresh: 9, Cached: 9, Output: 9},
	} {
		uiDB().logAdd("usage", r, 0)
	}
	m.cfg.HideSessionsIn = []string{hidden}
	s := m.usageSummary(14)
	if s.Tasks != 3 || s.Tokens != 24300 || s.Cached != 15000 || s.CachedShare != 62 || s.Saved != 13500 {
		t.Fatalf("summary = %+v", s)
	}
	if s.AllPerTurn != 5000 || s.FilteredPerTurn != 2000 || s.AllTasks != 2 || s.FilteredTasks != 1 || s.MCPSaved != 6000 {
		t.Fatalf("mcp = %+v", s)
	}
	if len(s.Daily) != 14 || s.Daily[13].Tasks != 3 || len(s.Folders) != 1 || s.Top[0].Title != "plain 1" && s.Top[0].Title != "plain 2" {
		t.Fatalf("shape = %+v", s)
	}
}

func TestUsageEndpoint(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/usage", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var s uiUsageSummary
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/usage?days=7", nil, uiAuthed), &s)
	if s.Days != 7 || len(s.Daily) != 7 {
		t.Fatalf("summary = %+v", s)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/usage?days=0", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bad days: %d", r.StatusCode)
	}
}

func TestSavingsPageShowsRealTaskTokens(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"/api/usage?days=", "Tokens used by your tasks", "Saved by the cache", "Limiting connected servers saved", "Where the tokens went", "Biggest tasks"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
