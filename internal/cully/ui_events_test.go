package cully

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseAssistantText(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello <b>x</b>"}]}}`))
	if len(p.Events) != 1 || p.Events[0].Kind != "text" || p.Events[0].Text != "hello <b>x</b>" {
		t.Fatalf("events = %+v", p.Events)
	}
}

func TestParseToolUseClassifiesAndSummarises(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[
		{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}},
		{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/w/a.go"}},
		{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"/w/b.go"}}]}}`))
	if len(p.Events) != 3 {
		t.Fatalf("events = %+v", p.Events)
	}
	if p.Events[0].Kind != "tool" || p.Events[0].Tool != "Bash" || p.Events[0].Text != "go test ./..." {
		t.Fatalf("bash event = %+v", p.Events[0])
	}
	if p.Uses["t1"].Sig == "" || p.Uses["t2"].Path != "/w/a.go" {
		t.Fatalf("uses = %+v", p.Uses)
	}
	for id, want := range map[string]string{"t1": journalCheck, "t2": journalEdit, "t3": journalSearch} {
		u := p.Uses[id]
		if got := journalClassFor(u.Name, u.Command); got != want {
			t.Fatalf("class for %s = %q, want %q", id, got, want)
		}
	}
	if got := journalClassFor("Bash", "ls -la"); got != journalOther {
		t.Fatalf("plain bash class = %q", got)
	}
}

func TestParseToolResultFailure(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"boom"}]}}`))
	if len(p.Events) != 1 || p.Events[0].Kind != "tool_result" || !p.Events[0].Failed {
		t.Fatalf("events = %+v", p.Events)
	}
	if len(p.Results) != 1 || p.Results[0].ID != "t1" || !p.Results[0].Failed {
		t.Fatalf("results = %+v", p.Results)
	}
}

func TestParseResultEndsTurn(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`))
	if !p.TurnDone || len(p.Events) != 1 || p.Events[0].Kind != "result" {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestParseMalformedLine(t *testing.T) {
	p := parseStreamLine([]byte(`{not json`))
	if len(p.Events) != 1 || p.Events[0].Kind != "system" {
		t.Fatalf("events = %+v", p.Events)
	}
}

func TestParseTruncatesLongText(t *testing.T) {
	long := strings.Repeat("x", 5000)
	p := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"` + long + `"}]}}`))
	if n := len([]rune(p.Events[0].Text)); n > uiEventTextMax+1 || !strings.HasSuffix(p.Events[0].Text, "…") {
		t.Fatalf("text length %d, suffix %q", n, p.Events[0].Text[len(p.Events[0].Text)-3:])
	}
}

func TestParseInitReportsMCPServers(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"system","subtype":"init","mcp_servers":[{"name":"cully","status":"needs-auth"},{"name":"gitlab","status":"connected"}]}`))
	if len(p.MCP) != 2 || p.MCP[0].Name != "cully" || p.MCP[0].Status != "needs-auth" {
		t.Fatalf("mcp = %+v", p.MCP)
	}
	if !p.MCP[0].NeedsAuth() || p.MCP[1].NeedsAuth() {
		t.Fatalf("NeedsAuth wrong: %+v", p.MCP)
	}
	var warn bool
	for _, e := range p.Events {
		warn = warn || (e.Kind == "system" && strings.Contains(e.Text, "cully") && strings.Contains(e.Text, "authentication"))
	}
	if !warn {
		t.Fatalf("no visible note about cully needing authentication: %+v", p.Events)
	}
}

func TestParseInitWithoutMCPServers(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"system","subtype":"init"}`))
	if len(p.MCP) != 0 {
		t.Fatalf("mcp = %+v", p.MCP)
	}
}

