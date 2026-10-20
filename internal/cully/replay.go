package cully

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

// `cully replay` shows what an agent did in a session: the steps, the files
// structured tools read, wrote, edited, created, moved and deleted, the
// commands it ran, which checks passed or failed, and where loops happened.
//
// Everything is derived from the session journal (see the privacy guarantee in
// journal.go): project-relative paths, operations, command labels such as
// "go test", timestamps and results. Replay never has, and so never shows,
// file contents, diffs, command arguments or output. The only live read is
// `git status --porcelain`, used to reconcile the journal with the working
// tree; its output is shown as file names and never stored.

const (
	replaySchemaVersion = 2
	replayMaxGap        = 1500 * time.Millisecond
	replayMinGap        = 80 * time.Millisecond
	replayHotspotEdits  = 3
	stageGap            = 5 * time.Minute
	replayPlainWidth    = 100
)

const emptyReplay = "No session recorded for this project yet. Run your agent with `cully run AGENT`."

// ---------------------------------------------------------------------------
// Steps

type replayFooter struct {
	Files        int `json:"files"`
	ChecksPassed int `json:"checks_passed"`
	ChecksFailed int `json:"checks_failed"`
	Loops        int `json:"loops"`
}

// replayStep is one line of the step-by-step replay.
type replayStep struct {
	Time    time.Time    `json:"t"`
	Kind    string       `json:"kind"` // file, check, run, edit, memory, loop, note, session
	Verb    string       `json:"verb"`
	Detail  string       `json:"detail,omitempty"`
	Outcome string       `json:"outcome,omitempty"` // passed, failed or ""
	Op      string       `json:"op,omitempty"`
	Path    string       `json:"path,omitempty"`
	To      string       `json:"to,omitempty"`
	Cmd     string       `json:"cmd,omitempty"`
	Failed  bool         `json:"failed,omitempty"`
	Footer  replayFooter `json:"footer"`
}

func opVerb(op string, failed bool) string {
	if failed {
		switch op {
		case opRead:
			return "Read"
		case opWrite:
			return "Write"
		case opEdit:
			return "Edit"
		case opCreate:
			return "Create"
		case opDelete:
			return "Delete"
		case opMove:
			return "Move"
		}
		return "Tool"
	}
	switch op {
	case opRead:
		return "Read"
	case opWrite:
		return "Wrote"
	case opEdit:
		return "Edited"
	case opCreate:
		return "Created"
	case opDelete:
		return "Deleted"
	case opMove:
		return "Moved"
	}
	return "Tool"
}

func loopText(f loopFinding) string {
	switch f.Kind {
	case loopRepeatFailure:
		return fmt.Sprintf("Cully noticed a loop: the same command failed %d times", f.Attempts)
	case loopEditCycle:
		return fmt.Sprintf("Cully noticed a loop: %d edits were each followed by a failed check", f.Attempts)
	}
	return "Cully noticed a loop: the same check failed repeatedly"
}

func isChangeOp(op string) bool {
	return op == opWrite || op == opEdit || op == opCreate || op == opDelete || op == opMove
}

