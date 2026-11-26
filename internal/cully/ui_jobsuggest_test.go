package cully

import (
	"os"
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
	got := uiParseJobSuggestions(out, folders, map[string]bool{})
	if len(got) != 1 || got[0].Name != "nightly-tests" || got[0].Command[2] != "/tmp" || got[0].After != "02:00" {
		t.Fatalf("proposals = %+v", got)
	}
	if uiJobCommandOK([]string{"make", "-C", "/usr", "test"}, "/tmp") == nil || uiJobCommandOK([]string{"pytest", "-q", "/usr"}, "/tmp") == nil {
		t.Fatal("a command named a folder other than the proposal's")
	}
	if got := uiParseJobSuggestions(out, folders, map[string]bool{"nightly-tests": true}); len(got) != 0 {
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
	s := uiJobSuggestion{ID: "j1", Name: "nightly-tests", Command: []string{"make", "-C", repo, "test"}, After: "02:00", Folder: repo, Why: "w", State: "new"}
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
	uiDB().put("jobsuggest", "j2", uiJobSuggestion{ID: "j2", Name: "fetch-all", Command: []string{"git", "-C", repo, "fetch", "--all"}, After: "01:00", Folder: repo, Why: "You fetch every morning", State: "new"})
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
