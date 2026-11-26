package cully

import (
	"os"
	"path/filepath"
	"strconv"
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
	// One idea per run, however many the advisor offers.
	if len(got) != 1 || got[0].Name != "PR watcher" || got[0].Schedule != "daily" || !got[0].Groups["commands"] || got[0].Folder != "/tmp/proj" {
		t.Fatalf("got = %+v", got)
	}
	more := uiParseAgentSuggestions("AGENT|Tools check|Looks at things.|Careful.|nonsense,web|none|proj|why", folders, map[string]bool{})
	if len(more) != 1 || !more[0].Groups["web"] || more[0].Groups["nonsense"] || more[0].Schedule != "" {
		t.Fatalf("tools check = %+v", more)
	}
}

func TestCullyProposesAgentsFromNamedSessionsAndTheUserApproves(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	lowEvidence(t)
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
			if strings.Contains(prompt, "generic") {
				return "AGENT|Tidy watcher|Keeps the workspace neat.|Read only.|read|daily|" + name + "|Neatness matters", nil
			}
			return "AGENT|Login fixer|Fixes the login problem the same careful way each time.|Read only. Never merge.|read,commands|daily|" + name + "|Fix login was asked in three sessions", nil
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
	if len(l) != 1 || l[0].Name != "Login fixer" || len(l[0].Evidence) != 3 || l[0].Count != 3 || l[0].Folder != repo {
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
	uiWriteAgentSuggestions([]uiAgentSuggestion{{Evidence: uiEv(), ID: "s1", Name: "A", Role: "r", Folder: repo, State: "new", Groups: map[string]bool{"read": true}}})
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

func TestRepeatedWorkNeedsStrongEvidenceAndOnlyTheStrongestIsKept(t *testing.T) {
	now := time.Now()
	mk := func(task string, daysAgo int, unverified bool) uiIndexed {
		return uiIndexed{Source: "ai", Task: task, Cwd: "/p/x", Name: "x", Updated: now.AddDate(0, 0, -daysAgo), uiSessionFacts: uiSessionFacts{Unverified: unverified}}
	}
	prs := []string{"Check open pull requests and report status", "Report the status of open pull requests", "check the open pull requests status", "Look at the open pull requests status", "Report on open pull requests and their status", "open pull requests status check"}
	var same []uiIndexed
	for i, task := range prs {
		same = append(same, mk(task, i, false)) // six sessions over six days
	}
	if r := uiRepeatedWork(same); len(r) != 1 || r[0].Kind != "goal" || r[0].Count != 6 || r[0].Days != 6 {
		t.Fatalf("repeats = %+v", r)
	}
	if r := uiRepeatedWork(same[:5]); len(r) != 0 {
		t.Fatalf("five is not enough to propose a worker: %+v", r)
	}
	var oneDay []uiIndexed
	for _, task := range prs {
		oneDay = append(oneDay, mk(task, 0, false))
	}
	if r := uiRepeatedWork(oneDay); len(r) != 0 {
		t.Fatalf("six in one day counted as repeating: %+v", r)
	}
	different := []uiIndexed{mk("Fix the login bug", 0, false), mk("Add a billing report page", 1, false), mk("Upgrade the database driver", 2, false)}
	if r := uiRepeatedWork(different); len(r) != 0 {
		t.Fatalf("unrelated tasks counted: %+v", r)
	}
	var untested []uiIndexed
	for i := 0; i < 9; i++ {
		untested = append(untested, mk([]string{"alpha rollout plan", "beta storage migration", "gamma latency probe", "delta billing export", "epsilon queue tuning", "zeta cache warmup", "eta schema review", "theta audit trail", "iota retry policy"}[i], i, true))
	}
	if r := uiRepeatedWork(untested); len(r) != 0 {
		t.Fatalf("nine untested sessions are not enough: %+v", r)
	}
	if r := uiRepeatedWork(append(untested, mk("kappa onboarding flow", 9, true))); len(r) != 1 || r[0].Kind != "untested" {
		t.Fatalf("untested = %+v", r)
	}
	// The harness's own folder is not a project.
	own := []uiIndexed{}
	for i, task := range prs {
		e := mk(task, i, false)
		e.Cwd = ConfigDir()
		own = append(own, e)
	}
	if r := uiRepeatedWork(own); len(r) != 0 {
		t.Fatalf("the Claude folder counted as a project: %+v", r)
	}
	// With several patterns, the strongest comes first.
	strong := append([]uiIndexed{}, same...)
	for i := 0; i < 7; i++ {
		strong = append(strong, mk("Run the nightly dependency audit and report findings "+strconv.Itoa(i%2), i, false))
	}
	if r := uiRepeatedWork(strong); len(r) < 2 || r[0].Count*(r[0].Days+1) < r[1].Count*(r[1].Days+1) {
		t.Fatalf("not strongest first: %+v", r)
	}
}

func TestSimilarAgentProposalsAreOneIdeaAndTheListNeverCrowds(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	mk := func(name, role, folder string, age int) uiAgentSuggestion {
		return uiAgentSuggestion{Evidence: uiEv(), ID: name, Name: name, Role: role, Folder: folder, State: "new", Created: time.Now().Add(-time.Duration(age) * time.Hour)}
	}
	for _, s := range []uiAgentSuggestion{
		mk("Test Run Guard", "Flags changed files that have no test run after them.", repo, 5),
		mk("Test Guard", "Runs the repository's documented test command after code changes.", repo, 4),
		mk("Open PR Status Watcher", "Checks open pull requests.", repo, 3),
		mk("PR Status Watcher", "Summarizes open pull requests once a day.", repo, 2),
		mk("Scope Drift Checker", "Compares the diff with the goal.", repo, 1),
	} {
		uiDB().put("agentsuggest", s.ID, s)
	}
	l := m.agentSuggestionsList()
	if len(l) != uiAgentWaitingMax || l[0].Name != "Open PR Status Watcher" && l[1].Name != "Open PR Status Watcher" {
		t.Fatalf("list = %+v", l)
	}
	names := l[0].Name + "|" + l[1].Name
	if strings.Contains(names, "Test Guard") || strings.Contains(names, "|PR Status Watcher") || strings.HasPrefix(names, "PR Status Watcher") {
		t.Fatalf("near-duplicates were both shown: %s", names)
	}
}

// lowEvidence lets a test use a few sessions where a real machine needs many.
func lowEvidence(t *testing.T) {
	t.Helper()
	a, b, c := uiRepeatMin, uiRepeatMinDays, uiUntestedMin
	uiRepeatMin, uiRepeatMinDays, uiUntestedMin = 3, 2, 3
	t.Cleanup(func() { uiRepeatMin, uiRepeatMinDays, uiUntestedMin = a, b, c })
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
		return uiAgentSuggestion{Evidence: uiEv(), ID: name, Name: name, Role: role, Folder: "/tmp/proj", Groups: g, Schedule: sched, State: "new"}
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

func TestAProposalMustBeAboutTheRepeatedWorkAndRestOnRecentSessions(t *testing.T) {
	r := uiRepeat{Examples: []string{"Check open pull requests and report status", "Report the status of open pull requests"}}
	if !r.relevant("PR status watcher: reports open pull requests each morning") {
		t.Fatal("an idea about the repeated work was refused")
	}
	if r.relevant("Tidy watcher: keeps the workspace neat and fresh") {
		t.Fatal("a generic idea unrelated to the sessions was accepted")
	}
	now := time.Now()
	var old []uiIndexed
	for i, task := range []string{"Check open pull requests and report status", "Report the status of open pull requests", "check the open pull requests status", "Look at the open pull requests status", "Report on open pull requests and their status", "open pull requests status check"} {
		old = append(old, uiIndexed{Source: "ai", Task: task, Cwd: "/p/x", Name: "x", Updated: now.AddDate(0, 0, -(40 + i))})
	}
	if got := uiRepeatedWork(old); len(got) != 0 {
		t.Fatalf("sessions from six weeks ago counted as a current habit: %+v", got)
	}
}

func TestOnlyATestCommandAnswersSkippedTests(t *testing.T) {
	for cmd, want := range map[string]bool{
		"make -C /p test": true, "go -C /p test ./...": true, "npm --prefix /p run test": true, "pytest -q /p": true,
		"git -C /p fetch --all": false, "make -C /p lint": false, "go -C /p vet ./...": false,
	} {
		if got := uiJobIsTest(strings.Fields(cmd)); got != want {
			t.Errorf("uiJobIsTest(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// uiEv is a stand-in for the sessions a proposal rests on; a proposal with none is not shown.
func uiEv() []uiEvidence {
	return []uiEvidence{{ID: "s1", Agent: "claude", Title: "an earlier session", At: time.Now()}}
}