// buildSteps turns journal events into replay steps with a running footer.
// Loops come from the notes Cully wrote at the time; loops the journal shows
// but that were never noted (the agent ran outside the Cully terminal) are
// added where they were detected, so the count agrees with the timeline.
func buildSteps(events []journalEvent) []replayStep {
	var steps []replayStep
	findings := detectLoops(events).Loops
	noted := 0
	for idx, e := range events {
		base := replayStep{Time: e.Time, Failed: e.Failed}
		switch e.Class {
		case journalStart:
			base.Kind, base.Detail = "session", journalAgentName(e.Agent)+" started"
			steps = append(steps, base)
			continue
		case journalEnd:
			base.Kind, base.Detail = "session", journalAgentName(e.Agent)+" ended"
			steps = append(steps, base)
			continue
		case journalNote:
			base.Kind = "note"
			if e.Note == noteLoop {
				base.Kind = "loop"
				base.Detail = "Cully noticed a loop: the same check failed repeatedly"
				// Describe the loop as it looked when Cully noted it; a later
				// passing check may have resolved it since.
				if seen := detectLoops(events[:idx]).Loops; len(seen) > 0 {
					base.Detail = loopText(seen[0])
				}
				noted++
			} else if e.Note == noteVerifyGap {
				base.Detail = "Cully noticed edits that were not verified"
			} else if e.Note == noteHistoryTrimmed || e.Note == noteHistoryUnreadable || e.Note == noteHistoryInvalid {
				base.Detail = "Journal " + journalCoverage([]journalEvent{e})
			} else {
				base.Detail = "Cully made an observation"
			}
			steps = append(steps, base)
			continue
		case journalMemory:
			base.Kind, base.Verb, base.Detail = "memory", "Memory", e.Tool
			steps = append(steps, base)
			continue
		}
		if e.Class == journalCheck {
			s := base
			s.Kind, s.Verb, s.Cmd = "check", "Ran", e.Cmd
			s.Detail = e.Cmd
			if s.Detail == "" {
				s.Detail = "a check"
			}
			s.Outcome = "passed"
			if e.Unknown {
				s.Outcome = "unknown"
			}
			if e.Failed {
				s.Outcome = "failed"
			}
			steps = append(steps, s)
		}
		switch {
		case e.Op != "" && e.Path != "":
			s := base
			s.Kind, s.Op, s.Path, s.To = "file", e.Op, e.Path, e.To
			s.Verb = opVerb(e.Op, e.Failed)
			s.Detail = e.Path
			if e.Op == opMove && e.To != "" {
				s.Detail = e.Path + " -> " + e.To
			}
			if e.Failed {
				s.Outcome = "failed"
			}
			steps = append(steps, s)
		case e.Class == journalEdit:
			s := base
			s.Kind, s.Verb, s.Detail = "edit", "Edited", "a file (name not recorded)"
			if e.Failed {
				s.Verb, s.Outcome = "Edit", "failed"
			}
			steps = append(steps, s)
		case e.Class != journalCheck && e.Cmd != "":
			s := base
			s.Kind, s.Verb, s.Cmd, s.Detail = "run", "Ran", e.Cmd, e.Cmd
			if e.Failed {
				s.Outcome = "failed"
			}
			steps = append(steps, s)
		}
	}
	for i := noted; i < len(findings); i++ {
		steps = append(steps, replayStep{Time: findings[i].Last, Kind: "loop", Detail: loopText(findings[i])})
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Time.Before(steps[j].Time) })
	touched := map[string]bool{}
	var foot replayFooter
	for i := range steps {
		s := &steps[i]
		if s.Kind == "file" && !s.Failed && isChangeOp(s.Op) {
			touched[s.Path] = true
			if s.To != "" {
				touched[s.To] = true
			}
		}
		if s.Kind == "check" {
			if s.Failed {
				foot.ChecksFailed++
			} else {
				foot.ChecksPassed++
			}
		}
		if s.Kind == "loop" {
			foot.Loops++
		}
		foot.Files = len(touched)
		s.Footer = foot
	}
	return steps
}

// replayDelay is how long the player waits before the next step: the real gap,
// compressed to at most replayMaxGap, divided by the speed multiplier.
func replayDelay(gap time.Duration, speed float64) time.Duration {
	if speed <= 0 {
		speed = 1
	}
	if gap > replayMaxGap {
		gap = replayMaxGap
	}
	if gap < replayMinGap {
		gap = replayMinGap
	}
	return time.Duration(float64(gap) / speed)
}

// ---------------------------------------------------------------------------
// File activity

type fileEntry struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	To    string `json:"to,omitempty"`
}

type fileSummary struct {
	Created  []fileEntry `json:"created"`
	Edited   []fileEntry `json:"edited"`
	Deleted  []fileEntry `json:"deleted"`
	Moved    []fileEntry `json:"moved"`
	ReadOnly []fileEntry `json:"read_only"`
	Hotspots []fileEntry `json:"hotspots"`
	Recorded bool        `json:"recorded"`

	touched      map[string]bool
	finalDeleted []string
}

