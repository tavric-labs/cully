package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiSetHelp(t *testing.T, help string) {
	t.Helper()
	uiClaudeHelp = func() string { return help }
	uiFlagsMu.Lock()
	uiFlagsAt = time.Time{}
	uiFlagsMu.Unlock()
}

func TestCapabilitiesAreReadFromTheInstalledClaudeAndSayWhatIsMissing(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	saved := uiClaudeHelp
	t.Cleanup(func() { uiClaudeHelp = saved; uiFlagsMu.Lock(); uiFlagsAt = time.Time{}; uiFlagsMu.Unlock() })
	var all strings.Builder
	for _, n := range uiClaudeNeeds {
		all.WriteString("  " + n.Flag + " <value>   something\n")
	}
	uiSetHelp(t, all.String())
	c := m.capabilities()
	if !c.Flags || len(c.Missing) != 0 {
		t.Fatalf("a Claude with every flag: %+v", c)
	}
	for _, x := range c.List {
		if x.ID == "permission" && x.Claude != "yes" {
			t.Fatalf("permission = %+v", x)
		}
	}
	// A release that drops the permission flag turns that capability off, and says which flag.
	uiSetHelp(t, strings.ReplaceAll(all.String(), "--permission-prompt-tool", "--something-else"))
	c = m.capabilities()
	if len(c.Missing) != 1 || !strings.HasPrefix(c.Missing[0], "--permission-prompt-tool") {
		t.Fatalf("missing = %v", c.Missing)
	}
	for _, x := range c.List {
		if x.ID == "permission" && x.Claude != "no" {
			t.Fatalf("permission should be off: %+v", x)
		}
		if x.ID == "resume" && x.Claude != "yes" {
			t.Fatalf("an unrelated capability was switched off: %+v", x)
		}
	}
	// If the help cannot be read, nothing is switched off.
	uiSetHelp(t, "")
	if c = m.capabilities(); c.Flags || len(c.Missing) != 0 || !uiClaudeHas("--permission-prompt-tool") {
		t.Fatalf("unreadable help: %+v", c)
	}
}

func TestATaskDoesNotPassAFlagTheInstalledClaudeNoLongerHas(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	uiSetHelp(t, "--input-format --output-format --resume --allowedTools --disallowedTools")
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	if strings.Contains(string(args), "--permission-prompt-tool") || strings.Contains(string(args), "--settings") {
		t.Fatalf("a missing flag was passed: %s", args)
	}
	if !strings.Contains(string(args), "git push") { // so the defaults stay blocks
		t.Fatalf("without the card, the default blocks must stay: %s", args)
	}
	said := false
	for _, e := range a.allEvents() {
		said = said || (e.Kind == "note" && strings.Contains(e.Text, "cannot ask you"))
	}
	if !said {
		t.Fatal("the chat did not say why risky commands stay blocked")
	}
}
