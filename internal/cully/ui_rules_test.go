package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const uiGoodRules = "---\nname: dev-workflow-rules\ndescription: How this person works; read it at the start of repository work.\n---\n# How I work\n- Run the tests before saying done.\n- Projects: app (a web service).\n"

func uiRulesFixture(t *testing.T) (*uiManager, string) {
	m, repo, _ := uiIndexManager(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CURSOR_HOME", t.TempDir())
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	ids := []string{uiTestSessA, uiTestSessB, uiTestSessC, "44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555"}
	for _, id := range ids {
		uiWriteRichTranscript(t, dir, id, repo)
	}
	asked := ""
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		if strings.Contains(prompt, "dev-workflow-rules") && strings.Contains(prompt, "CURRENT FILE") {
			asked = prompt
			return "<<<FILE\n" + uiGoodRules + "FILE>>>\nCHANGES:\n- Added a rule about tests\n- Listed the app project\n", nil
		}
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: yes\nON_TASK: yes\nDRIFT: None.\nTASK: x\nCHECKS: y", nil
	}
	_ = asked
	m.indexSessions(20)
	m.setAnalysis(true)
	for i := 0; i < 5; i++ {
		m.analyzeNext()
	}
	return m, repo
}

func TestRulesAreDraftedFromSessionsShownAsADiffAndOnlyWrittenAfterApproval(t *testing.T) {
	m, _ := uiRulesFixture(t)
	v := m.rulesView()
	if v.Exists || v.Proposal != nil || v.Named != 5 {
		t.Fatalf("before = %+v", v)
	}
	added, err := m.rulesPropose()
	if err != nil || !added {
		t.Fatalf("propose: %v %v", added, err)
	}
	for _, tg := range uiRulesTargets() {
		if tg.Exists {
			t.Fatalf("the file was written before approval: %+v", tg)
		}
	}
	v = m.rulesView()
	if v.Proposal == nil || len(v.Proposal.Changes) != 2 || v.Proposal.Based != 5 {
		t.Fatalf("proposal = %+v", v.Proposal)
	}
	plus := 0
	for _, d := range v.Diff {
		if d.Op == "+" {
			plus++
		}
	}
	if plus < 5 {
		t.Fatalf("diff = %+v", v.Diff)
	}
	if _, err := m.rulesPropose(); err == nil {
		t.Fatal("a second draft was made while one waits")
	}
	written, err := m.rulesApprove(v.Proposal.ID)
	if err != nil || len(written) != 2 { // claude and codex both have a place for it
		t.Fatalf("approve: %v %v", written, err)
	}
	b, err := os.ReadFile(filepath.Join(ConfigDir(), "skills", uiRulesName, "SKILL.md"))
	if err != nil || string(b) != uiGoodRules {
		t.Fatalf("claude file = %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(uiCodexHome(), "skills", uiRulesName, "SKILL.md")); err != nil {
		t.Fatalf("codex file missing: %v", err)
	}
	if m.rulesView().Proposal != nil {
		t.Fatal("the proposal is still pending after approval")
	}
	// a changed file keeps the old one as .prev
	_ = os.WriteFile(filepath.Join(ConfigDir(), "skills", uiRulesName, "SKILL.md"), []byte("my own edit\n"), 0o644)
	m.idxMu.Lock()
	m.index.LastRulesCheck = m.index.LastRulesCheck.AddDate(0, 0, -2)
	m.idxMu.Unlock()
	if added, err := m.rulesPropose(); err != nil || !added {
		t.Fatalf("second propose: %v %v", added, err)
	}
	id := m.rulesView().Proposal.ID
	if _, err := m.rulesApprove(id); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.ReadFile(filepath.Join(ConfigDir(), "skills", uiRulesName, "SKILL.md.prev"))
	if string(prev) != "my own edit\n" {
		t.Fatalf("previous file = %q", prev)
	}
}

func TestRulesDraftsWithSecretsOrABadHeaderAreRefusedAndDismissedOnesStayDismissed(t *testing.T) {
	for _, bad := range []string{"no header", "---\nname: other\ndescription: x\n---\nbody", "---\nname: dev-workflow-rules\ndescription: x\n---\napi_key: sk-abcdefghijklmnopqrstuvwxyz", "---\nname: dev-workflow-rules\ndescription: x\n---\n" + strings.Repeat("a", uiRulesMax)} {
		if err := uiValidRules(bad); err == nil {
			t.Errorf("accepted %.40q", bad)
		}
	}
	if err := uiValidRules(uiGoodRules); err != nil {
		t.Fatal(err)
	}
	m, _ := uiRulesFixture(t)
	if added, _ := m.rulesPropose(); !added {
		t.Fatal("no draft")
	}
	id := m.rulesView().Proposal.ID
	if err := m.rulesDismiss(id); err != nil {
		t.Fatal(err)
	}
	m.idxMu.Lock()
	m.index.LastRulesCheck = m.index.LastRulesCheck.AddDate(0, 0, -2)
	m.idxMu.Unlock()
	if added, err := m.rulesPropose(); err != nil || added {
		t.Fatalf("the same draft came back after being dismissed: %v %v", added, err)
	}
}

func TestRulesAreCheckedOnceADayAndNeedNewSessions(t *testing.T) {
	m, _ := uiRulesFixture(t)
	asked := 0
	inner := m.cfg.Advisor
	m.cfg.Advisor = func(a, c string, r bool, p string) (string, error) {
		if strings.Contains(p, "CURRENT FILE") {
			asked++
		}
		return inner(a, c, r, p)
	}
	m.rulesIfDue()
	if asked != 1 {
		t.Fatalf("asked %d times", asked)
	}
	m.rulesIfDue()
	if asked != 1 {
		t.Fatalf("asked again within the day: %d", asked)
	}
}

func TestRulesEndpoints(t *testing.T) {
	srv, _, _ := uiTestServer(t, true)
	t.Setenv("CODEX_HOME", t.TempDir())
	if r := uiDo(t, "GET", srv.URL+"/api/rules", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var v uiRulesView
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/rules", nil, uiAuthed), &v)
	if len(v.Targets) == 0 || v.Needed != uiRulesMinNamed {
		t.Fatalf("view = %+v", v)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/rules/propose", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("propose with nothing named: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/rules/nope/approve", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("approve unknown: %d", r.StatusCode)
	}
}

func TestRulesNeedNamedSessions(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	if _, err := m.rulesPropose(); err == nil {
		t.Fatal("proposed with nothing named")
	}
}

func TestRulesPageShowsTheDiffAndNotifiesWhenADraftWaits(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"function renderRules", "Approve and save", "Proposed update", "What would change", "Review your workflow rules", "/api/rules", "Draft it from my sessions", "SKILL.md.prev"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