func TestResultEventsReportTokenUseAndCacheShare(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"result","is_error":false,"result":"ok","usage":{"input_tokens":200,"cache_read_input_tokens":800,"cache_creation_input_tokens":0,"output_tokens":50}}`))
	if p.Usage == nil || p.Usage.Input != 200 || p.Usage.CacheRead != 800 || p.Usage.Output != 50 {
		t.Fatalf("usage = %+v", p.Usage)
	}
	if p.Usage.CachedShare() != 80 || p.Usage.Total() != 1050 {
		t.Fatalf("share = %d, total = %d", p.Usage.CachedShare(), p.Usage.Total())
	}
	if q := parseStreamLine([]byte(`{"type":"result","result":"ok"}`)); q.Usage != nil {
		t.Fatal("usage invented for a result that reported none")
	}
}

func TestTheModelAndContextSizeAreReadFromTheStream(t *testing.T) {
	if p := parseStreamLine([]byte(`{"type":"system","subtype":"init","model":"claude-opus-5-5[1m]"}`)); p.Model != "claude-opus-5-5[1m]" {
		t.Fatalf("model = %q", p.Model)
	}
	p := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"cache_read_input_tokens":150000,"cache_creation_input_tokens":5000,"output_tokens":3}}}`))
	if p.Context != 155010 {
		t.Fatalf("context = %d", p.Context)
	}
	c := contextOf("claude-sonnet-5-5", 190000, 0, 0)
	if c.Window != 200000 || c.Percent != 95 || c.Left != 10000 || !c.Near {
		t.Fatalf("context = %+v", c)
	}
	if c := contextOf("claude-opus-5-5[1m]", 170000, 0, 0); c.Window != 1000000 || c.Near {
		t.Fatalf("1m context = %+v", c)
	}
	if contextOf("x", 0, 0, 0) != nil {
		t.Fatal("a context was invented")
	}
}

func TestATaskShowsHowFullItsContextIs(t *testing.T) {
	body := uiFakeHead + `  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1000,"cache_read_input_tokens":170000,"cache_creation_input_tokens":0,"output_tokens":5}}}'
` + uiFakeTail
	m, repo, _ := uiTestManager(t, uiWriteScript(t, strings.Replace(body, `echo '{"type":"system","subtype":"init"}'`, `echo '{"type":"system","subtype":"init","model":"claude-sonnet-5-5"}'`, 1)), "tok-aaa1")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	c := a.snapshot().Context
	if c == nil || c.Used != 171000 || c.Percent != 85 || c.Near || c.Model != "claude-sonnet-5-5" {
		t.Fatalf("context = %+v", c)
	}
}

func TestLimitsWindowAndCostAreReadFromTheStream(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","rateLimitType":"seven_day","utilization":0.26,"unifiedWindows":{"five_hour":{"utilization":0.62,"resetsAt":1795701000,"status":"allowed"},"seven_day":{"utilization":0.26,"resetsAt":1796216400,"status":"allowed"}}}}`))
	if p.Limits == nil || p.Limits.FiveHour.Used != 62 || p.Limits.SevenDay.Used != 26 || p.Limits.FiveHour.ResetsAt != 1795701000 || p.Limits.Status != "allowed_warning" {
		t.Fatalf("limits = %+v", p.Limits)
	}
	r := parseStreamLine([]byte(`{"type":"result","result":"ok","total_cost_usd":0.0031,"modelUsage":{"claude-sonnet-5-5":{"contextWindow":1000000,"inputTokens":2}}}`))
	if r.Window != 1000000 || r.Cost != 0.0031 {
		t.Fatalf("window = %d cost = %v", r.Window, r.Cost)
	}
	if c := contextOf("claude-sonnet-5-5", 170000, 1000000, 0); c.Window != 1000000 || c.Percent != 17 || c.Near {
		t.Fatalf("reported window ignored: %+v", c)
	}
}

