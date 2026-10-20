package cully

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateWorkflow(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, "cursor"))
}

func TestWorkflowOnboardingConsentPrivacyAndAdvisorContext(t *testing.T) {
	isolateWorkflow(t)
	if err := os.MkdirAll(CodexConfigDir(), 0700); err != nil {
		t.Fatal(err)
	}
	answers := "yes\nBackend and ops\n3: api, web, cli\ngo test; PR review\nstaging-vm and prod-cluster\nstaging: read via ssh alias; prod: read only; writes need approval\nKeep advice concise\ny\n"
	var out bytes.Buffer
	if err := RunWorkflowOnboarding(strings.NewReader(answers), &out, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(workflowSkillPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: dev-workflow-rules", "3: api, web, cli", "prod: read only", "grants no access"} {
		if !strings.Contains(string(data)+out.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
	info, _ := os.Stat(workflowSkillPath())
	if info.Mode().Perm() != 0600 {
		t.Fatalf("skill permissions %v", info.Mode())
	}
	linked, err := os.ReadFile(filepath.Join(CodexConfigDir(), "skills", "dev-workflow-rules", "SKILL.md"))
	if err != nil || string(linked) != string(data) {
		t.Fatalf("agent skill: %v", err)
	}
	if !strings.Contains(workflowAdvisorContext(), "staging-vm") {
		t.Fatal("advisor missed workflow")
	}
	if err := RunWorkflowOnboarding(strings.NewReader("yes\nreplacement"), &out, nil); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(workflowSkillPath())
	if string(again) != string(data) {
		t.Fatal("overwrote user skill")
	}
	// An integration configured after onboarding must also discover the skill.
	if err := installMemorySkill("cursor"); err != nil {
		t.Fatal(err)
	}
	late, err := os.ReadFile(filepath.Join(CursorConfigDir(), "skills", "dev-workflow-rules", "SKILL.md"))
	if err != nil || string(late) != string(data) {
		t.Fatalf("late agent integration missed workflow skill: %v", err)
	}
}

func TestWorkflowSkipCancelAndNonInteractive(t *testing.T) {
	for _, input := range []string{"n\n", "y\nrole\n", "y\nrole\nrepos\ntests\nnone\nread only\nquiet\nn\n", ""} {
		t.Run(input, func(t *testing.T) {
			isolateWorkflow(t)
			var out bytes.Buffer
			if err := RunWorkflowOnboarding(strings.NewReader(input), &out, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(workflowSkillPath()); !os.IsNotExist(err) {
				t.Fatal("saved without confirmation")
			}
		})
	}
	isolateWorkflow(t)
	input, _ := os.CreateTemp(t.TempDir(), "input")
	defer input.Close()
	output, _ := os.CreateTemp(t.TempDir(), "output")
	defer output.Close()
	if err := MaybeWorkflowOnboarding(input, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workflowOnboardingState()); !os.IsNotExist(err) {
		t.Fatal("non-TTY invocation changed onboarding")
	}
	if err := RunWorkflowOnboarding(strings.NewReader(""), output, []string{"--skip"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(workflowOnboardingState())
	if string(data) != "skipped\n" {
		t.Fatal("skip not remembered")
	}
}

func TestWorkflowExistingSkillAndBoundedRedactedContext(t *testing.T) {
	isolateWorkflow(t)
	path := filepath.Join(CodexConfigDir(), "skills", "dev-workflow-rules", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("existing rules token=secret12345678901234567890"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunWorkflowOnboarding(strings.NewReader("y\n"), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "existing") {
		t.Fatal(out.String())
	}
	context := workflowAdvisorContext()
	if !strings.Contains(context, "existing rules") || strings.Contains(context, "secret12345678901234567890") {
		t.Fatal("missing context or unredacted credential")
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 13<<10), 0600); err != nil {
		t.Fatal(err)
	}
	if workflowAdvisorContext() != "" {
		t.Fatal("unbounded context")
	}
}

func TestWorkflowRefusesLikelyCredentialsBeforeSaving(t *testing.T) {
	isolateWorkflow(t)
	var out bytes.Buffer
	if err := RunWorkflowOnboarding(strings.NewReader("y\ntoken=secret12345678901234567890\n"), &out, nil); err == nil {
		t.Fatal("accepted credential")
	}
	if _, err := os.Stat(workflowSkillPath()); !os.IsNotExist(err) {
		t.Fatal("saved credential")
	}
}

func TestWorkflowRejectsOversizedAnswerWithoutSaving(t *testing.T) {
	isolateWorkflow(t)
	var out bytes.Buffer
	err := RunWorkflowOnboarding(strings.NewReader("y\n"+strings.Repeat("a", 13<<10)+"\n"), &out, nil)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v, want too long", err)
	}
	if _, err := os.Stat(workflowSkillPath()); !os.IsNotExist(err) {
		t.Fatal("saved oversized answer")
	}
}
