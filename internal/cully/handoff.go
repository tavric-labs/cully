package cully

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

// A handoff lets a different coding agent continue without rebuilding context.
// It is derived from the journal's counts and recorded file activity
// (project-relative paths and operations only, see journal.go) plus git state
// read live at render time. Prompts, command arguments and file contents are
// not available, so none can appear here.

const handoffMaxFiles = 30

// handoffInput is everything buildHandoff renders. Empty fields mean "no data"
// and the matching section is omitted.
type handoffInput struct {
	Branch     string
	DiffStat   string
	Files      []string // changed file names read live from git
	TotalFiles int
	Events     []journalEvent
}

type handoffFacts struct {
	Edits, ChecksPassed, ChecksFailed int
	Loops                             int
	LastCheck                         string // "passed", "failed" or ""
	EditsSinceCheck                   int
	RepeatedFailures                  int // distinct failing signatures seen 2+ times
}

func handoffFactsOf(events []journalEvent) handoffFacts {
	var f handoffFacts
	fails := map[string]int{}
	for _, e := range events {
		switch e.Class {
		case journalEdit:
			if !e.Failed {
				f.Edits++
				f.EditsSinceCheck++
			}
		case journalCheck:
			if e.Unknown {
				f.LastCheck = "unknown"
				continue
			}
			if e.Failed {
				f.ChecksFailed++
				f.LastCheck = "failed"
				if e.Sig != "" {
					fails[e.Sig]++
				}
			} else {
				f.EditsSinceCheck = 0
				f.ChecksPassed++
				f.LastCheck = "passed"
			}
		}
	}
	for _, n := range fails {
		if n >= 2 {
			f.RepeatedFailures++
		}
	}
	f.Loops = loopCount(events)
	return f
}

