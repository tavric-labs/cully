package cully

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Asking you before a task does something it is not already allowed to do.
//
// A headless Claude cannot show a permission prompt, so it used to refuse any
// tool use outside the allowed list ("it should have asked for permission").
// Claude can instead be told to ask a tool of ours: --permission-prompt-tool.
// Cully gives each task a small helper (an MCP server of one tool, started by
// Claude) that passes the question over a private socket to this server. The
// task window shows it as a card with the exact tool and input, and you choose:
// allow once, allow this exact call for the rest of the task, or deny. Nothing
// is allowed unless you click; an unanswered question is denied after ten
// minutes, and stopping the task denies it at once. Tools on the always-blocked
// list never reach the question: Claude refuses them first.

const (
	uiPermTool    = "approve"
	uiPermServer  = "cullyperm"
	uiPermTimeout = 10 * time.Minute
	uiPermKeep    = 30
	// What is asked about must be shown in full. An input longer than this is refused, not clipped.
	uiPermShowMax = 20000
)

var uiPermWait = uiPermTimeout // a variable so tests need not wait

type uiPermission struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Input   json.RawMessage `json:"input"`
	Summary string          `json:"summary"` // one line a person can read: the command, the path, the query
	At      time.Time       `json:"at"`
	Status  string          `json:"status"` // pending, allowed, allowed-always, denied or expired
	Hash    string          `json:"-"`      // of the exact input shown; an allow must name it
	Risk    string          `json:"risk"`   // low, medium or high, from fixed rules
	RiskWhy string          `json:"riskWhy"`
	decide  chan string
}

type uiPermView struct {
	ID      string    `json:"id"`
	Tool    string    `json:"tool"`
	Input   string    `json:"input"`
	Summary string    `json:"summary"`
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Hash    string    `json:"hash"` // the answer must quote this, so it is about exactly what was shown
	Risk    string    `json:"risk"`
	RiskWhy string    `json:"riskWhy"`
}

type uiPermState struct {
	mu     sync.Mutex
	list   []*uiPermission
	always map[string]bool // tool and exact input the user allowed for the rest of this task
	ln     net.Listener
	dir    string
}

func uiPermKey(tool string, input json.RawMessage) string {
	var v any
	if json.Unmarshal(input, &v) == nil {
		if b, err := json.Marshal(v); err == nil { // canonical: sorted keys, no spacing
			return tool + "\x00" + string(b)
		}
	}
	return tool + "\x00" + string(input)
}

// uiPermSummary says in one line what a tool call would do.
func uiPermSummary(tool string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	pick := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		return ""
	}
	switch tool {
	case "Bash":
		return pick("command")
	case "Write", "Edit", "MultiEdit", "NotebookEdit", "Read":
		return pick("file_path", "notebook_path", "path")
	case "WebFetch":
		return pick("url")
	case "WebSearch":
		return pick("query")
	}
	if s := pick("command", "file_path", "path", "url", "query", "name"); s != "" {
		return s
	}
	return uiClip(string(input), 300)
}

func (a *uiAgent) permView(p *uiPermission) uiPermView {
	// The whole input is shown, never a clipped or chosen part of it: a request too long to show, or
	// one that holds a secret the page would have to hide, is refused before it gets here.
	return uiPermView{ID: p.ID, Tool: p.Tool, Input: prettyJSON(p.Input), Summary: p.Summary, At: p.At, Status: p.Status, Hash: p.Hash, Risk: p.Risk, RiskWhy: p.RiskWhy}
}

func prettyJSON(raw json.RawMessage) string {
	var b strings.Builder
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	b.Write(out)
	return b.String()
}

// pendingPermissions are the questions waiting for an answer, oldest first.
func (a *uiAgent) pendingPermissions() []uiPermView {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	var out []uiPermView
	for _, p := range a.perm.list {
		if p.Status == "pending" {
			out = append(out, a.permView(p))
		}
	}
	return out
}

