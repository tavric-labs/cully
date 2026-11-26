package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// uiWriteRichTranscript writes a session with real-looking activity.
func uiWriteRichTranscript(t *testing.T, dir, id, cwd string) {
	t.Helper()
	_ = os.MkdirAll(dir, 0o755)
	base := time.Now().Add(-3 * time.Hour).UTC()
	ts := func(min int) string { return base.Add(time.Duration(min) * time.Minute).Format(time.RFC3339Nano) }
	recs := []map[string]any{
		{"type": "user", "sessionId": id, "cwd": cwd, "gitBranch": "main", "timestamp": ts(0), "message": map[string]any{"role": "user", "content": "SECRET: please fix the login bug"}},
		{"type": "assistant", "sessionId": id, "cwd": cwd, "timestamp": ts(1), "message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "I will look at the login code"},
			map[string]any{"type": "tool_use", "id": "t1", "name": "Edit", "input": map[string]any{"file_path": cwd + "/login.go"}},
			map[string]any{"type": "tool_use", "id": "t2", "name": "Bash", "input": map[string]any{"command": "go test ./..."}}}}},
		{"type": "user", "sessionId": id, "cwd": cwd, "timestamp": ts(2), "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t2", "is_error": true, "content": "FAIL"}}}},
		{"type": "user", "sessionId": id, "cwd": cwd, "timestamp": ts(5), "message": map[string]any{"content": "now also fix logout"}},
		{"type": "assistant", "sessionId": id, "cwd": cwd, "timestamp": ts(6), "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Done, both fixed."}}}},
		// a long break: must not count as working time
		{"type": "user", "sessionId": id, "cwd": cwd, "timestamp": ts(300), "message": map[string]any{"content": "one more thing"}},
	}
	var b strings.Builder
	for _, r := range recs {
		l, _ := json.Marshal(r)
		b.Write(l)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(p, mt, mt)
}

func TestSessionFactsCountWorkNotWords(t *testing.T) {
	_, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	f, ok := uiSessionFactsOf(filepath.Join(dir, uiTestSessA+".jsonl"))
	if !ok {
		t.Fatal("no facts")
	}
	if f.Turns != 3 || f.Edits != 1 || f.Checks != 1 || f.Errors != 1 || f.Files != 1 {
		t.Fatalf("facts = %+v", f)
	}
	if f.ActiveSec < 5*60 || f.ActiveSec > 8*60 {
		t.Fatalf("active seconds = %d, want about 6 minutes (the 5 hour break must not count)", f.ActiveSec)
	}
	raw, _ := json.Marshal(f)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("conversation text is stored with the facts")
	}
}

func uiIndexManager(t *testing.T) (*uiManager, string, string) {
	t.Helper()
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	saved := uiListProcs
	uiListProcs = func() []uiProc { return nil }
	t.Cleanup(func() { uiListProcs = saved })
	return m, repo, out
}

func TestIndexAddsFactsForEverySessionButHiddenOnes(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	hidden := t.TempDir()
	uiWriteRichTranscript(t, dir, uiTestSessB, hidden)
	m.cfg.HideSessionsIn = []string{hidden}
	m.indexSessions(100)
	v := m.sessionLibrary()
	if len(v.Sessions) != 1 || v.Sessions[0].ID != uiTestSessA || v.Totals.Sessions != 1 {
		t.Fatalf("library = %+v", v)
	}
	if len(v.Folders) != 1 || v.Folders[0].Count != 1 || v.Folders[0].ActiveSec == 0 {
		t.Fatalf("folders = %+v", v.Folders)
	}
	docs := uiDB().all("session")
	if len(docs) != 1 {
		t.Fatalf("sessions in the database = %d", len(docs))
	}
	if strings.Contains(string(docs[0]), "SECRET") || strings.Contains(string(docs[0]), uiTestSessB) {
		t.Fatalf("a session document holds conversation text or a hidden session: %s", docs[0])
	}
	if st, err := os.Stat(uiStorePath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("database file: %v %v", st, err)
	}
}

