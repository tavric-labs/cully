package cully

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The tasks suite scores how efficiently coding agents finish work with Cully
// around them: whether the task ended verified, how long it took, how many
// steps it needed, how much orientation came before the first edit, how much
// rework and looping happened. Success is reported together with time and step
// cost, because two agents that both finish can differ a lot in what they spend.
// The rules are checked against hand-worked sessions; your own history is
// reported as measurements and never fails a run, since it has no ground truth.

const (
	// A session that reads or searches this many times before its first edit
	// is still orienting. It matches the advisor's exploration checkpoint.
	taskOrientationBudget = 20
	// More failed checks than this in one task counts as heavy rework.
	taskReworkBudget = 3
	// Fewer sessions than this on either side of a change is not a comparison.
	taskCompareMin = 3
)

type taskMetrics struct {
	Duration                   time.Duration
	Steps, Orientation, Edits  int
	ChecksPassed, ChecksFailed int
	Loops                      int
	Verified                   bool // a known passing check came after the last edit
}

func computeTaskMetrics(events []journalEvent) taskMetrics {
	var m taskMetrics
	if len(events) == 0 {
		return m
	}
	stats := computeStats(events)
	m.Duration, m.Edits, m.ChecksPassed, m.ChecksFailed, m.Loops = stats.Duration, stats.Edits, stats.ChecksPassed, stats.ChecksFailed, stats.Loops
	edited, lastEdit, lastPass := false, -1, -1
	for i, e := range events {
		switch e.Class {
		case journalStart, journalEnd, journalNote:
			continue
		}
		m.Steps++
		switch e.Class {
		case journalEdit:
			if !e.Failed {
				edited, lastEdit = true, i
			}
		case journalSearch:
			if !edited {
				m.Orientation++
			}
		case journalCheck:
			if !e.Failed && !e.Unknown {
				lastPass = i
			}
		}
	}
	m.Verified = m.Edits > 0 && lastPass > lastEdit
	return m
}

// taskViolations names the rules a task broke. An unknown check outcome never
// counts as a pass, and a task with no edits cannot be unverified.
func taskViolations(m taskMetrics) []string {
	var out []string
	if m.Edits > 0 && !m.Verified {
		out = append(out, "unverified")
	}
	if m.Orientation > taskOrientationBudget {
		out = append(out, "long-orientation")
	}
	if m.ChecksFailed > taskReworkBudget {
		out = append(out, "rework")
	}
	if m.Loops > 0 {
		out = append(out, "loops")
	}
	return out
}

type taskSummary struct {
	Tasks, Verified                       int
	MedianDuration                        time.Duration
	MedianSteps, MedianOrientation, Loops int
	Violations                            map[string]int
}

func medianInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// summarizeTasks folds the tasks that changed code; sessions without an edit
// are conversation, not tasks.
func summarizeTasks(all []taskMetrics) taskSummary {
	s := taskSummary{Violations: map[string]int{}}
	var durations, steps, orientation []int
	for _, m := range all {
		if m.Edits == 0 {
			continue
		}
		s.Tasks++
		if m.Verified {
			s.Verified++
		}
		s.Loops += m.Loops
		durations = append(durations, int(m.Duration/time.Second))
		steps = append(steps, m.Steps)
		orientation = append(orientation, m.Orientation)
		for _, v := range taskViolations(m) {
			s.Violations[v]++
		}
	}
	s.MedianDuration = time.Duration(medianInt(durations)) * time.Second
	s.MedianSteps, s.MedianOrientation = medianInt(steps), medianInt(orientation)
	return s
}

// plantedTask builds a session, a minute per event: S read or search, E edit,
// P passing check, F failed check, G failed check of another command, ? check
// with no reported outcome.
func plantedTask(spec string) []journalEvent {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var out []journalEvent
	for i, c := range spec {
		ev := journalEvent{Time: start.Add(time.Duration(i) * time.Minute)}
		switch c {
		case 'S':
			ev.Class = journalSearch
		case 'E':
			ev.Class = journalEdit
		case 'P':
			ev.Class, ev.Sig = journalCheck, "a"
		case 'F':
			ev.Class, ev.Failed, ev.Sig = journalCheck, true, "a"
		case 'G':
			ev.Class, ev.Failed, ev.Sig = journalCheck, true, "b"
		case '?':
			ev.Class, ev.Unknown = journalCheck, true
		}
		out = append(out, ev)
	}
	return out
}

