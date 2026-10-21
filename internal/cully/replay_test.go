package cully

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var replayT0 = time.Date(2026, 10, 8, 10, 31, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return replayT0.Add(d) }

func TestMain(m *testing.M) {
	// Set the fixture timezone before tests start background panel workers.
	// Per-test writes race with time.Now in those workers.
	time.Local = time.UTC
	// Run in a clean Cully environment. From inside a Cully terminal, or an
	// agent Cully wraps, these are set, and some behavior depends on them: a
	// statusline prints nothing inside a pane, and measurements follow the agent.
	for _, name := range []string{"CULLY_PANE_SESSION", "CULLY_SESSION", "CULLY_AGENT", "MODEL_HINT_GUARD", "CULLY_MCP_PROBE_SESSION", "CULLY_MCP_METRICS_SESSION", "CULLY_MCP_ORIGIN"} {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

// replayFixture is a session with an explore stage, an implement stage, a
// failing check loop with fixes, a delete and a passing check.
func replayFixture() []journalEvent {
	const res = "internal/auth/resource.go"
	sig := "s1"
	return []journalEvent{
		{Time: at(0), Agent: "claude", Class: journalStart},
		{Time: at(5 * time.Second), Agent: "claude", Class: journalOther, Op: opRead, Path: res},
		{Time: at(10 * time.Second), Agent: "claude", Class: journalSearch},
		{Time: at(3 * time.Minute), Agent: "claude", Class: journalEdit, Op: opEdit, Path: res},
		{Time: at(3*time.Minute + 5*time.Second), Agent: "claude", Class: journalEdit, Op: opEdit, Path: res},
		{Time: at(4 * time.Minute), Agent: "claude", Class: journalEdit, Op: opCreate, Path: "internal/auth/resource_test.go"},
		{Time: at(11 * time.Minute), Agent: "claude", Class: journalCheck, Failed: true, Cmd: "go test", Sig: sig},
		{Time: at(12 * time.Minute), Agent: "claude", Class: journalEdit, Op: opEdit, Path: res},
		{Time: at(13 * time.Minute), Agent: "claude", Class: journalCheck, Failed: true, Cmd: "go test", Sig: sig},
		{Time: at(14 * time.Minute), Agent: "claude", Class: journalEdit, Op: opEdit, Path: res},
		{Time: at(15 * time.Minute), Agent: "claude", Class: journalCheck, Failed: true, Cmd: "go test", Sig: sig},
		{Time: at(15*time.Minute + 1*time.Second), Class: journalNote, Note: noteLoop},
		{Time: at(16 * time.Minute), Agent: "claude", Class: journalFileOp, Op: opDelete, Path: "old/helper.go"},
		{Time: at(17 * time.Minute), Agent: "claude", Class: journalCheck, Cmd: "go test", Sig: sig},
		{Time: at(18 * time.Minute), Agent: "claude", Class: journalMemory, Tool: "cully_log"},
		{Time: at(19 * time.Minute), Agent: "claude", Class: journalEnd},
	}
}

func TestSummarizeFiles(t *testing.T) {
	events := []journalEvent{
		{Class: journalEdit, Op: opCreate, Path: "new.go"},
		{Class: journalEdit, Op: opEdit, Path: "a.go"},
		{Class: journalEdit, Op: opWrite, Path: "a.go"},
		{Class: journalEdit, Op: opEdit, Path: "a.go"},
		{Class: journalEdit, Op: opEdit, Path: "b.go"},
		{Class: journalEdit, Op: opEdit, Path: "failed.go", Failed: true},
		{Class: journalFileOp, Op: opDelete, Path: "old.go"},
		{Class: journalFileOp, Op: opDelete, Path: "again.go"},
		{Class: journalFileOp, Op: opCreate, Path: "again.go"},
		{Class: journalOther, Op: opMove, Path: "from.go", To: "to.go"},
		{Class: journalOther, Op: opRead, Path: "a.go"},
		{Class: journalOther, Op: opRead, Path: "only.go"},
		{Class: journalOther, Op: opRead, Path: "only.go"},
	}
	s := summarizeFiles(events)
	if !s.Recorded {
		t.Fatal("not recorded")
	}
	if got := s.Edited; len(got) != 2 || got[0].Path != "a.go" || got[0].Count != 3 || got[1].Path != "b.go" {
		t.Errorf("edited = %+v", got)
	}
	if len(s.Hotspots) != 1 || s.Hotspots[0].Path != "a.go" {
		t.Errorf("hotspots = %+v", s.Hotspots)
	}
	if len(s.Created) != 2 || len(s.Deleted) != 2 || len(s.Moved) != 1 || s.Moved[0].To != "to.go" {
		t.Errorf("created/deleted/moved = %+v %+v %+v", s.Created, s.Deleted, s.Moved)
	}
	if len(s.ReadOnly) != 1 || s.ReadOnly[0].Path != "only.go" || s.ReadOnly[0].Count != 2 {
		t.Errorf("read only = %+v", s.ReadOnly)
	}
	if !reflect.DeepEqual(s.finalDeleted, []string{"old.go", "from.go"}) {
		t.Errorf("final deleted = %v", s.finalDeleted)
	}
	for _, e := range s.Edited {
		if e.Path == "failed.go" {
			t.Error("failed edit counted")
		}
	}
	if (summarizeFiles([]journalEvent{{Class: journalEdit}})).Recorded {
		t.Error("journal without paths must not be Recorded")
	}
}

func TestReconcileBothDirections(t *testing.T) {
	sum := summarizeFiles([]journalEvent{
		{Class: journalEdit, Op: opEdit, Path: "a.go"},
		{Class: journalEdit, Op: opCreate, Path: "pkg/new.go"},
		{Class: journalFileOp, Op: opDelete, Path: "gone.go"},
		{Class: journalFileOp, Op: opDelete, Path: "really-gone.go"},
		{Class: journalOther, Op: opRead, Path: "readonly.go"},
	})
	reader := func(cwd string) ([]gitEntry, bool) {
		return []gitEntry{{Path: "a.go"}, {Path: "sneaky.go"}, {Path: "pkg/", Dir: true}, {Path: "newdir/", Dir: true}, {Path: "readonly.go"}, {Path: "renamed.go", Orig: "a.go"}}, true
	}
	exists := func(p string) bool { return p == "gone.go" }
	r := reconcileWithGit(sum, reader, "/x", exists)
	if !r.Available || !r.Comparable {
		t.Fatalf("result = %+v", r)
	}
	if !reflect.DeepEqual(r.Unseen, []string{"newdir/", "pkg/", "readonly.go", "renamed.go", "sneaky.go"}) {
		t.Errorf("unseen = %v", r.Unseen)
	}
	if !reflect.DeepEqual(r.DeletedStillExist, []string{"gone.go"}) {
		t.Errorf("deleted but present = %v", r.DeletedStillExist)
	}
	if r := reconcileWithGit(sum, func(string) ([]gitEntry, bool) { return nil, false }, "/x", exists); r.Available {
		t.Error("git failure must be reported as unavailable")
	}
	if r := reconcileWithGit(fileSummary{}, reader, "/x", exists); !r.Available || r.Comparable || len(r.Unseen) != 0 {
		t.Errorf("no paths = %+v", r)
	}
}

func TestReadGitStatusInRealRepo(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := execGit(dir, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "y.go"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	entries, ok := readGitStatus(filepath.Join(dir, "sub"))
	if !ok || len(entries) != 1 || entries[0].Path != "x.go" {
		t.Fatalf("entries from subdirectory = %+v %v", entries, ok)
	}
	for _, name := range []string{"agent.go", "outside.go"} {
		if err := os.WriteFile(filepath.Join(dir, "sub", name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, ok = readGitStatus(filepath.Join(dir, "sub"))
	if !ok {
		t.Fatal("git status unavailable")
	}
	sum := summarizeFiles([]journalEvent{{Class: journalEdit, Op: opCreate, Path: "agent.go"}})
	reconciled := reconcileWithGit(sum, func(string) ([]gitEntry, bool) { return entries, true }, filepath.Join(dir, "sub"), nil)
	if !reflect.DeepEqual(reconciled.Unseen, []string{"outside.go", "x.go"}) {
		t.Fatalf("unrecorded files hidden by directory status: %+v", reconciled)
	}
	if _, ok := readGitStatus(t.TempDir()); ok {
		t.Error("non-repo must be unavailable")
	}
}

func TestReplayDelayCompressesGaps(t *testing.T) {
	cases := []struct {
		gap   time.Duration
		speed float64
		want  time.Duration
	}{
		{10 * time.Minute, 1, 1500 * time.Millisecond},
		{10 * time.Minute, 3, 500 * time.Millisecond},
		{time.Second, 1, time.Second},
		{time.Second, 2, 500 * time.Millisecond},
		{0, 1, 80 * time.Millisecond},
		{time.Second, 0, time.Second},
	}
	for _, c := range cases {
		if got := replayDelay(c.gap, c.speed); got != c.want {
			t.Errorf("replayDelay(%v,%v) = %v, want %v", c.gap, c.speed, got, c.want)
		}
	}
}

func TestBuildStepsAndFooter(t *testing.T) {
	steps := buildSteps(replayFixture())
	var lines []string
	for _, s := range steps {
		lines = append(lines, ansi.Strip(renderStepLine(s, false)))
	}
	want := []string{
		"10:31  Claude Code started",
		"10:31  Read     internal/auth/resource.go",
		"10:34  Edited   internal/auth/resource.go",
		"10:34  Edited   internal/auth/resource.go",
		"10:35  Created  internal/auth/resource_test.go",
		"10:42  Ran      go test  ✕ failed",
		"10:43  Edited   internal/auth/resource.go",
		"10:44  Ran      go test  ✕ failed",
		"10:45  Edited   internal/auth/resource.go",
		"10:46  Ran      go test  ✕ failed",
		"10:46  Cully noticed a loop: the same command failed 3 times",
		"10:47  Deleted  old/helper.go",
		"10:48  Ran      go test  ✓ passed",
		"10:49  Memory   cully_log",
		"10:50  Claude Code ended",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("steps:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	last := steps[len(steps)-1].Footer
	if last.Files != 3 || last.ChecksPassed != 1 || last.ChecksFailed != 3 || last.Loops != 1 {
		t.Errorf("footer = %+v", last)
	}
	// Reads do not count as touched files.
	if steps[1].Footer.Files != 0 {
		t.Errorf("read footer = %+v", steps[1].Footer)
	}
}

func TestUnnotedLoopIsStillShown(t *testing.T) {
	var events []journalEvent
	for i := 0; i < 3; i++ {
		events = append(events, journalEvent{Time: at(time.Duration(i) * time.Minute), Class: journalCheck, Failed: true, Cmd: "go test", Sig: "z"})
	}
	steps := buildSteps(events)
	if last := steps[len(steps)-1]; last.Kind != "loop" || last.Footer.Loops != 1 {
		t.Fatalf("steps = %+v", steps)
	}
}

func fixtureDoc(t *testing.T) replayDoc {
	t.Helper()
	meta := replayMeta{Session: "sess-1", Project: "demo", Branch: "main"}
	git := func(string) ([]gitEntry, bool) {
		return []gitEntry{{Path: "internal/auth/resource.go"}, {Path: "stray.txt"}}, true
	}
	return buildReplay(replayFixture(), meta, git, "/x", func(p string) bool { return p == "old/helper.go" })
}

func TestInstantTranscriptGraphAndSummary(t *testing.T) {
	doc := fixtureDoc(t)
	out := renderReplayText(doc, replayOptions{}, 100, false)
	for _, want := range []string{
		"Replay · Claude Code · demo:main · 2026-10-08 10:31 · 19m",
		"Edited   internal/auth/resource.go", "Created  internal/auth/resource_test.go",
		"Ran      go test", "failed", "passed",
		"Summary:", "FILE ACTIVITY", "Created (1)", "Edited (1)", "Deleted (1)",
		"internal/auth/resource.go  edited ×4", "Hotspots", "RECONCILE",
		"Changed outside the agent's recorded tool calls:", "? stray.txt",
		"Recorded as deleted, but still present:", "! old/helper.go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Error("plain output has ANSI")
	}
	steps := renderReplayText(doc, replayOptions{Steps: true}, 100, false)
	if !strings.Contains(steps, "10:34  Edited   internal/auth/resource.go") || strings.Contains(steps, "┌") {
		t.Errorf("steps transcript = %s", steps)
	}
	files := renderReplayText(doc, replayOptions{FilesOnly: true}, 100, false)
	if strings.Contains(files, "Replay ·") || !strings.Contains(files, "FILE ACTIVITY") || !strings.Contains(files, "RECONCILE") {
		t.Errorf("files-only = %s", files)
	}
	colored := renderReplayText(doc, replayOptions{Steps: true}, 100, true)
	if !strings.Contains(colored, "\x1b[") {
		t.Error("color output has no ANSI")
	}
}

func TestPartialDataIsStated(t *testing.T) {
	noPaths := []journalEvent{
		{Time: at(0), Class: journalStart, Agent: "claude"},
		{Time: at(time.Minute), Class: journalEdit},
		{Time: at(2 * time.Minute), Class: journalCheck},
	}
	doc := buildReplay(noPaths, replayMeta{Session: "s"}, func(string) ([]gitEntry, bool) { return []gitEntry{{Path: "x"}}, true }, "/x", nil)
	out := renderReplayText(doc, replayOptions{Steps: true}, 100, false)
	for _, want := range []string{"CULLY_JOURNAL_PATHS", "Edited   a file (name not recorded)", "No file activity was recorded", "cannot be compared with git status"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Unseen") || strings.Contains(out, "?") {
		t.Errorf("must not claim changes outside the record without paths:\n%s", out)
	}
	hooks := buildReplay([]journalEvent{{Time: at(0), Class: journalStart}}, replayMeta{}, nil, "", nil)
	if !strings.Contains(renderReplayText(hooks, replayOptions{}, 100, false), "cully setup") {
		t.Error("missing hooks hint")
	}
}

func TestReplayJSONGolden(t *testing.T) {
	doc := fixtureDoc(t)
	got, err := renderReplayJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "replay.golden.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes.TrimSpace(want)) != string(got) {
		t.Fatalf("JSON differs from %s (run with UPDATE_GOLDEN=1 if intended)\n%s", golden, got)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil || parsed["schema_version"] != float64(2) || parsed["session"] != "sess-1" || parsed["stages"] != nil {
		t.Fatalf("json = %v %v", parsed, err)
	}
}

func TestRunReplayEndToEndAndPrivacy(t *testing.T) {
	cwd := journalTestEnv(t)
	secret := "TOPSECRET-BODY"
	recordJournalTool("claude", "s1", makeToolEvent(cwd, "Write", map[string]string{"file_path": "a.go", "content": secret}), 'E', false)
	recordJournalTool("claude", "s1", makeToolEvent(cwd, "Bash", map[string]string{"command": "go test -run " + secret + " ./..."}), 'T', true)
	recordJournalTool("claude", "s1", makeToolEvent(cwd, "Bash", map[string]string{"command": "rm old.go"}), 'O', false)
	for _, args := range [][]string{{"--cwd", cwd}, {"--cwd", cwd, "--steps"}, {"--cwd", cwd, "--files"}, {"--cwd", cwd, "--json"}, {"--cwd", cwd, "--instant"}} {
		var out bytes.Buffer
		if err := RunReplay(&out, args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		s := out.String()
		for _, leak := range []string{secret, "-run", "./..."} {
			if strings.Contains(s, leak) {
				t.Errorf("%v leaked %q:\n%s", args, leak, s)
			}
		}
		if !strings.Contains(s, "a.go") || (!strings.Contains(s, "go test") && args[len(args)-1] != "--files") {
			t.Errorf("%v missing recorded activity:\n%s", args, s)
		}
		if strings.Contains(s, "\x1b") {
			t.Errorf("%v: ANSI to non-terminal", args)
		}
	}
	var out bytes.Buffer
	if err := RunReplay(&out, []string{"--cwd", cwd, "--session", "nope"}); err == nil {
		t.Error("unknown session must fail")
	}
	out.Reset()
	if err := RunReplay(&out, []string{"--cwd", t.TempDir()}); err != nil || !strings.Contains(out.String(), "No session recorded") {
		t.Errorf("no journal = %q %v", out.String(), err)
	}
	if err := RunReplay(&out, []string{"--speed", "0"}); err == nil {
		t.Error("speed 0 must fail")
	}
	if err := RunReplay(&out, []string{"extra"}); err == nil {
		t.Error("positional args must fail")
	}
}

func TestParseReplayArgsHTML(t *testing.T) {
	for _, c := range []struct {
		args []string
		html bool
		path string
	}{
		{[]string{"--html"}, true, ""},
		{[]string{"--html", "out.html", "--json"}, true, "out.html"},
		{[]string{"--html", "--json"}, true, ""},
		{[]string{"--html=x.html"}, true, "x.html"},
		{[]string{"--steps", "--speed", "2"}, false, ""},
	} {
		o, err := parseReplayArgs(c.args)
		if err != nil || o.HTML != c.html || o.HTMLPath != c.path {
			t.Errorf("%v = %+v %v", c.args, o, err)
		}
	}
}

func TestHandoffListsRecordedFileActivity(t *testing.T) {
	events := replayFixture()
	text := buildHandoff(handoffInput{Events: events})
	for _, want := range []string{"## Current state", "- Recorded file activity: 1 created, 1 edited, 1 deleted", "  - internal/auth/resource.go (edited ×4)", "  - old/helper.go (deleted)"} {
		if !strings.Contains(text, want) {
			t.Errorf("handoff missing %q\n%s", want, text)
		}
	}
	if strings.Contains(buildHandoff(handoffInput{Events: []journalEvent{{Class: journalEdit}}}), "Recorded file activity") {
		t.Error("handoff must not invent file activity")
	}
}

func execGit(dir string, args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", dir}, args...)...)
}
