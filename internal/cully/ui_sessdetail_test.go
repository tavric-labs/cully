package cully

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionDetailShowsRecordedEvidenceWithoutTranscriptText(t *testing.T) {
	server, m, repo := uiTestServer(t, false)
	t.Setenv("CODEX_HOME", t.TempDir())
	dir := filepath.Join(ConfigDir(), "projects", "details")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	uiWriteCodexRollout(t, uiTestSessB, repo)
	path := server.URL + "/api/sessions/" + uiTestSessA
	if r := uiDo(t, "GET", path, nil, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", r.StatusCode)
	}

	var detail uiSessionDetail
	r := uiDo(t, "GET", path, nil, uiAuthed)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	text := string(body)
	for _, exposed := range []string{"SECRET", "Done, both fixed", "go test", "FAIL", dir} {
		if strings.Contains(text, exposed) {
			t.Fatalf("session detail contains %q", exposed)
		}
	}
	uiReadJSON(t, uiDo(t, "GET", path, nil, uiAuthed), &detail)
	x, activity := detail.Session, detail.Activity
	if x.ID != uiTestSessA || x.Title != "Edits to login.go" || x.Description == "" || x.Tools != 2 || !detail.Exists || x.File != "" {
		t.Fatalf("session = %+v", x)
	}
	if len(activity.Files) != 1 || activity.Files[0] != "login.go" {
		t.Fatalf("edited files = %v", activity.Files)
	}
	if len(activity.Calls) != 2 || activity.Calls[0].Name != "Edit" || activity.Calls[0].Result != "" ||
		activity.Calls[1].Name != "Bash" || activity.Calls[1].Result != "failed" || activity.Calls[1].At == nil {
		t.Fatalf("calls = %+v", activity.Calls)
	}
	uiReadJSON(t, uiDo(t, "GET", path+"?offset=1", nil, uiAuthed), &detail)
	if len(detail.Activity.Calls) != 1 || detail.Activity.Calls[0].Number != 2 || detail.Activity.Calls[0].Result != "failed" {
		t.Fatalf("offset calls = %+v", detail.Activity.Calls)
	}
	var codex uiSessionDetail
	uiReadJSON(t, uiDo(t, "GET", server.URL+"/api/sessions/"+uiTestSessB, nil, uiAuthed), &codex)
	if codex.Session.Agent != "codex" || len(codex.Activity.Calls) != 3 || codex.Activity.Calls[1].Name != "apply_patch" {
		t.Fatalf("Codex detail = %+v", codex)
	}

	m.cfg.HideSessionsIn = []string{repo}
	if r := uiDo(t, "GET", path, nil, uiAuthed); r.StatusCode != http.StatusNotFound {
		t.Fatalf("hidden status = %d", r.StatusCode)
	}
}

func TestSessionSummaryIncludesFollowUpsAndStaysOnTheSessionPage(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	dir := filepath.Join(ConfigDir(), "projects", "details")
	uiWriteRichTranscript(t, dir, uiTestSessA, repo)
	m.cfg.Advisor = func(_, _ string, _ bool, prompt string) (string, error) {
		for _, want := range []string{"Original request:", "Latest request: one more thing", "Latest assistant update: Done, both fixed.", "login.go"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("summary input missing %q", want)
			}
		}
		return "TITLE: Repair login and logout\nSUMMARY: Updated both flows. A test command failed.\nCOMPLETED: partly", nil
	}
	updated, err := m.summarizeOne(uiTestSessA)
	if err != nil || updated.Title != "Repair login and logout" || updated.File != "" {
		t.Fatalf("summary = %+v, err = %v", updated, err)
	}
	detail, err := m.sessionDetail(uiTestSessA, 0)
	if err != nil || detail.Session.Description != "Updated both flows. A test command failed." || detail.Session.Health != 85 {
		t.Fatalf("detail = %+v, err = %v", detail, err)
	}
}

func TestParseVerdictSteps(t *testing.T) {
	v := uiParseVerdict("COMPLETED: partly\nITEM: Build session UI | done | Built the page.\nITEM: Fix scan | weird | Left open.\nITEM: |done|no title\nITEM: bad line")
	if len(v.Steps) != 2 || v.Steps[0].Status != "done" || v.Steps[1].Status != "open" || v.Steps[1].Story != "Left open." {
		t.Fatalf("tasks = %+v", v.Steps)
	}
}

func TestDigestKeepsEveryRequest(t *testing.T) {
	var d uiDigest
	d.request("first job")
	d.reply("did first")
	d.request("second job")
	d.reply("did second")
	if len(d.Turns) != 2 || d.Turns[0].Reply != "did first" || d.Turns[1].Request != "second job" {
		t.Fatalf("turns = %+v", d.Turns)
	}
}