// taskScenarios have answers worked out by hand from the rules above.
var taskScenarios = []struct {
	name, spec string
	steps      int
	orient     int
	verified   bool
	duration   time.Duration
	want       []string
}{
	{"a verified task is clean", "SSEP", 4, 2, true, 3 * time.Minute, nil},
	{"edits never checked are unverified", "SSE", 3, 2, false, 2 * time.Minute, []string{"unverified"}},
	{"a failed check that is fixed and rechecked is verified", "SEFEP", 5, 1, true, 4 * time.Minute, nil},
	{"a check before the last edit does not verify it", "PE", 2, 0, false, time.Minute, []string{"unverified"}},
	{"an unreported check outcome does not verify", "SE?", 3, 1, false, 2 * time.Minute, []string{"unverified"}},
	{"twenty-one searches before the first edit is long orientation", strings.Repeat("S", 21) + "EP", 23, 21, true, 22 * time.Minute, []string{"long-orientation"}},
	{"four failed checks is heavy rework", "EFGFGP", 6, 0, true, 5 * time.Minute, []string{"rework"}},
	{"a command failing three times is a loop", "FFF", 3, 0, false, 2 * time.Minute, []string{"loops"}},
	{"searches after the first edit are not orientation", "SESSP", 5, 1, true, 4 * time.Minute, nil},
}

func evalTasks(o evalOptions) EvalSuite {
	return evalTasksWith(o, computeTaskMetrics, compareBeforeAfter)
}

func evalTasksWith(o evalOptions, compute func([]journalEvent) taskMetrics, compare func([]sessionTask, time.Time) (taskSummary, taskSummary, bool)) EvalSuite {
	s := EvalSuite{Name: "tasks"}
	// Detail is kept for failures only, so a passing run stays readable.
	check := func(name string, pass bool, format string, args ...any) {
		if pass {
			s.check(name, true, "")
			return
		}
		s.check(name, false, format, args...)
	}
	for _, sc := range taskScenarios {
		m := compute(plantedTask(sc.spec))
		got := taskViolations(m)
		pass := m.Steps == sc.steps && m.Orientation == sc.orient && m.Verified == sc.verified && m.Duration == sc.duration && strings.Join(got, ",") == strings.Join(sc.want, ",")
		check("tasks: "+sc.name, pass, "steps %d orientation %d verified %t duration %s rules %v", m.Steps, m.Orientation, m.Verified, m.Duration, got)
	}

	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	task := func(offsetDays int, minutes int, steps int, verified bool) sessionTask {
		return sessionTask{Project: "p", Start: at.Add(time.Duration(offsetDays) * 24 * time.Hour), Metrics: taskMetrics{Duration: time.Duration(minutes) * time.Minute, Steps: steps, Edits: 1, Verified: verified}}
	}
	sessions := []sessionTask{
		task(-3, 50, 60, false), task(-2, 30, 40, true), task(-1, 40, 50, true),
		task(1, 10, 20, true), task(2, 30, 30, true), task(3, 20, 25, true),
	}
	before, after, ok := compare(sessions, at)
	check("before/after: medians of similar work are computed exactly",
		ok && before.MedianDuration == 40*time.Minute && after.MedianDuration == 20*time.Minute && before.MedianSteps == 50 && after.MedianSteps == 25 && before.Verified == 2 && after.Verified == 3,
		"%v %+v %+v", ok, before, after)
	if _, _, ok := compare(sessions[:4], at); ok {
		check("before/after: too few sessions after a change make no claim", false, "compared with one session after")
	} else {
		check("before/after: too few sessions after a change make no claim", true, "")
	}
	_, _, ok = compare([]sessionTask{task(-3, 50, 60, true), task(-2, 30, 40, true), task(-1, 40, 50, true), task(1, 5, 5, true), task(2, 5, 5, true), {Project: "p", Start: at.Add(48 * time.Hour), Metrics: taskMetrics{Duration: time.Hour}}}, at)
	check("before/after: sessions without edits are not counted as tasks", !ok, "")

	sum := summarizeTasks([]taskMetrics{{Duration: time.Minute, Steps: 4, Edits: 1, Verified: true}, {Duration: 3 * time.Minute, Steps: 8, Edits: 2}, {Steps: 50}})
	check("summary counts only tasks that changed code", sum.Tasks == 2 && sum.Verified == 1 && sum.Violations["unverified"] == 1, "%+v", sum)

	replayTasks(&s, o)
	return s
}

// sessionTask is one recorded session with the project it belongs to.
type sessionTask struct {
	Project string
	Start   time.Time
	Metrics taskMetrics
}

