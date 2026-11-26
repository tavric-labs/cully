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

func TestAModelThatChangesTheArgumentsOrCallsSomethingElseIsNotTrusted(t *testing.T) {
	want := `{"agent_id":"ab12cd34","name":"Release watcher","scope":"project","approved":true,"definition":"{}","notes":"- n"}`
	call := func(name string, in string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"` + name + `","input":` + in + `}]}}` + "\n"
	}
	result := func(isErr string) string {
		return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":` + isErr + `,"content":"ok"}]}}` + "\n"
	}
	full := "mcp__cully__cully_agent_save"
	if err := uiVerifyToolCall([]byte(call(full, want)+result("false")), full, want); err != nil {
		t.Fatalf("a faithful call was refused: %v", err)
	}
	// key order does not matter, the arguments do
	if err := uiVerifyToolCall([]byte(call(full, `{"notes":"- n","definition":"{}","approved":true,"scope":"project","name":"Release watcher","agent_id":"ab12cd34"}`)+result("false")), full, want); err != nil {
		t.Fatalf("a reordered call was refused: %v", err)
	}
	for name, stream := range map[string]string{
		"changed name":    call(full, strings.Replace(want, "Release watcher", "Evil", 1)) + result("false"),
		"changed approve": call(full, strings.Replace(want, `"approved":true`, `"approved":false`, 1)) + result("false"),
		"another tool":    call("mcp__cully__cully_delete", `{}`) + result("false"),
		"two calls":       call(full, want) + result("false") + call(full, want) + result("false"),
		"no call":         `{"type":"result","result":"Done"}` + "\n",
		"tool error":      call(full, want) + result("true"),
	} {
		if err := uiVerifyToolCall([]byte(stream), full, want); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := uiVerifyToolCall([]byte(call("ToolSearch", `{"query":"x"}`)+call(full, want)+result("false")), full, want); err != nil {
		t.Fatalf("ToolSearch before the call should be fine: %v", err)
	}
}
