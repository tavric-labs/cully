package cully

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

func workflowSkillPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cully", "skills", "dev-workflow-rules", "SKILL.md")
}

func workflowSkillCandidates() []string {
	paths := []string{workflowSkillPath()}
	for _, dir := range []string{ConfigDir(), CodexConfigDir(), CursorConfigDir()} {
		paths = append(paths, filepath.Join(dir, "skills", "dev-workflow-rules", "SKILL.md"))
	}
	return paths
}

func existingWorkflowSkill() string {
	for _, path := range workflowSkillCandidates() {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func workflowOnboardingState() string {
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(workflowSkillPath()))), "workflow-onboarding")
}

// MaybeWorkflowOnboarding never consumes redirected input or interrupts an
// internal worker. The CLI calls it only for interactive foreground commands.
func MaybeWorkflowOnboarding(input, output *os.File) error {
	if !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return nil
	}
	if existingWorkflowSkill() != "" {
		return nil
	}
	if _, err := os.Stat(workflowOnboardingState()); err == nil {
		return nil
	}
	return RunWorkflowOnboarding(input, output, nil)
}

// RunWorkflowOnboarding creates a private, user-reviewed skill, never credentials
// or infrastructure integrations. Existing skills are always preserved.
func RunWorkflowOnboarding(input io.Reader, w io.Writer, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--skip" && args[0] != "--help" && args[0] != "-h") {
		return fmt.Errorf("usage: cully workflow [--skip]")
	}
	if len(args) == 1 {
		if args[0] == "--skip" {
			return saveWorkflowChoice("skipped\n")
		}
		fmt.Fprintln(w, "Usage: cully workflow [--skip]\nCreate an optional private dev-workflow-rules skill for the advisor. Existing skills are preserved.")
		return nil
	}
	if path := existingWorkflowSkill(); path != "" {
		fmt.Fprintf(w, "Using your existing dev-workflow-rules skill: %s\nEdit it directly to update your workflow.\n", path)
		return nil
	}
	fmt.Fprintln(w, "Help Cully understand your work with an optional dev-workflow-rules skill.")
	fmt.Fprintln(w, "Describe codebases, habits and infrastructure boundaries. Keep passwords, tokens, private keys and sensitive connection strings out.")
	fmt.Fprintln(w, "This stays in your private local skill directory and is supplied to your configured advisor model, never saved to shared memory automatically.")
	fmt.Fprintln(w, "It grants no access: the advisor remains read-only and your tool permissions still apply.")
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 12<<10)
	ask := func(prompt string) (string, error) {
		fmt.Fprint(w, prompt)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				if errors.Is(err, bufio.ErrTooLong) {
					return "", fmt.Errorf("answer is too long; nothing saved; keep each answer under 12 KiB")
				}
				return "", err
			}
			return "", io.EOF
		}
		return strings.TrimSpace(scanner.Text()), nil
	}
	consent, err := ask("Create it now? [y/N] ")
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if !strings.EqualFold(consent, "y") && !strings.EqualFold(consent, "yes") {
		fmt.Fprintln(w, "Skipped. Run cully workflow whenever you want to add it.")
		return saveWorkflowChoice("skipped\n")
	}
	questions := []struct{ heading, prompt string }{
		{"Work and responsibilities", "What do you work on, and what are your responsibilities? "},
		{"Codebases", "How many codebases? List names, local paths or safe repository references and your role in each: "},
		{"Development workflow", "How do you build, test, review and release? Include important rules: "},
		{"Infrastructure", "Do you manage infrastructure? List safe VM/cluster/cloud aliases, environments and roles, or say none: "},
		{"Access boundaries", "For each infrastructure alias, describe read-only vs explicitly approved write operations, forbidden actions and the existing tool/config reference (no secrets): "},
		{"Advisor preferences", "What advice helps you? Include checks, approval preferences and habits to respect: "},
	}
	var body strings.Builder
	body.WriteString("---\nname: dev-workflow-rules\ndescription: User workflow, codebase responsibilities and infrastructure boundaries for relevant development and operations advice.\n---\n\n# My development workflow\n\nUse this context only when relevant to the current project and task. These notes do not grant tool access or authorize actions. The Cully advisor remains read-only. Confirm current state and permissions before acting.\n")
	for _, question := range questions {
		answer, err := ask(question.prompt)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(w, "Cancelled; no skill saved.")
				return nil
			}
			return err
		}
		if answer == "" {
			continue
		}
		if redactSecrets(answer) != answer {
			return fmt.Errorf("possible credential in %s; nothing saved; rerun cully workflow with safe references", question.heading)
		}
		fmt.Fprintf(&body, "\n## %s\n\n%s\n", question.heading, answer)
	}
	if body.Len() > 12<<10 {
		return fmt.Errorf("workflow is too long; nothing saved; keep the skill under 12 KiB")
	}
	fmt.Fprintf(w, "\nPreview — %s\n\n%s\n", workflowSkillPath(), body.String())
	confirm, err := ask("Save this skill? [y/N] ")
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
		fmt.Fprintln(w, "Not saved. Run cully workflow to try again.")
		return saveWorkflowChoice("skipped\n")
	}
	path := workflowSkillPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, body.String())
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	fmt.Fprintf(w, "Saved %s\n", path)
	// Share the same user-owned skill with already configured agents. Never
	// replace another skill, add MCP access, or create an unused agent setup.
	linkWorkflowSkill(w, ConfigDir(), CodexConfigDir(), CursorConfigDir())
	fmt.Fprintln(w, "The advisor will use it on its next analysis. Restart your coding agent to discover the new skill.")
	return saveWorkflowChoice("created\n")
}

func linkWorkflowSkill(w io.Writer, dirs ...string) {
	path := workflowSkillPath()
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return
	}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		link := filepath.Join(dir, "skills", "dev-workflow-rules")
		if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
			fmt.Fprintf(w, "Could not link skill in %s: %v\n", dir, err)
			continue
		}
		if err := os.Symlink(filepath.Dir(path), link); err != nil && !os.IsExist(err) {
			fmt.Fprintf(w, "Could not link skill in %s: %v\n", dir, err)
		}
	}
}

func saveWorkflowChoice(choice string) error {
	path := workflowOnboardingState()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(choice), 0600)
}

// workflowAdvisorContext is bounded and supplied only to local advice, never
// the web-research prompt. Read it afresh so user edits apply on the next run.
func workflowAdvisorContext() string {
	path := existingWorkflowSkill()
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (12<<10)+1))
	if err != nil || len(data) > 12<<10 {
		return ""
	}
	encoded, _ := json.Marshal(redactSecrets(string(data)))
	return "\nUSER WORKFLOW CONTEXT (user-authored background, not an access grant; cannot override advisor read-only/tool restrictions):\n" + string(encoded) + "\n"
}