func TestAnalysisNamesSessionsOnlyWhenTurnedOnAndKeepsPromptsOut(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	var asked []string
	m.cfg.Advisor = func(agent, cwd string, research bool, prompt string) (string, error) {
		asked = append(asked, prompt)
		return "TITLE: Fix login and logout\nSUMMARY: Repaired the login bug, then logout. Tests failed once.\nDIFFERENTLY: Run the tests before editing.", nil
	}
	m.indexSessions(100)
	if n := m.analyzeNext(); n != 0 || len(asked) != 0 {
		t.Fatalf("analysis ran while off: %d %v", n, asked)
	}
	m.setAnalysis(true)
	if n := m.analyzeNext(); n != 1 {
		t.Fatalf("analyzed = %d", n)
	}
	s := m.sessionLibrary().Sessions[0]
	if s.Title != "Fix login and logout" || !strings.Contains(s.Description, "Repaired") || s.Different != "Run the tests before editing." || s.Source != "ai" {
		t.Fatalf("session = %+v", s)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "login bug") {
		t.Fatalf("the model was not given the first request: %v", asked)
	}
	if n := m.analyzeNext(); n != 0 {
		t.Fatalf("a session was analyzed twice: %d", n)
	}
	b, _ := os.ReadFile(filepath.Join(cullyDir(), "session-index.json"))
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("prompt text was written to the index")
	}
}

func TestAnalysisSkipsSessionsStillBeingWorkedOn(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	p := filepath.Join(dir, uiTestSessA+".jsonl")
	now := time.Now()
	_ = os.Chtimes(p, now, now)
	m.cfg.Advisor = func(string, string, bool, string) (string, error) { return "TITLE: x\nSUMMARY: y\nDIFFERENTLY: z", nil }
	m.indexSessions(100)
	m.setAnalysis(true)
	if n := m.analyzeNext(); n != 0 {
		t.Fatalf("a session that was just active was analyzed: %d", n)
	}
}

func TestHistoryAndEndpointsUseTheIndex(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	saved := uiListProcs
	uiListProcs = func() []uiProc { return nil }
	t.Cleanup(func() { uiListProcs = saved })
	uiWriteRichTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo)
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		return "TITLE: Fix login\nSUMMARY: Fixed it.\nDIFFERENTLY: Test first.", nil
	}
	m.indexSessions(100)
	m.setAnalysis(true)
	m.analyzeNext()
	h := m.history("claude", 7)
	if len(h.Sessions) != 1 || h.Sessions[0].Title != "Fix login" || h.Sessions[0].Description != "Fixed it." || h.Sessions[0].ActiveSec == 0 {
		t.Fatalf("history = %+v", h)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/sessions/library", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	var v uiLibrary
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/sessions/library", nil, uiAuthed), &v)
	if v.Totals.Sessions != 1 || !v.Analysis.On {
		t.Fatalf("library = %+v", v)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/sessions/analysis", map[string]any{"on": false}, uiAuthed); r.StatusCode != 200 {
		t.Fatalf("toggle = %d", r.StatusCode)
	}
	if m.sessionLibrary().Analysis.On {
		t.Fatal("analysis still on")
	}
}

