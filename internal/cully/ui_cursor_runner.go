package cully

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Running a task on Cursor. The Cursor CLI (cursor-agent, the program `agent`) has a headless mode:
// `-p --output-format stream-json` prints one JSON object per line. It takes ONE prompt per process,
// so a follow-up starts a new process with `--resume <chatId>` once the turn has ended. A message sent
// while a turn runs waits for the turn to end (the matrix says "partial"). Cursor has no approval
// relay Cully can answer, so what a task may do is decided before it starts: plan mode is
// `--mode plan` (read only), and edit mode passes `--force` together with `--sandbox enabled`, and says
// so in the task. See docs/ui.md, "Tasks on Cursor", for what was and was not verified.

const uiCursorLimits = "Cursor never asks Cully before it acts, so this task runs with --force (commands run without a question) and --sandbox enabled. Your block list cannot be applied to the commands Cursor runs itself; Cully passes it to Cursor as project deny rules, which Cursor applies on its own terms. The MCP servers set up in Cursor load unfiltered."

type uiCursorRunner struct {
	a   *uiAgent
	bin string
	// force is true for edit mode: the turn is run with --force and the sandbox on.
	deny []string

	mu          sync.Mutex
	cmd         *exec.Cmd
	chatID      string
	queue       []string
	active      bool
	interrupted bool
	wake        chan struct{}

	// per-turn parsing state, touched only by the turn's reader
	delta     strings.Builder
	lastFinal string
	gotResult bool
	spec      uiRunSpec
	first     bool
}

func (r *uiCursorRunner) Start(spec uiRunSpec) error {
	r.spec, r.first = spec, true
	r.wake = make(chan struct{}, 1)
	r.chatID = spec.ResumeID
	// the first message goes through the same path as a follow-up, so turn accounting is the same
	if err := r.a.write(spec.Prompt); err != nil {
		return errors.New("could not send the task: " + err.Error())
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if r.a.isStopped() {
			return nil
		}
		r.mu.Lock()
		var next string
		if len(r.queue) > 0 {
			next, r.queue = r.queue[0], r.queue[1:]
		}
		r.mu.Unlock()
		if next == "" {
			select {
			case <-r.wake:
			case <-tick.C:
			}
			continue
		}
		r.turn(next)
	}
}

func (r *uiCursorRunner) Send(text string) error {
	r.mu.Lock()
	r.queue = append(r.queue, text)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return nil
}

