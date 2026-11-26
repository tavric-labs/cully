package cully

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// Fake login CLI: prints what `claude mcp login --no-browser` prints, then
// waits for a pasted redirect URL.
const fakeLogin = `#!/bin/bash
echo 'Starting authentication for "cully"…'
echo 'Visit this URL to authorize:'
echo '  https://auth.example.com/authorize?client_id=abc&state=s1'
echo
echo 'Waiting for authorization… (^C to cancel)'
echo 'Or paste the redirect URL here:'
IFS= read -r line
case "$line" in
  http://localhost:*code=*) echo "authenticated"; exit 0 ;;
  *) echo "bad redirect" >&2; exit 1 ;;
esac
`

func uiLoginSetup(t *testing.T, script string, canPaste bool) *uiMCP {
	t.Helper()
	bin := uiWriteScript(t, script)
	old := uiLoginCommand
	uiLoginCommand = func(agent string) (string, []string, bool, error) { return bin, nil, canPaste, nil }
	t.Cleanup(func() { uiLoginCommand = old })
	m := newUIMCP()
	t.Cleanup(m.stopAll)
	return m
}

func uiWaitLogin(t *testing.T, m *uiMCP, agent, state string) uiLoginView {
	t.Helper()
	var v uiLoginView
	uiWaitFor(t, "login state "+state, func() bool { v, _ = m.loginView(agent); return v.State == state })
	return v
}

func TestLoginCapturesURLAndAcceptsPaste(t *testing.T) {
	m := uiLoginSetup(t, fakeLogin, true)
	if _, err := m.startLogin("claude"); err != nil {
		t.Fatal(err)
	}
	v := uiWaitLogin(t, m, "claude", "awaiting")
	if v.URL != "https://auth.example.com/authorize?client_id=abc&state=s1" || !v.CanPaste {
		t.Fatalf("view = %+v", v)
	}
	if err := m.paste("claude", "http://localhost:43871/callback?code=SECRETCODE&state=s1"); err != nil {
		t.Fatal(err)
	}
	done := uiWaitLogin(t, m, "claude", "done")
	if strings.Contains(done.Message, "SECRETCODE") || strings.Contains(done.URL, "SECRETCODE") {
		t.Fatalf("pasted code leaked into the view: %+v", done)
	}
}

func TestPasteRejectsUnsafeInput(t *testing.T) {
	m := uiLoginSetup(t, fakeLogin, true)
	m.startLogin("claude")
	uiWaitLogin(t, m, "claude", "awaiting")
	for _, bad := range []string{
		"", "not a url", "https://evil.example.com/cb?code=1", "http://localhost:1/cb",
		"http://localhost:1/cb?code=1\nrm -rf /", "http://localhost:1/cb?code=1\r", "ftp://localhost/cb?code=1",
		"http://localhost:1/cb?code=" + strings.Repeat("a", 5000),
	} {
		if err := m.paste("claude", bad); err == nil {
			t.Fatalf("paste %.40q accepted", bad)
		}
	}
	if v, _ := m.loginView("claude"); v.State != "awaiting" {
		t.Fatalf("rejected pastes changed state to %q", v.State)
	}
}

func TestLoginFailureIsReportedWithoutSecrets(t *testing.T) {
	m := uiLoginSetup(t, fakeLogin, true)
	m.startLogin("claude")
	uiWaitLogin(t, m, "claude", "awaiting")
	// Passes our validation (it carries error=) but the CLI rejects it.
	if err := m.paste("claude", "http://localhost:1/cb?error=access_denied&state=SECRETSTATE"); err != nil {
		t.Fatal(err)
	}
	v := uiWaitLogin(t, m, "claude", "failed")
	if !strings.Contains(v.Message, "bad redirect") {
		t.Fatalf("message lacks the CLI's reason: %q", v.Message)
	}
	if strings.Contains(v.Message, "SECRETSTATE") || strings.Contains(v.Message, "localhost:1") {
		t.Fatalf("pasted URL echoed into the message: %q", v.Message)
	}
}

func TestCursorLoginHasNoPaste(t *testing.T) {
	m := uiLoginSetup(t, fakeLogin, false)
	v, err := m.startLogin("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if v.CanPaste {
		t.Fatal("cursor login claims it can take a pasted URL")
	}
	uiWaitLogin(t, m, "cursor", "awaiting")
	if err := m.paste("cursor", "http://localhost:1/cb?code=1"); err == nil {
		t.Fatal("paste accepted for an agent without a paste step")
	}
}

func TestOneLoginPerAgentAndCancel(t *testing.T) {
	m := uiLoginSetup(t, fakeLogin, true)
	m.startLogin("codex")
	first := uiWaitLogin(t, m, "codex", "awaiting")
	second, err := m.startLogin("codex")
	if err != nil || second.URL != first.URL || second.State != "awaiting" {
		t.Fatalf("second start did not reuse the running login: %+v, %v", second, err)
	}
	m.cancel("codex")
	uiWaitLogin(t, m, "codex", "cancelled")
}

