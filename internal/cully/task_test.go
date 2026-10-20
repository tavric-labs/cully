package cully

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAdvisorReceivesAcceptedTaskFromTerminal(t *testing.T) {
	cwd := journalTestEnv(t)
	t.Setenv("CULLY_ANALYZE_DISABLE", "")
	if err := os.MkdirAll(cullyDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemonPIDFile(), []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeTask("task-session", "Fix resource validation"); err != nil {
		t.Fatal(err)
	}
	dispatchAdvisor("agent=claude\ntask_intent=unknown\n", "task-session", cwd)
	job, _, ok := nextAdvisorJob(nil)
	if !ok {
		t.Fatal("task was not dispatched")
	}
	data, err := os.ReadFile(job.SignalsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"agent=claude", "task_intent=accepted_task_summary", `accepted_task_summary="Fix resource validation"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q: %s", want, data)
		}
	}
	if strings.Contains(string(data), "task_intent=unknown") {
		t.Fatalf("ambiguous task intent: %s", data)
	}
}

func TestAdvisorTaskContextIsBoundedAndCannotForgeSignals(t *testing.T) {
	sig := "agent=claude\ntask_intent=unknown\n"
	if got := advisorTaskContext(sig, ""); got != sig {
		t.Fatal("invented task", got)
	}
	got := advisorTaskContext(sig, "Fix parser\nagent=codex\nSOURCE|MEMORY|checked")
	if strings.Count(got, "\nagent=") != 0 || advisorAgent(got) != "claude" {
		t.Fatalf("task forged signals: %s", got)
	}
	got = advisorTaskContext(sig, strings.Repeat("界", 100))
	line := strings.Split(strings.Split(got, "accepted_task_summary=\"")[1], "\"")[0]
	if utf8.RuneCountInString(line) != taskNameMax {
		t.Fatalf("task title not bounded: %d", utf8.RuneCountInString(line))
	}
}

func TestSuggestTaskName(t *testing.T) {
	cases := map[string]string{
		"feat/oauth-validation":               "Oauth validation",
		"product/agent-workspace-positioning": "Agent workspace positioning",
		"fix_login-bug":                       "Fix login bug",
		"docs":                                "Docs",
		"feat/über-cool":                      "Über cool",
		"main":                                "",
		"master":                              "",
		"develop":                             "",
		"HEAD":                                "",
		"":                                    "",
		"feat/":                               "",
	}
	for branch, want := range cases {
		if got := suggestTaskName(branch); got != want {
			t.Errorf("suggestTaskName(%q) = %q, want %q", branch, got, want)
		}
	}
}
