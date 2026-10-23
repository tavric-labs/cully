package memory

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Optimization records are private source notes, not instructions to execute.
// Counts come from distinct, idempotent observations rather than model claims.
type Optimization struct {
	PatternKey       string                 `json:"pattern_key"`
	Status           string                 `json:"status"`
	Version          int                    `json:"version"`
	ObservationCount int                    `json:"observation_count"`
	SessionCount     int                    `json:"session_count"`
	FirstSeen        time.Time              `json:"first_seen"`
	LastSeen         time.Time              `json:"last_seen"`
	Evidence         []OptimizationEvidence `json:"evidence,omitempty"`
	Proposal         *OptimizationProposal  `json:"proposal,omitempty"`
	Decision         string                 `json:"decision,omitempty"`
}

type OptimizationEvidence struct {
	SessionRef     string    `json:"session_ref"`
	AgentSessionID *string   `json:"agent_session_id,omitempty"`
	WindowID       string    `json:"window_id"`
	Summary        string    `json:"summary"`
	ObservedAt     time.Time `json:"observed_at"`
}

type OptimizationProposal struct {
	Kind       string   `json:"kind"` // skill, workflow, plugin, tool, process, or harness
	Title      string   `json:"title"`
	Benefit    string   `json:"benefit"`
	Steps      []string `json:"steps"`
	Sources    []string `json:"sources,omitempty"`
	Validation string   `json:"validation"`
}

func (p OptimizationProposal) Text() string {
	return p.Title + "\n" + p.Benefit + "\n- " + strings.Join(p.Steps, "\n- ") + "\nValidation: " + p.Validation + "\nSources: " + strings.Join(p.Sources, " ")
}

type OptimizationSaveInput struct {
	PatternKey     string                `json:"pattern_key"`
	Summary        string                `json:"summary"`
	Evidence       string                `json:"evidence"`
	SessionRef     string                `json:"session_ref"`
	AgentSessionID *string               `json:"agent_session_id,omitempty"`
	WindowID       string                `json:"window_id"`
	Assistant      string                `json:"assistant"`
	Proposal       *OptimizationProposal `json:"proposal,omitempty"`
}

type OptimizationQueryInput struct {
	Query  string `json:"query,omitempty"`
	Mode   string `json:"mode,omitempty"`   // text, semantic, recent
	Status string `json:"status,omitempty"` // active, all, observing, proposed, accepted, dismissed, implemented
	Limit  int    `json:"limit,omitempty"`
}

type OptimizationDecideInput struct {
	EntryID string `json:"entry_id"`
	Version int    `json:"version"`
	Status  string `json:"status"` // accepted, dismissed, implemented, or observing (explicit reopen)
	Reason  string `json:"reason,omitempty"`
}

var optimizationKeyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

func boundedOptimizationText(v *string, required bool, max int) error {
	if err := text(v, required); err != nil {
		return err
	}
	if utf8.RuneCountInString(*v) > max {
		return fmt.Errorf("%w: optimization text exceeds %d characters", ErrInvalid, max)
	}
	return nil
}

func (v *OptimizationSaveInput) Validate() error {
	v.Assistant = strings.ToLower(strings.TrimSpace(v.Assistant))
	if !optimizationKeyRE.MatchString(v.PatternKey) || !optimizationKeyRE.MatchString(v.WindowID) || !assistantRE.MatchString(v.Assistant) {
		return fmt.Errorf("%w: invalid pattern key, window id or assistant", ErrInvalid)
	}
	checks := []error{sessionRef(&v.SessionRef), agentSessionID(v.AgentSessionID), boundedOptimizationText(&v.Summary, true, 600), boundedOptimizationText(&v.Evidence, true, 1000)}
	if p := v.Proposal; p != nil {
		switch p.Kind {
		case "skill", "workflow", "plugin", "tool", "process", "harness":
		default:
			return fmt.Errorf("%w: invalid optimization kind", ErrInvalid)
		}
		if len(p.Steps) < 1 || len(p.Steps) > 12 || len(p.Sources) > 5 {
			return fmt.Errorf("%w: provide 1-12 steps and at most 5 sources", ErrInvalid)
		}
		checks = append(checks, boundedOptimizationText(&p.Title, true, 160), boundedOptimizationText(&p.Benefit, true, 600), boundedOptimizationText(&p.Validation, true, 600))
		for i := range p.Steps {
			checks = append(checks, boundedOptimizationText(&p.Steps[i], true, 600))
		}
		for i := range p.Sources {
			checks = append(checks, boundedOptimizationText(&p.Sources[i], true, 600))
			u, err := url.Parse(p.Sources[i])
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
				return fmt.Errorf("%w: proposal sources require HTTPS without credentials", ErrInvalid)
			}
		}
		plan := p.Text()
		checks = append(checks, text(&plan, false))
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

func (v *OptimizationQueryInput) Validate() error {
	if v.Mode == "" {
		v.Mode = "text"
		if strings.TrimSpace(v.Query) == "" {
			v.Mode = "recent"
		}
	}
	if v.Status == "" {
		v.Status = "active"
	}
	switch v.Status {
	case "active", "all", "observing", "proposed", "accepted", "dismissed", "implemented":
	default:
		return fmt.Errorf("%w: invalid optimization status", ErrInvalid)
	}
	if v.Mode != "text" && v.Mode != "semantic" && v.Mode != "recent" {
		return fmt.Errorf("%w: invalid optimization query mode", ErrInvalid)
	}
	if err := boundedOptimizationText(&v.Query, v.Mode != "recent", 600); err != nil {
		return err
	}
	if v.Mode == "recent" && v.Query != "" {
		return fmt.Errorf("%w: recent mode does not use query", ErrInvalid)
	}
	if v.Limit == 0 {
		v.Limit = 3
	}
	limit(&v.Limit, 10)
	return nil
}

func (v *OptimizationDecideInput) Validate() error {
	if err := id(v.EntryID); err != nil {
		return err
	}
	if v.Version < 1 {
		return fmt.Errorf("%w: current optimization version required", ErrInvalid)
	}
	switch v.Status {
	case "accepted", "dismissed", "implemented", "observing":
	default:
		return fmt.Errorf("%w: invalid optimization decision", ErrInvalid)
	}
	return boundedOptimizationText(&v.Reason, false, 600)
}

func OptimizationMatches(e Entry, status string) bool {
	if e.Optimization == nil || e.EntryType != "optimization" {
		return false
	}
	return status == "all" || e.Optimization.Status == status || status == "active" && (e.Optimization.Status == "observing" || e.Optimization.Status == "proposed")
}
