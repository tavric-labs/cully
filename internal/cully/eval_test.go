package cully

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func failures(s EvalSuite) []string {
	var out []string
	for _, c := range s.Checks {
		if !c.Pass {
			out = append(out, c.Name+": "+c.Detail)
		}
	}
	return out
}

func TestEvalSuitesPassOnCurrentCode(t *testing.T) {
	for _, s := range []EvalSuite{evalSetup(), evalHarness(), evalAdvisor(evalOptions{dir: t.TempDir() + "/none"})} {
		if len(s.Checks) == 0 || s.Failed() != 0 {
			t.Errorf("%s suite: %d checks, failures %v", s.Name, len(s.Checks), failures(s))
		}
	}
}

// A check that cannot fail proves nothing, so feed the harness suite wrong
// ground truth and require it to complain.
func TestEvalHarnessFailsWhenTruthDisagrees(t *testing.T) {
	scenarios := harnessScenarios()
	wrong := scenarios[2] // permissions only: Guardrails exists
	wrong.name = "claims no guardrails"
	wrong.want = map[string]bool{}
	missed := scenarios[1] // every component
	missed.name = "claims a skill that was never written"
	missed.build = func(d harnessDirs) {}
	suite := evalHarnessScenarios([]harnessScenario{wrong, missed})
	if suite.Failed() != 2 {
		t.Fatalf("expected both wrong scenarios to fail, got %v", failures(suite))
	}
	if !strings.Contains(suite.Checks[0].Detail, "false alarm: Guardrails") || !strings.Contains(suite.Checks[1].Detail, "missed:") {
		t.Fatalf("details do not name the mistake: %+v", suite.Checks)
	}
}

// Each broken implementation is paired with the same known-good fixture. The
// evaluator itself must flip, rather than just returning a plausible score.
func TestEvalSetupDetectsBrokenInstaller(t *testing.T) {
	suite := evalSetupWithInstaller(func(endpoint string, oauth bool, targets ...string) error {
		if err := installAgents(endpoint, oauth, targets...); err != nil {
			return err
		}
		return os.Remove(filepath.Join(CodexConfigDir(), "skills", "cully", "SKILL.md"))
	})
	if !strings.Contains(strings.Join(failures(suite), "\n"), "codex skill is the canonical one") {
		t.Fatalf("broken installation passed: %+v", suite)
	}
}

func TestEvalHarnessDetectsEmptyAndAlwaysPresentAudits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audit func(harnessDirs) []harnessItem
	}{
		{"no audit results", func(harnessDirs) []harnessItem { return nil }},
		{"everything installed", func(d harnessDirs) []harnessItem {
			items := auditHarness(d)
			for i := range items {
				items[i].Found = []string{"invented"}
			}
			return items
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if s := evalHarnessWithAudit(harnessScenarios(), tc.audit); s.Failed() == 0 {
				t.Fatalf("broken audit passed: %+v", s)
			}
		})
	}
}

func TestEvalAdvisorDetectsSilentAndNoisyMonitors(t *testing.T) {
	o := evalOptions{dir: t.TempDir() + "/none"}
	for _, tc := range []struct {
		name  string
		scan  func([][]byte) *TokenWaste
		loops func([]journalEvent) loopReport
		want  string
	}{
		{"waste always zero", func([][]byte) *TokenWaste { return newTokenWaste() }, detectLoops, "counts the planted oversized output exactly"},
		{"waste always high", func([][]byte) *TokenWaste { return &TokenWaste{BigResults: 12, FullReads: 12, BigTokens: 36000} }, detectLoops, "counts no oversized output in an ordinary session"},
		{"loops always quiet", scanWaste, func([]journalEvent) loopReport { return loopReport{} }, "loops: the same command fails three times"},
		{"loops always active", scanWaste, func([]journalEvent) loopReport { return loopReport{Loops: []loopFinding{{Kind: loopRepeatFailure}}} }, "loops: a passing check resets the count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := evalAdvisorWithDetectors(o, tc.scan, tc.loops)
			if !strings.Contains(strings.Join(failures(s), "\n"), tc.want) {
				t.Fatalf("missed broken monitor: %v", failures(s))
			}
		})
	}
}

func TestEvalExplicitLiveRequestFailsWithoutCheckpoints(t *testing.T) {
	for _, dir := range []string{t.TempDir() + "/missing", t.TempDir()} {
		s := evalAdvisor(evalOptions{dir: dir, live: 1, days: 30, max: 5})
		if !strings.Contains(strings.Join(failures(s), "\n"), "live advisor ran") {
			t.Fatalf("unexecuted live evaluation passed: %+v", s)
		}
	}
}

func TestEvalIsolatesTheUsersHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	evalSetup()
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("eval wrote into the real home: %v", entries)
	}
	if os.Getenv("HOME") != home || os.Getenv("CLAUDE_CONFIG_DIR") != filepath.Join(home, ".claude") {
		t.Fatal("eval did not restore the environment")
	}
}

func TestGroundFigures(t *testing.T) {
	signals := "big_results=12(36000 tok)  full_file_reads=12(36000 tok)  top_output_tool=Read(41200 tok)"
	for _, tc := range []struct {
		advice         string
		cited, invalid int
	}{
		{"ADV|12 whole-file reads cost 36,000 tokens", 2, 0},
		{"ADV|about 36k tokens, mostly Read at 41k", 2, 0},
		{"ADV|saves 90,000 tokens", 1, 1},
		{"ADV|that is 63% of the output", 0, 0}, // derived shares are not checked
		{"ADV|use offset and limit, 3 reads", 0, 0},
		{"ADV|see https://example.com/issues/123456", 0, 0},
	} {
		cited, bad := groundFigures([]string{tc.advice}, signals)
		if cited != tc.cited || len(bad) != tc.invalid {
			t.Errorf("%q: cited %d ungrounded %v, want %d and %d", tc.advice, cited, bad, tc.cited, tc.invalid)
		}
	}
}

