// Package evals scores how well Cully's memory retrieval returns the right
// prior work. Scoring is deterministic and needs no model judge.
//
// Metric definitions follow ContextBench (Li et al., 2026, arXiv:2602.05892),
// which scores a coding agent's retrieved context C^A against a gold context
// C^G: Recall = |C^A ∩ C^G| / |C^G|, Precision = |C^A ∩ C^G| / |C^A| and F1 as
// their harmonic mean. Here the unit is a memory note rather than a code
// region. Cases are grouped by the memory abilities LongMemEval (Wu et al.,
// ICLR 2025, arXiv:2410.10813) names: information extraction, multi-session
// reasoning, knowledge updates and abstention. Reciprocal rank is the standard
// information-retrieval rank measure. The dataset is small and written by the
// Cully team, so scores track regressions in this repository and are not
// comparable to published benchmark numbers.
package evals

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
)

//go:embed retrieval.json
var retrievalJSON []byte

// Abilities group cases the way LongMemEval does.
const (
	AbilityExtraction = "extraction"       // one earlier note holds the answer
	AbilityMultiNote  = "multi-note"       // the answer spans several notes
	AbilityUpdate     = "knowledge-update" // an older note was superseded; the newer must win
	AbilityAbstention = "abstention"       // nothing answers it; the right result is silence
)

// Modes a case can run in.
const (
	ModeText     = "text"     // cully_search: PostgreSQL full-text search
	ModeSemantic = "semantic" // cully_recall: Mem0 semantic recall
)

