package cully

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWizard(t *testing.T, o SetupWizardOptions, input string) (string, error) {
	t.Helper()
	if o.Repos == nil {
		o.Repos = testRepos
	}
	var out bytes.Buffer
	o.In, o.Out = strings.NewReader(input), &out
	err := RunSetupWizard(o)
	return out.String(), err
}

var testRepos []string

func TestWizardAcceptsSkipsQuitsAndAsksBeforeWriting(t *testing.T) {
	setupFixture(t)
	// Enter accepts the suggested test command, s skips lint, q stops; y approves.
	out, err := runWizard(t, SetupWizardOptions{Mode: "local"}, "\ns\nq\ny\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Step 1.", "Claude Code", "Based on: Found `make test`", "Approve these changes? [y/N]", "+ - Test command: make test", "Linked into: claude, codex, cursor", "Open them with: cully ui, then the Setup page", "Sessions on this machine: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	b, _ := os.ReadFile(workflowSkillPath())
	if !strings.Contains(string(b), "- Test command: make test") || strings.Contains(string(b), "Lint command") || strings.Contains(string(b), "Commit messages") {
		t.Fatalf("rules = %s", b)
	}
	if !SetupDone() {
		t.Fatal("setup is not done after the wizard")
	}
}

func TestWizardWritesNothingWithoutApproval(t *testing.T) {
	setupFixture(t)
	out, err := runWizard(t, SetupWizardOptions{Mode: "local"}, "\nq\nn\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing was written.") {
		t.Fatalf("out = %s", out)
	}
	if _, err := os.Stat(workflowSkillPath()); err == nil {
		t.Fatal("the rules were written without approval")
	}
	if _, err := os.Lstat(setupLinkDir(setupHarnessHomes()[0])); err == nil {
		t.Fatal("a link was made without approval")
	}
	// End of input behaves as q and as no.
	if _, err := runWizard(t, SetupWizardOptions{Mode: "local"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workflowSkillPath()); err == nil {
		t.Fatal("empty input wrote the rules")
	}
}

func TestWizardAsksTheModeExplainsItAndInstalls(t *testing.T) {
	setupFixture(t)
	endpoint, called := "", 0
	out, err := runWizard(t, SetupWizardOptions{Install: func(e string) error { endpoint, called = e, called+1; return nil }},
		"mcp\nnot a url\nhttp://mcp.example.com/mcp\nhttps://mcp.example.com/mcp\nq\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, SetupModeLocalText) || !strings.Contains(out, SetupModeMCPText) || !strings.Contains(out, "the preferred way if you can deploy it") {
		t.Fatalf("the two modes were not explained:\n%s", out)
	}
	if endpoint != "https://mcp.example.com/mcp" || called != 1 || setupMode() != "mcp" {
		t.Fatalf("endpoint %q called %d mode %q", endpoint, called, setupMode())
	}
	if strings.Count(out, "must use https") != 1 {
		t.Fatalf("plain http was not refused once:\n%s", out)
	}
}

func TestWizardAnswersFileAndNoInterview(t *testing.T) {
	setupFixture(t)
	f := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(f, []byte(`{"test_cmd":"make test","never_do":"push; deploy","secrets":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runWizard(t, SetupWizardOptions{Mode: "local", AnswersFile: f, Yes: true}, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "[y/N]") || strings.Contains(out, "Answer [") {
		t.Fatalf("a scripted run asked something:\n%s", out)
	}
	if b, _ := os.ReadFile(workflowSkillPath()); !strings.Contains(string(b), "- Never do without asking: push; deploy") {
		t.Fatalf("rules = %s", b)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`["x"]`), 0o600)
	if _, err := runWizard(t, SetupWizardOptions{Mode: "local", AnswersFile: bad}, ""); err == nil {
		t.Fatal("accepted a malformed answers file")
	}
	setupFixture(t)
	out, err = runWizard(t, SetupWizardOptions{Mode: "local", NoInterview: true}, "")
	if err != nil || !strings.Contains(out, "Skipped (--no-interview)") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !SetupDone() {
		t.Fatal("a skipped interview left setup unfinished")
	}
	if _, err := os.Stat(workflowSkillPath()); err == nil {
		t.Fatal("--no-interview wrote rules")
	}
}

func TestWizardNeverPrintsSecrets(t *testing.T) {
	setupFixture(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "tok-secret-9999")
	out, err := runWizard(t, SetupWizardOptions{Mode: "local"}, "\n\n\n\n\n\n\n\n\n\n\n\ny\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "tok-secret-9999") || strings.Contains(out, "NOT_SHOWN") {
		t.Fatalf("a secret reached the output:\n%s", out)
	}
}
