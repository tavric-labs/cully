package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEachHarnessSaysWhatItRecordsAboutContext(t *testing.T) {
	_, repo, _ := uiIndexManager(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CURSOR_HOME", t.TempDir())

	// Claude: tokens in the context after the last call; the window is an estimate.
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	_ = os.MkdirAll(dir, 0o755)
	cl := `{"type":"user","cwd":"` + repo + `","timestamp":"2026-11-26T01:00:00Z","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","message":{"model":"claude-sonnet-5-5","usage":{"input_tokens":10,"cache_read_input_tokens":150000,"cache_creation_input_tokens":0},"content":[]}}` + "\n"
	cp := filepath.Join(dir, uiTestSessA+".jsonl")
	_ = os.WriteFile(cp, []byte(cl), 0o600)
	s := uiHarnessSnapshot("claude", cp)
	if !s.Available || s.Context == nil || s.Context.Used != 150010 || !s.Context.Estimated || s.Context.Percent != 75 || s.Model != "claude-sonnet-5-5" {
		t.Fatalf("claude = %+v %+v", s, s.Context)
	}

	// Codex: the real window, and limits when the plan reports them.
	cdx := `{"type":"session_meta","payload":{"id":"` + uiTestSessB + `","cwd":"` + repo + `"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":900000},"last_token_usage":{"input_tokens":380000,"total_tokens":381000},"model_context_window":475000},"rate_limits":{"primary":{"used_percent":41.6,"resets_at":1795701000},"secondary":{"used_percent":12,"resets_at":1796216400}}}}` + "\n"
	xp := filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "2026", "11", "26")
	_ = os.MkdirAll(xp, 0o755)
	xf := filepath.Join(xp, "rollout-x-"+uiTestSessB+".jsonl")
	_ = os.WriteFile(xf, []byte(cdx), 0o600)
	c := uiHarnessSnapshot("codex", xf)
	if !c.Available || c.Context == nil || c.Context.Window != 475000 || c.Context.Percent != 80 || c.Context.Near || c.Context.Estimated || c.Tokens != 900000 {
		t.Fatalf("codex = %+v %+v", c, c.Context)
	}
	if c.Limits == nil || c.Limits.FiveHour.Used != 42 || c.Limits.SevenDay.Used != 12 {
		t.Fatalf("codex limits = %+v", c.Limits)
	}

	// Cursor: nothing recorded, and it says so.
	u := uiHarnessSnapshot("cursor", "")
	if u.Available || u.Context != nil || !strings.Contains(u.Note, "does not record") {
		t.Fatalf("cursor = %+v", u)
	}
}

func TestOpenSessionsCarryTheirSnapshotAndLimitsAreGatheredAcrossHarnesses(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	t.Setenv("CODEX_HOME", t.TempDir())
	dir := filepath.Join(ConfigDir(), "projects", "enc")
	_ = os.MkdirAll(dir, 0o755)
	cl := `{"type":"user","cwd":"` + repo + `","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","message":{"model":"claude-sonnet-5-5","usage":{"input_tokens":10,"cache_read_input_tokens":170000},"content":[]}}` + "\n"
	_ = os.WriteFile(filepath.Join(dir, uiTestSessA+".jsonl"), []byte(cl), 0o600)
	uiFakeProcs(t, []uiProc{{Interactive: true, PID: 1, PPID: 9, Cmd: []string{"claude"}, Cwd: repo, Started: time.Now()}})
	var list []uiSession
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/sessions", nil, uiAuthed), &list)
	if len(list) != 1 || list[0].Snapshot == nil || list[0].Snapshot.Context == nil || list[0].Snapshot.Context.Percent != 85 || list[0].Snapshot.Context.Near {
		b, _ := json.Marshal(list)
		t.Fatalf("sessions = %s", b)
	}
	m.setLimits(1, uiLimits{FiveHour: uiWindow{Used: 61, ResetsAt: 1795701000}, SevenDay: uiWindow{Used: 26}})
	var v uiLimitsView
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/limits", nil, uiAuthed), &v)
	if len(v.Claude) != 1 || v.Claude[0].Account != "token 1" || v.Claude[0].Limits.FiveHour.Used != 61 || v.Cursor == "" {
		t.Fatalf("limits = %+v", v)
	}
	if r := uiDo(t, "GET", srv.URL+"/api/limits", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
}

func TestTaskPanelShowsContextLimitsTokensAndEveryHarness(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"function drawTaskPanel", "Context window", "Usage limits", "Last 5 hours", "Last 7 days", "Tasks today", "Tasks this week", "Other harnesses", "Cursor does not record usage limits", "function snapChip", "'#/task/'", "#/agent/", "/api/limits"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	idx, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(idx), `id="taskpanel"`) {
		t.Error("index.html has no task panel")
	}
}
