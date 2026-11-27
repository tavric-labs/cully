package cully

import (
	"testing"
	"time"
)

func TestAutomatedAdvisorCallsDoNotCountAsRepeatedWork(t *testing.T) {
	old, oldDays := uiRepeatMin, uiRepeatMinDays
	uiRepeatMin, uiRepeatMinDays = 3, 1
	defer func() { uiRepeatMin, uiRepeatMinDays = old, oldDays }()
	mk := func(id, title string, turns, tools int) uiIndexed {
		return uiIndexed{ID: id, Agent: "claude", Cwd: "/work/repo", Name: "repo", Title: title, Task: "advise on session controls for nginx", Source: "ai", Updated: time.Now(), uiSessionFacts: uiSessionFacts{Turns: turns, Tools: tools}}
	}
	advisor := []uiIndexed{mk("a", "Cockpit advisor advice", 1, 0), mk("b", "Advisor advice on nginx", 1, 0), mk("c", "Cockpit advisor controls", 1, 0)}
	if got := uiRepeatedWork(advisor); len(got) != 0 {
		t.Fatalf("advisor calls produced %d repeat patterns", len(got))
	}
	real := []uiIndexed{mk("a", "Fix nginx limits", 6, 20), mk("b", "Fix nginx limits again", 5, 14), mk("c", "Fix nginx limits more", 8, 30)}
	if got := uiRepeatedWork(real); len(got) != 1 {
		t.Fatalf("real repeated work gave %d patterns", len(got))
	}
}

func TestSetupMergesGoalRepeatsPerFolder(t *testing.T) {
	in := []uiRepeat{
		{Kind: "goal", Path: "/w/a", Folder: "a", Count: 12, Days: 5, Examples: []string{"x"}},
		{Kind: "goal", Path: "/w/a", Folder: "a", Count: 11, Days: 4, Examples: []string{"y"}},
		{Kind: "goal", Path: "/w/b", Folder: "b", Count: 6, Days: 3},
		{Kind: "untested", Path: "/w/a", Folder: "a", Count: 10},
	}
	got := setupMergeGoalRepeats(in)
	if len(got) != 3 || got[0].Count != 23 || got[0].Days != 5 || len(got[0].Examples) != 2 {
		t.Fatalf("merged = %+v", got)
	}
}

func TestSetupSuggestionUpdatesExistingAgentInSameFolder(t *testing.T) {
	existing := []uiIdentity{{ID: "i1", Name: "Release Watcher", Scope: "project", Folder: "/w/a/", Role: "Watches releases"}}
	sg := setupAgentSuggestion{Name: "a helper", Role: "Takes over the repeated task in a: nginx", Folder: "/w/a"}
	if cur := setupCoveringAgent(existing, sg); cur == nil || cur.ID != "i1" {
		t.Fatalf("covering = %+v", cur)
	}
	sg.Folder = "/w/other"
	if cur := setupCoveringAgent(existing, sg); cur != nil {
		t.Fatalf("an agent in another folder covered it: %+v", cur)
	}
}