func TestParseEval(t *testing.T) {
	o, err := parseEval([]string{"advisor", "--live", "2", "harness"})
	if err != nil || o.live != 2 || len(o.suites) != 2 || !o.explicit {
		t.Fatalf("%+v %v", o, err)
	}
	if o, err = parseEval(nil); err != nil || len(o.suites) != len(evalSuites) || o.explicit {
		t.Fatalf("default suites: %+v %v", o, err)
	}
	for _, args := range [][]string{{"nonsense"}, {"memory"}} {
		if _, err := parseEval(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestEvalReplaysRealTranscriptFolders(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-Users-me-personal-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeLines(filepath.Join(dir, "s.jsonl"), plantedTranscript(12000)); err != nil {
		t.Fatal(err)
	}
	s := evalAdvisor(evalOptions{dir: root, project: "personal", days: 30, max: 5})
	joined := strings.Join(s.Notes, "\n")
	if !strings.Contains(joined, "replayed 1 sessions") || !strings.Contains(joined, "redundancy") || s.Failed() != 0 {
		t.Fatalf("notes %q failures %v", joined, failures(s))
	}
}

func TestEvalImprovePassesAndCatchesBrokenLoops(t *testing.T) {
	if s := evalImprove(); s.Failed() != 0 {
		t.Fatalf("real implementation failed: %v", failures(s))
	}
	for _, tc := range []struct {
		name     string
		update   func([]improvement, evalRun) []improvement
		sanitize func(applyPlan) (applyPlan, []string, error)
		want     string
	}{
		{"never proposes", func(l []improvement, _ evalRun) []improvement { return l }, sanitizeImprovePlan, "becomes one pending proposal"},
		{"duplicates every failure", func(l []improvement, r evalRun) []improvement {
			out := updateImprovements(l, r)
			return append(out, out...)
		}, sanitizeImprovePlan, "not proposed twice"},
		{"never resolves", func(l []improvement, r evalRun) []improvement {
			out := updateImprovements(l, r)
			for i := range out {
				if out[i].Status == "resolved" {
					out[i].Status = "applied"
				}
			}
			return out
		}, sanitizeImprovePlan, "marks its proposal resolved"},
		{"lets everything through", updateImprovements, func(p applyPlan) (applyPlan, []string, error) { return p, nil, nil }, "never kept"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := evalImproveWith(tc.update, tc.sanitize)
			if !strings.Contains(strings.Join(failures(s), "\n"), tc.want) {
				t.Fatalf("missed broken loop %q: %v", tc.name, failures(s))
			}
		})
	}
}

func TestRepeatSuiteFlagsFlakyEvaluators(t *testing.T) {
	stable := func() EvalSuite {
		s := EvalSuite{Name: "stable"}
		s.check("a", true, "")
		return s
	}
	if s := repeatSuite(stable, 3); s.Failed() != 0 || len(s.Checks) != 2 {
		t.Fatalf("stable suite: %+v", s)
	}
	calls := 0
	flaky := func() EvalSuite {
		calls++
		s := EvalSuite{Name: "flaky"}
		s.check("a", calls%2 == 1, "")
		return s
	}
	if s := repeatSuite(flaky, 2); !strings.Contains(strings.Join(failures(s), "\n"), "identical across 2 runs") {
		t.Fatalf("flaky suite passed: %+v", s)
	}
	skipped := func() EvalSuite { return EvalSuite{Name: "skipped", Skipped: "no server"} }
	if s := repeatSuite(skipped, 3); len(s.Checks) != 0 {
		t.Fatalf("skipped suite gained checks: %+v", s)
	}
	if _, err := parseEval([]string{"--repeat", "0"}); err == nil {
		t.Fatal("repeat 0 accepted")
	}
}

func TestEvalImproveSuiteIsStableAcrossRuns(t *testing.T) {
	if s := repeatSuite(evalImprove, 6); s.Failed() != 0 {
		t.Fatalf("improve suite is not deterministic: %v", failures(s))
	}
}

func TestEvalTasksPassesAndCatchesBrokenScorers(t *testing.T) {
	o := evalOptions{days: 30}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if s := evalTasks(o); s.Failed() != 0 {
		t.Fatalf("real implementation failed: %v", failures(s))
	}
	if s := repeatSuite(func() EvalSuite { return evalTasks(o) }, 4); s.Failed() != 0 {
		t.Fatalf("tasks suite is not deterministic: %v", failures(s))
	}
	allVerified := func(events []journalEvent) taskMetrics {
		m := computeTaskMetrics(events)
		m.Verified = m.Edits > 0
		return m
	}
	noOrientation := func(events []journalEvent) taskMetrics {
		m := computeTaskMetrics(events)
		m.Orientation = 0
		return m
	}
	alwaysCompares := func(sessions []sessionTask, at time.Time) (taskSummary, taskSummary, bool) {
		return summarizeTasks(nil), summarizeTasks(nil), true
	}
	for _, tc := range []struct {
		name    string
		compute func([]journalEvent) taskMetrics
		compare func([]sessionTask, time.Time) (taskSummary, taskSummary, bool)
		want    string
	}{
		{"everything verified", allVerified, compareBeforeAfter, "edits never checked are unverified"},
		{"no orientation counted", noOrientation, compareBeforeAfter, "twenty-one searches"},
		{"always claims a comparison", computeTaskMetrics, alwaysCompares, "medians of similar work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := evalTasksWith(o, tc.compute, tc.compare)
			if !strings.Contains(strings.Join(failures(s), "\n"), tc.want) {
				t.Fatalf("missed broken scorer %q: %v", tc.name, failures(s))
			}
		})
	}
}

func TestAppliedChangesAreRecordedAndComparedPerProject(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	recordAppliedChange("project", "abc", "Use a repo map")
	changes := readAppliedChanges(time.Now().Add(-time.Hour))
	if len(changes) != 1 || changes[0].Project != "abc" || changes[0].Kind != "project" {
		t.Fatalf("%+v", changes)
	}
	if len(readAppliedChanges(time.Now().Add(time.Hour))) != 0 {
		t.Fatal("age filter ignored")
	}
	// Journals for two projects around the change: only the changed project is compared.
	dir := journalDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	at := changes[0].At
	for i, offset := range []time.Duration{-3 * time.Hour, -2 * time.Hour, -time.Hour, time.Hour, 2 * time.Hour, 3 * time.Hour} {
		start := at.Add(offset)
		var lines []string
		for j, c := range []string{"E", "T"} {
			ev := journalEvent{Time: start.Add(time.Duration(j+1) * time.Minute * time.Duration(10+i)), Class: c}
			data, _ := json.Marshal(ev)
			lines = append(lines, string(data))
		}
		path := filepath.Join(dir, fmt.Sprintf("abc-s%d.jsonl", i))
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var scope []sessionTask
	for _, task := range loadSessionTasks(time.Now().Add(-24 * time.Hour)) {
		if task.Project == "abc" {
			scope = append(scope, task)
		}
	}
	if before, after, ok := compareBeforeAfter(scope, at); !ok || before.Tasks != 3 || after.Tasks != 3 {
		t.Fatalf("journals not compared: %v %+v %+v", ok, before, after)
	}
	var s EvalSuite
	replayTasks(&s, evalOptions{days: 30})
	if !strings.Contains(strings.Join(s.Notes, "\n"), "project change") {
		t.Fatalf("no before/after note: %v", s.Notes)
	}
}
