package cully

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRecallFindsNamedSessionsByKeyword(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	for _, id := range []string{uiTestSessA, uiTestSessB, uiTestSessC} {
		uiWriteRichTranscript(t, dir, id, repo)
	}
	m.cfg.Advisor = func(_ string, _ string, _ bool, _ string) (string, error) {
		return "TITLE: Fix the login bug\nSUMMARY: S\nDIFFERENTLY: Write a regression test first\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: Fix login\nCHECKS: login works", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	for i := 0; i < 3; i++ {
		m.analyzeNext()
	}
	got := m.localRecall("please fix the login problem again")
	if got == "" || !strings.Contains(got, "login") || !strings.Contains(got, "regression test") {
		t.Fatalf("recall = %q", got)
	}
	if strings.Count(got, "\n") >= uiRecallHits {
		t.Fatalf("too many hits: %q", got)
	}
	if m.localRecall("kubernetes ingress certificates") != "" {
		t.Fatal("matched an unrelated task")
	}
	t.Setenv("CULLY_MEMORY", "local")
	if !m.useLocalRecall() {
		t.Fatal("pinned local mode did not use local recall")
	}
	t.Setenv("CULLY_MEMORY", "mcp")
	if m.useLocalRecall() {
		t.Fatal("pinned mcp mode used local recall")
	}
}
