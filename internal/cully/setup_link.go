package cully

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Writing and linking the rules. The single source of truth is
// ~/.cully/skills/dev-workflow-rules/SKILL.md. Each installed harness gets
// skills/dev-workflow-rules pointing at that folder: a symlink when the
// filesystem allows it, otherwise a copy carrying a header comment that says
// where it came from. A real folder the user wrote is never overwritten: it is
// renamed to .bak.<timestamp> first.

const setupCopyMarker = "<!-- cully:linked-copy of "

type setupHarnessHome struct{ ID, Name, Dir string }

func setupHarnessHomes() []setupHarnessHome {
	return []setupHarnessHome{
		{"claude", "Claude Code", ConfigDir()},
		{"codex", "Codex", CodexConfigDir()},
		{"cursor", "Cursor", CursorConfigDir()},
	}
}

func setupSourceDir() string { return filepath.Dir(workflowSkillPath()) }

func setupLinkDir(h setupHarnessHome) string {
	return filepath.Join(h.Dir, "skills", uiRulesName)
}

func setupLinkedMarker() string {
	return filepath.Join(filepath.Dir(filepath.Dir(setupSourceDir())), "setup-rules-linked")
}

// setupRulesLinked reports that the user approved rules and linking, so a later
// `cully setup` keeps the links instead of removing them.
func setupRulesLinked() bool {
	_, err := os.Stat(setupLinkedMarker())
	return err == nil
}

// setupLinkState reports whether the harness's skills folder is linked to the source.
func setupLinkState(h setupHarnessHome) (bool, string) {
	link := setupLinkDir(h)
	skill := filepath.Join(link, "SKILL.md")
	st, err := os.Lstat(link)
	if err != nil {
		return false, skill
	}
	if st.Mode()&os.ModeSymlink != 0 {
		a, e1 := filepath.EvalSymlinks(link)
		b, e2 := filepath.EvalSymlinks(setupSourceDir())
		return e1 == nil && e2 == nil && a == b, skill
	}
	return setupIsLinkedCopy(skill), skill
}

func setupIsLinkedCopy(skill string) bool {
	b, err := os.ReadFile(skill)
	return err == nil && strings.Contains(string(b), setupCopyMarker)
}

func setupCopyWithHeader(text string) string {
	note := setupCopyMarker + workflowSkillPath() + "; do not edit here, edit the source -->"
	header, body, ok := setupSplitHeader(text)
	if !ok {
		return note + "\n" + text
	}
	return header + note + "\n" + body
}

func setupWriteFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o644)
	return os.Rename(tmp, path)
}

// setupLinkRules links one harness to the source folder and returns how: "symlink" or "copy".
func setupLinkRules(h setupHarnessHome, text string) (string, error) {
	link := setupLinkDir(h)
	if linked, _ := setupLinkState(h); linked {
		if st, err := os.Lstat(link); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "symlink", nil
		}
		return "copy", setupWriteFile(filepath.Join(link, "SKILL.md"), setupCopyWithHeader(text)) // refresh the copy
	}
	if _, err := os.Lstat(link); err == nil {
		bak := link + ".bak." + time.Now().Format("20060102-150405")
		for i := 1; ; i++ {
			if _, err := os.Lstat(bak); err != nil {
				break
			}
			bak = fmt.Sprintf("%s.bak.%s-%d", link, time.Now().Format("20060102-150405"), i)
		}
		if err := os.Rename(link, bak); err != nil {
			return "", fmt.Errorf("could not back up %s: %w", link, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return "", err
	}
	if err := os.Symlink(setupSourceDir(), link); err == nil {
		return "symlink", nil
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		return "", err
	}
	return "copy", setupWriteFile(filepath.Join(link, "SKILL.md"), setupCopyWithHeader(text))
}

type setupApproved struct {
	OK      bool              `json:"ok"`
	Path    string            `json:"path"`
	Linked  []string          `json:"linked"`
	Methods map[string]string `json:"methods,omitempty"` // how each harness was linked: symlink or copy
	Notes   []string          `json:"notes,omitempty"`
}

// Approve writes the proposal to the source file and links every installed harness.
func (e *setupEngine) Approve(id string) (setupApproved, error) {
	uiRulesMu.Lock()
	defer uiRulesMu.Unlock()
	var sp setupStoredProposal
	if !uiDB().get("setupproposal", id, &sp) {
		return setupApproved{}, errors.New("no such proposal")
	}
	if err := uiValidRules(sp.After); err != nil {
		return setupApproved{}, err
	}
	if sp.After == sp.Before {
		return setupApproved{}, errors.New("nothing to write: the rules file already matches your answers")
	}
	if setupCurrentRules() != sp.Before {
		return setupApproved{}, errors.New("the rules file changed since this proposal was made; review it again")
	}
	src := workflowSkillPath()
	if err := setupWriteFile(src, sp.After); err != nil {
		return setupApproved{}, fmt.Errorf("could not write the rules: %w", err)
	}
	out := setupApproved{OK: true, Path: src, Linked: []string{}, Methods: map[string]string{}}
	for _, h := range setupHarnessHomes() {
		if !codingAgentInstalled(h.ID) {
			continue
		}
		how, err := setupLinkRules(h, sp.After)
		if err != nil {
			out.Notes = append(out.Notes, h.Name+": "+err.Error())
			continue
		}
		out.Linked = append(out.Linked, h.ID)
		out.Methods[h.ID] = how
		if how == "copy" {
			out.Notes = append(out.Notes, h.Name+": linked as a marked copy because symlinks are not available; Cully refreshes it on each approval.")
		}
	}
	_ = setupWriteFile(setupLinkedMarker(), time.Now().UTC().Format(time.RFC3339)+"\n")
	uiDB().metaSet(metaSetupSkipped, "")
	uiDB().del("setupproposal", id)
	e.m.ledgerRules(sp.Before, sp.After, "setup:"+id)
	return out, nil
}
