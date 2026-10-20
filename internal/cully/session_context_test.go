package cully

import (
	"fmt"
	"testing"
)

func readLine(path string) []byte {
	return []byte(fmt.Sprintf(`{"message":{"role":"assistant","content":[{"type":"tool_use","id":"r","name":"Read","input":{"file_path":%q}}]}}`, path))
}

func editLine(path string) []byte {
	return []byte(fmt.Sprintf(`{"message":{"role":"assistant","content":[{"type":"tool_use","id":"e","name":"Edit","input":{"file_path":%q}}]}}`, path))
}

// The expected values are worked out by hand from the paper's definitions.
func TestSessionContextMatchesContextBenchDefinitions(t *testing.T) {
	// Reads a, b, c, a again; edits a. Steps T=4.
	// Redundancy: t=2 b new, t=3 c new, t=4 a seen -> (0+0+1)/(T-1) = 1/3.
	// Gold {a}: A(1)={a} recall 1 and stays 1 -> AUC-Cov = 1. Precision = 1/3.
	early := measureSessionContext([][]byte{readLine("/a"), readLine("/b"), readLine("/c"), readLine("/a"), editLine("/a")})
	if early.Steps != 4 || early.Viewed != 3 || early.Edited != 1 || !near(early.Redundant, 1.0/3) || !near(early.AUCCov, 1) || !near(early.Recall, 1) || !near(early.Precision, 1.0/3) {
		t.Fatalf("early gold: %+v", early)
	}
	// Reads x, y, z; edits z. Recall by step is 0, 0, 1 -> AUC-Cov = 1/3: the
	// session reached the gold file late. No file is read twice.
	late := measureSessionContext([][]byte{readLine("/x"), readLine("/y"), readLine("/z"), editLine("/z")})
	if !near(late.AUCCov, 1.0/3) || late.Redundant != 0 || !near(late.Precision, 1.0/3) {
		t.Fatalf("late gold: %+v", late)
	}
	// Two gold files, one found: recall 1/2, AUC-Cov = (1/2+1/2)/2.
	partial := measureSessionContext([][]byte{readLine("/a"), readLine("/b"), editLine("/a"), editLine("/c")})
	if !near(partial.Recall, 0.5) || !near(partial.AUCCov, 0.5) {
		t.Fatalf("partial: %+v", partial)
	}
	// Without edits there is no gold context, so only redundancy is defined.
	none := measureSessionContext([][]byte{readLine("/a"), readLine("/a")})
	if none.AUCCov != 0 || none.Recall != 0 || !near(none.Redundant, 1) {
		t.Fatalf("no gold: %+v", none)
	}
	if empty := measureSessionContext(nil); empty != (SessionContext{}) {
		t.Fatalf("empty: %+v", empty)
	}
}
