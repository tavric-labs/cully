package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCursor speaks the stream-json shapes of `cursor-agent -p --output-format stream-json
// --stream-partial-output`. The tool_call and result shapes follow Cursor's published format; they
// could not be recorded for real (the account's usage limit was reached), see docs/ui.md.
const fakeCursor = `#!/usr/bin/env python3
import json, os, sys, time
out = os.environ["FAKE_OUT"]
args = sys.argv[1:]
open(out + "/cursor.log", "a").write("args " + " ".join(args) + "\n")
prompt = args[-1]
sid = "5b66e9f6-0031-452d-878e-91b74df22259"
if "--resume" in args:
    sid = args[args.index("--resume") + 1]
def send(o):
    sys.stdout.write(json.dumps(o) + "\n"); sys.stdout.flush()
if "limit" in prompt:
    sys.stderr.write("ActionRequiredError: Your team has reached its usage limit\n"); sys.exit(1)
send({"type": "system", "subtype": "init", "session_id": sid, "model": "Auto Balance", "cwd": os.getcwd(), "permissionMode": "default"})
send({"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": prompt}]}, "session_id": sid})
if "slow" in prompt:
    time.sleep(30)
for piece in ["wor", "king"]:
    send({"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": piece}]}, "session_id": sid, "timestamp_ms": 1})
send({"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "working"}]}, "session_id": sid})
send({"type": "tool_call", "subtype": "started", "call_id": "c1", "tool_call": {"shellToolCall": {"args": {"command": "ls"}}}, "session_id": sid})
send({"type": "tool_call", "subtype": "completed", "call_id": "c1", "tool_call": {"shellToolCall": {"args": {"command": "ls"}, "result": {"success": {"stdout": "a.txt\n", "exitCode": 0}}}}, "session_id": sid})
send({"type": "tool_call", "subtype": "started", "call_id": "c2", "tool_call": {"writeToolCall": {"args": {"path": os.getcwd() + "/b.txt"}}}, "session_id": sid})
send({"type": "tool_call", "subtype": "completed", "call_id": "c2", "tool_call": {"writeToolCall": {"args": {"path": os.getcwd() + "/b.txt"}, "result": {"success": {}}}}, "session_id": sid})
send({"type": "result", "subtype": "success", "is_error": False, "duration_ms": 1200, "result": "done: " + prompt, "session_id": sid, "usage": {"inputTokens": 100, "outputTokens": 5, "cacheReadTokens": 40, "cacheWriteTokens": 0}})
`

func uiCursorManager(t *testing.T) (*uiManager, string, string) {
	m, repo, out := uiTestManager(t, "")
	m.cfg.CursorBin = uiWriteScript(t, fakeCursor)
	return m, repo, out
}

func uiCursorLog(t *testing.T, out string) string {
	b, _ := os.ReadFile(filepath.Join(out, "cursor.log"))
	return string(b)
}

func TestCursorRunnerNormalTurn(t *testing.T) {
	m, _, out := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "do it", Harness: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	v := a.snapshot()
	if v.Agent != "cursor" || v.Run == nil || v.Run.Harness != "Cursor" {
		t.Fatalf("not shown as a Cursor task: %+v", v.Agent)
	}
	evs := a.allEvents()
	texts := 0
	for _, e := range evs {
		if e.Kind == "text" && e.Text == "working" {
			texts++
		}
	}
	if texts != 1 || !uiCodexHas(evs, "tool", "ls") || !uiCodexHas(evs, "tool_result", "a.txt") || !uiCodexHas(evs, "tool", "b.txt") || !uiCodexHas(evs, "result", "done: do it") {
		t.Fatalf("events (streamed text must show once): %+v", evs)
	}
	if v.Usage == nil || v.Usage.Tokens != 145 {
		t.Fatalf("usage %+v", v.Usage)
	}
	if !uiCodexHas(evs, "note", "--force") {
		t.Fatal("edit mode must say it runs with --force")
	}
	log := uiCursorLog(t, out)
	if !strings.Contains(log, "-p --output-format stream-json --stream-partial-output") || !strings.Contains(log, "--force") || !strings.Contains(log, "--sandbox enabled") || strings.Contains(log, "--resume") {
		t.Fatalf("log:\n%s", log)
	}
	a.stop()
	uiWaitFor(t, "stopped", func() bool { return a.snapshot().State == uiStopped })
}

