package evals

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mcp-runtime/cully/internal/memory"
)

// fakeStore matches every query word as a substring of the stored record.
// ignoreProject simulates a store that leaks other projects' notes.
type fakeStore struct {
	entries       []memory.Entry
	deleted       []string
	ignoreProject bool
}

func (f *fakeStore) Log(_ context.Context, in memory.LogInput) (string, error) {
	id := fmt.Sprintf("id-%d", len(f.entries))
	text := in.Summary
	for _, p := range []*string{in.Approach, in.Outcome, in.Learning} {
		if p != nil {
			text += " " + *p
		}
	}
	f.entries = append(f.entries, memory.Entry{ID: id, Summary: text, ProjectURL: in.ProjectURL})
	return id, nil
}

func (f *fakeStore) Search(_ context.Context, mode string, in memory.SearchInput) ([]memory.Entry, error) {
	if mode == ModeSemantic {
		return nil, nil
	}
	var out []memory.Entry
	for _, e := range f.entries {
		if !f.ignoreProject && *e.ProjectURL != *in.ProjectURL {
			continue
		}
		match := true
		for _, word := range strings.Fields(strings.ToLower(in.Query)) {
			match = match && strings.Contains(strings.ToLower(e.Summary), word)
		}
		if match {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) Delete(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func TestDatasetIsValid(t *testing.T) {
	ds, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]int{}
	silent := 0
	for _, c := range ds.Cases {
		modes[c.Mode]++
		if len(c.Relevant) == 0 {
			silent++
		}
	}
	if modes[ModeText] < 5 || modes[ModeSemantic] < 3 || silent == 0 {
		t.Fatalf("dataset too thin: %v silent=%d", modes, silent)
	}
}

func TestValidateRejectsBadCases(t *testing.T) {
	notes := []Note{{Key: "a", Summary: "x"}, {Key: "o", Other: true, Summary: "y"}}
	for name, c := range map[string]Case{
		"unknown note":  {ID: "1", Mode: ModeText, Query: "q", Relevant: []string{"zz"}},
		"other project": {ID: "1", Mode: ModeText, Query: "q", Relevant: []string{"o"}},
		"bad mode":      {ID: "1", Mode: "fuzzy", Query: "q"},
		"empty query":   {ID: "1", Mode: ModeText, Query: " "},
	} {
		if err := (Dataset{Notes: notes, Cases: []Case{c}}).Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRunScoresAndCleansUp(t *testing.T) {
	ds, _ := Load()
	store := &fakeStore{}
	report, err := Run(context.Background(), store, ds, "https://github.com/e/app", "https://github.com/e/other", "run1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != len(ds.Notes) {
		t.Fatalf("cleaned %d of %d seeded notes", len(store.deleted), len(ds.Notes))
	}
	// The fake finds text cases exactly, and has no semantic ability.
	if err := report.Check(ModeText, Thresholds{Recall: 1, MRR: 1, Silence: 1}); err != nil {
		t.Fatalf("%v\n%s", err, report.Format())
	}
	if err := report.Check(ModeSemantic, Thresholds{Recall: 0.1}); err == nil {
		t.Fatal("semantic misses passed a recall floor")
	}
}

func TestRunDetectsProjectLeak(t *testing.T) {
	ds, _ := Load()
	report, err := Run(context.Background(), &fakeStore{ignoreProject: true}, ds, "https://github.com/e/app", "https://github.com/e/other", "run2", 5)
	if err != nil {
		t.Fatal(err)
	}
	err = report.Check(ModeText, Thresholds{MaxLeaked: 0})
	if err == nil || !strings.Contains(err.Error(), "leaked") {
		t.Fatalf("leak not reported: %v", err)
	}
}

func TestScoreMetrics(t *testing.T) {
	other := map[string]bool{"o": true}
	got := score(Case{Relevant: []string{"a", "b"}}, []string{"x", "a", "o"}, other)
	if got.Recall != 0.5 || got.RR != 0.5 || got.Leaked != 1 || got.Precision < 0.33 || got.Precision > 0.34 {
		t.Fatalf("score = %+v", got)
	}
	if s := score(Case{}, nil, other); s.Recall != 1 || !s.Silent {
		t.Fatalf("silence = %+v", s)
	}
	if s := score(Case{}, []string{"a"}, other); s.Recall != 0 {
		t.Fatalf("noise on a silent case scored %+v", s)
	}
}
