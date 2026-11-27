package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCodex speaks the app-server shapes recorded in docs/ui.md.
const fakeCodex = `#!/usr/bin/env python3
import json, os, sys
out = os.environ["FAKE_OUT"]
def log(s):
    open(out + "/codex.log", "a").write(s + "\n")
def send(o):
    sys.stdout.write(json.dumps(o) + "\n"); sys.stdout.flush()
def note(m, p): send({"method": m, "params": p})
log("args " + " ".join(sys.argv[1:]))
tid = "01a21c07-9011-72e1-a80c-73d877a9c666"
total = 0
for line in sys.stdin:
    m = json.loads(line)
    meth = m.get("method")
    if meth == "initialize":
        send({"id": m["id"], "result": {"userAgent": "fake", "codexHome": "/x"}})
    elif meth in ("thread/start", "thread/resume"):
        log(meth + " " + json.dumps(m["params"], sort_keys=True))
        send({"id": m["id"], "result": {"thread": {"id": tid}, "model": "gpt-fake"}})
    elif meth == "turn/start":
        text = m["params"]["input"][0]["text"]
        log("turn " + text)
        send({"id": m["id"], "result": {"turn": {"id": "turn1", "status": "inProgress"}}})
        note("turn/started", {"threadId": tid, "turn": {"id": "turn1"}})
        item = {"type": "commandExecution", "id": "exec-1", "command": "/bin/bash -lc 'touch a.txt'", "cwd": os.getcwd(), "status": "inProgress"}
        if "approve" in text:
            note("item/started", {"item": item})
            send({"method": "item/commandExecution/requestApproval", "id": 7, "params": {"kind": "command", "itemId": "exec-1", "command": item["command"], "cwd": item["cwd"]}})
            resp = json.loads(sys.stdin.readline())
            log("approval " + json.dumps(resp, sort_keys=True))
            d = resp["result"]["decision"]
            done = dict(item, status="declined" if d == "decline" else "completed", aggregatedOutput=None if d == "decline" else "", exitCode=None if d == "decline" else 0)
            note("item/completed", {"item": done})
        else:
            note("item/completed", {"item": {"type": "agentMessage", "id": "m1", "text": "working", "phase": "commentary"}})
            note("item/started", {"item": {"type": "fileChange", "id": "fc1", "status": "inProgress", "changes": [{"path": os.getcwd() + "/a.txt", "kind": {"type": "add"}, "diff": "x"}]}})
            note("item/completed", {"item": {"type": "fileChange", "id": "fc1", "status": "completed", "changes": [{"path": os.getcwd() + "/a.txt", "kind": {"type": "add"}, "diff": "x"}]}})
        note("item/completed", {"item": {"type": "agentMessage", "id": "m2", "text": "done: " + text, "phase": "final_answer"}})
        total += 100
        b = {"totalTokens": total + 5, "inputTokens": total, "cachedInputTokens": 40, "cacheWriteInputTokens": 0, "outputTokens": 5, "reasoningOutputTokens": 0}
        note("thread/tokenUsage/updated", {"threadId": tid, "turnId": "turn1", "tokenUsage": {"total": b, "last": b, "modelContextWindow": 475000}})
        note("turn/completed", {"threadId": tid, "turn": {"id": "turn1", "status": "completed", "error": None}})
`

func uiCodexManager(t *testing.T) (*uiManager, string, string) {
	m, repo, out := uiTestManager(t, "")
	m.cfg.CodexBin = uiWriteScript(t, fakeCodex)
	return m, repo, out
}

func uiCodexHas(evs []uiEvent, kind, sub string) bool {
	for _, e := range evs {
		if e.Kind == kind && strings.Contains(e.Text, sub) {
			return true
		}
	}
	return false
}

