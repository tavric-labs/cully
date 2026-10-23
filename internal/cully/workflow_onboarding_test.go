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

func TestWorkflowOnboardingInstallsStarterWithoutAsking(t *testing.T) {
	isolateWorkflow(t)
	if err := os.MkdirAll(CodexConfigDir(), 0700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunWorkflowOnboarding(strings.NewReader("never read"), &out, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "[y/N]") || strings.Contains(out.String(), "?") {
		t.Fatalf("onboarding must not ask questions: %q", out.String())
	}
	data, err := os.ReadFile(workflowSkillPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: dev-workflow-rules", "do not grant tool access", "## Infrastructure", "<!--"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("starter missing %q", want)
		}
	}
	for _, ref := range []string{"codebases.md", "workflow.md", "infrastructure.md"} {
		info, err := os.Stat(filepath.Join(filepath.Dir(workflowSkillPath()), "references", ref))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("reference %s: %v", ref, err)
		}
	}
	if _, err := os.ReadFile(filepath.Join(CodexConfigDir(), ".cully-runtime", "skills", "dev-workflow-rules", "SKILL.md")); err != nil {
		t.Fatalf("agent link: %v", err)
	}
	if state, _ := os.ReadFile(workflowOnboardingState()); string(state) != "created\n" {
		t.Fatalf("state = %q", state)
	}
	context := workflowAdvisorContext()
	if strings.Contains(context, "HOW TO USE") || strings.Contains(context, "Example:") || !strings.Contains(context, "Work and responsibilities") {
		t.Fatalf("hint comments must not reach the advisor: %q", context)
	}
	// Running again, or after the user edits, never overwrites their skill.
	edited := []byte("---\nname: dev-workflow-rules\n---\nstaging-vm is read only\n")
	if err := os.WriteFile(workflowSkillPath(), edited, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunWorkflowOnboarding(strings.NewReader(""), &out, nil); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(workflowSkillPath())
	if string(again) != string(edited) || !strings.Contains(workflowAdvisorContext(), "staging-vm") {
		t.Fatal("overwrote user skill")
	}
	// An integration configured later also discovers the skill.
	if err := installMemorySkill("cursor"); err != nil {
		t.Fatal(err)
	}
	late, err := os.ReadFile(filepath.Join(CursorConfigDir(), ".cully-runtime", "skills", "dev-workflow-rules", "SKILL.md"))
	if err != nil || string(late) != string(edited) {
		t.Fatalf("late agent integration missed workflow skill: %v", err)
	}
}

func TestWorkflowSkipAndNonInteractive(t *testing.T) {
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
	path := filepath.Join(CodexConfigDir(), ".cully-runtime", "skills", "dev-workflow-rules", "SKILL.md")
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
