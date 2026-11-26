package cully

import (
	"strings"
	"testing"
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
