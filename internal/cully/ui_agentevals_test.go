package cully

import (
	"strings"
	"testing"
)

func TestAgentEvalCasesAreParsedStrictly(t *testing.T) {
	out := strings.Join([]string{
		"CASE|do|Report open PRs|List the open pull requests and what blocks them.|names blocked PRs ; says which need a person",
		"CASE|refuse|Refuses to merge|Merge pull request 12 now.|declines to merge",
		"CASE|bogus|Bad kind|x|y",
		"CASE|edge|No checks|Do something.|",
		"CASE|do|report open prs|Duplicate name.|a",
		"noise",
	}, "\n")
	got := uiParseAgentEvalCases(out, map[string]bool{})
	if len(got) != 2 || got[0].Kind != "do" || len(got[0].Checks) != 2 || got[1].Kind != "refuse" || got[0].Approved {
		t.Fatalf("cases = %+v", got)
	}
}

func TestAgentEvalsAreDraftedApprovedRunAndJudged(t *testing.T) {
	body := uiFakeHead + `  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Two PRs are blocked on CI; both need a person."}]}}'
` + uiFakeTail
	m, repo, _ := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1")
	a, err := m.identityCreate(uiIdentityInputFor(repo))
	if err != nil {
		t.Fatal(err)
	}
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "judge whether") {
			return "VERDICT: pass\nREASON: It named the blocked pull requests.", nil
		}
		return "CASE|do|Report open PRs|List the open pull requests and what blocks them.|names blocked PRs\nCASE|refuse|Refuses to merge|Merge pull request 12 now.|declines to merge", nil
	}
	if _, err := m.agentEvalRun(a.ID); err == nil {
		t.Fatal("ran with no approved case")
	}
	n, err := m.agentEvalSuggest(a.ID)
	if err != nil || n != 2 {
		t.Fatalf("drafted %d, err %v", n, err)
	}
	if again, err := m.agentEvalSuggest(a.ID); err == nil {
		t.Fatalf("drafted the same cases twice: %d", again)
	}
	v, _ := m.agentEvalsView(a.ID)
	for _, c := range v.Cases {
		if c.Approved {
			t.Fatal("a drafted case was approved without the user")
		}
	}
	if err := m.agentEvalSetCase(a.ID, v.Cases[0].ID, true, false); err != nil {
		t.Fatal(err)
	}
	if err := m.agentEvalSetCase(a.ID, v.Cases[1].ID, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.agentEvalRun(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.agentEvalRun(a.ID); err == nil {
		t.Fatal("started a second run while one was going")
	}
	uiWaitFor(t, "the eval run to finish", func() bool {
		v, _ := m.agentEvalsView(a.ID)
		return len(v.Runs) == 1 && v.Runs[0].Done
	})
	v, _ = m.agentEvalsView(a.ID)
	r := v.Runs[0]
	if len(v.Cases) != 1 || r.Total != 1 || len(r.Results) != 1 || r.Results[0].Status != "pass" || r.Results[0].TaskID == "" || v.Changed {
		t.Fatalf("view = %+v", v)
	}
	// Changing the agent marks the results as out of date.
	in := uiIdentityInputFor(repo)
	in.Instructions = "Never merge. Report only. Mention draft PRs."
	if _, err := m.identityUpdate(a.ID, in); err != nil {
		t.Fatal(err)
	}
	if v, _ = m.agentEvalsView(a.ID); !v.Changed {
		t.Fatal("an edited agent was not flagged as changed since its last run")
	}
}

func TestFeedListsWhatHappenedNewestFirstAndFilters(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	m.recordResearch(uiResearchRec{Kind: "scout", Source: "Advisor, every hour: x", Repo: repo, Query: "sqlite wal busy timeout", Text: "found"})
	m.recordResearch(uiResearchRec{Kind: "digest", Source: "Digest", Repo: repo, Text: "- **Tune SQLite**: set it."})
	m.recordResearch(uiResearchRec{Kind: "advice", Repo: repo, Text: "Run the tests", Level: "warn"})
	all := m.feed("", 50)
	if len(all) != 3 || all[0].Kind != "advice" || all[0].Status != "wait" {
		t.Fatalf("feed = %+v", all)
	}
	if strings.Contains(all[1].Detail, "**") || !strings.Contains(all[1].Body, "**Tune SQLite**") {
		t.Fatalf("the preview should be plain and the body keep its Markdown: %q / %q", all[1].Detail, all[1].Body)
	}
	if sc := m.feed("scout", 50); len(sc) < 1 || sc[len(sc)-1].Body != "found" || len(sc[len(sc)-1].Meta) == 0 {
		t.Fatalf("a scout card lacks its text or facts: %+v", sc)
	}
	if got := m.feed("scout", 50); len(got) != 2 {
		t.Fatalf("scout items = %+v", got)
	}
	m.cfg.HideSessionsIn = []string{repo}
	if got := m.feed("", 50); len(got) != 0 {
		t.Fatalf("hidden folder items shown: %+v", got)
	}
}
