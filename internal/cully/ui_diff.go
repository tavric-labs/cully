package cully

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	uiDiffMax      = 400_000
	uiUntrackedMax = 100_000 // bytes shown per untracked file
	uiUntrackedN   = 30      // untracked files shown with content
)

// diff returns the agent's whole change against the commit its worktree
// started from: committed and uncommitted edits together, plus untracked
// files by name. Reviewing only the last turn can miss earlier edits.
func (a *uiAgent) diff() string {
	a.mu.Lock()
	base := a.base
	a.mu.Unlock()
	if base == "" {
		return "No worktree yet."
	}
	git := func(args ...string) string {
		out, _ := exec.Command("git", append([]string{"-C", a.worktree}, args...)...).Output()
		return string(out)
	}
	var b strings.Builder
	if stat := strings.TrimSpace(git("diff", "--stat", base)); stat != "" {
		b.WriteString(stat + "\n\n")
	}
	b.WriteString(git("diff", base))
	if untracked := strings.TrimSpace(git("ls-files", "--others", "--exclude-standard")); untracked != "" {
		names := strings.Split(untracked, "\n")
		b.WriteString("\nUntracked files:\n" + untracked + "\n")
		for i, name := range names {
			if i >= uiUntrackedN {
				fmt.Fprintf(&b, "\n… %d more untracked files not shown\n", len(names)-i)
				break
			}
			if info, err := os.Lstat(filepath.Join(a.worktree, name)); err == nil && info.Mode().IsRegular() && info.Size() > uiUntrackedMax {
				fmt.Fprintf(&b, "\n%s: %d bytes, too large to show\n", name, info.Size())
				continue
			}
			// --no-index diffs against /dev/null without touching the index;
			// it exits 1 when the file differs, which is the normal case.
			cmd := exec.Command("git", "-C", a.worktree, "diff", "--no-index", "--", "/dev/null", name)
			out, _ := cmd.Output()
			b.WriteString("\n" + string(out))
		}
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "No changes yet."
	}
	if len(out) > uiDiffMax {
		out = out[:uiDiffMax] + "\n… diff truncated"
	}
	return out
}
