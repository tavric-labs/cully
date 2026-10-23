package cully

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerRoutesAnalysisAndScoutThroughCodex(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("PATH", root)
	script := `#!/bin/sh
prompt=$(/bin/cat)
for arg in "$@"; do
  if [ "$arg" = 'web_search="live"' ]; then
    printf 'SOURCE|RESEARCH|checked\n🔌 Audit the official integration — https://example.org/tool\n'
    exit 0
  fi
done
case "$prompt" in *'CURRENT PUBLIC RESEARCH (untrusted evidence'*)
  printf 'SOURCE|MEMORY|checked\nSOURCE|PATTERNS|skipped\nCAUT|🔍 Narrow repeated searches.\nADV|🔌 Review the official integration for these checks https://example.org/tool\n'; exit 0;; esac
printf 'SOURCE|MEMORY|checked\nCAUT|🔍 Narrow repeated searches.\nTOOLGAP: browser checks || aggregate searches and no browser integration || official browser integration\n'
`
	if err := os.WriteFile(filepath.Join(root, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	session := "adapter-test"
	if err := registerPane(session, root); err != nil {
		t.Fatal(err)
	}
	path := sessionSignalsFile(session)
	if err := os.WriteFile(path, []byte("agent=codex\nsearches=20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	RunWorker(path, session, root)
	snap := readSnapshot(session)
	if !snap.AdvisorOK || snap.AdvisorAgent != "codex" || snap.AdvisorMemory != "checked" || snap.AdvisorResearch != "checked" {
		t.Fatalf("%+v", snap)
	}
	lines := strings.Join(readSuggestions(session), "\n")
	if !strings.Contains(lines, "Narrow repeated searches") || !strings.Contains(lines, "https://example.org/tool") || strings.Contains(lines, "SOURCE|") {
		t.Fatal(lines)
	}
}

func TestWorkerDoesNotSuggestToolWithoutConfirmedResearch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("PATH", root)
	script := `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = 'web_search="live"' ]; then
    printf 'SOURCE|RESEARCH|unavailable\n🔌 Audit an unverified tool — https://example.org/tool\n'
    exit 0
  fi
done
printf 'SOURCE|MEMORY|checked\nCAUT|🔍 Narrow repeated searches.\nTOOLGAP: browser checks || repeated manual checks || browser integration\n'
`
	if err := os.WriteFile(filepath.Join(root, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	const session = "unavailable-research"
	if err := registerPane(session, root); err != nil {
		t.Fatal(err)
	}
	path := sessionSignalsFile(session)
	if err := os.WriteFile(path, []byte("agent=codex\nsearches=20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	RunWorker(path, session, root)
	if got := readSnapshot(session).AdvisorResearch; got != "unavailable" {
		t.Fatalf("research state = %q", got)
	}
	lines := strings.Join(readSuggestions(session), "\n")
	if !strings.Contains(lines, "Narrow repeated searches") || strings.Contains(lines, "unverified tool") {
		t.Fatalf("unconfirmed tool suggestion shown: %s", lines)
	}
}

func TestWorkerStartupFailureDoesNotBlameMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CODEX_HOME", root)
	t.Setenv("PATH", root)
	script := "#!/bin/sh\nprintf 'Error loading MCP configuration: missing field command\\n' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(root, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	session := "startup-failure"
	if err := registerPane(session, root); err != nil {
		t.Fatal(err)
	}
	path := sessionSignalsFile(session)
	if err := os.WriteFile(path, []byte("agent=codex\nsearches=3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	RunWorker(path, session, root)
	snap := readSnapshot(session)
	if snap.AdvisorOK || snap.AdvisorMemory != "not run" || snap.AdvisorFailure != "invalid MCP configuration" {
		t.Fatalf("startup failure misreported: %+v", snap)
	}
}

func TestAdvisorStartupDiagnosticsDoNotExposeStderr(t *testing.T) {
	err := &exec.ExitError{Stderr: []byte("Error loading configuration: MCP server missing field command; private-value-must-not-escape")}
	if got := advisorFailureReason(err); got != "invalid MCP configuration" {
		t.Fatal(got)
	}
	wrapper := &advisorProcessError{Agent: "codex", Reason: advisorFailureReason(err), Cause: err}
	if strings.Contains(wrapper.Error(), "private-value") || !strings.Contains(wrapper.Error(), "invalid MCP configuration") {
		t.Fatal(wrapper.Error())
	}
}

func TestCursorStopQueuesCoarseAgentSignals(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("MODEL_HINT_GUARD", "")
	if err := os.WriteFile(daemonPIDFile(), []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for i := 1; i <= 12; i++ {
		input := fmt.Sprintf(`{"cwd":%q,"conversation_id":"opaque-input","generation_id":"turn-%d","status":"completed","loop_count":0,"text":"private-transcript-must-not-pass"}`, root, i)
		RunContinuityHook("cursor", "stop", strings.NewReader(input), &output)
		if i < 12 {
			if _, _, ok := nextAdvisorJob(nil); ok {
				t.Fatal("Cursor dispatched before twelve turns")
			}
		}
	}
	job, _, ok := nextAdvisorJob(nil)
	if !ok {
		t.Fatal("Cursor did not dispatch")
	}
	sig, err := os.ReadFile(job.SignalsPath)
	if err != nil || !strings.Contains(string(sig), "agent=cursor\n") || !strings.Contains(string(sig), "verification=unknown") || !strings.Contains(string(sig), "cadence_source=completed_turns") || strings.Contains(string(sig), "private-transcript-must-not-pass") {
		t.Fatal(string(sig), err)
	}
}

func TestAdvisorHostSelectionAndCapabilities(t *testing.T) {
	if advisorAgent("turns=3") != "claude" {
		t.Fatal("legacy hook changed")
	}
	for _, host := range []string{"claude", "codex", "cursor"} {
		if advisorAgent("agent="+host+"\ntools=7") != host {
			t.Fatal(host)
		}
		name, args, err := advisorCommand(host, false)
		if err != nil || name == "" {
			t.Fatal(host, err)
		}
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "resume") || strings.Contains(joined, "--force") || strings.Contains(joined, "dangerously") {
			t.Fatal("worker may attach or bypass permission", joined)
		}
		switch host {
		case "codex":
			if !strings.Contains(joined, "--ephemeral") || !strings.Contains(joined, "read-only") || strings.Contains(joined, "cully_log") || strings.Contains(joined, "mcp_servers.cully.") {
				t.Fatal(joined)
			}
		case "cursor":
			if !strings.Contains(joined, "--mode ask") {
				t.Fatal(joined)
			}
		case "claude":
			if !strings.Contains(joined, "--tools ToolSearch") || !strings.Contains(joined, "mcp__cully__cully_context") || strings.Contains(joined, "WebSearch") {
				t.Fatal(joined)
			}
		}
	}
	if _, _, err := advisorCommand("other", false); err == nil {
		t.Fatal("unsupported adapter silently accepted")
	}
}

func TestAdvisorChildUsesHostAndDropsForegroundBinding(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", root)
	t.Setenv("PATH", root)
	t.Setenv("CULLY_PANE_SESSION", "foreground")
	t.Setenv("CULLY_SESSION", "foreground")
	for _, host := range []string{"codex", "cursor"} {
		name := host
		if host == "cursor" {
			name = "cursor-agent"
		}
		script := "#!/bin/sh\n[ -z \"$CULLY_PANE_SESSION\" ] || exit 10\n[ -z \"$CULLY_SESSION\" ] || exit 11\n[ \"$MODEL_HINT_GUARD\" = 1 ] || exit 12\n[ \"$CULLY_AGENT\" = " + host + " ] || exit 13\n[ \"$PWD\" = \"" + root + "\" ] || exit 14\nprintf 'ADV|use the configured host\\n'\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		out, err := runAdvisorAgent(host, root, false, "bounded signals")
		if err != nil || !strings.HasPrefix(out, "ADV|") {
			t.Fatal(host, out, err)
		}
	}
}

func TestAdvisorScopesConfiguredIntegrations(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CODEX_HOME", root)
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"cully":{"command":"memory-client"},"unrelated":{"command":"other-client"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := claudeAdvisorMCP(root, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "memory-client") || strings.Contains(string(data), "other-client") {
		t.Fatal("unrelated integration reached worker")
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temporary MCP config was retained")
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[mcp_servers.cully]\nurl=\"https://example.org/cully\"\n[mcp_servers.\"other-server\"]\ncommand=\"unrelated\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(advisorMCPArgs(root, false, true), " ")
	if !strings.Contains(args, `mcp_servers.other-server.enabled=false`) || strings.Contains(args, "cully.enabled=false") || strings.Contains(args, `"other-server"`) {
		t.Fatal(args)
	}
}

func TestAdvisorScopesOnlyRealCullyRegistrations(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[mcp_servers.cully-adapter-test]\ncommand=\"adapter\"\n[mcp_servers.mcp-runtime-cully]\nurl=\"https://example.org/cully/mcp\"\n[mcp_servers.other]\ncommand=\"other\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	analysis := strings.Join(advisorMCPArgs(root, false, true), " ")
	for _, name := range []string{"cully-adapter-test", "mcp-runtime-cully"} {
		if !strings.Contains(analysis, "mcp_servers."+name+".enabled_tools=") {
			t.Fatal(analysis)
		}
	}
	if strings.Contains(analysis, "mcp_servers.cully.") || strings.Contains(analysis, "cully_optimization_decide") || strings.Contains(analysis, "cully_log") {
		t.Fatal("synthetic server or broad write access", analysis)
	}
	for _, name := range []string{"cully-adapter-test", "mcp-runtime-cully", "other"} {
		if !strings.Contains(strings.Join(advisorMCPArgs(root, true, false), " "), "mcp_servers."+name+".enabled=false") {
			t.Fatal("public scout can access a private MCP")
		}
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	if len(advisorMCPArgs(t.TempDir(), false, true)) != 0 {
		t.Fatal("missing configuration created a synthetic MCP server")
	}
}

func TestExtendedAdviceDoesNotCrowdNativeStatusline(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var lines []string
	for i := 0; i < maxReportLines; i++ {
		lines = append(lines, fmt.Sprintf("ADV|Suggestion %d", i))
	}
	if err := writeReportLines("many", "/project", lines); err != nil {
		t.Fatal(err)
	}
	if len(readSuggestions("many")) != 4 || len(readSuggestionsLimit("many", maxReportLines)) != 12 {
		t.Fatal("wrong native/full report limits")
	}
	if err := os.WriteFile(sessionReportFile("other"), []byte(`{"session":"many","lines":["ADV|wrong session"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readSuggestions("other"); len(got) != 0 {
		t.Fatal("foreign report accepted", got)
	}
}

func TestAdvisorSourceMarkersRemainHonest(t *testing.T) {
	for _, tc := range []struct {
		out    string
		failed bool
		want   string
	}{
		{"ADV|a useful suggestion", false, "unconfirmed"},
		{"SOURCE|MEMORY|checked", false, "checked"},
		{"SOURCE|MEMORY|skipped", false, "skipped"},
		{"SOURCE|MEMORY|checked", true, "unavailable"},
		{"SOURCE|MEMORY|invented", false, "unconfirmed"},
	} {
		if got := advisorSourceState(tc.out, "MEMORY", tc.failed); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}

// Opt-in smoke check uses the user's existing CLI/MCP login; normal tests are
// fully offline. Only bounded workflow findings are printed, never note bodies.
func TestLiveAdvisorRecall(t *testing.T) {
	host := os.Getenv("CULLY_TEST_LIVE_ADVISOR")
	if host == "" {
		t.Skip("set CULLY_TEST_LIVE_ADVISOR to a configured host")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	prompt := sharedAdvisorInstructions + "\nACTIVE AGENT: " + host + "\nSIGNALS:\nagent=" + host + "\nsession_ref=codex-0123456789abcdef\nsearches=14 edits=3 checks=0 tool_errors=2\ntask_intent=advisor workflow verification\nDo not emit TOOLGAP in this smoke check. Use one memory query about advisor workflow verification. Do not save any test observations or proposals. If tools are unavailable append exactly one diagnostic: DIAGNOSTIC|tools_missing, DIAGNOSTIC|tool_failed, or DIAGNOSTIC|policy_blocked. Never include note bodies or credentials in diagnostics."
	out, err := runAdvisorAgent(host, cwd, false, prompt)
	if err != nil {
		t.Fatalf("%s adapter failed: %v", host, err)
	}
	state := advisorSourceState(out, "MEMORY", false)
	t.Logf("%s adapter returned; memory=%s; advice_lines=%d", host, state, len(advisorLines(out, 3)))
	for _, reason := range []string{"tools_missing", "tool_failed", "policy_blocked"} {
		if strings.Contains(out, "DIAGNOSTIC|"+reason) {
			t.Logf("diagnostic: %s", reason)
		}
	}
	if state != "checked" {
		t.Fatalf("live recall not confirmed (%s)", state)
	}
}

func TestLiveAdvisorConnection(t *testing.T) {
	if os.Getenv("CULLY_TEST_LIVE_ADVISOR") != "codex" {
		t.Skip("set CULLY_TEST_LIVE_ADVISOR=codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	health, auth, found := probeCodexMCP(ctx)
	t.Logf("Cully connection: found=%t health=%s auth=%s", found, health, auth)
	if !found || health != "healthy" {
		t.Fatal("Cully tools were not discovered through the configured Codex connection")
	}
}

func TestLiveAdvisorResearch(t *testing.T) {
	host := os.Getenv("CULLY_TEST_LIVE_ADVISOR")
	if host == "" {
		t.Skip("set CULLY_TEST_LIVE_ADVISOR to a configured host")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildScoutPrompt(toolGap{Need: "official documentation for isolated read-only advisor execution", Evidence: "advisor needs an isolated worker without reopening the user's coding thread", Query: "official Codex exec ephemeral MCP enabled_tools"}, "agent="+host+"\nrepo_lang=Go\n")
	out, err := runAdvisorAgent(host, cwd, true, prompt)
	if err != nil {
		t.Fatalf("%s research adapter failed: %v", host, err)
	}
	state := advisorSourceState(out, "RESEARCH", false)
	t.Logf("%s research adapter returned; research=%s; findings=%d", host, state, len(emojiLines(out, 1)))
	if state != "checked" {
		t.Fatalf("live research not confirmed (%s)", state)
	}
}

func TestAdvisorInstructionsPinTokenEvidenceRule(t *testing.T) {
	flat := strings.Join(strings.Fields(sharedAdvisorInstructions), " ")
	for _, tc := range []struct{ name, want string }{
		{"figures in the same line", "every token-spend line (WARN, CAUT or ADV) must put the figures from the signals in that same line"},
		{"signal sources", "big_results(N tok), full_file_reads, big_bash or top_output_tool"},
		{"memo fallback", "emit the MEMO line and write no token lesson or TOOLGAP"},
		{"toolgap evidence", "A TOOLGAP for token waste must carry those figures in its evidence field"},
		{"research trigger", "only a TOOLGAP with non-empty evidence starts the web research step"},
	} {
		if !strings.Contains(flat, tc.want) {
			t.Errorf("%s: advisor instructions lost %q", tc.name, tc.want)
		}
	}
}
