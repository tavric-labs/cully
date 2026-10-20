package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyCheckpointInHindsight(t *testing.T) {
	for _, tc := range []struct {
		advised bool
		future  int
		want    evalVerdict
	}{
		{true, 9000, verdictUseful}, {true, 100, verdictLowValue},
		{false, 9000, verdictMissed}, {false, 100, verdictCorrectQuiet},
	} {
		if got := classifyCheckpoint(tc.advised, tc.future); got != tc.want {
			t.Errorf("advised=%v future=%d: %s want %s", tc.advised, tc.future, got, tc.want)
		}
	}
}

func TestSummarizeEvalPrecisionAndRecall(t *testing.T) {
	ev := summarizeEval([]evalCheckpoint{
		{Verdict: verdictUseful}, {Verdict: verdictUseful}, {Verdict: verdictLowValue},
		{Verdict: verdictMissed}, {Verdict: verdictCorrectQuiet},
	}, 2)
	if ev.Precision < 0.66 || ev.Precision > 0.67 || ev.Recall < 0.66 || ev.Recall > 0.67 {
		t.Fatalf("precision=%v recall=%v", ev.Precision, ev.Recall)
	}
	if empty := summarizeEval(nil, 0); empty.Precision != 0 || empty.Recall != 0 {
		t.Fatal("empty eval must not divide by zero")
	}
}

// replayTranscript builds a session whose big reads all come late, so an early
// checkpoint is quiet and misses the waste that follows.
func replayTranscript(t *testing.T) string {
	t.Helper()
	big := strings.Repeat("x", 12000) // 3000 tokens
	var lines []string
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("t%d", i)
		lines = append(lines, svUse(id, "Grep"), svResult(id, "small"))
	}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("r%d", i)
		lines = append(lines, svUse(id, "Read"), svResult(id, big))
	}
	path := filepath.Join(t.TempDir(), "sess.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEvalSessionMissesLateWasteAndAdvisesOnceItShows(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := replayTranscript(t)
	points := evalSession(path, t.TempDir(), t.TempDir())
	if len(points) != 3 {
		t.Fatalf("checkpoints=%d", len(points))
	}
	first, last := points[0], points[2]
	if first.Percent != 30 || first.Advised || first.FutureWaste < futureWasteTokens || first.Verdict != verdictMissed {
		t.Fatalf("early checkpoint should miss the late waste: %+v", first)
	}
	if !last.Advised || last.SeenWaste == 0 {
		t.Fatalf("late checkpoint should see the waste and advise: %+v", last)
	}
	if !strings.Contains(last.Signals, "token_waste_significant=yes") {
		t.Fatalf("replayed signals must match what the live advisor receives: %s", last.Signals)
	}
}

func TestEvalSkipsShortSessionsAndReportWritesLiveScores(t *testing.T) {
	short := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(short, []byte(strings.Repeat(svUse("a", "Read")+"\n", 5)), 0o600)
	if got := evalSession(short, ".", t.TempDir()); got != nil {
		t.Fatal("a session too short to replay must be skipped")
	}
	ev := summarizeEval(nil, 0)
	ev.Live = []liveResult{{Session: "abcdef123456", Verdict: "useful", Lines: []string{"ADV|use offset/limit: full_file_reads=4(9000 tok)"}, HasEvidence: true, ToolGap: true, Researched: true, URL: true}}
	ev.LiveOK, ev.LiveWithEvidence, ev.LiveResearched = 1, 1, 1
	var out strings.Builder
	ev.Write(&out)
	for _, want := range []string{"ADVISOR REPLAY", "precision", "answered 1", "cited numbers 1", "ran research 1", "abcdef12", "full_file_reads=4"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	if !evidenceRe.MatchString("saves about 9k tokens") || evidenceRe.MatchString("consider being more careful") {
		t.Fatal("evidence detector must want numbers and ignore vague advice")
	}
}
