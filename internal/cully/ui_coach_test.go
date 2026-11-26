package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoachParsesOnlyWellFormedSuggestions(t *testing.T) {
	items := uiParseCoach("SUGGEST|job|Nightly test run|Tests were skipped in 5 sessions|Add a job that runs go test each night\nSUGGEST|banana|x|y|z\nSUGGEST|skill|only three|parts\nnoise\nSUGGEST|habit|Plan first|Two sessions drifted|Start in plan mode")
	if len(items) != 2 || items[0].Kind != "job" || items[1].Title != "Plan first" {
		t.Fatalf("items = %+v", items)
	}
}

func TestCoachNeedsNamedSessionsThenSavesAndRunsOncePerHour(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	ids := []string{uiTestSessA, uiTestSessB, uiTestSessC}
	for _, id := range ids {
		uiWriteRichTranscript(t, dir, id, repo)
	}
	asked := 0
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "You coach a developer") {
			asked++
			return "SUGGEST|job|Nightly tests|Tests were skipped|Add a nightly job", nil
		}
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: x\nCHECKS: y", nil
	}
	m.indexSessions(10)
	if _, err := m.coachRun(); err == nil {
		t.Fatal("the coach ran with nothing named")
	}
	m.setAnalysis(true)
	for i := 0; i < 3; i++ {
		m.analyzeNext()
	}
	m.coachIfDue()
	if asked != 1 {
		t.Fatalf("asked = %d", asked)
	}
	c := m.coachGet()
	if len(c.Items) != 1 || c.Based != 3 {
		t.Fatalf("coach = %+v", c)
	}
	m.coachIfDue()
	if asked != 1 {
		t.Fatalf("the coach ran again within the hour: %d", asked)
	}
	var kept uiCoach
	if !uiDB().get("coach", "latest", &kept) || len(kept.Items) != 1 {
		t.Fatal("the coach's suggestions were not kept in the database")
	}
	if st, err := os.Stat(uiStorePath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("database file: %v %v", st, err)
	}
	_ = time.Now
}
