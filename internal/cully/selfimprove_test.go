package cully

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateSelfImprove(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("CULLY_EVAL_INTERVAL", "")
	return home
}

func failingSuites() []EvalSuite {
	return []EvalSuite{
		{Name: "setup", Checks: []EvalCheck{{Name: "hooks installed", Pass: true}, {Name: "skill linked", Pass: false, Detail: "missing token=secret12345678901234567890"}}},
		{Name: "advisor", Skipped: "no transcripts"},
	}
}

func TestSummarizeEvalRunRedactsAndCounts(t *testing.T) {
	run := summarizeEvalRun(time.Unix(100, 0), failingSuites())
	if run.Failed != 1 || run.Suites[0].Passed != 1 || len(run.Suites[0].Failing) != 1 || run.Suites[1].Skipped == "" {
		t.Fatalf("%+v", run)
	}
	if strings.Contains(run.Suites[0].Failing[0].Detail, "secret12345678901234567890") {
		t.Fatal("credential kept in history")
	}
}

func TestImprovementsLifecycle(t *testing.T) {
	failing := summarizeEvalRun(time.Unix(100, 0), failingSuites())
	list := updateImprovements(nil, failing)
	if len(list) != 1 || list[0].Status != "pending" || !strings.HasPrefix(list[0].ID, "setup-") {
		t.Fatalf("new failure should be proposed: %+v", list)
	}
	again := updateImprovements(list, summarizeEvalRun(time.Unix(200, 0), failingSuites()))
	if len(again) != 1 {
		t.Fatalf("same failure duplicated: %+v", again)
	}
	again[0].Status = "applied"
	// An applied fix that still fails is reopened; one that stops failing is resolved.
	reopened := updateImprovements(again, summarizeEvalRun(time.Unix(300, 0), failingSuites()))
	if reopened[0].Status != "pending" {
		t.Fatalf("failed fix not reopened: %+v", reopened[0])
	}
	reopened[0].Status = "applied"
	clean := summarizeEvalRun(time.Unix(400, 0), []EvalSuite{{Name: "setup", Checks: []EvalCheck{{Name: "hooks installed", Pass: true}}}})
	resolved := updateImprovements(reopened, clean)
	if resolved[0].Status != "resolved" {
		t.Fatalf("fixed failure not resolved: %+v", resolved[0])
	}
	resolved[0].Status = "dismissed"
	if kept := updateImprovements(resolved, summarizeEvalRun(time.Unix(500, 0), failingSuites())); kept[0].Status != "dismissed" {
		t.Fatalf("dismissed proposal came back: %+v", kept[0])
	}
	// A skipped suite says nothing about earlier failures.
	pending := updateImprovements(nil, failing)
	skipped := updateImprovements(pending, summarizeEvalRun(time.Unix(600, 0), []EvalSuite{{Name: "setup", Skipped: "no stack"}}))
	if skipped[0].Status != "pending" {
		t.Fatalf("skipped suite resolved a proposal: %+v", skipped[0])
	}
}

func TestSelfEvalScheduleAndInterval(t *testing.T) {
	isolateSelfImprove(t)
	if d, on := selfEvalInterval(); !on || d != 5*time.Hour {
		t.Fatalf("default interval %v %v", d, on)
	}
	t.Setenv("CULLY_EVAL_INTERVAL", "off")
	if _, on := selfEvalInterval(); on {
		t.Fatal("off ignored")
	}
	t.Setenv("CULLY_EVAL_INTERVAL", "1m")
	if d, _ := selfEvalInterval(); d != selfEvalMinInterval {
		t.Fatalf("interval below floor: %v", d)
	}
	now := time.Unix(1_000_000, 0)
	if !selfEvalDue(now, 5*time.Hour) {
		t.Fatal("first run should be due")
	}
	appendEvalHistory(evalRun{At: now.Add(-time.Hour)})
	if selfEvalDue(now, 5*time.Hour) {
		t.Fatal("ran an hour ago; not due")
	}
	if !selfEvalDue(now.Add(5*time.Hour), 5*time.Hour) {
		t.Fatal("five hours later should be due")
	}
}

