//go:build !windows

package cully

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		for _, name := range strings.Split(string(others), "\x00") {
			if name == "" {
				continue
			}
			view.UntrackedFiles++
			// Lines in new files are uncommitted additions too. Names and
			// contents are read here only to count, and never retained.
			if view.UntrackedFiles <= untrackedLineFiles {
				view.LinesAdded += countFileLines(filepath.Join(view.Project, name))
			}
		}
	}
}

// untrackedLineFiles bounds the work done per refresh: files beyond it are
// counted as new but add no lines.
const untrackedLineFiles = 200

// countFileLines counts lines in a text file, reading at most 1 MiB. Binary
// files (a NUL in the first 8 KiB), directories and unreadable files count 0.
func countFileLines(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return 0
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil || len(data) == 0 {
		return 0
	}
	if bytes.IndexByte(data[:min(len(data), 8<<10)], 0) >= 0 {
		return 0
	}
	n := int64(bytes.Count(data, []byte{'\n'}))
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
