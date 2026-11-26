package cully

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiEvalReady(t *testing.T) (*uiManager, string, string) {
	t.Helper()
	m, repo, _ := uiIndexManager(t)
	uiWriteRichTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo)
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "You judge whether") {
			return "VERDICT: pass\nREASON: The login check was met.", nil
		}
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: Fix the login bug.\nCHECKS: login works", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	m.analyzeNext()
	if _, err := m.createEval(uiTestSessA); err != nil {
		t.Fatal(err)
	}
	return m, repo, uiTestSessA
}

func TestRunningAnEvalStartsAnAgentAndJudgesIt(t *testing.T) {
	m, _, id := uiEvalReady(t)
	run, err := m.runEval(id)
	if err != nil || run.Status != "running" || run.AgentID == "" {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
	uiWaitFor(t, "the run to be judged", func() bool {
		l := m.evalRunList()
		return len(l) == 1 && l[0].Status == "pass"
	})
	r := m.evalRunList()[0]
	if !strings.Contains(r.Reason, "login check") || r.Ended.IsZero() || r.Task != "Fix the login bug." {
		t.Fatalf("run = %+v", r)
	}
	if a, ok := m.get(run.AgentID); !ok || a.snapshot().State != uiStopped {
		uiWaitFor(t, "the agent to stop", func() bool { a, _ := m.get(run.AgentID); return a.snapshot().State == uiStopped })
	}
	if _, err := m.runEval("33333333-3333-4333-8333-333333333333"); err == nil {
		t.Fatal("run of an unknown eval accepted")
	}
}

func TestEvalRunsSurviveARestartAndInterruptedOnesAreMarked(t *testing.T) {
	m, _, id := uiEvalReady(t)
	m.updateEvalRunForTest(&uiEvalRun{ID: "r1", EvalID: id, Task: "x", Started: time.Now(), Status: "running"})
	m2 := newUIManager(m.cfg)
	l := m2.evalRunList()
	if len(l) != 1 || l[0].Status != "error" || !strings.Contains(l[0].Reason, "restarted") {
		t.Fatalf("runs = %+v", l)
	}
}

func (m *uiManager) updateEvalRunForTest(r *uiEvalRun) {
	uiEvalRunMu.Lock()
	defer uiEvalRunMu.Unlock()
	m.loadEvalRunsLocked()
	m.evalRuns = append(m.evalRuns, r)
	m.saveEvalRunsLocked()
}

func TestJudgeReplyParsing(t *testing.T) {
	if s, r := uiParseJudge("VERDICT: Partial.\nREASON: One check missed."); s != "partial" || r != "One check missed." {
		t.Fatalf("%s %s", s, r)
	}
	if s, _ := uiParseJudge("nonsense"); s != "error" {
		t.Fatalf("status = %s", s)
	}
	if s, _ := uiParseJudge("VERDICT: amazing"); s != "error" {
		t.Fatalf("status = %s", s)
	}
}

func TestEvalRunEndpoints(t *testing.T) {
	srv, m, _ := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/evals/runs", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/evals/"+uiTestSessA+"/run", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("unknown: %d", r.StatusCode)
	}
	_ = m
	var l []uiEvalRun
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/evals/runs", nil, uiAuthed), &l)
	if l == nil || len(l) != 0 {
		t.Fatalf("runs = %v", l)
	}
}
