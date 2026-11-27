package cully

import "testing"

func TestTaskTitlesAreShortReadableAndSkipTheFiller(t *testing.T) {
	for in, want := range map[string]string{
		"can you please fix the flaky gateway test. It fails on CI":                                     "Fix the flaky gateway test",
		"Hey, I want you to add voice input to the task box":                                            "Add voice input to the task box",
		"look at /twbuild/proshan/personal/mcp-runtime/buddy/internal/cully/ui_agent.go and explain it": "Look at ui_agent.go and explain it",
		"https://example.com/x fix the build":                                                           "Fix the build",
		"`go test ./...` fails with a race, find out why":                                               "Go test ./... fails with a race, find",
		"":       "New task",
		"please": "New task",
		"Refactor the session library so naming, health, evals and coach share one index": "Refactor the session library so naming, health, evals",
	} {
		if got := uiTaskTitle(in); got != want {
			t.Errorf("uiTaskTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWeakTaskTitles(t *testing.T) {
	for title, weak := range map[string]bool{"Fix": true, "": true, "ee541522": true, "Continue: Implemented team intent flow": true, "fix the login": false, "Build session UI and token display": false, "do it": true} {
		if got := uiWeakTaskTitle(title); got != weak {
			t.Errorf("%q weak = %v, want %v", title, got, weak)
		}
	}
}
