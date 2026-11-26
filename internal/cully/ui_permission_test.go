package cully

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func uiPermAgent(t *testing.T) (*uiAgent, string) {
	t.Helper()
	m, _, _ := uiIndexManager(t)
	a := &uiAgent{id: "t1", tokens: m.cfg.Tokens, notify: make(chan struct{}), done: make(chan struct{})}
	if a.startPermServer() == "" {
		t.Fatal("the permission socket could not be made")
	}
	t.Cleanup(a.stopPermServer)
	return a, filepath.Join(a.perm.dir, "p.sock")
}

// uiAskSocket plays the helper: sends one question and returns Cully's answer.
func uiAskSocket(t *testing.T, sock, tool, input string) chan map[string]any {
	t.Helper()
	out := make(chan map[string]any, 1)
	go func() {
		c, err := net.Dial("unix", sock)
		if err != nil {
			out <- map[string]any{"error": err.Error()}
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte(`{"tool_name":"` + tool + `","input":` + input + `,"tool_use_id":"u1"}` + "\n"))
		line, _ := bufio.NewReader(c).ReadBytes('\n')
		var v map[string]any
		_ = json.Unmarshal(line, &v)
		out <- v
	}()
	return out
}

func uiWaitPending(t *testing.T, a *uiAgent, n int) []uiPermView {
	t.Helper()
	var got []uiPermView
	uiWaitFor(t, "a question to wait for the user", func() bool { got = a.pendingPermissions(); return len(got) == n })
	return got
}

func TestATaskAsksBeforeUsingATool_AllowGoesBackUnchangedAndDenyRefuses(t *testing.T) {
	a, sock := uiPermAgent(t)
	cmd := `{"command":"ssh -o BatchMode=yes sjc-mcp-03 'hostname; uptime'","description":"look"}`
	ans := uiAskSocket(t, sock, "Bash", cmd)
	pend := uiWaitPending(t, a, 1)
	if pend[0].Tool != "Bash" || !strings.HasPrefix(pend[0].Summary, "ssh -o BatchMode=yes sjc-mcp-03") || !strings.Contains(pend[0].Input, "uptime") {
		t.Fatalf("pending = %+v", pend[0])
	}
	select {
	case v := <-ans:
		t.Fatalf("answered before the user did: %v", v)
	case <-time.After(150 * time.Millisecond):
	}
	if err := a.decidePermission(pend[0].ID, "allow", pend[0].Hash); err != nil {
		t.Fatal(err)
	}
	v := <-ans
	in, _ := v["updatedInput"].(map[string]any)
	if v["behavior"] != "allow" || in["command"] == nil || !strings.Contains(in["command"].(string), "hostname") {
		t.Fatalf("allow reply = %v", v)
	}
	if err := a.decidePermission(pend[0].ID, "allow", pend[0].Hash); err == nil {
		t.Fatal("a question was answered twice")
	}
	ans = uiAskSocket(t, sock, "Bash", `{"command":"rm -rf x"}`)
	pend = uiWaitPending(t, a, 1)
	if err := a.decidePermission(pend[0].ID, "deny", ""); err != nil {
		t.Fatal(err)
	}
	if v := <-ans; v["behavior"] != "deny" || !strings.Contains(v["message"].(string), "declined") {
		t.Fatalf("deny reply = %v", v)
	}
	if err := a.decidePermission("nope", "allow", "x"); err == nil || a.decidePermission(pend[0].ID, "maybe", "") == nil {
		t.Fatal("bad answers were accepted")
	}
}

func TestAllowForTheRestOfTheTaskCoversOnlyTheExactCall(t *testing.T) {
	a, sock := uiPermAgent(t)
	ans := uiAskSocket(t, sock, "Bash", `{"command":"go test ./...","description":"d"}`)
	pend := uiWaitPending(t, a, 1)
	_ = a.decidePermission(pend[0].ID, "allow_always", pend[0].Hash)
	if v := <-ans; v["behavior"] != "allow" {
		t.Fatalf("reply = %v", v)
	}
	// The same call again, with the keys in another order, goes through with no question.
	if v := <-uiAskSocket(t, sock, "Bash", `{"description":"d","command":"go test ./..."}`); v["behavior"] != "allow" || len(a.pendingPermissions()) != 0 {
		t.Fatalf("the repeat was asked about: %v", v)
	}
	// A different command, or the same text through another tool, is still asked.
	_ = uiAskSocket(t, sock, "Bash", `{"command":"go test ./... && curl x","description":"d"}`)
	uiWaitPending(t, a, 1)
	_ = uiAskSocket(t, sock, "Write", `{"command":"go test ./...","description":"d"}`)
	uiWaitPending(t, a, 2)
}

