package cully

import (
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The summary is what the UI shows where raw handoff text used to be: a plain
// description of where the work stands. The agent-facing handoff (with its
// instructions to another agent) stays behind a Copy button.

const uiSummaryFiles = 12

type uiChecks struct {
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
	Last   string `json:"last"` // passed, failed or none
}

type uiHandoffView struct {
	Branch          string   `json:"branch"`
	Task            string   `json:"task,omitempty"`
	LastReply       string   `json:"lastReply,omitempty"`
	Files           []string `json:"files"`
	FileCount       int      `json:"fileCount"`
	Added           int      `json:"added"`
	Removed         int      `json:"removed"`
	Checks          uiChecks `json:"checks"`
	Edits           int      `json:"edits"`
	EditsSinceCheck int      `json:"editsSinceCheck"`
	Loops           int      `json:"loops"`
	Activity        bool     `json:"activity"`
}

func (v *uiHandoffView) applyFacts(events []journalEvent) {
	f := handoffFactsOf(events)
	v.Activity = len(events) > 0
	v.Edits, v.EditsSinceCheck, v.Loops = f.Edits, f.EditsSinceCheck, f.Loops
	v.Checks = uiChecks{Passed: f.ChecksPassed, Failed: f.ChecksFailed, Last: "none"}
	if f.LastCheck == "passed" || f.LastCheck == "failed" {
		v.Checks.Last = f.LastCheck
	}
}

func (v *uiHandoffView) setFiles(files []string, total int) {
	v.FileCount = total
	if len(files) > uiSummaryFiles {
		files = files[:uiSummaryFiles]
	}
	v.Files = append([]string{}, files...)
}

// uiWorktreeChanges lists what a worktree changed against its starting commit:
// committed and uncommitted edits plus untracked files.
func uiWorktreeChanges(worktree, base string) (files []string, added, removed int) {
	git := func(args ...string) string {
		out, _ := exec.Command("git", append([]string{"-C", worktree}, args...)...).Output()
		return string(out)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(git("diff", "--numstat", base), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		a, _ := strconv.Atoi(parts[0]) // "-" for binary files
		r, _ := strconv.Atoi(parts[1])
		added, removed = added+a, removed+r
		if !seen[parts[2]] {
			seen[parts[2]] = true
			files = append(files, parts[2])
		}
	}
	for _, name := range strings.Split(strings.TrimSpace(git("ls-files", "--others", "--exclude-standard")), "\n") {
		if name != "" && !seen[name] {
			seen[name] = true
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, added, removed
}

func (a *uiAgent) summary() uiHandoffView {
	a.mu.Lock()
	base := a.base
	a.mu.Unlock()
	v := uiHandoffView{Branch: a.branch, Task: uiClip(a.req.Prompt, 400), LastReply: uiClip(a.lastReply(), 600), Files: []string{}}
	v.applyFacts(readJournal(journalPath(a.worktree, "ui-"+a.id)))
	if base != "" {
		files, added, removed := uiWorktreeChanges(a.worktree, base)
		v.Added, v.Removed = added, removed
		v.setFiles(files, len(files))
	}
	return v
}

var (
	uiStatAdded   = regexp.MustCompile(`(\d+) insertion`)
	uiStatRemoved = regexp.MustCompile(`(\d+) deletion`)
)

// uiRepoSummary describes the repository's current state and its latest
// recorded session, for the Dashboard.
func uiRepoSummary(repo string) uiHandoffView {
	v := uiHandoffView{Files: []string{}}
	var events []journalEvent
	if _, ev, ok := latestJournal(repo); ok {
		events = ev
	}
	v.applyFacts(events)
	branch, stat, files, total := gitLive(repo)
	v.Branch = branch
	if m := uiStatAdded.FindStringSubmatch(stat); m != nil {
		v.Added, _ = strconv.Atoi(m[1])
	}
	if m := uiStatRemoved.FindStringSubmatch(stat); m != nil {
		v.Removed, _ = strconv.Atoi(m[1])
	}
	v.setFiles(files, total)
	return v
}
