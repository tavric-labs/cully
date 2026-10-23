package memory

import (
	"errors"
	"strings"
	"testing"
)

func validOptimization() OptimizationSaveInput {
	return OptimizationSaveInput{PatternKey: "repeat-release-checks", Summary: "Repeat release checks", Evidence: "Repeated the same validation sequence in this window.", SessionRef: "codex-0123456789abcdef", WindowID: "prompts-1-12", Assistant: "codex"}
}

func TestOptimizationValidation(t *testing.T) {
	for _, change := range []func(*OptimizationSaveInput){
		func(v *OptimizationSaveInput) { v.SessionRef = "raw-session" },
		func(v *OptimizationSaveInput) { v.WindowID = "" },
		func(v *OptimizationSaveInput) { v.PatternKey = "private path/with spaces" },
		func(v *OptimizationSaveInput) { v.Evidence = "api_key=private-value-must-not-save" },
		func(v *OptimizationSaveInput) { v.Summary = strings.Repeat("x", 601) },
		func(v *OptimizationSaveInput) { v.Evidence = "" },
		func(v *OptimizationSaveInput) { v.Proposal = &OptimizationProposal{Kind: "execute"} },
		func(v *OptimizationSaveInput) {
			v.Proposal = &OptimizationProposal{Kind: "skill", Title: "Save checks", Benefit: "Avoid repeating setup", Steps: []string{"Check existing scripts"}, Validation: "Run fixture", Sources: []string{"https://user:password@example.org/paper"}}
		},
	} {
		v := validOptimization()
		change(&v)
		if !errors.Is(v.Validate(), ErrInvalid) {
			t.Fatalf("invalid optimization accepted: %+v", v)
		}
	}
	v := validOptimization()
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.Proposal = &OptimizationProposal{Kind: "harness", Title: "Evaluate a harness method", Benefit: "Potentially reduce repeated setup", Steps: []string{"Reproduce baseline", "Compare method on a held-out task"}, Sources: []string{"https://arxiv.org/abs/2401.00001"}, Validation: "Report task completion and time against baseline"}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	r := Request{Operation: "optimization_save", OptimizationSave: &v}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Log = &LogInput{Summary: "extra", Assistant: "codex"}
	if !errors.Is(r.Validate(), ErrInvalid) {
		t.Fatal("ambiguous input accepted")
	}
	q := OptimizationQueryInput{}
	if err := q.Validate(); err != nil || q.Limit != 3 || q.Status != "active" || q.Mode != "recent" {
		t.Fatalf("invalid defaults: %+v %v", q, err)
	}
	q.Mode = "semantic"
	if !errors.Is(q.Validate(), ErrInvalid) {
		t.Fatal("semantic query without text accepted")
	}
	decision := OptimizationDecideInput{EntryID: "00000000-0000-0000-0000-000000000001", Status: "dismissed"}
	if !errors.Is(decision.Validate(), ErrInvalid) {
		t.Fatal("unversioned decision accepted")
	}
}