func TestAnUnansweredQuestionIsDeniedAndStoppingTheTaskDeniesWhatWaits(t *testing.T) {
	a, sock := uiPermAgent(t)
	old := uiPermWait
	uiPermWait = 80 * time.Millisecond
	defer func() { uiPermWait = old }()
	if v := <-uiAskSocket(t, sock, "Bash", `{"command":"ls"}`); v["behavior"] != "deny" || !strings.Contains(v["message"].(string), "did not answer") {
		t.Fatalf("expiry reply = %v", v)
	}
	uiPermWait = time.Minute
	ans := uiAskSocket(t, sock, "Bash", `{"command":"ls -la"}`)
	uiWaitPending(t, a, 1)
	a.stopPermServer()
	if v := <-ans; v["behavior"] != "deny" {
		t.Fatalf("stop reply = %v", v)
	}
	if _, err := os.Stat(sock); err == nil {
		t.Fatal("the socket was left behind")
	}
}

func TestThePermissionHelperAsAnMCPToolPassesTheAnswerOn(t *testing.T) {
	a, sock := uiPermAgent(t)
	server := sdk.NewServer(&sdk.Implementation{Name: "cully-permission", Version: "1"}, nil)
	registerPermissionTool(server, sock)
	st, ct := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "claude", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := make(chan string, 1)
	go func() {
		r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: uiPermTool, Arguments: map[string]any{"tool_name": "Bash", "input": map[string]any{"command": "uptime"}, "tool_use_id": "x"}})
		if err != nil || len(r.Content) == 0 {
			res <- "error"
			return
		}
		res <- r.Content[0].(*sdk.TextContent).Text
	}()
	pend := uiWaitPending(t, a, 1)
	_ = a.decidePermission(pend[0].ID, "allow", pend[0].Hash)
	if got := <-res; !strings.Contains(got, `"behavior":"allow"`) || !strings.Contains(got, "uptime") {
		t.Fatalf("tool result = %s", got)
	}
}

func TestATaskIsStartedWithThePermissionHelper(t *testing.T) {
	m, _, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, err := m.start(uiStartRequest{Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(args), "--permission-prompt-tool mcp__cullyperm__approve") || !strings.Contains(string(args), "--mcp-config") {
		t.Fatalf("args = %s", args)
	}
}

func TestAnAnswerMustBeAboutExactlyWhatWasShown(t *testing.T) {
	a, sock := uiPermAgent(t)
	ans := uiAskSocket(t, sock, "Bash", `{"command":"ls"}`)
	pend := uiWaitPending(t, a, 1)
	if err := a.decidePermission(pend[0].ID, "allow", "not-the-hash"); err == nil {
		t.Fatal("an allow with the wrong hash was accepted")
	}
	if err := a.decidePermission(pend[0].ID, "allow", ""); err == nil {
		t.Fatal("an allow with no hash was accepted")
	}
	if pend[0].Input == "" || !strings.Contains(pend[0].Input, "ls") {
		t.Fatalf("the page was not given the whole input: %q", pend[0].Input)
	}
	_ = a.decidePermission(pend[0].ID, "allow", pend[0].Hash)
	if v := <-ans; v["behavior"] != "allow" {
		t.Fatalf("reply = %v", v)
	}
}

func TestARequestTooLongToShowOrHoldingASecretIsRefusedNotClipped(t *testing.T) {
	a, sock := uiPermAgent(t)
	long := `{"command":"echo ` + strings.Repeat("a", uiPermShowMax+10) + `"}`
	if v := <-uiAskSocket(t, sock, "Bash", long); v["behavior"] != "deny" || !strings.Contains(v["message"].(string), "too large") || len(a.pendingPermissions()) != 0 {
		t.Fatalf("a long request was not refused: %v", v)
	}
	if v := <-uiAskSocket(t, sock, "Bash", `{"command":"curl -H tok-aaa1 x"}`); v["behavior"] != "deny" || !strings.Contains(v["message"].(string), "secret") {
		t.Fatalf("a request holding a token was not refused: %v", v)
	}
}