func TestUnknownAgentRejected(t *testing.T) {
	m := newUIMCP()
	if _, err := m.startLogin("emacs"); err == nil {
		t.Fatal("unknown agent accepted")
	}
	if err := m.paste("emacs", "http://localhost:1/cb?code=1"); err == nil {
		t.Fatal("paste for unknown agent accepted")
	}
}

func TestMCPStatusMapsProbeResults(t *testing.T) {
	oldProbe, oldInstalled := agentMCPProbe, uiAgentInstalled
	t.Cleanup(func() { agentMCPProbe, uiAgentInstalled = oldProbe, oldInstalled })
	uiAgentInstalled = func(agent string) bool { return agent != "cursor" }
	var calls atomic.Int32
	agentMCPProbe = func(ctx context.Context, agent string) (string, string, bool) {
		calls.Add(1)
		switch agent {
		case "claude":
			return "unhealthy", "unauthenticated", true
		case "codex":
			return "healthy", "authenticated", true
		}
		return "", "", false
	}
	m := newUIMCP()
	got := map[string]string{}
	for _, a := range m.status(context.Background()) {
		got[a.Agent] = a.State
	}
	want := map[string]string{"claude": "needs-auth", "codex": "connected", "cursor": "not-installed"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("state[%s] = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	before := calls.Load()
	m.status(context.Background())
	if calls.Load() != before {
		t.Fatalf("status not cached: %d probes after %d", calls.Load(), before)
	}
}

func TestMCPStatusNotConfiguredAndFailed(t *testing.T) {
	oldProbe, oldInstalled := agentMCPProbe, uiAgentInstalled
	t.Cleanup(func() { agentMCPProbe, uiAgentInstalled = oldProbe, oldInstalled })
	uiAgentInstalled = func(string) bool { return true }
	agentMCPProbe = func(ctx context.Context, agent string) (string, string, bool) {
		if agent == "claude" {
			return "unhealthy", "unknown", true // failed, not an auth problem
		}
		return "", "", false
	}
	got := map[string]string{}
	for _, a := range newUIMCP().status(context.Background()) {
		got[a.Agent] = a.State
	}
	if got["claude"] != "unhealthy" || got["codex"] != "not-configured" {
		t.Fatalf("states = %v", got)
	}
}

func TestLoginProcessKilledOnStop(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := "#!/bin/bash\necho $$ > " + pidFile + "\necho 'Visit this URL to authorize:'\necho '  https://auth.example.com/a'\nsleep 300\n"
	m := uiLoginSetup(t, script, true)
	m.startLogin("claude")
	uiWaitLogin(t, m, "claude", "awaiting")
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	for _, c := range strings.TrimSpace(string(raw)) {
		pid = pid*10 + int(c-'0')
	}
	m.stopAll()
	uiWaitFor(t, "login process to die", func() bool { return syscall.Kill(pid, 0) != nil || uiZombie(pid) })
}

func uiZombie(pid int) bool {
	b, err := os.ReadFile("/proc/" + itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	i := strings.LastIndex(string(b), ") ")
	return i >= 0 && len(b) > i+2 && b[i+2] == 'Z'
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

func uiStubProbes(t *testing.T) {
	t.Helper()
	oldProbe, oldInstalled := agentMCPProbe, uiAgentInstalled
	t.Cleanup(func() { agentMCPProbe, uiAgentInstalled = oldProbe, oldInstalled })
	uiAgentInstalled = func(string) bool { return true }
	agentMCPProbe = func(ctx context.Context, agent string) (string, string, bool) {
		return "unhealthy", "unauthenticated", true
	}
}

func uiInUse(m *uiMCP) map[string]bool {
	got := map[string]bool{}
	for _, a := range m.status(context.Background()) {
		got[a.Agent] = a.InUse
	}
	return got
}

func TestInUseDefaultsToClaude(t *testing.T) {
	uiStubProbes(t)
	got := uiInUse(newUIMCP())
	if !got["claude"] || got["codex"] || got["cursor"] {
		t.Fatalf("inUse = %v, want only claude by default", got)
	}
}

func TestMarkInUseFocusesOnTheLaunchingAgent(t *testing.T) {
	uiStubProbes(t)
	m := newUIMCP()
	if err := m.markInUse("codex"); err != nil {
		t.Fatal(err)
	}
	got := uiInUse(m)
	if !got["codex"] || got["claude"] || got["cursor"] {
		t.Fatalf("inUse = %v, want only codex", got)
	}
	m.markInUse("claude")
	got = uiInUse(m)
	if !got["codex"] || !got["claude"] || got["cursor"] {
		t.Fatalf("inUse = %v, want codex and claude", got)
	}
	if err := m.markInUse("emacs"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestStartingAUIAgentMarksClaudeInUse(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.mcp.markInUse("codex")
	if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	uiStubProbes(t)
	got := uiInUse(m.mcp)
	if !got["claude"] || !got["codex"] {
		t.Fatalf("inUse = %v: a UI-spawned agent runs Claude, so Claude is in use", got)
	}
}