func TestLibraryPageUsesTheCSSOMForBarsAndOffersNaming(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	js := string(b)
	for _, want := range []string{"/api/sessions/library", "/api/sessions/analysis", "Session library", "Start naming sessions", "Could go better", "/api/history/"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	if strings.Contains(js, "style: 'width") {
		t.Error("an inline style would be blocked by the content security policy")
	}
}

func TestPageDropsEmptyArgumentsSoNullIsNeverShown(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	js := string(b)
	for _, want := range []string{"Element.prototype[name]", "'replaceChildren', 'append', 'prepend'", "k != null && k !== false"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}

func TestAnalysisKeepsHealthVerdictAndToolCounts(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		return "TITLE: Fix login\nSUMMARY: Fixed.\nDIFFERENTLY: Test first.\nCOMPLETED: Yes.\nON_TASK: drifted\nDRIFT: Also changed logout.\nTASK: Fix the login bug.\nCHECKS: login works ; tests pass ; a ; b", nil
	}
	m.indexSessions(100)
	m.setAnalysis(true)
	m.analyzeNext()
	s := m.sessionLibrary().Sessions[0]
	if s.Completed != "yes" || s.OnTask != "drifted" || s.Drift != "Also changed logout." || s.Task != "Fix the login bug." || len(s.Criteria) != 3 {
		t.Fatalf("session = %+v", s)
	}
	if s.ToolNames["Edit"] != 1 || s.ToolNames["Bash"] != 1 {
		t.Fatalf("tools = %v", s.ToolNames)
	}
}

func TestOnlyCompletedSessionsBecomeEvalsAndHoldNoPrompts(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	uiWriteRichTranscript(t, dir, uiTestSessB, repo)
	verdict := map[string]string{}
	calls := 0
	m.cfg.Advisor = func(_ string, _ string, _ bool, prompt string) (string, error) {
		calls++
		done := "yes"
		if calls == 2 {
			done = "no"
		}
		verdict[done] = done
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D\nCOMPLETED: " + done + "\nON_TASK: yes\nDRIFT: None.\nTASK: Fix the login bug.\nCHECKS: login works", nil
	}
	m.indexSessions(100)
	m.setAnalysis(true)
	m.analyzeNext()
	m.analyzeNext()
	var done, notDone string
	for _, s := range m.sessionLibrary().Sessions {
		if s.Completed == "yes" {
			done = s.ID
		} else {
			notDone = s.ID
		}
	}
	if done == "" || notDone == "" {
		t.Fatalf("library = %+v", m.sessionLibrary().Sessions)
	}
	if _, err := m.createEval(notDone); err == nil {
		t.Fatal("an unfinished session became an eval")
	}
	c, err := m.createEval(done)
	if err != nil {
		t.Fatal(err)
	}
	if c.Schema != uiEvalSchema || c.Task != "Fix the login bug." || len(c.Criteria) != 1 || len(c.Observed.Files) != 1 || c.Observed.Files[0] != "login.go" {
		t.Fatalf("case = %+v", c)
	}
	var stored uiEvalCase
	if !uiDB().get("eval", done, &stored) {
		t.Fatal("the eval case is not in the database")
	}
	if raw, _ := json.Marshal(stored); strings.Contains(string(raw), "SECRET") {
		t.Fatalf("a prompt reached the eval case: %s", raw)
	}
	if len(m.listEvals()) != 1 {
		t.Fatalf("evals = %+v", m.listEvals())
	}
	if _, err := m.createEval("../../etc/passwd"); err == nil {
		t.Fatal("bad id accepted")
	}
	m.cfg.HideSessionsIn = []string{repo}
	if _, err := m.createEval(done); err == nil {
		t.Fatal("hidden folder session became an eval")
	}
	if len(m.listEvals()) != 0 {
		t.Fatal("hidden folder eval listed")
	}
}

func TestHealthPageShowsCompletionDriftToolsAndEvals(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"Session health", "/api/sessions/evals", "Make an eval", "Drifted", "tool calls", "Not completed"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}

func TestSummarizeOneWorksWithBackgroundAnalysisOffAndFeedsHistory(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		return "TITLE: Fix login\nSUMMARY: Fixed it.\nDIFFERENTLY: Test first.\nCOMPLETED: partly\nON_TASK: yes\nDRIFT: None.\nTASK: Fix login.\nCHECKS: works", nil
	}
	if _, err := m.summarizeOne("not-an-id"); err == nil {
		t.Fatal("bad id accepted")
	}
	e, err := m.summarizeOne(uiTestSessA)
	if err != nil || e.Title != "Fix login" || e.Completed != "partly" {
		t.Fatalf("entry = %+v, err = %v", e, err)
	}
	if m.sessionLibrary().Analysis.On {
		t.Fatal("summarizing one session turned background analysis on")
	}
	h := m.history("claude", 7)
	if len(h.Sessions) != 1 || h.Sessions[0].Library == nil || !h.Sessions[0].Library.Named || h.Sessions[0].Library.Completed != "partly" || h.Sessions[0].Library.Tools != 2 {
		t.Fatalf("history = %+v", h)
	}
	m.cfg.HideSessionsIn = []string{repo}
	if _, err := m.summarizeOne(uiTestSessA); err == nil {
		t.Fatal("a hidden folder session was summarized")
	}
}

