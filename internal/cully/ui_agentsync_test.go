package cully

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApprovedAgentsAreSavedToCullyMemoryOnlyWhenTheMCPIsConnected(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	var calls []string
	savedCall, savedConn := uiCullyToolCall, uiCullyMCPConnected
	uiCullyToolCall = func(cwd, tool, args string) error { calls = append(calls, tool+" "+args); return nil }
	connected := false
	uiCullyMCPConnected = func(*uiManager) bool { return connected }
	t.Cleanup(func() { uiCullyToolCall, uiCullyMCPConnected = savedCall, savedConn })

	in := uiIdentityInputFor(repo)
	in.Name = "Release watcher"
	a, _ := m.identityCreate(in)
	draft := uiIdentityInputFor(repo)
	draft.Name, draft.Approve = "Draft agent", false
	m.identityCreate(draft)

	if n := m.syncAgents(); n != 0 || len(calls) != 0 {
		t.Fatalf("saved %d agents while the MCP was not connected: %v", n, calls)
	}
	connected = true
	if n := m.syncAgents(); n != 1 || len(calls) != 1 {
		t.Fatalf("saved %d agents, calls %v: only the approved one should be saved", n, calls)
	}
	if !strings.HasPrefix(calls[0], "cully_agent_save ") {
		t.Fatalf("call = %s", calls[0])
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(calls[0], "cully_agent_save ")), &args); err != nil {
		t.Fatal(err)
	}
	if args["agent_id"] != a.ID || args["name"] != "Release watcher" || args["scope"] != "project" || args["approved"] != true {
		t.Fatalf("args = %v", args)
	}
	def, _ := args["definition"].(string)
	if strings.Contains(def, repo) || strings.Contains(def, "notes") && false {
		t.Fatalf("a path on this machine went into the saved definition: %s", def)
	}
	var d map[string]any
	if json.Unmarshal([]byte(def), &d) != nil || d["role"] == "" {
		t.Fatalf("definition = %s", def)
	}
	if n := m.syncAgents(); n != 0 || len(calls) != 1 {
		t.Fatalf("an unchanged agent was saved again: %d %v", n, calls)
	}
	// a change makes it unsaved again
	upd := in
	upd.Role = "Checks pull requests and tests."
	if _, err := m.identityUpdate(a.ID, upd); err != nil {
		t.Fatal(err)
	}
	if n := m.syncAgents(); n != 1 || len(calls) != 2 {
		t.Fatalf("a changed agent was not saved: %d %v", n, calls)
	}
}

func TestAFailedSaveKeepsTheAgentToBeRetriedAndNeverLosesIt(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	savedCall, savedConn := uiCullyToolCall, uiCullyMCPConnected
	fail := true
	tries := 0
	uiCullyToolCall = func(string, string, string) error {
		tries++
		if fail {
			return errTest("the Cully MCP did not accept it")
		}
		return nil
	}
	uiCullyMCPConnected = func(*uiManager) bool { return true }
	t.Cleanup(func() { uiCullyToolCall, uiCullyMCPConnected = savedCall, savedConn })
	m.identityCreate(uiIdentityInputFor(repo))
	if n := m.syncAgents(); n != 0 || tries != 1 {
		t.Fatalf("saved %d, tries %d", n, tries)
	}
	if l := m.identityList(); len(l) != 1 || l[0].Synced {
		t.Fatalf("after a failed save: %+v", l)
	}
	fail = false
	if n := m.syncAgents(); n != 1 {
		t.Fatalf("the retry saved %d", n)
	}
	if l := m.identityList(); !l[0].Synced || l[0].SyncedAt.IsZero() {
		t.Fatalf("after the retry: %+v", l[0])
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestAgentSaveArgumentsHoldNoPathNotesAreSeparate(t *testing.T) {
	id := uiIdentity{ID: "ab12cd34", Name: "N", Scope: "project", Role: "r", Folder: "/home/me/secret/project", Memory: "- note", Approved: true}
	args, err := uiAgentSaveArgs(id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(args, "/home/me") {
		t.Fatalf("a local path was sent: %s", args)
	}
	var v map[string]any
	_ = json.Unmarshal([]byte(args), &v)
	if v["notes"] != "- note" {
		t.Fatalf("notes = %v", v["notes"])
	}
	if strings.Contains(v["definition"].(string), "- note") {
		t.Fatal("notes were also put in the definition")
	}
}
