package memory

import (
	"errors"
	"strings"
	"testing"
)

func validOptimization() OptimizationSaveInput {
	return OptimizationSaveInput{PatternKey: "repeat-release-checks", Summary: "Repeat release checks", Evidence: "Repeated the same validation sequence in this window.", SessionRef: "codex-0123456789abcdef", WindowID: "prompts-1-12", Assistant: "codex"}
}

func TestOptimizationRisksAndUndo(t *testing.T) {
	base := func() *OptimizationProposal {
		return &OptimizationProposal{Kind: "skill", Title: "Save checks", Benefit: "Avoid repeating setup", Steps: []string{"Check existing scripts"}, Validation: "Run fixture"}
	}
	v := validOptimization()
	v.Proposal = base()
	if err := v.Validate(); err != nil {
		t.Fatalf("a proposal saved without risks or undo must stay valid: %v", err)
	}
	if strings.Contains(v.Proposal.Text(), "Risks:") || strings.Contains(v.Proposal.Text(), "Undo:") {
		t.Fatal("absent risks or undo must not be rendered")
	}
	v.Proposal = base()
	v.Proposal.Risks, v.Proposal.Undo = "Adds a skill the agent may over-apply", "Delete the project skill folder"
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	text := v.Proposal.Text()
	if !strings.Contains(text, "Risks: Adds a skill") || !strings.Contains(text, "Undo: Delete the project skill folder") {
		t.Fatalf("risks and undo missing from the plan text: %q", text)
	}
	for _, change := range []func(*OptimizationProposal){
		func(p *OptimizationProposal) { p.Risks = strings.Repeat("x", 601) },
		func(p *OptimizationProposal) { p.Undo = strings.Repeat("x", 601) },
		func(p *OptimizationProposal) { p.Risks = "api_key=private-value-must-not-save" },
	} {
		v := validOptimization()
		v.Proposal = base()
		change(v.Proposal)
		if !errors.Is(v.Validate(), ErrInvalid) {
			t.Fatalf("invalid risks or undo accepted: %+v", v.Proposal)
		}
	}
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
