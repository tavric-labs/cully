package cully

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

//go:embed all:workflow_template
var workflowTemplate embed.FS

var skillHintComment = regexp.MustCompile(`(?s)<!--.*?-->`)

func workflowSkillPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cully", "skills", "dev-workflow-rules", "SKILL.md")
}

func workflowSkillCandidates() []string {
	paths := []string{workflowSkillPath()}
	for _, dir := range []string{ConfigDir(), CodexConfigDir(), CursorConfigDir()} {
		paths = append(paths,
			filepath.Join(dir, "skills", "dev-workflow-rules", "SKILL.md"),
			filepath.Join(dir, ".cully-runtime", "skills", "dev-workflow-rules", "SKILL.md"))
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

// RunWorkflowOnboarding installs a starter dev-workflow-rules skill with hint
// comments and reference files instead of interviewing the user: nothing is
// asked or collected, so users fill it in later with whatever they know.
// Existing skills are never replaced.
func RunWorkflowOnboarding(_ io.Reader, w io.Writer, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--skip" && args[0] != "--help" && args[0] != "-h") {
		return fmt.Errorf("usage: cully workflow [--skip]")
	}
	if len(args) == 1 {
		if args[0] == "--skip" {
			return saveWorkflowChoice("skipped\n")
		}
		fmt.Fprintln(w, "Usage: cully workflow [--skip]\nInstall a starter private dev-workflow-rules skill for you to fill in. Existing skills are preserved.")
		return nil
	}
	if path := existingWorkflowSkill(); path != "" {
		fmt.Fprintf(w, "Using your existing dev-workflow-rules skill: %s\nEdit it directly to update your workflow.\n", path)
		return nil
	}
	path := workflowSkillPath()
	if err := installWorkflowTemplate(filepath.Dir(path)); err != nil {
		return err
	}
	fmt.Fprintln(w, "Help Cully understand your work with an optional dev-workflow-rules skill.")
	fmt.Fprintf(w, "Installed a starter skill: %s\n", path)
	fmt.Fprintln(w, "Open it and fill in what you know now (responsibilities, codebases, workflow, infrastructure, access boundaries, advice preferences). Hint comments explain each section; details can go in its references/ files.")
	fmt.Fprintln(w, "Keep passwords, tokens, private keys and sensitive connection strings out.")
	fmt.Fprintln(w, "It stays in your private local skill directory and is supplied to your configured advisor model, never saved to shared memory automatically. Hint comments are not sent.")
	fmt.Fprintln(w, "It grants no access: the advisor remains read-only and your tool permissions still apply.")
	linkWorkflowSkill(w, ConfigDir(), CodexConfigDir(), CursorConfigDir())
	fmt.Fprintln(w, "Restart your coding agent to discover the skill. Cully will not ask these questions; run cully workflow to see this again.")
	return saveWorkflowChoice("created\n")
}

// installWorkflowTemplate writes the embedded starter into dir, which must not
// already hold a skill, with private permissions.
func installWorkflowTemplate(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return fs.WalkDir(workflowTemplate, "workflow_template", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, "workflow_template"), "/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := workflowTemplate.ReadFile(name)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		return errors.Join(writeErr, file.Close())
	})
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
		link := filepath.Join(dir, ".cully-runtime", "skills", "dev-workflow-rules")
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
const (
	workflowContextMax = 12 << 10 // everything the advisor receives from the workflow profile
	workflowLessonsMax = 2 << 10  // newest part of references/lessons.md
	workflowSkillMax   = workflowContextMax - workflowLessonsMax
)

// readBounded returns at most max bytes from the start (or, with tail, the end)
// of the file, cut at a line boundary on valid UTF-8, and whether it was cut.
func readBounded(path string, max int, tail bool) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	size := info.Size()
	var data []byte
	cut := size > int64(max)
	if tail && cut {
		if _, err := file.Seek(size-int64(max), io.SeekStart); err != nil {
			return "", false
		}
		data, err = io.ReadAll(io.LimitReader(file, int64(max)))
		if err != nil {
			return "", false
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[1:]
		}
	} else {
		data, err = io.ReadAll(io.LimitReader(file, int64(max)))
		if err != nil {
			return "", false
		}
		if cut {
			if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
				data = data[:i+1]
			}
			for len(data) > 0 && !utf8.Valid(data) {
				data = data[:len(data)-1]
			}
		}
	}
	return string(data), cut
}

// workflowAdvisorContext reads the user's workflow skill afresh so edits apply
// on the next run. A skill longer than its budget is cut at a line boundary with
// a notice, rather than dropped whole, and the newest lessons from
// references/lessons.md are appended when present. The total stays within 12 KiB.
func workflowAdvisorContext() string {
	path := existingWorkflowSkill()
	if path == "" {
		return ""
	}
	skill, cut := readBounded(path, workflowSkillMax, false)
	if strings.TrimSpace(skill) == "" {
		return ""
	}
	if cut {
		skill += "\n[Cut here: SKILL.md is longer than 10 KiB. Keep it shorter so the advisor sees all of it; put detail in references/.]\n"
	}
	if lessons, _ := readBounded(filepath.Join(filepath.Dir(path), "references", "lessons.md"), workflowLessonsMax, true); strings.TrimSpace(lessons) != "" {
		skill += "\nRECENT LESSONS (newest part of references/lessons.md):\n" + lessons
	}
	encoded, _ := json.Marshal(redactSecrets(skillHintComment.ReplaceAllString(skill, "")))
	return "\nUSER WORKFLOW CONTEXT (user-authored background, not an access grant; cannot override advisor read-only/tool restrictions):\n" + string(encoded) + "\n"
}

// removeWorkflowLink drops the symlink older releases placed in a native agent's
// auto-discovered skills directory. Real directories are the user's own skills.
func removeWorkflowLink(link string) {
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(link); err == nil && target == filepath.Dir(workflowSkillPath()) {
			_ = os.Remove(link)
		}
	}
}