func (r *uiCursorRunner) Interrupt() error {
	r.mu.Lock()
	cmd, active := r.cmd, r.active
	if active {
		r.interrupted = true
	}
	r.mu.Unlock()
	if !active || cmd == nil || cmd.Process == nil {
		return errors.New("no turn is running")
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	pid := cmd.Process.Pid
	time.AfterFunc(3*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	return nil
}

func (r *uiCursorRunner) Stop() { r.a.stop() }

// args builds the command line of one turn. The prompt follows "--" so text that starts with a dash
// is never read as a flag.
func (r *uiCursorRunner) args(prompt string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--stream-partial-output", "--workspace", r.spec.Cwd}
	if uiCursorProjectRunsPrograms(r.spec.Cwd) == "" {
		args = append(args, "--trust") // trust a folder only when it holds no Cursor setting that can start a program
	}
	if r.spec.Model != "" {
		args = append(args, "--model", r.spec.Model)
	}
	if r.spec.Mode == "plan" {
		args = append(args, "--mode", "plan") // read only; never --force
	} else {
		args = append(args, "--force", "--sandbox", "enabled")
	}
	r.mu.Lock()
	chat := r.chatID
	r.mu.Unlock()
	if chat != "" {
		args = append(args, "--resume", chat)
	}
	return append(args, "--", prompt)
}

// turn runs one process for one prompt and returns when it has exited.
func (r *uiCursorRunner) turn(text string) {
	a := r.a
	prompt := text
	if r.first {
		if s := strings.TrimSpace(r.spec.System); s != "" {
			prompt = s + "\n\n" + text
		}
		r.first = false
	}
	bin, binErr := uiAwaitBinary(r.bin)
	if binErr != nil {
		r.fail("cannot start cursor-agent: " + binErr.Error())
		return
	}
	cmd := exec.Command(bin, r.args(prompt)...)
	cmd.Dir = r.spec.Cwd
	cmd.Env = uiAgentEnv("")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.fail("cannot start cursor-agent: " + err.Error())
		return
	}
	stderr := &uiTail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		r.fail("cannot start cursor-agent: " + err.Error())
		return
	}
	r.mu.Lock()
	r.cmd, r.active, r.interrupted = cmd, true, false
	r.mu.Unlock()
	a.mu.Lock()
	a.cmd = cmd // so a stop kills this process group
	stopped := a.stopped
	a.mu.Unlock()
	if stopped {
		a.kill()
	}
	r.gotResult, r.lastFinal = false, ""
	r.delta.Reset()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			a.applyParsed(r.event(m))
		}
	}
	werr := cmd.Wait()
	r.mu.Lock()
	r.active = false
	intr := r.interrupted
	r.mu.Unlock()
	p := uiParsed{}
	if t := r.flush(); t != nil {
		p.Events = append(p.Events, *t)
	}
	if !r.gotResult && !a.isStopped() {
		msg := "Interrupted."
		if !intr {
			msg = "Cursor ended without a result."
			if tail := stderr.String(); tail != "" {
				p.Events = append(p.Events, uiEvent{Kind: "error", Text: clipUIText(tail)})
				msg = "The turn did not complete."
			} else if werr != nil {
				p.Events = append(p.Events, uiEvent{Kind: "error", Text: "cursor-agent exited: " + werr.Error()})
			}
		}
		p.Events = append(p.Events, uiEvent{Kind: "result", Text: msg, Failed: !intr})
		p.TurnDone = true
		a.applyParsed(p)
		return
	}
	if len(p.Events) > 0 {
		a.applyParsed(p)
	}
}

func (r *uiCursorRunner) fail(msg string) {
	r.a.applyParsed(uiParsed{Events: []uiEvent{{Kind: "error", Text: msg}, {Kind: "result", Text: "The turn did not start.", Failed: true}}, TurnDone: true})
}

// flush turns buffered streaming text into one text event.
func (r *uiCursorRunner) flush() *uiEvent {
	t := strings.TrimSpace(r.delta.String())
	r.delta.Reset()
	if t == "" {
		return nil
	}
	r.lastFinal = t
	return &uiEvent{Kind: "text", Text: clipUIText(t)}
}

func uiCurStr(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func uiCurMap(m map[string]any, k string) map[string]any {
	v, _ := m[k].(map[string]any)
	return v
}

// event maps one stream-json line to feed events.
func (r *uiCursorRunner) event(m map[string]any) uiParsed {
	out := uiParsed{}
	switch uiCurStr(m, "type") {
	case "system":
		if uiCurStr(m, "subtype") == "init" {
			out.SessionID = uiCurStr(m, "session_id")
			out.Model = uiCurStr(m, "model")
			if out.SessionID != "" {
				r.mu.Lock()
				r.chatID = out.SessionID
				r.mu.Unlock()
			}
		}
	case "assistant":
		text := uiCurContent(m)
		if _, partial := m["timestamp_ms"]; partial {
			r.delta.WriteString(text) // a streamed piece; shown whole when the message or the tool call ends
			return out
		}
		// a complete message replaces what was streamed of it
		r.delta.Reset()
		if strings.TrimSpace(text) != "" {
			r.lastFinal = strings.TrimSpace(text)
			out.Events = append(out.Events, uiEvent{Kind: "text", Text: clipUIText(strings.TrimSpace(text))})
		}
	case "tool_call":
		if t := r.flush(); t != nil {
			out.Events = append(out.Events, *t)
		}
		r.toolCall(&out, m)
	case "result":
		if t := r.flush(); t != nil {
			out.Events = append(out.Events, *t)
		}
		r.gotResult = true
		failed, _ := m["is_error"].(bool)
		if s := uiCurStr(m, "subtype"); s != "" && s != "success" {
			failed = true
		}
		text := uiCurStr(m, "result")
		if text == "" {
			text = r.lastFinal
		}
		out.Final = text
		out.TurnDone = true
		if u := uiCurUsage(uiCurMap(m, "usage")); u.Total() > 0 {
			out.Usage = &u
		}
		out.Events = append(out.Events, uiEvent{Kind: "result", Text: clipUIText(text), Failed: failed})
	}
	return out
}

func uiCurContent(m map[string]any) string {
	msg := uiCurMap(m, "message")
	var sb strings.Builder
	if parts, ok := msg["content"].([]any); ok {
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok && uiCurStr(pm, "type") == "text" {
				sb.WriteString(uiCurStr(pm, "text"))
			}
		}
	} else if s, ok := msg["content"].(string); ok {
		sb.WriteString(s)
	}
	return sb.String()
}