func buildHandoff(in handoffInput) string {
	facts := handoffFactsOf(in.Events)
	var b strings.Builder
	section := func(title string, lines ...string) {
		b.WriteString("\n## " + title + "\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	b.WriteString("# Handoff from a previous coding session\n")

	task := []string{"The task is not recorded here; see Cully memory."}
	task = append(task, "Run cully_context with a focused task query and the session_ref when known, then cully_get for any record whose details matter.")
	section("Task", task...)
	h := measureHealth(in.Events)
	var coverage []string
	if h.Coverage != "" {
		coverage = append(coverage, "- Journal "+h.Coverage+"; counts cover only observed events.")
	}
	if len(in.Events) == 0 || strings.HasPrefix(h.Signals, "partial:") || strings.HasPrefix(h.Signals, "unavailable:") {
		coverage = append(coverage, "- Signals "+h.Signals+". Verify the current state before continuing.")
	}
	if len(coverage) > 0 {
		section("Evidence limits", coverage...)
	}

	if facts.Edits > 0 || facts.ChecksPassed > 0 {
		section("Completed work",
			fmt.Sprintf("- %s recorded", plural(facts.Edits, "edit", "edits")),
			fmt.Sprintf("- %s", plural(facts.ChecksPassed, "passing check", "passing checks")))
	}

	var state []string
	if in.Branch != "" {
		state = append(state, "- Branch: "+in.Branch)
	}
	if in.TotalFiles > 0 {
		state = append(state, fmt.Sprintf("- %s changed", plural(in.TotalFiles, "file", "files")))
		if in.DiffStat != "" {
			state = append(state, "- Diff: "+in.DiffStat)
		}
		for _, f := range in.Files {
			state = append(state, "  - "+f)
		}
		if more := in.TotalFiles - len(in.Files); more > 0 {
			state = append(state, fmt.Sprintf("  - ... and %d more", more))
		}
	} else if in.Branch != "" {
		state = append(state, "- Working tree is clean")
	}
	state = append(state, handoffActivityLines(summarizeFiles(in.Events))...)
	if len(state) > 0 {
		section("Current state", state...)
	}

	if h.Coverage != "" || h.ChecksUnknown > 0 {
		section("Verification", "- "+h.Verification+"; run the project's checks before relying on this handoff")
	} else if facts.ChecksPassed+facts.ChecksFailed > 0 {
		switch {
		case facts.EditsSinceCheck > 0:
			section("Verification", "- Last check "+facts.LastCheck+"; edits were made after it, so current state is unverified")
		default:
			section("Verification", "- Last check "+facts.LastCheck)
		}
	} else if facts.Edits > 0 {
		section("Verification", "- No check ran in this session")
	}

	var open []string
	if facts.Loops > 0 {
		open = append(open, fmt.Sprintf("- Cully reported %s (the same check failing repeatedly)", plural(facts.Loops, "loop", "loops")))
	}
	if facts.ChecksFailed > 0 {
		open = append(open, fmt.Sprintf("- %s recorded", plural(facts.ChecksFailed, "failing check", "failing checks")))
	}
	if len(open) > 0 {
		section("Open problems", open...)
	}

	if facts.EditsSinceCheck > 0 {
		section("Remaining work", fmt.Sprintf("- %s not yet verified by a check", plural(facts.EditsSinceCheck, "edit", "edits")))
	}

	if facts.RepeatedFailures > 0 {
		section("Known dead ends", fmt.Sprintf("- %s failed repeatedly; avoid repeating it unchanged", plural(facts.RepeatedFailures, "check", "checks")))
	}

	section("For the next agent",
		"- Call cully_context first to recover the task and earlier decisions.",
		"- Verify the current state with the project's checks before building on it.",
		"- Save progress with cully_log before you finish.")
	return b.String()
}

const handoffTopFiles = 10

// handoffActivityLines lists what the journal recorded the agent doing to
// files: counts by operation and the most touched files.
func handoffActivityLines(sum fileSummary) []string {
	if !sum.Recorded {
		return nil
	}
	var parts []string
	for _, p := range []struct {
		n    int
		word string
	}{{len(sum.Created), "created"}, {len(sum.Edited), "edited"}, {len(sum.Deleted), "deleted"}, {len(sum.Moved), "moved"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.word))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	lines := []string{"- Recorded file activity: " + strings.Join(parts, ", ")}
	type touch struct {
		path  string
		count int
		what  string
	}
	var all []touch
	for _, group := range []struct {
		list []fileEntry
		what string
	}{{sum.Edited, "edited"}, {sum.Created, "created"}, {sum.Deleted, "deleted"}, {sum.Moved, "moved"}} {
		for _, e := range group.list {
			all = append(all, touch{e.Path, e.Count, group.what})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].count > all[j].count })
	for i, t := range all {
		if i >= handoffTopFiles {
			break
		}
		line := "  - " + safeText(t.path) + " (" + t.what
		if t.count > 1 {
			line += fmt.Sprintf(" ×%d", t.count)
		}
		lines = append(lines, line+")")
	}
	return lines
}

// gitLive reads the working-tree state from git at render time only.
func gitLive(cwd string) (branch, diffStat string, files []string, total int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	git := func(args ...string) string {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimRight(string(out), "\n")
	}
	branch = gitBranch(cwd)
	diffStat = strings.TrimSpace(git("diff", "--shortstat", "HEAD"))
	for _, line := range strings.Split(git("status", "--porcelain"), "\n") {
		if len(line) < 4 {
			continue
		}
		name := line[3:]
		if i := strings.Index(name, " -> "); i >= 0 {
			name = name[i+4:]
		}
		name = strings.Trim(name, `"`)
		total++
		if len(files) < handoffMaxFiles {
			files = append(files, name)
		}
	}
	return
}

// RunHandoff implements `cully handoff [AGENT] [--print] [--cwd DIR]`.
func RunHandoff(args []string, input, output *os.File) error {
	const usage = "usage: cully handoff [AGENT] [--print] [--cwd DIR]"
	var agent, cwd string
	printOnly := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--print":
			printOnly = true
		case a == "--cwd":
			if i+1 >= len(args) {
				return fmt.Errorf("%s", usage)
			}
			i++
			cwd = args[i]
		case strings.HasPrefix(a, "--cwd="):
			cwd = strings.TrimPrefix(a, "--cwd=")
		case strings.HasPrefix(a, "-") || agent != "":
			return fmt.Errorf("%s", usage)
		default:
			agent = a
		}
	}
	if cwd == "" {
		cwd = currentDir()
	}
	var events []journalEvent
	if _, ev, ok := latestJournal(cwd); ok {
		events = ev
	}
	in := handoffInput{Events: events}
	in.Branch, in.DiffStat, in.Files, in.TotalFiles = gitLive(cwd)
	text := buildHandoff(in)
	if agent == "" || printOnly || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		_, err := io.WriteString(output, text)
		return err
	}
	return RunPane(agent, []string{text}, input, output)
}