func TestRunSelfEvalRecordsHistoryAndProposals(t *testing.T) {
	isolateSelfImprove(t)
	old := evalRunner
	defer func() { evalRunner = old }()
	evalRunner = func(context.Context) ([]EvalSuite, error) { return failingSuites(), nil }
	run, err := runSelfEval(context.Background(), time.Now())
	if err != nil || run.Failed != 1 {
		t.Fatalf("%+v %v", run, err)
	}
	if len(readEvalHistory(0)) != 1 || len(pendingImprovements()) != 1 {
		t.Fatal("history or proposals missing")
	}
	var out strings.Builder
	writeSelfImproveHint(&out)
	if !strings.Contains(out.String(), "cully improve") {
		t.Fatalf("no hint: %q", out.String())
	}
}

func TestApplyImprovementWritesOnlyRulesAndSkillAfterApproval(t *testing.T) {
	home := isolateSelfImprove(t)
	old := improvePlanner
	defer func() { improvePlanner = old }()
	improvePlanner = func(string) (string, error) {
		return `{"summary":"Guard skill links","claude_md_section":"Check the skill link after setup.","mcp_servers":{"x":{"command":"npx"}},"shell_commands":["rm -rf /"],"skill_name":"setup-check","skill_content":"# Setup check\nVerify links."}`, nil
	}
	p := improvement{ID: "setup-1234", Suite: "setup", Evidence: []evalFailure{{Check: "skill linked"}}, Status: "pending", Created: time.Now()}
	if err := writeImprovements([]improvement{p}); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	// Declining writes nothing.
	if err := applyImprovement(&out, strings.NewReader("n\n"), p, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sharedSkillPath(home, "setup-check")); !os.IsNotExist(err) {
		t.Fatal("wrote without approval")
	}
	// A dry run writes nothing either.
	if err := applyImprovement(&out, strings.NewReader(""), p, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sharedSkillPath(home, "setup-check")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote")
	}
	if err := applyImprovement(&out, strings.NewReader("y\n"), p, false, false); err != nil {
		t.Fatal(err)
	}
	skill, err := os.ReadFile(sharedSkillPath(home, "setup-check"))
	if err != nil || !strings.Contains(string(skill), "Verify links.") {
		t.Fatalf("skill not written: %v", err)
	}
	rules, _ := os.ReadFile(sharedSkillPath(home, "cully"))
	if !strings.Contains(string(rules), "Check the skill link") {
		t.Fatal("rules not written")
	}
	if _, err := os.Stat(filepath.Join(home, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatal("self-improvement added an MCP server")
	}
	if !strings.Contains(out.String(), "Ignored") {
		t.Fatalf("dropped actions not disclosed: %s", out.String())
	}
	if got := readImprovements(); got[0].Status != "applied" || got[0].Summary == "" {
		t.Fatalf("not marked applied: %+v", got[0])
	}
}

func TestSanitizeImprovePlanRefusesUnsafeContent(t *testing.T) {
	for name, plan := range map[string]applyPlan{
		"traversal":  {SkillName: "../evil", SkillContent: "x"},
		"credential": {ClaudeMDSection: "use token=secret12345678901234567890"},
		"huge":       {SkillName: "ok", SkillContent: strings.Repeat("a", improveContentMax+1)},
	} {
		if _, _, err := sanitizeImprovePlan(plan); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := writeSkill(t.TempDir(), "../evil", "x"); err == nil {
		t.Error("writeSkill accepted a traversal name")
	}
}

func TestRunImproveListsAndDismisses(t *testing.T) {
	isolateSelfImprove(t)
	old := evalRunner
	defer func() { evalRunner = old }()
	evalRunner = func(context.Context) ([]EvalSuite, error) { return failingSuites(), nil }
	var out strings.Builder
	if err := RunImprove(&out, strings.NewReader(""), []string{"--run"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Last eval", "Proposed improvements", "skill linked", "nothing changes until you approve"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
	if err := RunImprove(&out, strings.NewReader(""), []string{"--apply", "9"}); err == nil {
		t.Fatal("unknown proposal accepted")
	}
	if err := RunImprove(&out, strings.NewReader(""), []string{"--dismiss", "1"}); err != nil {
		t.Fatal(err)
	}
	if len(pendingImprovements()) != 0 {
		t.Fatal("dismiss kept the proposal pending")
	}
}