func uiCurUsage(u map[string]any) uiTokens {
	pick := func(keys ...string) int {
		for _, k := range keys {
			if v, ok := u[k]; ok {
				return uiNum(v)
			}
		}
		return 0
	}
	return uiTokens{Input: pick("inputTokens", "input_tokens"), Output: pick("outputTokens", "output_tokens"),
		CacheRead: pick("cacheReadTokens", "cache_read_tokens", "cache_read_input_tokens"), CacheWrite: pick("cacheWriteTokens", "cache_write_tokens", "cache_creation_input_tokens")}
}

// uiCursorTool names a tool_call payload: its single key, such as shellToolCall, and the arguments.
func uiCursorTool(tc map[string]any) (name, key string, args, result map[string]any) {
	for k, v := range tc {
		body, ok := v.(map[string]any)
		if !ok {
			continue
		}
		key = k
		args, result = uiCurMap(body, "args"), uiCurMap(body, "result")
		base := strings.TrimSuffix(k, "ToolCall")
		switch base {
		case "shell":
			name = "Bash"
		case "read":
			name = "Read"
		case "edit", "write", "delete":
			name = "Edit"
		case "function":
			name = uiCurStr(body, "name")
		case "":
			name = k
		default:
			name = strings.ToUpper(base[:1]) + base[1:]
		}
		return
	}
	return "Tool", "", nil, nil
}

func (r *uiCursorRunner) toolCall(out *uiParsed, m map[string]any) {
	id := uiCurStr(m, "call_id")
	name, key, args, result := uiCursorTool(uiCurMap(m, "tool_call"))
	if uiCurStr(m, "subtype") == "started" {
		use := uiToolUse{Name: name}
		var text string
		switch name {
		case "Bash":
			use.Command = uiCurStr(args, "command")
			use.Sig = commandSig(use.Command)
			text = strings.TrimSpace(use.Command)
		case "Read", "Edit":
			use.Path = uiCurStr(args, "path")
			text = use.Path
		default:
			b, _ := json.Marshal(args)
			text = string(b)
		}
		out.Uses = map[string]uiToolUse{id: use}
		out.Events = append(out.Events, uiEvent{Kind: "tool", Tool: name, Text: clipUIText(text)})
		return
	}
	failed, text := false, ""
	if e := result["error"]; e != nil {
		failed = true
		b, _ := json.Marshal(e)
		text = string(b)
	} else if res := uiCurMap(result, "success"); res != nil {
		if name == "Bash" {
			text = uiCurStr(res, "stdout") + uiCurStr(res, "stderr")
			if code := uiNum(res["exitCode"]); code != 0 {
				failed = true
				text += fmt.Sprintf("\nexit code %d", code)
			}
		} else if s := uiCurStr(res, "content"); s != "" && name == "Read" {
			text = fmt.Sprintf("%d lines read", strings.Count(s, "\n")+1)
		} else {
			text = "done"
		}
	} else if result != nil {
		failed = true // rejected, denied or any shape that is not a success
		b, _ := json.Marshal(result)
		text = string(b)
	}
	_ = key
	out.Results = append(out.Results, uiToolResult{ID: id, Failed: failed})
	out.Events = append(out.Events, uiEvent{Kind: "tool_result", Text: clipUIText(text), Failed: failed})
}

