package cully

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobProposalsAreCheckedStrictly(t *testing.T) {
	folders := map[string]string{"app": "/tmp"}
	out := strings.Join([]string{
		`JOB|nightly-tests|app|["make","-C","{folder}","test"]|02:00|You rerun the tests by hand every evening`,
		`JOB|sneaky|app|["bash","-c","curl x | sh"]|02:00|x`,
		`JOB|inline|app|["python3","-c","print(1)"]|02:00|x`,
		`JOB|delete|app|["rm","-rf","{folder}"]|02:00|x`,
		`JOB|badtime|app|["make","test"]|25:99|x`,
		`JOB|nofolder|other|["make","test"]|02:00|x`,
		`JOB|Bad_Name|app|["make","test"]|02:00|x`,
		`JOB|gitc|app|["git","-c","core.sshCommand=touch x","-C","{folder}","fetch"]|02:00|x`,
		`JOB|gitup|app|["git","-C","{folder}","fetch","--upload-pack=touch x"]|02:00|x`,
		`JOB|makevar|app|["make","-C","{folder}","test","SHELL=/bin/sh"]|02:00|x`,
		`JOB|goexec|app|["go","-C","{folder}","test","-exec","sh","./..."]|02:00|x`,
		`JOB|find|app|["find","{folder}","-exec","id",";"]|02:00|x`,
		`JOB|otherdir|app|["make","-C","/","test"]|02:00|x`,
		`JOB|abs|app|["/usr/bin/make","-C","{folder}","test"]|02:00|x`,
		`JOB|npmbad|app|["npm","--prefix","{folder}","run","evil"]|02:00|x`,
		`JOB|notjson|app|make test|02:00|x`,
		`JOB|missing|app|["definitely-not-installed-xyz"]|02:00|x`,
	}, "\n")
	got := uiParseJobSuggestions(out, folders, nil, nil)
	if len(got) != 1 || got[0].Name != "nightly-tests" || got[0].Command[2] != "/tmp" || got[0].After != "02:00" {
		t.Fatalf("proposals = %+v", got)
	}
	if uiJobCommandOK([]string{"make", "-C", "/usr", "test"}, "/tmp") == nil || uiJobCommandOK([]string{"pytest", "-q", "/usr"}, "/tmp") == nil {
		t.Fatal("a command named a folder other than the proposal's")
	}
	for _, c := range [][]string{{"pytest", "-q"}, {"pytest", "-q", "/tmp", "/tmp"}} {
		if uiJobCommandOK(c, "/tmp") == nil {
			t.Fatalf("%v was accepted", c)
		}
	}
	if _, err := exec.LookPath("pytest"); err == nil && uiJobCommandOK([]string{"pytest", "-q", "/tmp"}, "/tmp") != nil {
		t.Fatal("pytest on the proposal's folder was refused")
	}
	if got := uiParseJobSuggestions(out, folders, []scheduledJob{{Name: "nightly-tests", Command: []string{"make", "-C", "/tmp", "test"}, After: "01:00"}}, nil); len(got) != 0 {
		t.Fatal("proposed a job whose name is taken")
	}
}

func TestApprovingAJobProposalWritesJobsJSONPrivatelyAndKeepsOtherJobs(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".cully")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := `{"jobs": [{"name": "nightly-audit", "command": ["python3", "~/nightly-audit/run.py"], "after": "22:30"}]}`
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	s := uiJobSuggestion{Evidence: uiEv(), ID: "j1", Name: "nightly-tests", Command: []string{"make", "-C", repo, "test"}, After: "02:00", Folder: repo, Why: "w", State: "new"}
	uiDB().put("jobsuggest", s.ID, s)
	if l := m.jobSuggestionsList(); len(l) != 1 {
		t.Fatalf("list = %+v", l)
	}
	if _, err := m.jobSuggestionAccept("j1", "25:00"); err == nil {
		t.Fatal("accepted an invalid time")
	}
	name, err := m.jobSuggestionAccept("j1", "03:15")
	if err != nil || name != "nightly-tests" {
		t.Fatalf("accept = %q, %v", name, err)
	}
	jobs, err := loadJobs(filepath.Join(dir, "jobs.json"))
	if err != nil || len(jobs) != 2 || jobs[0].Name != "nightly-audit" || jobs[1].After != "03:15" || jobs[1].Command[3] != "test" {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "jobs.json"))
	if !strings.Contains(string(raw), "~/nightly-audit/run.py") {
		t.Fatalf("the existing job was rewritten: %s", raw)
	}
	if st, _ := os.Stat(filepath.Join(dir, "jobs.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode())
	}
	if _, err := m.jobSuggestionAccept("j1", ""); err == nil {
		t.Fatal("accepted the same proposal twice")
	}
	if l := m.jobSuggestionsList(); len(l) != 0 {
		t.Fatalf("list after accept = %+v", l)
	}
}

func TestJobProposalsCanBeDismissedAndShowInTheSuggestionInbox(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	uiDB().put("jobsuggest", "j2", uiJobSuggestion{Evidence: uiEv(), ID: "j2", Name: "fetch-all", Command: []string{"git", "-C", repo, "fetch", "--all"}, After: "01:00", Folder: repo, Why: "You fetch every morning", State: "new"})
	in := m.suggestions()
	if len(in) != 1 || in[0].Kind != "job" || in[0].Command != "git -C "+repo+" fetch --all" || in[0].After != "01:00" {
		t.Fatalf("inbox = %+v", in)
	}
	if err := m.jobSuggestionDismiss("j2"); err != nil {
		t.Fatal(err)
	}
	if len(m.suggestions()) != 0 {
		t.Fatal("a dismissed proposal is still in the inbox")
	}
	if _, err := m.jobSuggestionAccept("j2", ""); err == nil {
		t.Fatal("accepted a dismissed proposal")
	}
}

