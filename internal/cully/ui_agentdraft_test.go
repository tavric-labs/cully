package cully

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestScheduleForms(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "hourly": time.Hour, "daily": 24 * time.Hour, "weekdays": 24 * time.Hour, "every 30m": 30 * time.Minute, "Every 6h": 6 * time.Hour} {
		if d, ok := uiScheduleInterval(in); !ok || d != want {
			t.Errorf("%q: got %v %v, want %v", in, d, ok, want)
		}
	}
	for _, bad := range []string{"every 5m", "every 0h", "every 200h", "weekly", "every 30s", "cron * * * * *"} {
		if _, ok := uiScheduleInterval(bad); ok {
			t.Errorf("%q should be refused", bad)
		}
	}
	sat := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	if !uiScheduleSkips("weekdays", sat) || uiScheduleSkips("daily", sat) || uiScheduleSkips("weekdays", sat.AddDate(0, 0, 2)) {
		t.Error("weekdays must skip the weekend only")
	}
}

func TestParseAgentDraft(t *testing.T) {
	d, err := uiParseAgentDraft("noise\nAGENT|PR watcher|Watches open pull requests.|Report which need a person. Never merge.|read, web, bogus|every 2h\n")
	if err != nil || d.Name != "PR watcher" || d.Schedule != "every 2h" || !d.Groups["read"] || !d.Groups["web"] || d.Groups["bogus"] || d.Mode != "plan" {
		t.Fatalf("got %+v %v", d, err)
	}
	d, _ = uiParseAgentDraft("AGENT|Fixer|Fixes lint.|Edit files.|edit,commands|whenever")
	if d.Schedule != "" || d.Mode != "edit" {
		t.Errorf("unknown schedule should become on demand and edit tools mean edit mode: %+v", d)
	}
	if _, err := uiParseAgentDraft("I cannot do that"); err == nil {
		t.Error("a reply with no agent line must fail")
	}
}

func TestAgentMarkdownCarriesNoNotesOrPaths(t *testing.T) {
	a := uiIdentity{Name: "Test guardian", Role: "Runs tests.", Instructions: "Run the suite.", Mode: "plan", Schedule: "daily", Folder: "/secret/path", Memory: "private note", Groups: map[string]bool{"read": true, "commands": true}}
	md := uiAgentMarkdown(a)
	if strings.Contains(md, "/secret/path") || strings.Contains(md, "private note") || !strings.Contains(md, `name: "Test guardian"`) || !strings.Contains(md, "tools: [read, commands]") || !strings.HasSuffix(md, "Run the suite.\n") {
		t.Errorf("bad export:\n%s", md)
	}
}

func TestDiagramFrameIsSandboxedAndPageStaysStrict(t *testing.T) {
	srv, _, _ := uiTestServer(t, false)
	get := func(path string) *http.Response {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	f := get("/diagram/frame.html")
	if f.StatusCode != 200 || f.Header.Get("Content-Security-Policy") != uiDiagramCSP {
		t.Fatalf("frame: %d %q", f.StatusCode, f.Header.Get("Content-Security-Policy"))
	}
	if strings.Contains(uiDiagramCSP, "connect-src") || strings.Contains(uiDiagramCSP, "unsafe-eval") || !strings.Contains(uiDiagramCSP, "frame-ancestors 'self'") {
		t.Error("the diagram frame must have no network and no eval, and only this site may frame it")
	}
	if m := get("/diagram/mermaid.min.js"); m.StatusCode != 200 {
		t.Errorf("mermaid.min.js: %d", m.StatusCode)
	}
	page := get("/")
	if csp := page.Header.Get("Content-Security-Policy"); csp != uiCSP || strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "frame-src 'self'") {
		t.Errorf("the page CSP must stay strict: %q", csp)
	}
}

func TestDiagramHintCanBeTurnedOff(t *testing.T) {
	t.Setenv("CULLY_DIAGRAMS", "")
	if !strings.Contains(uiDiagramHint(), "mermaid") {
		t.Error("hint should ask for mermaid")
	}
	t.Setenv("CULLY_DIAGRAMS", "0")
	if uiDiagramHint() != "" {
		t.Error("CULLY_DIAGRAMS=0 should turn the hint off")
	}
}