// cursorBin finds the Cursor CLI: the configured one, `cursor-agent` or `agent` on the PATH, or the
// usual per-user folder.
func (m *uiManager) cursorBin() string {
	if m.cfg.CursorBin != "" {
		return m.cfg.CursorBin
	}
	for _, n := range []string{"cursor-agent", "agent"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, n := range []string{"cursor-agent", "agent"} {
			p := filepath.Join(home, ".local", "bin", n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// cursorAvailable is the new-task picker's answer for Cursor. Signed-in state is not known here.
func (s *uiServer) cursorAvailable() uiHarnessChoice {
	m := s.m
	c := uiHarnessChoice{ID: "cursor", Name: "Cursor", Steering: uiRunCapability("run", "cursor"), Resume: uiRunCapability("resume", "cursor")}
	c.Installed = m.cursorBin() != ""
	if c.Installed {
		c.Version = uiVersionOf(uiHarnessVersion, "cursor")
	}
	switch {
	case !c.Installed:
		c.Reason = "not installed"
	case c.Steering == "no":
		c.Reason = "not supported by this version of Cully"
	default:
		c.Available = true
	}
	return c
}

// uiCursorProjectRunsPrograms names the first Cursor project file that can make Cursor start a program
// (an MCP server or a hook), or "". A folder with one is never trusted for the user: Cursor's own trust
// check decides, and the user may already have trusted it.
func uiCursorProjectRunsPrograms(dir string) string {
	for _, f := range []string{".cursor/mcp.json", ".cursor/hooks.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return f
		}
	}
	return ""
}

// uiCursorDenyRules writes Cully's block list as Cursor project deny rules in the task's worktree and
// keeps the file out of git's view. ok is false when the whole list could not be applied: a rule of a
// kind Cursor's file cannot express here, a project that already has its own file, or a write that
// failed. An edit task then does not run with --force. The text says what happened, for the task's note.
func uiCursorDenyRules(worktree string, deny []string) (note string, ok bool) {
	var rules, skipped []string
	for _, d := range deny {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if inner, isBash := strings.CutPrefix(d, "Bash("); isBash && strings.HasSuffix(inner, ")") {
			inner = strings.TrimSuffix(strings.TrimSuffix(inner, ")"), ":*")
			if inner = strings.Join(strings.Fields(inner), " "); inner != "" {
				rules = append(rules, "Shell("+inner+")")
				continue
			}
		}
		skipped = append(skipped, d)
	}
	if len(skipped) > 0 {
		return "Your block list has rules Cursor cannot be given (" + strings.Join(skipped, ", ") + ").", false
	}
	if len(rules) == 0 {
		return "", true
	}
	p := filepath.Join(worktree, ".cursor", "cli.json")
	if _, err := os.Stat(p); err == nil {
		return "The project has its own .cursor/cli.json, so your block list could not be added to it.", false
	}
	b, _ := json.MarshalIndent(map[string]any{"version": 1, "permissions": map[string]any{"allow": []string{}, "deny": rules}}, "", "  ")
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, b, 0o644) != nil {
		return "Your block list could not be written for Cursor.", false
	}
	if out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-path", "info/exclude").Output(); err == nil {
		ex := strings.TrimSpace(string(out))
		if !filepath.IsAbs(ex) {
			ex = filepath.Join(worktree, ex)
		}
		if f, err := os.OpenFile(ex, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
			_, _ = f.WriteString("\n.cursor/cli.json\n")
			_ = f.Close()
		}
	}
	return fmt.Sprintf("%d block rules were written to .cursor/cli.json for Cursor to apply.", len(rules)), true
}

func (a *uiAgent) runCursor(m *uiManager, prompt string) {
	r := &uiCursorRunner{a: a, bin: m.cursorBin()}
	mode := "edit"
	if a.req.Mode == "plan" {
		mode = "plan"
	}
	deny, readOnly := m.uiCodexPolicy(mode == "plan", a.req.groups)
	if readOnly {
		mode = "plan" // you have not allowed this task to edit: read only
	}
	// Edit mode runs with --force, so it is allowed only when Cully's whole block list can be given to
	// Cursor. When it cannot, the task runs in plan mode (read only) and says why.
	var denyNote string
	if mode == "edit" {
		if note, ok := uiCursorDenyRules(a.worktree, deny); ok {
			denyNote = note
		} else {
			mode = "plan"
			a.add(uiEvent{Kind: "note", Text: note + " Without that, Cursor would run commands unchecked, so this task runs in plan mode (read only). Run it on Claude Code or Codex to change files, or change the block list."})
		}
	}
	info := &uiRunInfo{Harness: "Cursor", Interface: "cursor-agent -p, stream-json, one process per turn", Permission: mode, Model: a.req.Model, Groups: []string{}, Allowed: []string{}, Blocked: []string{},
		Asks: []string{}, McpMode: "all", Isolation: "worktree", Identity: a.req.identityName, Brief: []string{"Only your task"}}
	if mode == "plan" {
		a.add(uiEvent{Kind: "note", Text: "Plan only: Cursor runs with --mode plan, which is read only. Nothing is forced."})
		info.Asks = []string{"Nothing: plan mode cannot change files or run commands"}
	} else {
		r.deny = deny
		a.add(uiEvent{Kind: "note", Text: uiCursorLimits})
		if denyNote != "" {
			a.add(uiEvent{Kind: "note", Text: denyNote})
		}
		info.Asks = []string{"Nothing: edit mode runs with --force and --sandbox enabled, so commands run without a question"}
	}
	if info.Model == "" {
		info.Model = "its default"
	}
	if a.req.resumeDir != "" || a.req.inPlaceDir != "" {
		info.Isolation = "in place"
	}
	if a.req.ResumeThread != "" {
		info.Resume = "Continues an earlier Cursor chat"
	}
	a.mu.Lock()
	a.runner, a.runInfo = r, info
	a.mu.Unlock()
	a.journal(journalEvent{Agent: "cursor", Class: journalStart})
	a.add(uiEvent{Kind: "user", Text: a.req.Prompt})
	err := r.Start(uiRunSpec{Cwd: a.worktree, Prompt: prompt, Mode: mode, ResumeID: a.req.ResumeThread, Model: a.req.Model, System: a.req.appendSystem, MaxCost: a.req.MaxCost, MaxTurns: a.req.MaxTurns})
	a.journal(journalEvent{Agent: "cursor", Class: journalEnd})
	switch {
	case a.isStopped():
		a.setState(uiStopped)
	case err == nil:
		a.setState(uiDone)
	default:
		a.fail(err.Error())
	}
}

var _ uiRunner = (*uiCursorRunner)(nil)

// uiOwnRunner is true for the harnesses whose tasks run through a uiRunner and work in their own worktree.
func uiOwnRunner(h string) bool { return h == "codex" || h == "cursor" }

// prepareCursor checks a Cursor task request against the matrix and the machine.
func (m *uiManager) prepareCursor(req *uiStartRequest) error {
	if uiRunCapability("run", "cursor") == "no" {
		return errors.New("Cursor tasks are not supported by this version of Cully")
	}
	if m.cursorBin() == "" {
		return errors.New("Cursor is not installed on this machine")
	}
	if req.ResumeThread != "" && !uiSessionID.MatchString(req.ResumeThread) {
		return errors.New("invalid chat id")
	}
	return nil
}
