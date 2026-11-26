package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnlyWellFormedAgentProposalsAreKept(t *testing.T) {
	folders := map[string]string{"proj": "/tmp/proj"}
	taken := map[string]bool{"test guardian": true}
	out := strings.Join([]string{
		"AGENT|PR watcher|Reports open pull requests.|Read only. Never merge.|read,commands|daily|proj|Checked PRs by hand in 3 sessions",
		"AGENT|Test guardian|dup|x|read|none|proj|already exists",
		"AGENT|Bad folder|x|y|read|daily|elsewhere|z",
		"AGENT|Bad schedule|x|y|read|weekly|proj|z",
		"AGENT|Bad/name|x|y|read|none|proj|z",
		"AGENT|Tools check|Looks at things.|Careful.|nonsense,web|none|proj|why",
		"AGENT|too|few|fields",
		"noise",
	}, "\n")
	got := uiParseAgentSuggestions(out, folders, taken)
	if len(got) != 2 || got[0].Name != "PR watcher" || got[0].Schedule != "daily" || !got[0].Groups["commands"] || got[0].Folder != "/tmp/proj" {
		t.Fatalf("got = %+v", got)
	}
	if !got[1].Groups["web"] || got[1].Groups["nonsense"] || got[1].Schedule != "" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestCullyProposesAgentsFromNamedSessionsAndTheUserApproves(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	for i, id := range []string{uiTestSessA, uiTestSessB, uiTestSessC} {
		uiWriteRichTranscript(t, dir, id, repo)
		// spread over two days so the same kind of task counts as repeating
		p := filepath.Join(dir, id+".jsonl")
		mt := time.Now().Add(-time.Duration(2+i*30) * time.Hour)
		_ = os.Chtimes(p, mt, mt)
	}
	name := filepath.Base(repo)
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "propose named agents") {
			return "AGENT|Test guardian|Runs the tests each morning.|Read only. Never edit files.|read,commands|daily|" + name + "|Tests were skipped in three sessions", nil
		}
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: Fix login\nCHECKS: y", nil
	}
	if _, err := m.agentSuggestRun(); err == nil {
		t.Fatal("proposed agents with nothing named")
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	for i := 0; i < 3; i++ {
		m.analyzeNext()
	}
	n, err := m.agentSuggestRun()
	if err != nil || n != 1 {
		t.Fatalf("added %d, err %v", n, err)
	}
	l := m.agentSuggestionsList()
	if len(l) != 1 || l[0].Name != "Test guardian" || l[0].Folder != repo {
		t.Fatalf("suggestions = %+v", l)
	}
	if len(m.identityList()) != 0 {
		t.Fatal("an agent was created without the user's approval")
	}
	if n, _ := m.agentSuggestRun(); n != 0 {
		t.Fatalf("the same proposal was made again: %d", n)
	}
	if err := m.agentSuggestionSet(l[0].ID, "accepted"); err != nil || len(m.agentSuggestionsList()) != 0 {
		t.Fatalf("accept: %v", err)
	}
	if err := m.agentSuggestionSet("nope", "dismissed"); err == nil {
		t.Fatal("unknown suggestion accepted")
	}
}

