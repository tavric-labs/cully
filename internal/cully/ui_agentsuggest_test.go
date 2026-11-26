package cully

import (
	"path/filepath"
	"strings"
	"testing"
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
	for _, id := range []string{uiTestSessA, uiTestSessB, uiTestSessC} {
		uiWriteRichTranscript(t, dir, id, repo)
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
	for _, want := range []string{"Suggested for you", "Review and create", "/api/agent-suggestions", "fromSuggestion", "Ask Cully for ideas"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