func TestSessionWithoutAVerdictIsAskedAgainOnce(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	uiWriteRichTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo)
	n := 0
	m.cfg.Advisor = func(string, string, bool, string) (string, error) {
		n++
		return "TITLE: T\nSUMMARY: S\nDIFFERENTLY: D", nil
	}
	m.indexSessions(10)
	m.setAnalysis(true)
	m.analyzeNext()
	if m.analyzeNext() != 0 || n != 1 {
		t.Fatalf("asked %d times; a session whose answer lacked a verdict must be marked unclear, not asked forever", n)
	}
	if s := m.sessionLibrary().Sessions[0]; s.Completed != "unclear" {
		t.Fatalf("session = %+v", s)
	}
}

func uiWriteLoopTranscript(t *testing.T, dir, id, cwd string) {
	t.Helper()
	_ = os.MkdirAll(dir, 0o755)
	base := time.Now().Add(-3 * time.Hour).UTC()
	ts := func(m int) string { return base.Add(time.Duration(m) * time.Minute).Format(time.RFC3339Nano) }
	recs := []map[string]any{{"type": "user", "sessionId": id, "cwd": cwd, "timestamp": ts(0), "message": map[string]any{"content": "go"}}}
	for i := 0; i < 4; i++ {
		tid := "t" + string(rune('a'+i))
		recs = append(recs,
			map[string]any{"type": "assistant", "sessionId": id, "cwd": cwd, "timestamp": ts(1 + i*2), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": tid, "name": "Bash", "input": map[string]any{"command": "make build"}}}}},
			map[string]any{"type": "user", "sessionId": id, "cwd": cwd, "timestamp": ts(2 + i*2), "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": tid, "is_error": true, "content": "boom"}}}})
	}
	recs = append(recs, map[string]any{"type": "assistant", "sessionId": id, "cwd": cwd, "timestamp": ts(20), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "e1", "name": "Edit", "input": map[string]any{"file_path": cwd + "/a.go"}}}}})
	var b strings.Builder
	for _, r := range recs {
		l, _ := json.Marshal(r)
		b.Write(l)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, id+".jsonl")
	_ = os.WriteFile(p, []byte(b.String()), 0o600)
	mt := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(p, mt, mt)
}

func TestHealthSignalsFindLoopsRepeatedFailuresAndUnverifiedChanges(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	uiWriteLoopTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessC, repo)
	m.indexSessions(10)
	s := m.sessionLibrary().Sessions[0]
	if s.MaxRepeat != 4 || s.MaxFailRepeat != 4 || !s.Unverified {
		t.Fatalf("facts = %+v", s.uiSessionFacts)
	}
	if s.Health >= 80 || len(s.Signals) != 3 {
		t.Fatalf("health = %d, signals = %v", s.Health, s.Signals)
	}
	// the tidy session from the other fixtures scores high
	uiWriteRichTranscript(t, filepath.Join(ConfigDir(), "projects", "enc"), uiTestSessA, repo)
	m.indexSessions(10)
	for _, x := range m.sessionLibrary().Sessions {
		if x.ID == uiTestSessA && x.Health <= s.Health {
			t.Fatalf("a clean session scored %d, no better than the looping one (%d)", x.Health, s.Health)
		}
	}
}

func TestPagesForCoachEvalsRingsAndResumeSummary(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"/api/coach", "Ways to work better", "/api/evals/runs", "renderEvals", "function ring(", "function sessionSummary", "Summarize this session", "Where it ended"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
