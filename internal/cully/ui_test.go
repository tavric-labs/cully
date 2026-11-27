package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseUIFlagsRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{
		{"--addr"},
		{"--bogus"},
		{"--tls-cert", "c.pem"},
		{"--max-agents", "0"},
		{"url", "extra"},
		{"extra-positional"},
	} {
		if _, err := parseUIFlags(args); err == nil {
			t.Fatalf("args %v accepted", args)
		}
	}
}

func TestParseUIFlagsDefaultsAndRepeatedRepo(t *testing.T) {
	o, err := parseUIFlags([]string{"--repo", "/a", "--repo", "/b", "--max-agents", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Addr != "127.0.0.1:7777" || len(o.Repos) != 2 || o.MaxAgents != 2 {
		t.Fatalf("options = %+v", o)
	}
	d, _ := parseUIFlags(nil)
	if len(d.Repos) != 1 {
		t.Fatalf("default repo list = %v", d.Repos)
	}
}

func TestUITokenFileCreated0600AndReused(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	first, err := loadOrCreateUIToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 40 {
		t.Fatalf("token too short: %d", len(first))
	}
	path := filepath.Join(cullyDir(), "ui-token")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", info.Mode().Perm())
	}
	second, _ := loadOrCreateUIToken()
	if first != second {
		t.Fatal("token changed between runs")
	}
}

func TestNonLoopbackWithoutTLSWarns(t *testing.T) {
	if w := uiExposureWarning(uiOptions{Addr: "0.0.0.0:7777"}); !strings.Contains(w, "plain HTTP") {
		t.Fatalf("warning = %q", w)
	}
	if w := uiExposureWarning(uiOptions{Addr: "0.0.0.0:7777", TLSCert: "c", TLSKey: "k"}); w != "" {
		t.Fatalf("warned despite TLS: %q", w)
	}
	if w := uiExposureWarning(uiOptions{Addr: "127.0.0.1:7777"}); w != "" {
		t.Fatalf("warned for loopback: %q", w)
	}
	if !uiLoopbackAddr("localhost:1") || uiLoopbackAddr("10.0.0.5:1") || uiLoopbackAddr(":7777") {
		t.Fatal("uiLoopbackAddr misclassified")
	}
}

func TestStaticAssetsEmbedded(t *testing.T) {
	for _, name := range []string{"app.js", "md.js", "palette.js", "setup.js"} {
		js, err := uiStaticFS.ReadFile("ui_static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
			if strings.Contains(string(js), banned) {
				t.Fatalf("%s uses %s", name, banned)
			}
		}
	}
	html, _ := uiStaticFS.ReadFile("ui_static/index.html")
	for _, want := range []string{"Tasks", "Board", "Dashboard", `src="/app.js"`, `src="/palette.js"`, `href="/style.css"`, `href="/polish.css"`} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("index.html lacks %q", want)
		}
	}
	if strings.Contains(string(html), "<script>") || strings.Contains(string(html), "onclick=") {
		t.Fatal("index.html has inline script, blocked by CSP")
	}
	css, _ := uiStaticFS.ReadFile("ui_static/style.css")
	if !strings.Contains(string(css), "#174c38") {
		t.Fatal("style.css does not use the Cully forest green")
	}
}

func TestThemesAreSelectableWithoutInlineScript(t *testing.T) {
	html, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(html), `src="/theme.js"`) {
		t.Fatal("index.html does not load theme.js before the page paints")
	}
	theme, err := uiStaticFS.ReadFile("ui_static/theme.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"localStorage", "data-theme", "harbor", "graphite", "try"} {
		if !strings.Contains(string(theme), want) {
			t.Fatalf("theme.js lacks %q", want)
		}
	}
	css, _ := uiStaticFS.ReadFile("ui_static/style.css")
	for _, want := range []string{`[data-theme="harbor"]`, `[data-theme="graphite"]`, "--serif"} {
		if !strings.Contains(string(css), want) {
			t.Fatalf("style.css lacks %q", want)
		}
	}
}

func TestStyleAvoidsGenericTells(t *testing.T) {
	for _, name := range []string{"style.css", "polish.css"} {
		css, err := uiStaticFS.ReadFile("ui_static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		s := string(css)
		if strings.Contains(s, "text-transform: uppercase") {
			t.Fatalf("%s uses ALL-CAPS labels", name)
		}
		if strings.Contains(s, "Inter,") || strings.Contains(s, "radial-gradient") {
			t.Fatalf("%s uses a default webfont stack or a gradient wash", name)
		}
		if strings.Contains(s, "translateY(-2px)") {
			t.Fatalf("%s lifts cards on hover", name)
		}
	}
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	if strings.Contains(string(js), " · ") {
		t.Fatal("app.js joins labels with middle dots")
	}
}

func TestParseUIFlagsQuiet(t *testing.T) {
	o, err := parseUIFlags([]string{"--quiet"})
	if err != nil || !o.Quiet {
		t.Fatalf("options = %+v, err = %v", o, err)
	}
	if d, _ := parseUIFlags(nil); d.Quiet {
		t.Fatal("quiet is on by default")
	}
}

func TestParseUIFlagsNoConflictAlerts(t *testing.T) {
	o, err := parseUIFlags([]string{"--no-conflict-alerts"})
	if err != nil || !o.NoConflictAlerts {
		t.Fatalf("options = %+v, err = %v", o, err)
	}
	if d, _ := parseUIFlags(nil); d.NoConflictAlerts {
		t.Fatal("alerts disabled by default")
	}
}

func TestParseUIFlagsAgents(t *testing.T) {
	o, err := parseUIFlags([]string{"--agent", "claude", "--agent", "codex"})
	if err != nil || len(o.Agents) != 2 || o.Agents[0] != "claude" || o.Agents[1] != "codex" {
		t.Fatalf("options = %+v, err = %v", o, err)
	}
	if _, err := parseUIFlags([]string{"--agent", "emacs"}); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestAppFocusesOnAgentsInUse(t *testing.T) {
	js, _ := uiStaticFS.ReadFile("ui_static/app.js")
	if !strings.Contains(string(js), "inUse") {
		t.Fatal("app.js ignores which agents are in use")
	}
}
