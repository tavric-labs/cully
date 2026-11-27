package cully

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Running a task on Codex. Cully starts `codex app-server` and speaks JSON-RPC over its stdin and
// stdout (one JSON object per line). The message shapes here are the ones recorded in docs/ui.md.
// Codex runs in the task's worktree with a workspace-write sandbox (read-only for plan mode) and the
// "on-request" approval policy, so a command that needs to leave the sandbox is asked about through
// the same permission card Claude tasks use.

var uiCodexShellWrap = regexp.MustCompile(`(?s)^/bin/(?:ba)?sh -lc '(.*)'$`)

type uiCodexRunner struct {
	a   *uiAgent
	bin string

	deny []string // Cully's block rules, applied to the commands Codex asks about

	wmu   sync.Mutex // serialises writes to stdin
	stdin io.WriteCloser

	mu        sync.Mutex
	nextID    int
	waiters   map[int]chan uiRPC
	threadID  string
	turnID    string
	active    bool // a turn is running
	ready     bool // the thread is open
	queue     []string
	lastTotal uiTokens
	turnBase  uiTokens
	lastFinal string
	fileItems map[string][]string // fileChange item id -> paths
	exited    chan struct{}
}

// uiRPC is one line from Codex: a response, a notification or a request of ours to answer.
type uiRPC struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params map[string]any  `json:"params"`
	Result map[string]any  `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r *uiCodexRunner) request(method string, params any) (chan uiRPC, error) {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	ch := make(chan uiRPC, 1)
	if r.waiters == nil {
		r.waiters = map[int]chan uiRPC{}
	}
	r.waiters[id] = ch
	r.mu.Unlock()
	if err := r.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	return ch, nil
}

func (r *uiCodexRunner) call(method string, params any, wait time.Duration) (map[string]any, error) {
	ch, err := r.request(method, params)
	if err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, errors.New(m.Error.Message)
		}
		return m.Result, nil
	case <-r.exited:
		return nil, errors.New("codex exited")
	case <-time.After(wait):
		return nil, errors.New(method + " timed out")
	}
}

func (r *uiCodexRunner) write(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.wmu.Lock()
	defer r.wmu.Unlock()
	_, err = r.stdin.Write(append(line, '\n'))
	return err
}

func (r *uiCodexRunner) Start(spec uiRunSpec) error {
	a := r.a
	cmd := exec.Command(r.bin, "app-server")
	cmd.Dir = spec.Cwd
	cmd.Env = uiAgentEnv("")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &uiTail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return errors.New("cannot start codex: " + err.Error())
	}
	r.stdin = stdin
	r.exited = make(chan struct{})
	a.mu.Lock()
	a.cmd, a.stdin = cmd, stdin
	stopped := a.stopped
	a.mu.Unlock()
	if stopped {
		a.kill()
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			var m uiRPC
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				r.dispatch(m)
			}
		}
		close(r.exited)
	}()
	go func() {
		if err := r.open(spec); err != nil {
			if !a.isStopped() {
				a.add(uiEvent{Kind: "error", Text: err.Error()})
			}
			a.stop()
		}
	}()
	<-r.exited
	werr := cmd.Wait()
	a.denyPendingPermissions()
	if werr != nil && !a.isStopped() {
		if tail := stderr.String(); tail != "" {
			return fmt.Errorf("codex exited: %v\n%s", werr, tail)
		}
		return fmt.Errorf("codex exited: %v", werr)
	}
	return nil
}

// open does the handshake, opens or resumes the thread and sends the first turn.
func (r *uiCodexRunner) open(spec uiRunSpec) error {
	a := r.a
	if _, err := r.call("initialize", map[string]any{"clientInfo": map[string]any{"name": "cully", "title": "Cully", "version": "1"}, "capabilities": map[string]any{}}, 30*time.Second); err != nil {
		return errors.New("codex did not start: " + err.Error())
	}
	if err := r.write(map[string]any{"method": "initialized"}); err != nil {
		return err
	}
	sandbox, instr := "workspace-write", strings.TrimSpace(spec.System)
	if spec.Mode == "plan" {
		sandbox = "read-only"
		instr = strings.TrimSpace(instr + "\n\nPlan only: do not change any files. Investigate and propose a plan.")
	}
	params := map[string]any{"cwd": spec.Cwd, "approvalPolicy": "on-request", "sandbox": sandbox}
	if instr != "" {
		params["developerInstructions"] = instr
	}
	if spec.Model != "" {
		params["model"] = spec.Model
	}
	method := "thread/start"
	if spec.ResumeID != "" {
		method, params["threadId"] = "thread/resume", spec.ResumeID
	}
	res, err := r.call(method, params, 60*time.Second)
	if err != nil {
		return errors.New("codex could not open the thread: " + err.Error())
	}
	th, _ := res["thread"].(map[string]any)
	id, _ := th["id"].(string)
	model, _ := res["model"].(string)
	if id == "" {
		return errors.New("codex did not return a thread id")
	}
	r.mu.Lock()
	r.threadID, r.ready = id, true
	r.mu.Unlock()
	a.applyParsed(uiParsed{SessionID: id, Model: model})
	if err := a.write(spec.Prompt); err != nil {
		return errors.New("could not send the task: " + err.Error())
	}
	return nil
}

func (r *uiCodexRunner) Send(text string) error {
	r.mu.Lock()
	if !r.ready || r.active {
		r.queue = append(r.queue, text) // goes out when the current turn ends
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	return r.startTurn(text)
}

func (r *uiCodexRunner) startTurn(text string) error {
	r.mu.Lock()
	r.active = true
	r.turnBase = r.lastTotal
	tid := r.threadID
	r.mu.Unlock()
	ch, err := r.request("turn/start", map[string]any{"threadId": tid, "input": []any{map[string]any{"type": "text", "text": text}}})
	if err != nil {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return err
	}
	go func() {
		var m uiRPC
		select {
		case m = <-ch:
		case <-r.exited:
			return
		}
		if m.Error != nil {
			r.mu.Lock()
			r.active = false
			r.mu.Unlock()
			r.a.applyParsed(uiParsed{Events: []uiEvent{{Kind: "error", Text: "Codex refused the turn: " + m.Error.Message}, {Kind: "result", Text: "The turn did not start.", Failed: true}}, TurnDone: true})
			return
		}
		if t, ok := m.Result["turn"].(map[string]any); ok {
			if id, _ := t["id"].(string); id != "" {
				r.mu.Lock()
				if r.turnID == "" {
					r.turnID = id
				}
				r.mu.Unlock()
			}
		}
	}()
	return nil
}

func (r *uiCodexRunner) Interrupt() error {
	r.mu.Lock()
	tid, turn, active := r.threadID, r.turnID, r.active
	r.mu.Unlock()
	if !active || turn == "" {
		return errors.New("no turn is running")
	}
	_, err := r.call("turn/interrupt", map[string]any{"threadId": tid, "turnId": turn}, 10*time.Second)
	return err
}

func (r *uiCodexRunner) Stop() { r.a.stop() }

func (r *uiCodexRunner) dispatch(m uiRPC) {
	if m.Method == "" { // a response to one of our requests
		var id int
		if json.Unmarshal(m.ID, &id) == nil {
			r.mu.Lock()
			ch := r.waiters[id]
			delete(r.waiters, id)
			r.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
		return
	}
	if len(m.ID) > 0 { // a request from Codex that needs our answer
		go r.serverRequest(m)
		return
	}
	r.notification(m.Method, m.Params)
}

func uiCodexStr(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func uiCodexMap(m map[string]any, k string) map[string]any {
	v, _ := m[k].(map[string]any)
	return v
}

func uiCodexCommand(item map[string]any) string {
	c := uiCodexStr(item, "command")
	if m := uiCodexShellWrap.FindStringSubmatch(c); m != nil {
		return m[1]
	}
	return c
}

func uiCodexTokens(b map[string]any) uiTokens {
	in, cached, write, out := uiNum(b["inputTokens"]), uiNum(b["cachedInputTokens"]), uiNum(b["cacheWriteInputTokens"]), uiNum(b["outputTokens"])
	fresh := in - cached - write
	if fresh < 0 {
		fresh = 0
	}
	return uiTokens{Input: fresh, CacheRead: cached, CacheWrite: write, Output: out}
}

func uiTokensSub(a, b uiTokens) uiTokens {
	pos := func(x int) int {
		if x < 0 {
			return 0
		}
		return x
	}
	return uiTokens{pos(a.Input - b.Input), pos(a.CacheRead - b.CacheRead), pos(a.CacheWrite - b.CacheWrite), pos(a.Output - b.Output)}
}

func uiCodexFilePaths(item map[string]any) []string {
	var out []string
	ch, _ := item["changes"].([]any)
	for _, c := range ch {
		if m, ok := c.(map[string]any); ok {
			if p := uiCodexStr(m, "path"); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (r *uiCodexRunner) notification(method string, p map[string]any) {
	a := r.a
	switch method {
	case "turn/started":
		if t := uiCodexMap(p, "turn"); t != nil {
			r.mu.Lock()
			r.turnID = uiCodexStr(t, "id")
			r.mu.Unlock()
		}
	case "item/started", "item/completed":
		item := uiCodexMap(p, "item")
		if item != nil {
			a.applyParsed(r.item(method == "item/completed", item))
		}
	case "turn/plan/updated":
		if text := uiCodexPlan(p["plan"]); text != "" {
			a.applyParsed(uiParsed{Events: []uiEvent{{Kind: "plan", Text: text}}})
		}
	case "thread/tokenUsage/updated":
		tu := uiCodexMap(p, "tokenUsage")
		if tu == nil {
			return
		}
		total, last := uiCodexMap(tu, "total"), uiCodexMap(tu, "last")
		r.mu.Lock()
		r.lastTotal = uiCodexTokens(total)
		r.mu.Unlock()
		a.applyParsed(uiParsed{Context: uiNum(last["inputTokens"]), Window: uiNum(tu["modelContextWindow"])})
	case "thread/compacted":
		a.applyParsed(uiParsed{Compacted: true, Events: []uiEvent{{Kind: "system", Text: "Codex compacted this task's history to make room"}}})
	case "error":
		msg := uiCodexStr(p, "message")
		if e := uiCodexMap(p, "error"); e != nil && msg == "" {
			msg = uiCodexStr(e, "message")
		}
		if willRetry, _ := p["willRetry"].(bool); !willRetry && msg != "" {
			a.applyParsed(uiParsed{Events: []uiEvent{{Kind: "error", Text: clipUIText(msg)}}})
		}
	case "turn/completed":
		r.turnDone(uiCodexMap(p, "turn"))
	}
}

func (r *uiCodexRunner) turnDone(t map[string]any) {
	r.mu.Lock()
	r.active, r.turnID = false, ""
	usage := uiTokensSub(r.lastTotal, r.turnBase)
	final := r.lastFinal
	r.lastFinal = ""
	var next string
	if len(r.queue) > 0 {
		next, r.queue = r.queue[0], r.queue[1:]
	}
	r.mu.Unlock()
	status := uiCodexStr(t, "status")
	p := uiParsed{TurnDone: true, Final: final}
	if usage.Total() > 0 {
		p.Usage = &usage
	}
	failed := status == "failed"
	text := final
	if e := uiCodexMap(t, "error"); e != nil {
		if msg := uiCodexStr(e, "message"); msg != "" {
			p.Events = append(p.Events, uiEvent{Kind: "error", Text: clipUIText(msg)})
			text = msg
		}
	}
	if status == "interrupted" && text == "" {
		text = "Interrupted."
	}
	p.Events = append(p.Events, uiEvent{Kind: "result", Text: clipUIText(text), Failed: failed})
	r.a.applyParsed(p)
	if next != "" {
		if err := r.startTurn(next); err != nil {
			r.a.add(uiEvent{Kind: "error", Text: "could not send the next message: " + err.Error()})
		}
	}
}

// item maps a thread item to feed events and tool bookkeeping.
func (r *uiCodexRunner) item(done bool, item map[string]any) uiParsed {
	out := uiParsed{Uses: map[string]uiToolUse{}}
	id := uiCodexStr(item, "id")
	status := uiCodexStr(item, "status")
	switch uiCodexStr(item, "type") {
	case "agentMessage":
		if !done {
			return out
		}
		text := uiCodexStr(item, "text")
		if strings.TrimSpace(text) != "" {
			out.Events = append(out.Events, uiEvent{Kind: "text", Text: clipUIText(text)})
			if uiCodexStr(item, "phase") != "commentary" {
				r.mu.Lock()
				r.lastFinal = text
				r.mu.Unlock()
			}
		}
	case "commandExecution":
		cmd := uiCodexCommand(item)
		if !done {
			out.Uses[id] = uiToolUse{Name: "Bash", Command: cmd, Sig: commandSig(cmd)}
			out.Events = append(out.Events, uiEvent{Kind: "tool", Tool: "Bash", Text: clipUIText(strings.TrimSpace(cmd))})
			return out
		}
		failed := status == "failed" || status == "declined" || (item["exitCode"] != nil && uiNum(item["exitCode"]) != 0)
		text := uiCodexStr(item, "aggregatedOutput")
		if status == "declined" {
			text = "Declined."
		}
		out.Results = append(out.Results, uiToolResult{ID: id, Failed: failed})
		out.Events = append(out.Events, uiEvent{Kind: "tool_result", Text: clipUIText(text), Failed: failed})
	case "fileChange":
		paths := uiCodexFilePaths(item)
		r.mu.Lock()
		if r.fileItems == nil {
			r.fileItems = map[string][]string{}
		}
		_, seen := r.fileItems[id]
		if len(paths) > 0 {
			r.fileItems[id] = paths
		}
		r.mu.Unlock()
		if !seen && len(paths) > 0 {
			for i, p := range paths {
				out.Uses[fmt.Sprintf("%s#%d", id, i)] = uiToolUse{Name: "Edit", Path: p}
			}
			out.Events = append(out.Events, uiEvent{Kind: "tool", Tool: "Edit", Text: clipUIText(strings.Join(paths, ", "))})
		}
		if done {
			failed := status == "failed" || status == "declined"
			for i := range paths {
				out.Results = append(out.Results, uiToolResult{ID: fmt.Sprintf("%s#%d", id, i), Failed: failed})
			}
			text := fmt.Sprintf("%d file(s) changed", len(paths))
			if failed {
				text = "Not applied (" + status + ")."
			}
			out.Events = append(out.Events, uiEvent{Kind: "tool_result", Text: text, Failed: failed})
		}
	case "mcpToolCall":
		name := "mcp__" + uiCodexStr(item, "server") + "__" + uiCodexStr(item, "tool")
		if !done {
			args, _ := json.Marshal(item["arguments"])
			out.Events = append(out.Events, uiEvent{Kind: "tool", Tool: name, Text: clipUIText(string(args))})
			return out
		}
		failed := status == "failed" || item["error"] != nil
		res, _ := json.Marshal(item["result"])
		out.Events = append(out.Events, uiEvent{Kind: "tool_result", Text: clipUIText(string(res)), Failed: failed})
	}
	// reasoning, plan text, user messages and hooks are not shown
	return out
}

func uiCodexPlan(v any) string {
	list, _ := v.([]any)
	todos := make([]any, 0, len(list))
	for _, x := range list {
		m, _ := x.(map[string]any)
		st := "pending"
		switch uiCodexStr(m, "status") {
		case "completed":
			st = "completed"
		case "inProgress", "in_progress":
			st = "in_progress"
		}
		todos = append(todos, map[string]any{"content": uiCodexStr(m, "step"), "status": st})
	}
	return uiPlanText(todos)
}

// serverRequest answers a question from Codex. Approvals go through the permission card.
func (r *uiCodexRunner) serverRequest(m uiRPC) {
	reply := func(result any) { _ = r.write(map[string]any{"id": m.ID, "result": result}) }
	var tool string
	var input map[string]any
	var rated, ratedWhy string
	var grantRoot string
	switch m.Method {
	case "item/commandExecution/requestApproval":
		tool = "Bash"
		cmd := uiCodexCommand(m.Params)
		if len(r.deny) > 0 && uiCommandOpaque(cmd) {
			reply(map[string]any{"decision": "decline"})
			r.a.add(uiEvent{Kind: "note", Text: "Declined a command Codex asked to run: the program is a variable, so your block list cannot be checked against it."})
			return
		}
		if rule := uiRuleBlocksCommand(r.deny, cmd); rule != "" {
			reply(map[string]any{"decision": "decline"})
			r.a.add(uiEvent{Kind: "note", Text: "Declined a command Codex asked to run: it matches your block list (" + rule + ")."})
			return
		}
		input = map[string]any{"command": cmd, "cwd": uiCodexStr(m.Params, "cwd")}
	case "item/fileChange/requestApproval":
		tool = "Edit"
		input = map[string]any{}
		r.mu.Lock()
		paths := r.fileItems[uiCodexStr(m.Params, "itemId")]
		r.mu.Unlock()
		if len(paths) > 0 {
			input["file_path"], input["files"] = paths[0], paths
		}
		if s := uiCodexStr(m.Params, "reason"); s != "" {
			input["reason"] = s
		}
		if s := uiCodexStr(m.Params, "grantRoot"); s != "" {
			input["grantRoot"] = s
			grantRoot = s
		}
		rated, ratedWhy = uiCodexFileRisk(paths, grantRoot, r.a.worktree)
	default:
		_ = r.write(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "Cully does not handle " + m.Method + " yet"}})
		r.a.add(uiEvent{Kind: "note", Text: "Codex asked for something Cully cannot answer (" + m.Method + "); it was refused."})
		return
	}
	raw, _ := json.Marshal(input)
	decision, _ := r.a.askPermissionRated(tool, raw, rated, ratedWhy)
	switch decision {
	case "allow":
		reply(map[string]any{"decision": "accept"})
	case "allow_always":
		if grantRoot != "" || (tool == "Bash" && len(r.deny) > 0) {
			// a folder grant is never kept for the session, and while block rules exist every command
			// is asked about again so each one is checked
			reply(map[string]any{"decision": "accept"})
		} else {
			reply(map[string]any{"decision": "acceptForSession"})
		}
	default:
		reply(map[string]any{"decision": "decline"})
	}
}

// denyPendingPermissions refuses every question still waiting, when the task has ended.
func (a *uiAgent) denyPendingPermissions() {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	for _, p := range a.perm.list {
		if p.Status == "pending" {
			select {
			case p.decide <- "deny":
			default:
			}
		}
	}
}

func (a *uiAgent) runCodex(m *uiManager, prompt string) {
	r := &uiCodexRunner{a: a, bin: m.codexBin()}
	mode := "edit"
	if a.req.Mode == "plan" {
		mode = "plan"
	}
	deny, readOnly := m.uiCodexPolicy(mode == "plan", a.req.groups)
	r.deny = deny
	if readOnly {
		mode = "plan" // Codex's sandbox is read only when you have not allowed it to edit
	}
	a.add(uiEvent{Kind: "note", Text: uiCodexLimits})
	info := &uiRunInfo{Harness: "Codex", Interface: "Codex app-server, JSON-RPC over stdio", Permission: mode, Model: a.req.Model, Groups: []string{}, Allowed: []string{}, Blocked: []string{},
		Asks: []string{"Commands that need to leave the sandbox", "File changes outside the workspace"}, McpMode: "all", Isolation: "worktree", Identity: a.req.identityName, Brief: []string{"Only your task"}}
	if info.Model == "" {
		info.Model = "its default"
	}
	if a.req.resumeDir != "" || a.req.inPlaceDir != "" {
		info.Isolation = "in place"
	}
	if a.req.ResumeThread != "" {
		info.Resume = "Continues an earlier Codex thread"
	}
	a.mu.Lock()
	a.runner, a.runInfo = r, info
	a.mu.Unlock()
	a.journal(journalEvent{Agent: "codex", Class: journalStart})
	a.add(uiEvent{Kind: "user", Text: a.req.Prompt})
	err := r.Start(uiRunSpec{Cwd: a.worktree, Prompt: prompt, Mode: mode, ResumeID: a.req.ResumeThread, Model: a.req.Model, System: a.req.appendSystem, MaxCost: a.req.MaxCost, MaxTurns: a.req.MaxTurns})
	a.journal(journalEvent{Agent: "codex", Class: journalEnd})
	switch {
	case a.isStopped():
		a.setState(uiStopped)
	case err == nil:
		a.setState(uiDone)
	default:
		a.fail(err.Error())
	}
}
