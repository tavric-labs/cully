package cully

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyGeneralTopicsMayGoToTheWeb(t *testing.T) {
	private := []string{"mcp-runtime", "taranadocs"}
	good := []string{"go context cancellation in worker pools", "postgres advisory lock retry pattern"}
	bad := []string{"fix bug in /twbuild/proshan/x", "taranadocs upload token flow", "GEN3-1234 root cause search", "email me@example.com about errors", "https://example.com thing here ok", "too short", "mcp-runtime gateway timeouts explained"}
	for _, q := range good {
		if !uiSafeScoutQuery(q, private) {
			t.Errorf("%q refused", q)
		}
	}
	for _, q := range bad {
		if uiSafeScoutQuery(q, private) {
			t.Errorf("%q allowed", q)
		}
	}
	topics := uiParseTopics("TOPIC|go context cancellation in worker pools|You hit goroutine leaks\nTOPIC|taranadocs upload token flow|x\nnoise\nTOPIC|a|b|c|d", private)
	if len(topics) != 1 || topics[0].Why == "" {
		t.Fatalf("topics = %+v", topics)
	}
}

func TestHourlyScoutPicksATopicFromTheSessionsAndRunsItOncePerHour(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	for _, id := range []string{uiTestSessA, uiTestSessB, uiTestSessC} {
		uiWriteRichTranscript(t, dir, id, repo)
	}
	asked := 0
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "picking web searches") {
			asked++
			return "TOPIC|go test flakiness from shared state|Tests were skipped in several sessions\nTOPIC|" + filepath.Base(repo) + " secrets in logs|leaky", nil
		}
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: Fix login\nCHECKS: y", nil
	}
	var queries []string
	m.cfg.ScoutStream = func(ctx context.Context, cwd, prompt string, progress func(string)) (string, error) {
		queries = append(queries, prompt)
		return "finding https://example.com", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	for i := 0; i < 3; i++ {
		m.analyzeNext()
	}
	if n := m.scoutIfDue(); n != 0 || asked != 0 {
		t.Fatalf("the scout ran while off: %d %d", n, asked)
	}
	m.setAutoScout(true)
	if n := m.scoutIfDue(); n != 1 {
		t.Fatalf("started %d scouts, want 1 (the second topic names the project)", n)
	}
	uiWaitFor(t, "the scout to be recorded", func() bool { return len(m.researchList("scout", 5)) == 1 })
	r := m.researchList("scout", 5)[0]
	if !strings.HasPrefix(r.Source, "Advisor, every hour") || r.Query != "go test flakiness from shared state" {
		t.Fatalf("record = %+v", r)
	}
	if m.scoutIfDue() != 0 || asked != 1 {
		t.Fatalf("the scout ran again within the hour (asked %d)", asked)
	}
	if lib := m.sessionLibrary(); !lib.Analysis.Scout || lib.Analysis.LastScout.IsZero() {
		t.Fatalf("analysis = %+v", lib.Analysis)
	}
}

func TestHourlyScoutDoesNotRepeatATopicAndNeedsNamedSessions(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	m.setAutoScout(true)
	if m.scoutIfDue() != 0 {
		t.Fatal("ran with no sessions")
	}
	m.recordResearch(uiResearchRec{Kind: "scout", Repo: repo, Query: "Go Test Flakiness From Shared State", Text: "x"})
	if got := uiTopicKey("go  test flakiness from shared state"); got != uiTopicKey("Go Test Flakiness From Shared State") {
		t.Fatalf("keys differ: %q", got)
	}
}

func TestAutoScoutEndpoint(t *testing.T) {
	srv, m, _ := uiTestServer(t, true)
	if r := uiDo(t, "POST", srv.URL+"/api/scout/auto", map[string]any{"on": true}, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/scout/auto", map[string]any{"on": true}, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("on: %d", r.StatusCode)
	}
	if !m.sessionLibrary().Analysis.Scout {
		t.Fatal("hourly scout not on")
	}
}
