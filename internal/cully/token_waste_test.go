package cully

import (
	"strings"
	"testing"
)

func TestTokenWasteSeparatesFullReadsBashAndCully(t *testing.T) {
	w := newTokenWaste()
	big := strings.Repeat("x", 8000) // 2000 tokens
	w.useCall("r1", "Read", true)
	w.useCall("r2", "Read", false)
	w.useCall("b1", "Bash", false)
	w.useCall("c1", "mcp__cully__cully_log", false)
	w.result("r1", big)
	w.result("r2", big) // ranged read: big but not a whole-file read
	w.result("b1", big+big+big)
	w.result("c1", strings.Repeat("y", 400))
	w.result("unknown", big) // result of an unseen call is ignored
	w.finish(40000)
	if w.FullReads != 1 || w.FullReadTokens != 2000 || w.BigBash != 1 || w.BigBashTokens != 6000 || w.BigResults != 3 {
		t.Fatalf("%+v", w)
	}
	if w.TopTool != "Bash" || w.CullyTokens != 100 {
		t.Fatalf("top=%s cully=%d", w.TopTool, w.CullyTokens)
	}
	if !w.Significant() || !strings.Contains(w.String(), "token_waste_significant=yes") {
		t.Fatalf("should be significant: %s", w)
	}
}

func TestTokenWasteStaysQuietBelowThreshold(t *testing.T) {
	w := newTokenWaste()
	for _, id := range []string{"a", "b"} {
		w.useCall(id, "Read", true)
		w.result(id, strings.Repeat("x", 12000))
	}
	w.finish(200000)
	if w.Significant() || !strings.Contains(w.String(), "token_waste_significant=no") {
		t.Fatalf("two big results must not raise advice: %s", w)
	}
	// Many small results never count as big.
	small := newTokenWaste()
	for i := 0; i < 50; i++ {
		id := string(rune('A' + i))
		small.useCall(id, "Bash", false)
		small.result(id, strings.Repeat("x", 400))
	}
	small.finish(1000)
	if small.BigResults != 0 || small.Significant() {
		t.Fatalf("small results flagged: %+v", small)
	}
}

func TestAdvisorGetsTokenWasteSignalAndSingleSuggestionRule(t *testing.T) {
	sig := formatSignals(Signals{TokenWaste: "token_waste_significant=yes  big_results=4(12000 tok)"})
	if !strings.Contains(sig, "token_waste_significant=yes") {
		t.Fatalf("signal missing from advisor input: %s", sig)
	}
	for _, want := range []string{"token_waste_significant=yes", "at most one token optimization", "TOOLGAP"} {
		if !strings.Contains(sharedAdvisorInstructions, want) {
			t.Errorf("advisor instructions lost %q", want)
		}
	}
}

func TestTranscriptScansCountEachToolIDOnce(t *testing.T) {
	var lines [][]byte
	for _, line := range plantedTranscript(12000) {
		lines = append(lines, line, line) // every entry repeated, as some transcripts do
	}
	task := []byte(`{"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Task","input":{}}]}}`)
	once := scanWaste(append(plantedTranscript(12000), task))
	twice := scanWaste(append(lines, task, task))
	if once.BigResults != twice.BigResults || once.BigTokens != twice.BigTokens || once.ResultTokens != twice.ResultTokens || once.FullReads != twice.FullReads {
		t.Fatalf("duplicated entries were counted twice: %+v vs %+v", once, twice)
	}
	if once.BigResults != 12 {
		t.Fatalf("planted ground truth changed: %d", once.BigResults)
	}
	w := newTokenWaste()
	delegations := scanWasteLine(w, task) + scanWasteLine(w, task)
	if delegations != 1 {
		t.Fatalf("a repeated delegation counted %d times", delegations)
	}
}

func TestScanSessionCountsEachCullyCallOnce(t *testing.T) {
	use := `{"message":{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"mcp__cully__cully_log","input":{"summary":"abcdefgh"}}]}}`
	res := `{"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"saved note"}]}}`
	once := scanSession(strings.NewReader(use + "\n" + res + "\n"))
	twice := scanSession(strings.NewReader(use + "\n" + use + "\n" + res + "\n" + res + "\n"))
	if once.cullyCalls != 1 || twice.cullyCalls != 1 || twice.args != once.args || twice.result != once.result {
		t.Fatalf("repeated entries were counted twice: %+v vs %+v", once, twice)
	}
}