func TestCursorRunnerPlanModeNeverForces(t *testing.T) {
	m, _, out := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "look", Harness: "cursor", Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	log := uiCursorLog(t, out)
	if !strings.Contains(log, "--mode plan") || strings.Contains(log, "--force") {
		t.Fatalf("log:\n%s", log)
	}
	a.stop()
}

func TestCursorRunnerFollowUpResumesChat(t *testing.T) {
	m, _, out := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "first", Harness: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	if err := a.send("again"); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "second turn", func() bool { return a.snapshot().Usage != nil && a.snapshot().Usage.Tokens == 290 })
	log := uiCursorLog(t, out)
	if !strings.Contains(log, "--resume 5b66e9f6-0031-452d-878e-91b74df22259 -- again") {
		t.Fatalf("log:\n%s", log)
	}
	a.stop()
}

func TestCursorRunnerResumeChat(t *testing.T) {
	m, _, out := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "continue", Harness: "cursor", ResumeThread: "01a21c07-9011-72e1-a80c-73d877a9c666"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	if !strings.Contains(uiCursorLog(t, out), "--resume 01a21c07-9011-72e1-a80c-73d877a9c666") {
		t.Fatalf("log:\n%s", uiCursorLog(t, out))
	}
	a.stop()
}

func TestCursorRunnerStopKillsTurn(t *testing.T) {
	m, _, _ := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "slow", Harness: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "working", func() bool { return a.snapshot().State == uiWorking && a.snapshot().Run != nil })
	a.stop()
	uiWaitFor(t, "stopped", func() bool { return a.snapshot().State == uiStopped })
}

func TestCursorRunnerInterruptEndsTurnKeepsTask(t *testing.T) {
	m, _, _ := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "slow", Harness: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "running", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		r, ok := a.runner.(*uiCursorRunner)
		if !ok {
			return false
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.active
	})
	if err := a.runner.Interrupt(); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	if !uiCodexHas(a.allEvents(), "result", "Interrupted") {
		t.Fatalf("events %+v", a.allEvents())
	}
	a.stop()
}

func TestCursorRunnerShowsErrorFromStderr(t *testing.T) {
	m, _, _ := uiCursorManager(t)
	a, err := m.start(uiStartRequest{Prompt: "limit", Harness: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "waiting", func() bool { return a.snapshot().State == uiWaiting })
	if !uiCodexHas(a.allEvents(), "error", "usage limit") {
		t.Fatalf("events %+v", a.allEvents())
	}
	a.stop()
}

func TestCursorDenyRulesAreWrittenOnce(t *testing.T) {
	wt := t.TempDir()
	msg := uiCursorDenyRules(wt, []string{"Bash(git push:*)", "Bash(rm:*)", "WebFetch"})
	b, err := os.ReadFile(filepath.Join(wt, ".cursor", "cli.json"))
	if err != nil || !strings.Contains(string(b), "Shell(git push)") || !strings.Contains(string(b), "Shell(rm)") || !strings.Contains(msg, "2 block rules") {
		t.Fatalf("%v %s %s", err, b, msg)
	}
	if msg := uiCursorDenyRules(wt, []string{"Bash(ls:*)"}); !strings.Contains(msg, "own .cursor/cli.json") {
		t.Fatal(msg)
	}
	if uiCursorDenyRules(t.TempDir(), nil) != "" {
		t.Fatal("no rules, no file")
	}
}

func TestCursorMatrixAndAvailability(t *testing.T) {
	if uiRunCapability("run", "cursor") != "yes" || uiRunCapability("midturn", "cursor") != "partial" {
		t.Fatal("matrix")
	}
}
