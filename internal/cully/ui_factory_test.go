package cully

import (
	"strings"
	"testing"
)

func TestFactoryModeNeedsScenariosAndSetsUpTheLoop(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "add export", Factory: true}); err == nil || !strings.Contains(err.Error(), "acceptance scenario") {
		t.Fatalf("factory without scenarios: %v", err)
	}
	if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "add export", Factory: true, DoneWhen: "x", Mode: "plan"}); err == nil {
		t.Fatal("factory mode accepted plan only")
	}
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "add the export button to the report page", Factory: true, DoneWhen: "the report page shows an Export button"})
	if err != nil {
		t.Fatal(err)
	}
	r := a.req
	if !r.Loop || !r.Context || !r.Holdout || r.Mode != "edit" {
		t.Fatalf("factory did not set up the loop: %+v", r)
	}
	if !strings.HasPrefix(r.Prompt, "add the export button") || !strings.Contains(r.Prompt, "Work in factory mode") {
		t.Fatalf("prompt = %q", r.Prompt)
	}
	if strings.Contains(a.snapshot().Title, "factory mode") || !strings.Contains(strings.ToLower(a.snapshot().Title), "export") {
		t.Fatalf("title came from the brief: %q", a.snapshot().Title)
	}
}