func TestAgentSuggestionEndpoints(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/agent-suggestions", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	uiSuggestMu.Lock()
	uiWriteAgentSuggestions([]uiAgentSuggestion{{ID: "s1", Name: "A", Role: "r", Folder: repo, State: "new", Groups: map[string]bool{"read": true}}})
	uiSuggestMu.Unlock()
	_ = m
	var l []uiAgentSuggestion
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/agent-suggestions", nil, uiAuthed), &l)
	if len(l) != 1 {
		t.Fatalf("list = %+v", l)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agent-suggestions/s1/bogus", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bad state: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agent-suggestions/s1/dismissed", nil, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("dismiss: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/agent-suggestions/run", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("run with nothing named: %d", r.StatusCode)
	}
}

func TestAgentsPageShowsProposalsForApproval(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"Suggested for you", "Review and approve", "/api/agent-suggestions", "fromSuggestion", "Ask Cully for ideas"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}

func TestRepeatedWorkNeedsTheSameGoalThreeTimesOnTwoDays(t *testing.T) {
	now := time.Now()
	mk := func(task string, daysAgo int, unverified bool) uiIndexed {
		return uiIndexed{Source: "ai", Task: task, Cwd: "/p/x", Name: "x", Updated: now.AddDate(0, 0, -daysAgo), uiSessionFacts: uiSessionFacts{Unverified: unverified}}
	}
	same := []uiIndexed{mk("Check open pull requests and report status", 0, false), mk("Report the status of open pull requests", 1, false), mk("check the open pull requests status", 3, false)}
	if r := uiRepeatedWork(same); len(r) != 1 || r[0].Kind != "goal" || r[0].Count != 3 || r[0].Days != 3 {
		t.Fatalf("repeats = %+v", r)
	}
	oneDay := []uiIndexed{mk("Check open pull requests and report status", 0, false), mk("Report the status of open pull requests", 0, false), mk("check the open pull requests status", 0, false)}
	if r := uiRepeatedWork(oneDay); len(r) != 0 {
		t.Fatalf("three in one day counted as repeating: %+v", r)
	}
	twice := same[:2]
	if r := uiRepeatedWork(twice); len(r) != 0 {
		t.Fatalf("two is not enough: %+v", r)
	}
	different := []uiIndexed{mk("Fix the login bug", 0, false), mk("Add a billing report page", 1, false), mk("Upgrade the database driver", 2, false)}
	if r := uiRepeatedWork(different); len(r) != 0 {
		t.Fatalf("unrelated tasks counted: %+v", r)
	}
	untested := []uiIndexed{mk("a one", 0, true), mk("b two", 1, true), mk("c three", 2, true)}
	if r := uiRepeatedWork(untested); len(r) != 1 || r[0].Kind != "untested" {
		t.Fatalf("untested = %+v", r)
	}
}

func TestNoAgentIsProposedWhenNothingRepeats(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	tasks := []string{"Fix the login bug", "Add a billing page", "Upgrade the driver"}
	for i, id := range []string{uiTestSessA, uiTestSessB, uiTestSessC} {
		uiWriteRichTranscript(t, dir, id, repo)
		_ = tasks[i]
	}
	asked := 0
	n := 0
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "propose named agents") {
			asked++
			return "", nil
		}
		n++
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: " + tasks[(n-1)%3] + "\nCHECKS: y", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	for i := 0; i < 3; i++ {
		m.analyzeNext()
	}
	if added, err := m.agentSuggestRun(); err != nil || added != 0 || asked != 0 {
		t.Fatalf("added %d, asked %d, err %v: the advisor was asked with nothing repeating", added, asked, err)
	}
	m.agentSuggestIfDue()
	if asked != 0 {
		t.Fatal("asked")
	}
}

func TestAgentProposalsCheckExistingAgentsFirst(t *testing.T) {
	groups := map[string]bool{"read": true}
	existing := []uiIdentity{{ID: "a1", Name: "PR watcher", Role: "Reports open pull requests and what blocks them.", Folder: "/tmp/proj", Groups: groups, Schedule: "daily"}}
	mk := func(name, role string, g map[string]bool, sched string) uiAgentSuggestion {
		return uiAgentSuggestion{ID: name, Name: name, Role: role, Folder: "/tmp/proj", Groups: g, Schedule: sched, State: "new"}
	}
	// Says the same thing as an agent that exists: nothing to propose.
	if got := uiAgentSuggestFilter([]uiAgentSuggestion{mk("Pull request watcher", "Reports open pull requests and what blocks them.", groups, "daily")}, existing, nil); len(got) != 0 {
		t.Fatalf("proposed what exists: %+v", got)
	}
	// Same job with a change (now also runs commands, hourly): an update of that agent.
	got := uiAgentSuggestFilter([]uiAgentSuggestion{mk("PR watcher", "Reports open pull requests and what blocks them.", map[string]bool{"read": true, "commands": true}, "hourly")}, existing, nil)
	if len(got) != 1 || got[0].Replaces != "a1" || got[0].Name != "PR watcher" {
		t.Fatalf("update = %+v", got)
	}
	// A different job in the same folder is a new agent.
	got = uiAgentSuggestFilter([]uiAgentSuggestion{mk("Release notes", "Drafts release notes from merged changes.", groups, "none")}, existing, nil)
	if len(got) != 1 || got[0].Replaces != "" {
		t.Fatalf("new = %+v", got)
	}
	// One already waiting, or rejected, is not proposed again.
	prior := []uiAgentSuggestion{{Name: "Release notes", Role: "Drafts release notes from merged changes.", Folder: "/tmp/proj", State: "dismissed"}}
	if got := uiAgentSuggestFilter([]uiAgentSuggestion{mk("Release writer", "Drafts release notes from merged changes.", groups, "none")}, existing, prior); len(got) != 0 {
		t.Fatalf("proposed a rejected idea again: %+v", got)
	}
}