// startPermServer opens the private socket the helper talks to. It returns the
// MCP config to hand to Claude, or "" when the socket could not be made, in
// which case the task runs as before and refused tools stay refused.
func (a *uiAgent) startPermServer() (configPath string) {
	// A unix socket path is short-limited, so it lives in a private folder under /tmp.
	dir, err := os.MkdirTemp("", "cully-perm-")
	if err != nil {
		return ""
	}
	_ = os.Chmod(dir, 0o700)
	sock := filepath.Join(dir, "p.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return ""
	}
	_ = os.Chmod(sock, 0o600)
	exe, err := os.Executable()
	if err != nil {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
		return ""
	}
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{uiPermServer: map[string]any{"command": exe, "args": []string{"_internal", "perm-mcp", sock}}}})
	configPath = filepath.Join(dir, "mcp.json")
	if os.WriteFile(configPath, cfg, 0o600) != nil {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
		return ""
	}
	a.perm.mu.Lock()
	a.perm.ln, a.perm.dir = ln, dir
	if a.perm.always == nil {
		a.perm.always = map[string]bool{}
	}
	a.perm.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go a.answerPermission(c)
		}
	}()
	return configPath
}

// stopPermServer closes the socket and denies anything still waiting.
func (a *uiAgent) stopPermServer() {
	a.perm.mu.Lock()
	ln, dir := a.perm.ln, a.perm.dir
	a.perm.ln, a.perm.dir = nil, ""
	for _, p := range a.perm.list {
		if p.Status == "pending" {
			select {
			case p.decide <- "deny":
			default:
			}
		}
	}
	a.perm.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

type uiPermAsk struct {
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

func (a *uiAgent) answerPermission(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReaderSize(c, 1<<20).ReadBytes('\n')
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	var ask uiPermAsk
	if json.Unmarshal(line, &ask) != nil || ask.ToolName == "" || len(ask.ToolName) > 200 {
		_ = json.NewEncoder(c).Encode(map[string]string{"behavior": "deny", "message": "Cully could not read that request."})
		return
	}
	if len(ask.Input) == 0 {
		ask.Input = json.RawMessage("{}")
	}
	reply := func(decision string) {
		if decision == "allow" || decision == "allow_always" {
			_ = json.NewEncoder(c).Encode(map[string]any{"behavior": "allow", "updatedInput": ask.Input})
			return
		}
		msg := "The user declined this."
		if decision == "expired" {
			msg = "The user did not answer in time, so it was not allowed."
		}
		_ = json.NewEncoder(c).Encode(map[string]string{"behavior": "deny", "message": msg})
	}
	shown := prettyJSON(ask.Input)
	if len(shown) > uiPermShowMax {
		a.add(uiEvent{Kind: "note", Text: "Refused " + ask.ToolName + ": the request is too long to show you in full."})
		_ = json.NewEncoder(c).Encode(map[string]string{"behavior": "deny", "message": "The request was too large for the user to review, so it was refused."})
		return
	}
	if a.tokens.redact(shown) != shown {
		a.add(uiEvent{Kind: "note", Text: "Refused " + ask.ToolName + ": the request contains a secret that cannot be shown."})
		_ = json.NewEncoder(c).Encode(map[string]string{"behavior": "deny", "message": "The request contained a secret, so it was refused."})
		return
	}
	key := uiPermKey(ask.ToolName, ask.Input)
	sum := sha256.Sum256([]byte(key))
	a.perm.mu.Lock()
	if a.perm.always[key] {
		a.perm.mu.Unlock()
		reply("allow")
		return
	}
	risk, why := uiToolRisk(ask.ToolName, ask.Input, a.worktree)
	p := &uiPermission{Risk: risk, RiskWhy: why, ID: uiNewID(), Tool: ask.ToolName, Input: ask.Input, Summary: uiPermSummary(ask.ToolName, ask.Input), At: time.Now().UTC(), Status: "pending", Hash: hex.EncodeToString(sum[:]), decide: make(chan string, 1)}
	a.perm.list = append(a.perm.list, p)
	if len(a.perm.list) > uiPermKeep {
		a.perm.list = a.perm.list[len(a.perm.list)-uiPermKeep:]
	}
	a.perm.mu.Unlock()
	a.add(uiEvent{Kind: "note", Text: "Waiting for you to allow: " + p.Tool + " " + uiClip(p.Summary, 200)})
	a.pingPermission()
	var decision string
	select {
	case decision = <-p.decide:
	case <-time.After(uiPermWait):
		decision = "expired"
	}
	a.perm.mu.Lock()
	switch decision {
	case "allow":
		p.Status = "allowed"
	case "allow_always":
		p.Status = "allowed-always"
		a.perm.always[key] = true
	case "expired":
		p.Status = "expired"
	default:
		p.Status = "denied"
	}
	a.perm.mu.Unlock()
	a.add(uiEvent{Kind: "note", Text: "You answered: " + p.Status + " for " + p.Tool + " " + uiClip(p.Summary, 120)})
	reply(decision)
}

// pingPermission wakes the page so the question shows at once.
func (a *uiAgent) pingPermission() {
	a.mu.Lock()
	a.updated = time.Now().UTC()
	a.mu.Unlock()
}

// decidePermission records the user's answer to a waiting question.
func (a *uiAgent) decidePermission(id, decision, hash string) error {
	if decision != "allow" && decision != "allow_always" && decision != "deny" {
		return errors.New(`decision must be "allow", "allow_always" or "deny"`)
	}
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	for _, p := range a.perm.list {
		if p.ID == id {
			if p.Status != "pending" {
				return errors.New("that question was already answered")
			}
			if decision == "allow_always" && p.Risk == uiRiskHigh {
				return errors.New("a high-risk call is allowed one at a time, never for the rest of the task")
			}
			if decision != "deny" && hash != p.Hash {
				return errors.New("what you were shown does not match what the task asked; reload and look again")
			}
			select {
			case p.decide <- decision:
			default:
			}
			return nil
		}
	}
	return errors.New("no such question")
}

func (s *uiServer) permissionDecide(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var body struct {
		Decision string `json:"decision"`
		Hash     string `json:"hash"`
	}
	if !uiDecode(w, r, &body) {
		return
	}
	if err := a.decidePermission(r.PathValue("pid"), body.Decision, body.Hash); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunPermissionMCP is the helper Claude starts for a task: an MCP server with one
// tool, approve, that Claude calls when a tool needs permission. It asks the Cully
// server over the task's private socket and returns the answer unchanged.
func RunPermissionMCP(sock string) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "cully-permission", Version: "1"}, nil)
	registerPermissionTool(server, sock)
	return server.Run(context.Background(), &sdk.StdioTransport{})
}