func TestCodexRunnerNormalTurn(t *testing.T) {
	m, _, out := uiCodexManager(t)
	a, err := m.start(uiStartRequest{Prompt: "do it", Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	v := a.snapshot()
	if v.Agent != "codex" || v.Run == nil || v.Run.Harness != "Codex" {
		t.Fatalf("not shown as a Codex task: %+v", v.Agent)
	}
	evs := a.allEvents()
	if !uiCodexHas(evs, "text", "done: do it") || !uiCodexHas(evs, "tool", "a.txt") || !uiCodexHas(evs, "result", "done: do it") {
		t.Fatalf("events: %+v", evs)
	}
	if v.Usage == nil || v.Usage.Tokens != 105 || v.Context == nil || v.Context.Used != 100 {
		t.Fatalf("usage %+v context %+v", v.Usage, v.Context)
	}
	if len(v.Files) != 1 || v.Files[0] != "a.txt" {
		t.Fatalf("files %v", v.Files)
	}
	if err := a.send("again"); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "second turn", func() bool { return a.snapshot().Usage != nil && a.snapshot().Usage.Tokens == 105+100 })
	uiWaitFor(t, "waiting again", func() bool { return a.snapshot().State == uiWaiting })
	log, _ := os.ReadFile(filepath.Join(out, "codex.log"))
	if !strings.Contains(string(log), `"sandbox": "workspace-write"`) || !strings.Contains(string(log), `"approvalPolicy": "on-request"`) || !strings.Contains(string(log), "turn again") {
		t.Fatalf("log:\n%s", log)
	}
	a.stop()
	uiWaitFor(t, "stopped", func() bool { return a.snapshot().State == uiStopped })
}

func TestCodexRunnerPlanModeIsReadOnly(t *testing.T) {
	m, _, out := uiCodexManager(t)
	a, err := m.start(uiStartRequest{Prompt: "look", Harness: "codex", Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	log, _ := os.ReadFile(filepath.Join(out, "codex.log"))
	if !strings.Contains(string(log), `"sandbox": "read-only"`) {
		t.Fatalf("log:\n%s", log)
	}
}

func uiCodexApproval(t *testing.T, decision string) (string, *uiAgent) {
	m, _, out := uiCodexManager(t)
	a, err := m.start(uiStartRequest{Prompt: "approve me", Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "a question", func() bool { return len(a.pendingPermissions()) == 1 })
	p := a.pendingPermissions()[0]
	if p.Tool != "Bash" || !strings.Contains(p.Summary, "touch a.txt") || p.Risk == "" {
		t.Fatalf("card %+v", p)
	}
	if err := a.decidePermission(p.ID, decision, p.Hash); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	log, _ := os.ReadFile(filepath.Join(out, "codex.log"))
	return string(log), a
}

func TestCodexRunnerApprovalAllowed(t *testing.T) {
	log, a := uiCodexApproval(t, "allow")
	if !strings.Contains(log, `"decision": "accept"`) {
		t.Fatalf("log:\n%s", log)
	}
	if uiCodexHas(a.allEvents(), "tool_result", "Declined") {
		t.Fatal("allowed command shown as declined")
	}
}

func TestCodexRunnerApprovalDenied(t *testing.T) {
	log, a := uiCodexApproval(t, "deny")
	if !strings.Contains(log, `"decision": "decline"`) {
		t.Fatalf("log:\n%s", log)
	}
	if !uiCodexHas(a.allEvents(), "tool_result", "Declined") {
		t.Fatal("declined command not shown")
	}
}

func TestCodexRunnerResumeThread(t *testing.T) {
	m, _, out := uiCodexManager(t)
	a, err := m.start(uiStartRequest{Prompt: "continue", Harness: "codex", ResumeThread: "01a21c07-9011-72e1-a80c-73d877a9c666"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	log, _ := os.ReadFile(filepath.Join(out, "codex.log"))
	if !strings.Contains(string(log), "thread/resume") || !strings.Contains(string(log), `"threadId": "01a21c07-9011-72e1-a80c-73d877a9c666"`) {
		t.Fatalf("log:\n%s", log)
	}
}

func TestCodexHarnessSelection(t *testing.T) {
	m, _, _ := uiTestManager(t, "")
	m.cfg.CodexBin = ""
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, err := m.start(uiStartRequest{Prompt: "x", Harness: "codex"}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err %v", err)
	}
	if _, err := m.start(uiStartRequest{Prompt: "x", Harness: "cursor"}); err == nil {
		t.Fatal("cursor tasks must be refused")
	}
	if uiRunCapability("run", "codex") != "yes" || uiRunCapability("run", "cursor") != "no" {
		t.Fatal("matrix")
	}
}
