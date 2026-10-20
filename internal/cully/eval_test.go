package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