func TestACompactionIsCountedAndAHandoffIsOnlySuggestedWhenItMatters(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"auto"}}`))
	if !p.Compacted || len(p.Events) == 0 {
		t.Fatalf("parsed = %+v", p)
	}
	if c := contextOf("claude-sonnet-5-5", 170000, 200000, 0); c.Near {
		t.Fatal("85 percent was called nearly full, but the harness compacts on its own")
	}
	if c := contextOf("claude-sonnet-5-5", 190000, 200000, 2); !c.Near || c.Compactions != 2 {
		t.Fatalf("context = %+v", c)
	}
}

func TestAWindowWithNoFigureIsUnknownNotZeroAndAnEarlierFigureIsKept(t *testing.T) {
	soon := time.Now().Add(2 * time.Hour).Unix()
	none := parseStreamLine([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"resetsAt":` + uiItoa64(soon) + `,"status":"allowed"}}}}`))
	if none.Limits == nil || none.Limits.FiveHour.Known || none.Limits.FiveHour.Used != 0 {
		t.Fatalf("a report with no figure was read as a real zero: %+v", none.Limits)
	}
	old := uiLimits{FiveHour: uiWindow{Used: 62, Known: true, ResetsAt: soon}, SevenDay: uiWindow{Used: 26, Known: true, ResetsAt: soon + 86400}}
	got := uiMergeLimits(old, *none.Limits)
	if !got.FiveHour.Known || got.FiveHour.Used != 62 {
		t.Fatalf("the earlier figure was lost: %+v", got.FiveHour)
	}
	// Once the window has reset, an old figure no longer applies.
	past := uiLimits{FiveHour: uiWindow{Used: 90, Known: true, ResetsAt: time.Now().Add(-time.Hour).Unix()}}
	if got := uiMergeLimits(past, *none.Limits); got.FiveHour.Known {
		t.Fatalf("a figure from a window that has reset was kept: %+v", got.FiveHour)
	}
}

func TestRefreshingLimitsAsksEachAccountAndTheNewestWordWins(t *testing.T) {
	soon := time.Now().Add(3 * time.Hour).Unix()
	body := "#!/bin/sh\necho '{\"type\":\"rate_limit_event\",\"rate_limit_info\":{\"status\":\"allowed\",\"unifiedWindows\":{\"five_hour\":{\"utilization\":0.31,\"resetsAt\":" + uiItoa64(soon) + ",\"status\":\"allowed\"},\"seven_day\":{\"utilization\":0.12,\"resetsAt\":" + uiItoa64(soon+86400) + ",\"status\":\"allowed\"}}}}'\necho '{\"type\":\"result\",\"result\":\"OK\"}'\n"
	m, _, _ := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1", "tok-bbb2")
	n, err := m.refreshLimits()
	if err != nil || n != 2 {
		t.Fatalf("heard %d, %v", n, err)
	}
	all := m.limitsAll()
	if len(all.Claude) != 2 || all.Claude[0].Limits.FiveHour.Used != 31 || !all.Claude[0].Limits.FiveHour.Known {
		t.Fatalf("limits = %+v", all.Claude)
	}
	// A task that heard older limits at its start shows the newer account figure.
	a := &uiAgent{token: 1, mgr: m, limits: &uiLimits{FiveHour: uiWindow{Used: 5, Known: true}, Seen: 1}}
	a.mu.Lock()
	v := a.limitsView()
	a.mu.Unlock()
	if v == nil || v.FiveHour.Used != 31 {
		t.Fatalf("limitsView = %+v", v)
	}
}

func uiItoa64(n int64) string { return strconv.FormatInt(n, 10) }

func TestAnAgentsTodoListBecomesAPlanEventNotAToolCall(t *testing.T) {
	p := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"TodoWrite","input":{"todos":[{"content":"Add the endpoint","status":"completed"},{"content":"Write tests","status":"in_progress"},{"content":"  ","status":"pending"},{"content":"Update docs","status":"weird"}]}}]}}`))
	if len(p.Events) != 1 || p.Events[0].Kind != "plan" {
		t.Fatalf("events = %+v", p.Events)
	}
	want := `[{"c":"Add the endpoint","s":"completed"},{"c":"Write tests","s":"in_progress"},{"c":"Update docs","s":"pending"}]`
	if p.Events[0].Text != want {
		t.Fatalf("plan = %s", p.Events[0].Text)
	}
	if bad := parseStreamLine([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"TodoWrite","input":{"todos":[]}}]}}`)); len(bad.Events) != 1 || bad.Events[0].Kind != "tool" {
		t.Fatalf("an empty list should fall back to a tool row: %+v", bad.Events)
	}
}
