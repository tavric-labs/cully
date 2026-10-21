//go:build !windows

package cully

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Read only Git's aggregate diff statistics. These are tracked working-tree
// changes against HEAD, not Claude's per-session edit totals. No diff contents
// or filenames are retained in the status view.
func readGitChanges(view *sessionView) {
	view.ChangesKnown = false
	view.LinesAdded, view.LinesRemoved, view.ChangedFiles, view.UntrackedFiles = 0, 0, 0, 0
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	output, err := exec.CommandContext(ctx, "git", "-C", view.Project,
		"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--numstat", "-z", "HEAD", "--").Output()
	if err != nil {
		return
	}
	for _, record := range strings.Split(string(output), "\x00") {
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\t", 3)
		if len(fields) != 3 {
			return
		}
		view.ChangedFiles++
		if fields[0] == "-" && fields[1] == "-" {
			continue // Binary files have no line counts.
		}
		added, addErr := strconv.ParseInt(fields[0], 10, 64)
		removed, removeErr := strconv.ParseInt(fields[1], 10, 64)
		if addErr != nil || removeErr != nil || added < 0 || removed < 0 {
			return
		}
		view.LinesAdded += added
		view.LinesRemoved += removed
	}
	view.ChangesKnown = true
	// New files are invisible to a diff against HEAD, so an agent that mostly
	// creates files looks idle. Count them without reading names or contents.
	if others, err := exec.CommandContext(ctx, "git", "-C", view.Project, "ls-files", "--others", "--exclude-standard", "-z").Output(); err == nil {
		view.UntrackedFiles = strings.Count(string(others), "\x00")
	}
}