func registerPermissionTool(server *sdk.Server, sock string) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"tool_name":   map[string]any{"type": "string"},
		"input":       map[string]any{"type": "object"},
		"tool_use_id": map[string]any{"type": "string"},
	}, "required": []string{"tool_name", "input"}}
	server.AddTool(&sdk.Tool{Name: uiPermTool, Description: "Asks the person using Cully whether a tool call may go ahead.", InputSchema: schema},
		func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			deny := func(msg string) (*sdk.CallToolResult, error) {
				b, _ := json.Marshal(map[string]string{"behavior": "deny", "message": msg})
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil
			}
			c, err := net.DialTimeout("unix", sock, 5*time.Second)
			if err != nil {
				return deny("Cully could not be reached to ask.")
			}
			defer c.Close()
			if _, err := c.Write(append(append([]byte{}, req.Params.Arguments...), '\n')); err != nil {
				return deny("Cully could not be reached to ask.")
			}
			line, err := bufio.NewReaderSize(c, 1<<20).ReadBytes('\n')
			if err != nil {
				return deny("Cully did not answer.")
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.TrimSpace(string(line))}}}, nil
		})
}

// sortedPermissions keeps the answered ones for the task's record, newest first.
func (a *uiAgent) answeredPermissions() []uiPermView {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	var out []uiPermView
	for _, p := range a.perm.list {
		if p.Status != "pending" {
			out = append(out, a.permView(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}