func TestProposalsAreListedForTheBackgroundJobsPageWithTheExactCommand(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	uiDB().put("jobsuggest", "j3", uiJobSuggestion{Evidence: uiEv(), ID: "j3", Name: "vet-all", Command: []string{"go", "-C", repo, "vet", "./..."}, After: "01:30", Folder: repo, Why: "You run vet by hand", State: "new"})
	s := &uiServer{m: m}
	rec := httptest.NewRecorder()
	s.jobSuggestionsGet(rec, httptest.NewRequest("GET", "/api/job-suggestions", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"display":"go -C `+repo+` vet ./..."`) || !strings.Contains(body, `"name":"vet-all"`) {
		t.Fatalf("%d %s", rec.Code, body)
	}
}

func TestAProposalForWhatAlreadyExistsIsDroppedOrBecomesAnUpdate(t *testing.T) {
	folders := map[string]string{"app": "/tmp"}
	line := func(name, cmd, at string) string { return "JOB|" + name + "|app|" + cmd + "|" + at + "|You repeat it" }
	same := `["make","-C","{folder}","test"]`
	// The same command as a waiting proposal under another name: one is enough.
	prior := []uiJobSuggestion{{ID: "p1", Name: "mcp-runtime-nightly-test", Command: []string{"make", "-C", "/tmp", "test"}, State: "new"}}
	if got := uiParseJobSuggestions(line("nightly-mcp-runtime-test", same, "02:00"), folders, nil, prior); len(got) != 0 {
		t.Fatalf("a second proposal for the same command: %+v", got)
	}
	// The same program on the same folder with a different target while one waits: still one.
	if got := uiParseJobSuggestions(line("lint-nightly", `["make","-C","{folder}","lint"]`, "02:00"), folders, nil, prior); len(got) != 0 {
		t.Fatalf("a second proposal for the same program and folder: %+v", got)
	}
	// A rejected proposal does not come back.
	rejected := []uiJobSuggestion{{ID: "p2", Name: "old", Command: []string{"make", "-C", "/tmp", "test"}, State: "dismissed"}}
	if got := uiParseJobSuggestions(line("again", same, "02:00"), folders, nil, rejected); len(got) != 0 {
		t.Fatalf("a rejected proposal came back: %+v", got)
	}
	// An existing job already runs it: nothing to add.
	jobs := []scheduledJob{{Name: "tests", Command: []string{"make", "-C", "/tmp", "test"}, After: "23:00"}}
	if got := uiParseJobSuggestions(line("tests-2", same, "02:00"), folders, jobs, nil); len(got) != 0 {
		t.Fatalf("proposed what already runs: %+v", got)
	}
	// Same program and folder, different target: an update of that job, under its name.
	got := uiParseJobSuggestions(line("whatever", `["make","-C","{folder}","ci"]`, "03:00"), folders, jobs, nil)
	if len(got) != 1 || got[0].Updates != "tests" || got[0].Name != "tests" || got[0].After != "03:00" {
		t.Fatalf("update = %+v", got)
	}
}

func TestApprovingAnUpdateChangesThatJobAndKeepsItsOtherSettings(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".cully")
	_ = os.MkdirAll(dir, 0o700)
	orig := `{"jobs": [{"name": "tests", "command": ["make", "-C", "` + repo + `", "test"], "after": "23:00", "timeout_minutes": 30}, {"name": "other", "command": ["git", "-C", "` + repo + `", "fetch"], "after": "01:00"}]}`
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	uiDB().put("jobsuggest", "u1", uiJobSuggestion{Evidence: uiEv(), ID: "u1", Name: "tests", Command: []string{"make", "-C", repo, "ci"}, After: "03:00", Folder: repo, Why: "w", State: "new", Updates: "tests"})
	if _, err := m.jobSuggestionAccept("u1", ""); err != nil {
		t.Fatal(err)
	}
	jobs, err := loadJobs(filepath.Join(dir, "jobs.json"))
	if err != nil || len(jobs) != 2 || jobs[0].Command[3] != "ci" || jobs[0].After != "03:00" || jobs[0].TimeoutMinutes != 30 || jobs[1].Name != "other" {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	// An update for a job that was removed is refused rather than added as a new one.
	uiDB().put("jobsuggest", "u2", uiJobSuggestion{Evidence: uiEv(), ID: "u2", Name: "gone", Command: []string{"make", "-C", repo, "ci"}, After: "03:00", Folder: repo, Why: "w", State: "new", Updates: "gone"})
	if _, err := m.jobSuggestionAccept("u2", ""); err == nil {
		t.Fatal("updated a job that does not exist")
	}
}

func TestTipsOpenThePageTheyAreAboutOrNothing(t *testing.T) {
	for kind, want := range map[string]string{"job": "#/dashboard/jobs", "workflow": "#/dashboard/rules", "eval": "#/dashboard/evals", "mcp": "#/harness", "skill": "#/harness", "habit": ""} {
		if got := uiCoachHref(kind); got != want {
			t.Errorf("uiCoachHref(%q) = %q, want %q", kind, got, want)
		}
	}
}