// Note is one seeded memory. Other marks a note that lives in a different
// project and must never appear in results scoped to the main project.
type Note struct {
	Key      string `json:"key"`
	Other    bool   `json:"other,omitempty"`
	AgeDays  int    `json:"age_days,omitempty"` // how long ago the note was recorded
	Summary  string `json:"summary"`
	Approach string `json:"approach,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Learning string `json:"learning,omitempty"`
}

// Case is one retrieval question. An empty Relevant list means nothing in
// memory answers it, so the correct result is silence.
type Case struct {
	ID       string   `json:"id"`
	Ability  string   `json:"ability"`
	Mode     string   `json:"mode"`
	Query    string   `json:"query"`
	Relevant []string `json:"relevant"`
}

type Dataset struct {
	Notes []Note `json:"notes"`
	Cases []Case `json:"cases"`
}

// Load returns the embedded retrieval dataset.
func Load() (Dataset, error) {
	var ds Dataset
	if err := json.Unmarshal(retrievalJSON, &ds); err != nil {
		return Dataset{}, err
	}
	return ds, ds.Validate()
}

// Validate rejects datasets whose cases name unknown or out-of-project notes.
func (ds Dataset) Validate() error {
	notes := map[string]Note{}
	for _, n := range ds.Notes {
		if n.Key == "" || n.Summary == "" {
			return fmt.Errorf("note needs key and summary: %+v", n)
		}
		if _, dup := notes[n.Key]; dup {
			return fmt.Errorf("duplicate note key %q", n.Key)
		}
		notes[n.Key] = n
	}
	ids := map[string]bool{}
	for _, c := range ds.Cases {
		if c.ID == "" || strings.TrimSpace(c.Query) == "" || ids[c.ID] {
			return fmt.Errorf("case needs a unique id and query: %+v", c)
		}
		ids[c.ID] = true
		if c.Mode != ModeText && c.Mode != ModeSemantic {
			return fmt.Errorf("case %s: unknown mode %q", c.ID, c.Mode)
		}
		switch c.Ability {
		case AbilityExtraction, AbilityMultiNote, AbilityUpdate, AbilityAbstention:
		default:
			return fmt.Errorf("case %s: unknown ability %q", c.ID, c.Ability)
		}
		if (c.Ability == AbilityAbstention) != (len(c.Relevant) == 0) {
			return fmt.Errorf("case %s: abstention cases, and only those, have no relevant notes", c.ID)
		}
		if (c.Ability == AbilityMultiNote) != (len(c.Relevant) > 1) {
			return fmt.Errorf("case %s: multi-note cases, and only those, have several relevant notes", c.ID)
		}
		for _, key := range c.Relevant {
			n, ok := notes[key]
			if !ok || n.Other {
				return fmt.Errorf("case %s: relevant note %q is missing or in another project", c.ID, key)
			}
		}
	}
	return nil
}

// Store is the memory surface an eval needs. The live test implements it over
// MCP; unit tests use an in-process fake.
type Store interface {
	Log(ctx context.Context, in memory.LogInput) (id string, err error)
	Search(ctx context.Context, mode string, in memory.SearchInput) ([]memory.Entry, error)
	Delete(ctx context.Context, id string) error
}

// CaseResult is the score for one case.
type CaseResult struct {
	ID, Mode  string
	Returned  []string // note keys in rank order
	Ability   string
	Recall    float64 // |returned ∩ relevant| / |relevant|
	Precision float64 // |returned ∩ relevant| / |returned|
	F1        float64
	RR        float64 // reciprocal rank of the first relevant note
	Leaked    int     // returned notes from another project
	Silent    bool    // the case expects no results
}

// Report aggregates results per mode.
type Report struct {
	K       int
	Results []CaseResult
}

// Summary is the mean score for one mode.
type Summary struct {
	Mode                                string
	Cases                               int
	Recall, Precision, F1, MRR, Silence float64
	Leaked                              int
	HasAnswerable, HasSilent            bool
}

// Run seeds the dataset under two fresh projects, scores every case and
// removes what it seeded. projectURL and otherURL must be distinct, unused
// projects so a failed earlier run cannot change the result.
func Run(ctx context.Context, store Store, ds Dataset, projectURL, otherURL, runTag string, k int) (Report, error) {
	if err := ds.Validate(); err != nil {
		return Report{}, err
	}
	ids := map[string]string{} // entry ID -> note key
	var created []string
	defer func() {
		for _, id := range created {
			_ = store.Delete(context.WithoutCancel(ctx), id)
		}
	}()
	for _, n := range ds.Notes {
		project := projectURL
		if n.Other {
			project = otherURL
		}
		in := memory.LogInput{Summary: n.Summary, Assistant: "other", Section: "personal", ProjectURL: &project, Tags: []string{"eval", runTag},
			OccurredAt: time.Now().Add(-time.Duration(n.AgeDays) * 24 * time.Hour).UTC().Format(time.RFC3339)}
		if n.Approach != "" {
			in.Approach = &n.Approach
		}
		if n.Outcome != "" {
			in.Outcome = &n.Outcome
		}
		if n.Learning != "" {
			in.Learning = &n.Learning
		}
		id, err := store.Log(ctx, in)
		if err != nil {
			return Report{}, fmt.Errorf("seed %s: %w", n.Key, err)
		}
		created = append(created, id)
		ids[id] = n.Key
	}
	other := map[string]bool{}
	for _, n := range ds.Notes {
		other[n.Key] = n.Other
	}
	report := Report{K: k}
	for _, c := range ds.Cases {
		entries, err := store.Search(ctx, c.Mode, memory.SearchInput{Query: c.Query, ProjectURL: &projectURL, Limit: k})
		if err != nil {
			return Report{}, fmt.Errorf("case %s: %w", c.ID, err)
		}
		var keys []string
		for _, e := range entries {
			if key, ok := ids[e.ID]; ok { // ignore records this run did not seed
				keys = append(keys, key)
			}
		}
		report.Results = append(report.Results, score(c, keys, other))
	}
	return report, nil
}

func score(c Case, returned []string, other map[string]bool) CaseResult {
	r := CaseResult{ID: c.ID, Mode: c.Mode, Ability: c.Ability, Returned: returned, Silent: len(c.Relevant) == 0}
	want := map[string]bool{}
	for _, key := range c.Relevant {
		want[key] = true
	}
	hits := 0
	for i, key := range returned {
		if other[key] {
			r.Leaked++
		}
		if want[key] {
			hits++
			if r.RR == 0 {
				r.RR = 1 / float64(i+1)
			}
		}
	}
	switch {
	case r.Silent:
		if len(returned) == 0 {
			r.Recall, r.Precision = 1, 1
		}
	default:
		r.Recall = float64(hits) / float64(len(want))
		if len(returned) > 0 {
			r.Precision = float64(hits) / float64(len(returned))
		}
	}
	if r.Precision+r.Recall > 0 {
		r.F1 = 2 * r.Precision * r.Recall / (r.Precision + r.Recall)
	}
	return r
}

// ByAbility returns the mean F1 of each ability, which is how LongMemEval
// breaks results down. Abstention cases score 1 for silence and 0 otherwise.
func (r Report) ByAbility() map[string]float64 {
	sum, n := map[string]float64{}, map[string]int{}
	for _, c := range r.Results {
		sum[c.Ability] += c.F1
		n[c.Ability]++
	}
	for ability := range sum {
		sum[ability] /= float64(n[ability])
	}
	return sum
}

// Summaries returns one Summary per mode, sorted by mode. Answerable cases
// feed recall, precision and MRR; silent cases feed the silence rate.
func (r Report) Summaries() []Summary {
	by := map[string]*Summary{}
	silent := map[string]int{}
	for _, c := range r.Results {
		s := by[c.Mode]
		if s == nil {
			s = &Summary{Mode: c.Mode}
			by[c.Mode] = s
		}
		s.Cases++
		s.Leaked += c.Leaked
		if c.Silent {
			s.HasSilent = true
			s.Silence += c.Recall
			silent[c.Mode]++
			continue
		}
		s.HasAnswerable = true
		s.Recall += c.Recall
		s.Precision += c.Precision
		s.F1 += c.F1
		s.MRR += c.RR
	}
	var out []Summary
	for mode, s := range by {
		if answerable := s.Cases - silent[mode]; answerable > 0 {
			n := float64(answerable)
			s.Recall, s.Precision, s.F1, s.MRR = s.Recall/n, s.Precision/n, s.F1/n, s.MRR/n
		}
		if silent[mode] > 0 {
			s.Silence /= float64(silent[mode])
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mode < out[j].Mode })
	return out
}

// Format renders a plain-text report for test logs.
func (r Report) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "retrieval eval (k=%d)\n", r.K)
	for _, s := range r.Summaries() {
		fmt.Fprintf(&b, "  %-8s cases=%d recall=%.2f precision=%.2f f1=%.2f mrr=%.2f abstention=%.2f leaked=%d\n", s.Mode, s.Cases, s.Recall, s.Precision, s.F1, s.MRR, s.Silence, s.Leaked)
	}
	abilities := r.ByAbility()
	names := make([]string, 0, len(abilities))
	for name := range abilities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  ability %-17s f1=%.2f\n", name, abilities[name])
	}
	for _, c := range r.Results {
		if c.Recall < 1 || c.Leaked > 0 {
			fmt.Fprintf(&b, "  miss %s/%s: returned %v recall=%.2f leaked=%d\n", c.Mode, c.ID, c.Returned, c.Recall, c.Leaked)
		}
	}
	return b.String()
}

// Thresholds are minimum acceptable scores for one mode. A zero value for a
// metric the mode has no cases for is ignored.
type Thresholds struct {
	Recall, MRR, Silence float64
	MaxLeaked            int
}

// Check returns an error naming every threshold the report misses.
func (r Report) Check(mode string, t Thresholds) error {
	for _, s := range r.Summaries() {
		if s.Mode != mode {
			continue
		}
		var failed []string
		if s.HasAnswerable && s.Recall < t.Recall {
			failed = append(failed, fmt.Sprintf("recall %.2f < %.2f", s.Recall, t.Recall))
		}
		if s.HasAnswerable && s.MRR < t.MRR {
			failed = append(failed, fmt.Sprintf("mrr %.2f < %.2f", s.MRR, t.MRR))
		}
		if s.HasSilent && s.Silence < t.Silence {
			failed = append(failed, fmt.Sprintf("silence %.2f < %.2f", s.Silence, t.Silence))
		}
		if s.Leaked > t.MaxLeaked {
			failed = append(failed, fmt.Sprintf("leaked %d > %d", s.Leaked, t.MaxLeaked))
		}
		if len(failed) > 0 {
			return fmt.Errorf("%s retrieval below threshold: %s", mode, strings.Join(failed, "; "))
		}
		return nil
	}
	return fmt.Errorf("no %s cases in report", mode)
}