func sortedEntries(counts map[string]int) []fileEntry {
	out := make([]fileEntry, 0, len(counts))
	for p, n := range counts {
		out = append(out, fileEntry{Path: p, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// summarizeFiles groups the recorded file operations. Failed operations are
// excluded: they changed nothing. A write counts as an edit.
func summarizeFiles(events []journalEvent) fileSummary {
	created, edited, deleted, read := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	moved := map[string]int{}
	alive := map[string]bool{}
	sum := fileSummary{touched: map[string]bool{}}
	var deletedOrder []string
	for _, e := range events {
		if e.Path == "" || e.Op == "" {
			continue
		}
		sum.Recorded = true
		if e.Failed {
			continue
		}
		switch e.Op {
		case opCreate:
			created[e.Path]++
			alive[e.Path] = true
		case opEdit, opWrite:
			edited[e.Path]++
			alive[e.Path] = true
		case opDelete:
			deleted[e.Path]++
			alive[e.Path] = false
			deletedOrder = append(deletedOrder, e.Path)
		case opMove:
			moved[e.Path+"\x00"+e.To]++
			alive[e.Path] = false
			deletedOrder = append(deletedOrder, e.Path)
			if e.To != "" {
				alive[e.To] = true
				sum.touched[e.To] = true
			}
		case opRead:
			read[e.Path]++
			continue
		default:
			continue
		}
		sum.touched[e.Path] = true
	}
	sum.Created, sum.Edited, sum.Deleted = sortedEntries(created), sortedEntries(edited), sortedEntries(deleted)
	for _, m := range sortedEntries(moved) {
		parts := strings.SplitN(m.Path, "\x00", 2)
		m.Path = parts[0]
		if len(parts) == 2 {
			m.To = parts[1]
		}
		sum.Moved = append(sum.Moved, m)
	}
	readOnly := map[string]int{}
	for p, n := range read {
		if !sum.touched[p] {
			readOnly[p] = n
		}
	}
	sum.ReadOnly = sortedEntries(readOnly)
	for _, f := range sum.Edited {
		if f.Count >= replayHotspotEdits {
			sum.Hotspots = append(sum.Hotspots, f)
		}
	}
	seen := map[string]bool{}
	for _, p := range deletedOrder {
		if !alive[p] && !seen[p] {
			seen[p] = true
			sum.finalDeleted = append(sum.finalDeleted, p)
		}
	}
	return sum
}

func (s fileSummary) changedCount() int { return len(s.touched) }

// ---------------------------------------------------------------------------
// Reconcile with git

type gitEntry struct {
	Path string
	Orig string
	Dir  bool // an untracked directory, shown by git as "dir/"
}

type gitReader func(cwd string) ([]gitEntry, bool)

type reconcileResult struct {
	Available         bool     `json:"available"`
	Comparable        bool     `json:"comparable"`
	Unseen            []string `json:"changed_outside_recorded_calls"`
	DeletedStillExist []string `json:"deleted_but_present"`
}

// readGitStatus reads `git status --porcelain` at run time. Paths are returned
// relative to cwd; entries outside it are ignored.
func readGitStatus(cwd string) ([]gitEntry, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...).Output()
	}
	top, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, false
	}
	out, err := run("status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, false
	}
	root := strings.TrimSpace(string(top))
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	rel := func(p string) (string, bool) {
		r, err := filepath.Rel(cwd, filepath.Join(root, p))
		if err != nil || r == "." || r == ".." || strings.HasPrefix(r, "../") {
			return "", false
		}
		if strings.HasSuffix(p, "/") {
			r += "/"
		}
		return filepath.ToSlash(r), true
	}
	var entries []gitEntry
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := string(fields[i])
		if len(f) < 4 {
			continue
		}
		e := gitEntry{Path: f[3:]}
		if f[0] == 'R' || f[0] == 'C' || f[1] == 'R' || f[1] == 'C' {
			if i+1 < len(fields) {
				i++
				e.Orig = string(fields[i])
			}
		}
		e.Dir = strings.HasSuffix(e.Path, "/")
		p, ok := rel(e.Path)
		if !ok {
			continue
		}
		e.Path = p
		if e.Orig != "" {
			e.Orig, _ = rel(e.Orig)
		}
		entries = append(entries, e)
	}
	return entries, true
}

// reconcileWithGit compares the recorded activity with the working tree, in
// both directions. exists reports whether a project-relative path is present.
func reconcileWithGit(sum fileSummary, read gitReader, cwd string, exists func(string) bool) reconcileResult {
	var res reconcileResult
	entries, ok := read(cwd)
	if !ok {
		return res
	}
	res.Available = true
	res.Comparable = sum.Recorded
	if !sum.Recorded {
		return res
	}
	for _, e := range entries {
		// A recorded child does not cover an entire untracked directory, and
		// editing the old side of a rename does not cover its new path.
		if sum.touched[e.Path] {
			continue
		}
		res.Unseen = append(res.Unseen, e.Path)
	}
	sort.Strings(res.Unseen)
	if exists == nil {
		exists = func(rel string) bool {
			_, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(rel)))
			return err == nil
		}
	}
	for _, p := range sum.finalDeleted {
		if exists(p) {
			res.DeletedStillExist = append(res.DeletedStillExist, p)
		}
	}
	sort.Strings(res.DeletedStillExist)
	return res
}