// compareBeforeAfter summarizes the most recent tasks before a change and the
// earliest after it, in the same project. It reports ok=false unless both sides
// have enough tasks, so a thin sample never produces a verdict.
func compareBeforeAfter(sessions []sessionTask, at time.Time) (before, after taskSummary, ok bool) {
	var pre, post []sessionTask
	for _, t := range sessions {
		if t.Metrics.Edits == 0 {
			continue
		}
		if t.Start.Before(at) {
			pre = append(pre, t)
		} else {
			post = append(post, t)
		}
	}
	if len(pre) < taskCompareMin || len(post) < taskCompareMin {
		return taskSummary{}, taskSummary{}, false
	}
	sort.SliceStable(pre, func(i, j int) bool { return pre[i].Start.Before(pre[j].Start) })
	sort.SliceStable(post, func(i, j int) bool { return post[i].Start.Before(post[j].Start) })
	pre = pre[max(0, len(pre)-10):]
	post = post[:min(len(post), 10)]
	pick := func(list []sessionTask) []taskMetrics {
		out := make([]taskMetrics, len(list))
		for i, t := range list {
			out[i] = t.Metrics
		}
		return out
	}
	return summarizeTasks(pick(pre)), summarizeTasks(pick(post)), true
}

// loadSessionTasks reads every recorded session journal in the window. The
// project is the journal's key, which does not store a path.
func loadSessionTasks(since time.Time) []sessionTask {
	entries, _ := os.ReadDir(journalDir())
	var out []sessionTask
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		project, _, found := strings.Cut(name, "-")
		if !found {
			continue
		}
		events := readJournal(filepath.Join(journalDir(), name))
		if len(events) == 0 || events[0].Time.Before(since) {
			continue
		}
		out = append(out, sessionTask{Project: project, Start: events[0].Time, Metrics: computeTaskMetrics(events)})
	}
	return out
}

// appliedChange records when an accepted change was written, so later work can
// be compared with earlier work in the same project.
type appliedChange struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"` // project or self
	Project string    `json:"project,omitempty"`
	Summary string    `json:"summary,omitempty"`
}

func appliedChangesFile() string { return filepath.Join(cullyDir(), "applied-changes.jsonl") }

// recordAppliedChange never fails the caller: a bookkeeping error must not
// undo a change the user approved.
func recordAppliedChange(kind, project, summary string) {
	line, err := json.Marshal(appliedChange{At: time.Now().UTC(), Kind: kind, Project: project, Summary: clipText(summary, 200)})
	if err != nil {
		return
	}
	f, err := os.OpenFile(appliedChangesFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func readAppliedChanges(since time.Time) []appliedChange {
	f, err := os.Open(appliedChangesFile())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []appliedChange
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var c appliedChange
		if json.Unmarshal(sc.Bytes(), &c) == nil && !c.At.IsZero() && !c.At.Before(since) {
			out = append(out, c)
		}
	}
	return out
}

func signedPercent(before, after int64) string {
	if before == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+d%%", (after-before)*100/before)
}

// replayTasks reports measurements from the user's own history. Nothing here
// can fail a run: the history has no ground truth, and a before/after pair is
// uncontrolled, since the work differs between sessions.
func replayTasks(s *EvalSuite, o evalOptions) {
	days := o.days
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	sessions := loadSessionTasks(since)
	sum := summarizeTasks(func() []taskMetrics {
		out := make([]taskMetrics, len(sessions))
		for i, t := range sessions {
			out[i] = t.Metrics
		}
		return out
	}())
	if sum.Tasks == 0 {
		s.note("no recorded tasks with edits in the last %d days", days)
		return
	}
	s.note("%d tasks in the last %d days: %d%% verified, median %s and %d steps, %d orientation steps before the first edit, %d loops",
		sum.Tasks, days, sum.Verified*100/sum.Tasks, formatDuration(sum.MedianDuration), sum.MedianSteps, sum.MedianOrientation, sum.Loops)
	var rules []string
	for _, name := range []string{"unverified", "long-orientation", "rework", "loops"} {
		if n := sum.Violations[name]; n > 0 {
			rules = append(rules, fmt.Sprintf("%s %d", name, n))
		}
	}
	if len(rules) > 0 {
		s.note("rules broken (tasks): %s", strings.Join(rules, ", "))
	}
	changes := readAppliedChanges(since)
	for _, c := range changes {
		var scope []sessionTask
		for _, t := range sessions {
			if c.Kind == "self" || t.Project == c.Project {
				scope = append(scope, t)
			}
		}
		before, after, ok := compareBeforeAfter(scope, c.At)
		label := fmt.Sprintf("%s change %s (%s)", c.Kind, c.At.Local().Format("Jan 02"), c.Summary)
		if !ok {
			s.note("%s: not enough similar tasks yet (needs %d before and %d after)", label, taskCompareMin, taskCompareMin)
			continue
		}
		s.note("%s: median time %s → %s (%s), steps %d → %d (%s), verified %d/%d → %d/%d; uncontrolled, work differs between sessions",
			label, formatDuration(before.MedianDuration), formatDuration(after.MedianDuration),
			signedPercent(int64(before.MedianDuration), int64(after.MedianDuration)),
			before.MedianSteps, after.MedianSteps, signedPercent(int64(before.MedianSteps), int64(after.MedianSteps)),
			before.Verified, before.Tasks, after.Verified, after.Tasks)
	}
}