// ---------------------------------------------------------------------------
// Document

type replayMeta struct {
	Session string    `json:"session"`
	Agent   string    `json:"agent"`
	Project string    `json:"project"`
	Branch  string    `json:"branch,omitempty"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
}

func (m replayMeta) Duration() time.Duration {
	if d := m.End.Sub(m.Start); d > 0 {
		return d
	}
	return 0
}

type replayDoc struct {
	Meta      replayMeta
	Events    []journalEvent
	Steps     []replayStep
	Files     fileSummary
	Reconcile reconcileResult
	Stats     journalStats
	Partial   []string
}

// replayPartial says plainly what the journal cannot tell us.
func replayPartial(events []journalEvent) []string {
	var notes []string
	if coverage := journalCoverage(events); coverage != "" {
		notes = append(notes, "Journal "+coverage+"; replay and counts cover observed events only.")
	}
	h := measureHealth(events)
	if strings.HasPrefix(h.Signals, "partial:") {
		notes = append(notes, "Signals "+h.Signals+".")
	}
	tools, withPath, edits, editsWithPath := 0, 0, 0, 0
	hasStart := false
	for _, e := range events {
		switch e.Class {
		case journalStart:
			hasStart = true
		case journalEdit, journalCheck, journalSearch, journalOther, journalMemory:
			tools++
		}
		if e.Class == journalEdit {
			edits++
			if e.Path != "" {
				editsWithPath++
			}
		}
		if e.Path != "" {
			withPath++
		}
	}
	switch {
	case tools == 0:
		notes = append(notes, "No tool calls were recorded. The agent's hooks may not be installed; run `cully setup`.")
	case withPath == 0:
		notes = append(notes, "This journal has no file names: it was recorded before replay support or with CULLY_JOURNAL_PATHS=0. File activity is unavailable. To record it, unset CULLY_JOURNAL_PATHS (or set it to 1) and run a new session.")
	case edits > editsWithPath:
		notes = append(notes, fmt.Sprintf("%s have no recorded file name, so file activity is partial.", plural(edits-editsWithPath, "edit", "edits")))
	}
	if len(events) > 0 && !hasStart && tools > 0 {
		notes = append(notes, "The start of the session is not in the journal (it was trimmed, or the hooks started late), so replay begins mid-session.")
	}
	return notes
}

func buildReplay(events []journalEvent, meta replayMeta, git gitReader, cwd string, exists func(string) bool) replayDoc {
	doc := replayDoc{Meta: meta, Events: events}
	if len(events) > 0 {
		if doc.Meta.Start.IsZero() {
			doc.Meta.Start = events[0].Time
		}
		if doc.Meta.End.IsZero() {
			doc.Meta.End = events[len(events)-1].Time
		}
		if doc.Meta.Agent == "" {
			doc.Meta.Agent = sessionAgent(events)
			if doc.Meta.Agent == "-" {
				doc.Meta.Agent = "The agent"
			}
		}
	}
	doc.Steps = buildSteps(events)
	doc.Files = summarizeFiles(events)
	doc.Stats = computeStats(events)
	doc.Partial = replayPartial(events)
	if git != nil {
		doc.Reconcile = reconcileWithGit(doc.Files, git, cwd, exists)
	}
	return doc
}

// ---------------------------------------------------------------------------
// Plain rendering

func paint(color bool, code, s string) string {
	if !color || code == "" || s == "" {
		return s
	}
	return code + s + rst
}

func stampOf(t time.Time) string { return t.Local().Format("15:04") }

// renderStepLine renders one step as `10:31  Read     path`.
func renderStepLine(s replayStep, color bool) string {
	stamp := paint(color, dim, stampOf(s.Time))
	var body string
	switch s.Kind {
	case "session", "note":
		body = s.Detail
	case "loop":
		body = paint(color, yellow, s.Detail)
	default:
		body = fmt.Sprintf("%-8s %s", s.Verb, safeText(s.Detail))
		if s.Outcome == "failed" {
			body += "  " + paint(color, red, "✕ failed")
		} else if s.Outcome == "passed" {
			body += "  " + paint(color, green, "✓ passed")
		}
	}
	return stamp + "  " + body
}

// safeText removes control characters so recorded text cannot drive the terminal.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

func renderFooterText(f replayFooter, color bool) string {
	checks := fmt.Sprintf("checks %d passed / %d failed", f.ChecksPassed, f.ChecksFailed)
	if f.ChecksFailed > 0 {
		checks = paint(color, red, checks)
	}
	loops := fmt.Sprintf("loops %d", f.Loops)
	if f.Loops > 0 {
		loops = paint(color, yellow, loops)
	}
	return fmt.Sprintf("files %d · %s · %s", f.Files, checks, loops)
}

func renderHeader(doc replayDoc, color bool) string {
	m := doc.Meta
	parts := []string{"Replay", m.Agent}
	if m.Project != "" {
		p := m.Project
		if m.Branch != "" {
			p += ":" + m.Branch
		}
		parts = append(parts, p)
	}
	if !m.Start.IsZero() {
		parts = append(parts, m.Start.Local().Format("2006-01-02 15:04"), formatDuration(m.Duration()))
	}
	return paint(color, bold, strings.Join(parts, " · "))
}

func renderStepsText(doc replayDoc, color bool) string {
	var b strings.Builder
	for _, s := range doc.Steps {
		b.WriteString(renderStepLine(s, color) + "\n")
	}
	if len(doc.Steps) > 0 {
		b.WriteString("\n" + renderFooterText(doc.Steps[len(doc.Steps)-1].Footer, color) + "\n")
	}
	return b.String()
}

func entryLabel(e fileEntry, verb string) string {
	name := safeText(e.Path)
	if e.To != "" {
		name += " -> " + safeText(e.To)
	}
	if e.Count > 1 {
		name += fmt.Sprintf("  %s ×%d", verb, e.Count)
	}
	return name
}

const fileGroupLimit = 15

func renderFileActivity(doc replayDoc, color bool) string {
	var b strings.Builder
	b.WriteString(paint(color, bold, "FILE ACTIVITY") + "\n")
	s := doc.Files
	if !s.Recorded {
		b.WriteString("  No file activity was recorded for this session.\n")
		return b.String()
	}
	group := func(title, badge, verb string, list []fileEntry) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(&b, "  %s (%d)\n", title, len(list))
		for i, e := range list {
			if i >= fileGroupLimit {
				fmt.Fprintf(&b, "    … and %d more\n", len(list)-fileGroupLimit)
				break
			}
			fmt.Fprintf(&b, "    %s %s\n", badge, entryLabel(e, verb))
		}
	}
	group("Created", "+", "created", s.Created)
	group("Edited", "~", "edited", s.Edited)
	group("Deleted", "-", "deleted", s.Deleted)
	group("Moved", ">", "moved", s.Moved)
	group("Read only", ".", "read", s.ReadOnly)
	if len(s.Hotspots) > 0 {
		var hs []string
		for _, h := range s.Hotspots {
			hs = append(hs, fmt.Sprintf("%s ×%d", safeText(h.Path), h.Count))
		}
		b.WriteString("  " + paint(color, yellow, "Hotspots") + " (edited " + fmt.Sprint(replayHotspotEdits) + "+ times): " + strings.Join(hs, ", ") + "\n")
	}
	return b.String()
}

func renderReconcile(doc replayDoc, color bool) string {
	var b strings.Builder
	b.WriteString(paint(color, bold, "RECONCILE") + "\n")
	r := doc.Reconcile
	switch {
	case !r.Available:
		b.WriteString("  Git status is not available here, so the journal could not be compared with the working tree.\n")
	case !r.Comparable:
		b.WriteString("  The journal has no file names, so it cannot be compared with git status.\n")
	default:
		if len(r.Unseen) == 0 && len(r.DeletedStillExist) == 0 {
			b.WriteString("  The recorded activity matches git status.\n")
		}
		if len(r.Unseen) > 0 {
			b.WriteString("  Changed outside the agent's recorded tool calls:\n")
			for i, p := range r.Unseen {
				if i >= fileGroupLimit {
					fmt.Fprintf(&b, "    … and %d more\n", len(r.Unseen)-fileGroupLimit)
					break
				}
				b.WriteString("    ? " + safeText(p) + "\n")
			}
		}
		if len(r.DeletedStillExist) > 0 {
			b.WriteString("  Recorded as deleted, but still present:\n")
			for _, p := range r.DeletedStillExist {
				b.WriteString("    ! " + safeText(p) + "\n")
			}
		}
	}
	for _, n := range doc.Partial {
		b.WriteString("  Note: " + n + "\n")
	}
	return b.String()
}

// renderReplayText is the non-interactive transcript: the step list, then file
// activity and the git reconcile.
func renderReplayText(doc replayDoc, o replayOptions, width int, color bool) string {
	if len(doc.Events) == 0 {
		return emptyReplay + "\n"
	}
	_ = width
	var b strings.Builder
	if !o.FilesOnly {
		b.WriteString(renderHeader(doc, color) + "\n\n")
		b.WriteString(renderStepsText(doc, color))
		b.WriteString("\n" + summaryLine(doc.Stats) + "\n\n")
	}
	b.WriteString(renderFileActivity(doc, color) + "\n")
	b.WriteString(renderReconcile(doc, color))
	return b.String()
}

// ---------------------------------------------------------------------------
// JSON

type replayJSONEvent struct {
	Time   string `json:"time"`
	Class  string `json:"class"`
	Op     string `json:"op,omitempty"`
	Path   string `json:"path,omitempty"`
	To     string `json:"to,omitempty"`
	Cmd    string `json:"cmd,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Note   string `json:"note,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

type replayJSONSummary struct {
	DurationSeconds int         `json:"duration_seconds"`
	Edits           int         `json:"edits"`
	ChecksPassed    int         `json:"checks_passed"`
	ChecksFailed    int         `json:"checks_failed"`
	Loops           int         `json:"loops"`
	Files           fileSummary `json:"files"`
}

type replayJSONMeta struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Project string `json:"project,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Start   string `json:"start,omitempty"`
	End     string `json:"end,omitempty"`
}

type replayJSON struct {
	SchemaVersion int               `json:"schema_version"`
	Session       string            `json:"session"`
	Agent         string            `json:"agent"`
	Meta          replayJSONMeta    `json:"meta"`
	Events        []replayJSONEvent `json:"events"`
	Steps         []replayStep      `json:"steps"`
	Summary       replayJSONSummary `json:"summary"`
	Reconcile     reconcileResult   `json:"reconcile"`
	Partial       []string          `json:"partial,omitempty"`
}

func utcStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// replayExport is the one document shared by --json and the HTML export. It
// holds only journal-derived data.
func replayExport(doc replayDoc) replayJSON {
	out := replayJSON{
		SchemaVersion: replaySchemaVersion,
		Session:       doc.Meta.Session,
		Agent:         doc.Meta.Agent,
		Meta: replayJSONMeta{Session: doc.Meta.Session, Agent: doc.Meta.Agent, Project: doc.Meta.Project, Branch: doc.Meta.Branch,
			Start: utcStamp(doc.Meta.Start), End: utcStamp(doc.Meta.End)},
		Events:    []replayJSONEvent{},
		Steps:     doc.Steps,
		Reconcile: doc.Reconcile,
		Partial:   doc.Partial,
		Summary: replayJSONSummary{
			DurationSeconds: int(doc.Stats.Duration.Seconds()), Edits: doc.Stats.Edits,
			ChecksPassed: doc.Stats.ChecksPassed, ChecksFailed: doc.Stats.ChecksFailed, Loops: doc.Stats.Loops, Files: doc.Files,
		},
	}
	if out.Steps == nil {
		out.Steps = []replayStep{}
	}
	for _, e := range doc.Events {
		out.Events = append(out.Events, replayJSONEvent{Time: utcStamp(e.Time), Class: e.Class, Op: e.Op, Path: e.Path, To: e.To,
			Cmd: e.Cmd, Tool: e.Tool, Note: e.Note, Failed: e.Failed})
	}
	for _, list := range []*[]fileEntry{&out.Summary.Files.Created, &out.Summary.Files.Edited, &out.Summary.Files.Deleted,
		&out.Summary.Files.Moved, &out.Summary.Files.ReadOnly, &out.Summary.Files.Hotspots} {
		if *list == nil {
			*list = []fileEntry{}
		}
	}
	if out.Reconcile.Unseen == nil {
		out.Reconcile.Unseen = []string{}
	}
	if out.Reconcile.DeletedStillExist == nil {
		out.Reconcile.DeletedStillExist = []string{}
	}
	return out
}

func renderReplayJSON(doc replayDoc) ([]byte, error) {
	return json.MarshalIndent(replayExport(doc), "", "  ")
}

// ---------------------------------------------------------------------------
// Command

type replayOptions struct {
	Session   string
	Cwd       string
	FilesOnly bool
	Steps     bool
	Instant   bool
	JSON      bool
	HTML      bool
	HTMLPath  string
	Speed     float64
}

const replayUsage = "usage: cully replay [--session ID] [--cwd DIR] [--files] [--steps] [--speed N|--instant] [--json] [--html [FILE]]"

func parseReplayArgs(args []string) (replayOptions, error) {
	o := replayOptions{Speed: 1}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--html":
			o.HTML = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				o.HTMLPath = args[i]
			}
		case strings.HasPrefix(a, "--html="):
			o.HTML, o.HTMLPath = true, strings.TrimPrefix(a, "--html=")
		default:
			rest = append(rest, a)
		}
	}
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Session, "session", "", "session id (or unique prefix)")
	fs.StringVar(&o.Cwd, "cwd", "", "project directory")
	fs.BoolVar(&o.FilesOnly, "files", false, "file activity only")
	fs.BoolVar(&o.Steps, "steps", false, "step-by-step view")
	fs.BoolVar(&o.Instant, "instant", false, "print without animation")
	fs.BoolVar(&o.JSON, "json", false, "print JSON")
	fs.Float64Var(&o.Speed, "speed", 1, "playback speed multiplier")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return o, fmt.Errorf("%s", replayUsage)
	}
	if o.Speed <= 0 || o.Speed > 100 {
		return o, fmt.Errorf("replay: --speed must be between 0 and 100")
	}
	return o, nil
}

// findSessionJournal resolves an id or unique prefix.
func findSessionJournal(files []journalFile, id string) (journalFile, bool) {
	var match []journalFile
	for _, f := range files {
		if f.Session == id {
			return f, true
		}
		if strings.HasPrefix(f.Session, id) {
			match = append(match, f)
		}
	}
	if len(match) == 1 {
		return match[0], true
	}
	return journalFile{}, false
}

func isTTYWriter(w io.Writer) (*os.File, bool) {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return nil, false
	}
	return f, true
}

// RunReplay implements `cully replay`.
func RunReplay(w io.Writer, args []string) error {
	o, err := parseReplayArgs(args)
	if err != nil {
		return err
	}
	if o.Cwd == "" {
		o.Cwd = currentDir()
	}
	files := projectJournals(o.Cwd)
	var file journalFile
	var events []journalEvent
	if o.Session != "" {
		var ok bool
		if file, ok = findSessionJournal(files, o.Session); !ok {
			return fmt.Errorf("no single session matches %q; run cully timeline --all", o.Session)
		}
	} else if len(files) > 0 {
		file = files[0]
	}
	if file.Path != "" {
		events = readJournal(file.Path)
	}
	if len(events) == 0 {
		_, err := io.WriteString(w, emptyReplay+"\n")
		return err
	}
	meta := replayMeta{Session: file.Session, Project: filepath.Base(filepath.Clean(o.Cwd)), Branch: gitBranch(o.Cwd)}
	doc := buildReplay(events, meta, readGitStatus, o.Cwd, nil)

	switch {
	case o.JSON:
		b, err := renderReplayJSON(doc)
		if err != nil {
			return err
		}
		_, err = w.Write(append(b, '\n'))
		return err
	case o.HTML:
		path := o.HTMLPath
		if path == "" {
			path = filepath.Join(currentDir(), "cully-replay-"+safeSession(file.Session)+".html")
		}
		if err := writeReplayHTML(path, doc); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, path)
		return err
	}
	f, tty := isTTYWriter(w)
	color := tty && os.Getenv("NO_COLOR") == ""
	if tty && !o.Instant && !o.FilesOnly && term.IsTerminal(int(os.Stdin.Fd())) {
		return runReplayTUI(f, os.Stdin, doc, o)
	}
	width := replayPlainWidth
	if tty {
		if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
			width = cols
		}
	}
	_, err = io.WriteString(w, renderReplayText(doc, o, width, color))
	return err
}
