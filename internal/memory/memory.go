// Package memory owns the shared Cully memory contract and validation.
package memory

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mcp-runtime/cully/internal/workspace"
)

var ErrInvalid = errors.New("invalid memory input")
var ErrUnavailable = errors.New("memory service unavailable")
var ErrForbidden = errors.New("insufficient permission")
var ErrConflict = errors.New("record version conflict")
var IST = time.FixedZone("Asia/Kolkata", 19800)

type Entry struct {
	ID             string    `json:"id"`
	Theme          string    `json:"theme"`
	Section        string    `json:"section"`
	ProjectURL     *string   `json:"project_url"`
	SessionRef     *string   `json:"session_ref"`
	AgentSessionID *string   `json:"agent_session_id"`
	Category       *string   `json:"category"`
	EntryType      string    `json:"entry_type"`
	Summary        string    `json:"summary"`
	Approach       *string   `json:"approach"`
	Outcome        *string   `json:"outcome"`
	Issue          *string   `json:"issue"`
	Learning       *string   `json:"learning"`
	NextSteps      *string   `json:"next_steps"`
	Assistant      string    `json:"assistant"`
	Tags           []string  `json:"tags"`
	OccurredAt     time.Time `json:"occurred_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (e *Entry) NormalizeTimes() {
	e.OccurredAt = e.OccurredAt.In(IST)
	e.CreatedAt = e.CreatedAt.In(IST)
	e.UpdatedAt = e.UpdatedAt.In(IST)
	if e.Tags == nil {
		e.Tags = []string{}
	}
}

type LogInput struct {
	Summary        string   `json:"summary"`
	Assistant      string   `json:"assistant"`
	Section        string   `json:"section"`
	ProjectURL     *string  `json:"project_url,omitempty"`
	SessionRef     *string  `json:"session_ref,omitempty"`
	AgentSessionID *string  `json:"agent_session_id,omitempty"`
	Category       *string  `json:"category,omitempty"`
	EntryType      string   `json:"entry_type,omitempty"`
	Approach       *string  `json:"approach,omitempty"`
	Outcome        *string  `json:"outcome,omitempty"`
	Issue          *string  `json:"issue,omitempty"`
	Learning       *string  `json:"learning,omitempty"`
	NextSteps      *string  `json:"next_steps,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	OccurredAt     string   `json:"occurred_at,omitempty"`
}
type SearchInput struct {
	Query      string  `json:"query,omitempty"`
	ProjectURL *string `json:"project_url,omitempty"`
	SessionRef *string `json:"session_ref,omitempty"`
	EntryType  *string `json:"entry_type,omitempty"`
	Section    *string `json:"section,omitempty"`
	Category   *string `json:"category,omitempty"`
	Since      string  `json:"since,omitempty"`
	Limit      int     `json:"limit,omitempty"`
}
type RecentInput struct {
	ProjectURL *string `json:"project_url,omitempty"`
	SessionRef *string `json:"session_ref,omitempty"`
	EntryType  *string `json:"entry_type,omitempty"`
	Section    *string `json:"section,omitempty"`
	Category   *string `json:"category,omitempty"`
	Since      string  `json:"since,omitempty"`
	Until      string  `json:"until,omitempty"`
	Limit      int     `json:"limit,omitempty"`
}
type IDInput struct {
	EntryID string `json:"entry_id"`
}
type UpdateInput struct {
	EntryID   string    `json:"entry_id"`
	Summary   *string   `json:"summary,omitempty"`
	Approach  *string   `json:"approach,omitempty"`
	Outcome   *string   `json:"outcome,omitempty"`
	Issue     *string   `json:"issue,omitempty"`
	Learning  *string   `json:"learning,omitempty"`
	NextSteps *string   `json:"next_steps,omitempty"`
	Section   *string   `json:"section,omitempty"`
	Category  *string   `json:"category,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
}
type ProjectsInput struct {
	Limit   int     `json:"limit,omitempty"`
	Section *string `json:"section,omitempty"`
}
type Project struct {
	ProjectURL   string    `json:"project_url"`
	Section      string    `json:"section"`
	EntryCount   int       `json:"entry_count"`
	LastActivity time.Time `json:"last_activity"`
}

// SessionInput starts or updates one agent session. A non-empty Task creates
// the session's task entry once and links it; repeating the same task keeps or relinks that record.
// An omitted Task preserves the current link; ClearTask unlinks it while
// keeping the task record. Task and ClearTask are mutually exclusive.
type SessionInput struct {
	SessionRef string  `json:"session_ref"`
	Assistant  string  `json:"assistant"`
	Section    string  `json:"section"`
	ProjectURL *string `json:"project_url,omitempty"`
	Branch     *string `json:"branch,omitempty"`
	Task       *string `json:"task,omitempty"`
	ClearTask  *bool   `json:"clear_task,omitempty"`
}
type SessionRefInput struct {
	SessionRef string `json:"session_ref"`
}
type Session struct {
	SessionRef string    `json:"session_ref"`
	Section    string    `json:"section"`
	ProjectURL *string   `json:"project_url"`
	Assistant  string    `json:"assistant"`
	Branch     *string   `json:"branch"`
	TaskID     *string   `json:"task_id"`
	Task       *string   `json:"task"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}
type Request struct {
	Workspace  *workspace.Input `json:"workspace,omitempty"`
	Operation  string           `json:"operation"`
	Session    *SessionInput    `json:"session,omitempty"`
	SessionGet *SessionRefInput `json:"session_get,omitempty"`
	Log        *LogInput        `json:"log,omitempty"`
	Search     *SearchInput     `json:"search,omitempty"`
	Recent     *RecentInput     `json:"recent,omitempty"`
	ID         *IDInput         `json:"id,omitempty"`
	Update     *UpdateInput     `json:"update,omitempty"`
	Projects   *ProjectsInput   `json:"projects,omitempty"`
}
type Result struct {
	Since     string            `json:"since,omitempty"` // verified recent-store bounds
	Until     string            `json:"until,omitempty"`
	Workspace *workspace.Result `json:"workspace,omitempty"`
	Session   *Session          `json:"session,omitempty"`
	Entry     *Entry            `json:"entry,omitempty"`
	Entries   []Entry           `json:"entries,omitempty"`
	Projects  []Project         `json:"projects,omitempty"`
	Deleted   bool              `json:"deleted,omitempty"`
}
type Repository interface {
	Execute(context.Context, string, Request) (Result, error)
}
type Service struct{ Store Repository }

func (s Service) Execute(ctx context.Context, owner string, r Request) (Result, error) {
	if strings.TrimSpace(owner) == "" || len(owner) > 512 {
		return Result{}, fmt.Errorf("%w: owner required", ErrInvalid)
	}
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.Store.Execute(ctx, owner, r)
}

var ownerRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var assistantRE = regexp.MustCompile(`(?i)^(codex|claude|cursor|chatgpt|other)(?:[- ][a-z0-9_.-]{1,40})?$`)
var sessionRefRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}-[0-9a-f]{16}$`)
var secrets = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`),
	regexp.MustCompile(`(?i)\b(?:password|secret|api[_-]?key|access[_-]?token)\s*[:=]\s*[^\s,;]{8,}`),
}

func NormalizeProject(value string) (string, error) {
	value = strings.TrimSpace(value)
	var parts []string
	if strings.HasPrefix(strings.ToLower(value), "git@github.com:") {
		parts = strings.Split(value[15:], "/")
	} else {
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", fmt.Errorf("%w: project_url must be a GitHub repository URL", ErrInvalid)
		}
		parts = strings.Split(strings.Trim(u.Path, "/"), "/")
	}
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: project_url must point to OWNER/REPO", ErrInvalid)
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	if !ownerRE.MatchString(parts[0]) || !repoRE.MatchString(parts[1]) {
		return "", fmt.Errorf("%w: invalid GitHub repository", ErrInvalid)
	}
	return "https://github.com/" + strings.ToLower(strings.Join(parts, "/")), nil
}
func text(v *string, required bool) error {
	if v == nil {
		return nil
	}
	*v = strings.TrimSpace(*v)
	if required && *v == "" {
		return fmt.Errorf("%w: summary is required", ErrInvalid)
	}
	if utf8.RuneCountInString(*v) > 8000 {
		return fmt.Errorf("%w: text exceeds 8000 characters", ErrInvalid)
	}
	for _, p := range secrets {
		if p.MatchString(*v) {
			return fmt.Errorf("%w: remove credentials before saving", ErrInvalid)
		}
	}
	return nil
}
func category(v *string) error {
	if v == nil {
		return nil
	}
	switch *v {
	case "", "career", "fitness", "relationship", "finance", "food", "water", "reading", "mood", "check-in", "other":
		return nil
	}
	return fmt.Errorf("%w: invalid category", ErrInvalid)
}

// taskNameLimit keeps task names short enough for the health panel row.
// The CLI truncates to the same limit in runes.
const taskNameLimit = 60

func section(v *string) error {
	if v != nil && *v != "personal" && *v != "company" {
		return fmt.Errorf("%w: section must be personal or company", ErrInvalid)
	}
	return nil
}
func entryType(v *string) error {
	if v != nil && *v != "work" && *v != "issue" && *v != "learning" && *v != "decision" && *v != "task" {
		return fmt.Errorf("%w: invalid entry_type", ErrInvalid)
	}
	return nil
}
func sessionRef(v *string) error {
	if v == nil {
		return nil
	}
	if !sessionRefRE.MatchString(*v) {
		return fmt.Errorf("%w: invalid session_ref", ErrInvalid)
	}
	return nil
}

var agentSessionIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func agentSessionID(v *string) error {
	if v != nil && !agentSessionIDRE.MatchString(*v) {
		return fmt.Errorf("%w: invalid agent_session_id", ErrInvalid)
	}
	return nil
}
func tags(v *[]string) error {
	if len(*v) > 20 {
		return fmt.Errorf("%w: at most 20 tags", ErrInvalid)
	}
	out := []string{}
	for _, t := range *v {
		t = strings.ToLower(strings.TrimSpace(t))
		if utf8.RuneCountInString(t) > 64 {
			return fmt.Errorf("%w: tag too long", ErrInvalid)
		}
		if t != "" {
			out = append(out, t)
		}
	}
	*v = out
	return nil
}
func project(v *string) error {
	if v == nil {
		return nil
	}
	p, e := NormalizeProject(*v)
	if e == nil {
		*v = p
	}
	return e
}
func stamp(v string) error {
	if v == "" {
		return nil
	}
	_, e := time.Parse(time.RFC3339, v)
	if e != nil {
		return fmt.Errorf("%w: timestamp requires RFC3339 with timezone offset", ErrInvalid)
	}
	return nil
}
func id(v string) error {
	if _, e := uuid.Parse(v); e != nil {
		return fmt.Errorf("%w: invalid entry_id", ErrInvalid)
	}
	return nil
}
func limit(v *int, max int) {
	if *v == 0 {
		*v = 20
	}
	if *v < 1 {
		*v = 1
	}
	if *v > max {
		*v = max
	}
}
func (r *Request) Validate() error {
	n := 0
	for _, b := range []bool{r.Workspace != nil, r.Log != nil, r.Search != nil, r.Recent != nil, r.ID != nil, r.Update != nil, r.Projects != nil, r.Session != nil, r.SessionGet != nil} {
		if b {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("%w: provide exactly one operation input", ErrInvalid)
	}
	if r.Workspace != nil {
		if r.Operation != "workspace" {
			return fmt.Errorf("%w: mismatched workspace input", ErrInvalid)
		}
		if err := r.Workspace.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		v := r.Workspace
		if v.SessionRef != "" {
			if err := sessionRef(&v.SessionRef); err != nil {
				return err
			}
		}
		texts := []string{v.Name, v.Principal, v.Repository, v.Agent, v.SessionRef, v.Branch, v.Checkpoint, v.NextStep, v.Revision, v.Artifact, v.Lesson, v.AppliesWhen, v.Limitations, v.Query}
		texts = append(texts, v.Criteria...)
		texts = append(texts, v.Steps...)
		for _, e := range v.Evidence {
			texts = append(texts, e.Check, e.Source, e.Revision)
		}
		for _, s := range texts {
			if err := text(&s, false); err != nil {
				return err
			}
		}
		return nil
	}
	checks := []error{}
	switch r.Operation {
	case "log":
		v := r.Log
		if v == nil {
			break
		}
		if v.EntryType == "" {
			v.EntryType = "work"
		}
		if v.EntryType == "task" {
			return fmt.Errorf("%w: save tasks through cully_session, not cully_log", ErrInvalid)
		}
		v.Assistant = strings.ToLower(strings.TrimSpace(v.Assistant))
		if !assistantRE.MatchString(v.Assistant) {
			return fmt.Errorf("%w: invalid assistant", ErrInvalid)
		}
		checks = append(checks, text(&v.Summary, true), section(&v.Section), category(v.Category), entryType(&v.EntryType), project(v.ProjectURL), sessionRef(v.SessionRef), agentSessionID(v.AgentSessionID), stamp(v.OccurredAt), tags(&v.Tags))
		for _, p := range []*string{v.Approach, v.Outcome, v.Issue, v.Learning, v.NextSteps} {
			checks = append(checks, text(p, false))
		}
	case "search", "recall":
		v := r.Search
		if v == nil {
			break
		}
		limit(&v.Limit, 50)
		checks = append(checks, text(&v.Query, false), section(v.Section), category(v.Category), entryType(v.EntryType), project(v.ProjectURL), sessionRef(v.SessionRef), stamp(v.Since))
		if v.Query == "" {
			return fmt.Errorf("%w: provide query text", ErrInvalid)
		}
		if r.Operation == "recall" && v.Query == "" {
			return fmt.Errorf("%w: recall requires query text", ErrInvalid)
		}
	case "recent":
		v := r.Recent
		if v == nil {
			break
		}
		limit(&v.Limit, 50)
		checks = append(checks, section(v.Section), category(v.Category), entryType(v.EntryType), project(v.ProjectURL), sessionRef(v.SessionRef), stamp(v.Since), stamp(v.Until))
		if v.Since != "" && v.Until != "" {
			since, e1 := time.Parse(time.RFC3339, v.Since)
			until, e2 := time.Parse(time.RFC3339, v.Until)
			if e1 == nil && e2 == nil && !since.Before(until) {
				return fmt.Errorf("%w: since must precede until", ErrInvalid)
			}
		}
	case "get", "delete":
		if r.ID == nil {
			break
		}
		checks = append(checks, id(r.ID.EntryID))
	case "update":
		v := r.Update
		if v == nil {
			break
		}
		checks = append(checks, id(v.EntryID), section(v.Section), category(v.Category), text(v.Summary, true))
		changed := v.Summary != nil || v.Section != nil || v.Category != nil || v.Tags != nil
		for _, p := range []*string{v.Approach, v.Outcome, v.Issue, v.Learning, v.NextSteps} {
			checks = append(checks, text(p, false))
			changed = changed || p != nil
		}
		if !changed {
			return fmt.Errorf("%w: provide a field to update", ErrInvalid)
		}
		if v.Tags != nil {
			checks = append(checks, tags(v.Tags))
		}
	case "session":
		v := r.Session
		if v == nil {
			break
		}
		v.Assistant = strings.ToLower(strings.TrimSpace(v.Assistant))
		if !assistantRE.MatchString(v.Assistant) {
			return fmt.Errorf("%w: invalid assistant", ErrInvalid)
		}
		checks = append(checks, sessionRef(&v.SessionRef), section(&v.Section), project(v.ProjectURL), text(v.Branch, false), text(v.Task, false))
		if v.Task != nil && utf8.RuneCountInString(*v.Task) > taskNameLimit {
			return fmt.Errorf("%w: task exceeds 60 characters", ErrInvalid)
		}
		if v.ClearTask != nil && *v.ClearTask && v.Task != nil && *v.Task != "" {
			return fmt.Errorf("%w: task and clear_task are mutually exclusive", ErrInvalid)
		}
		if v.Branch != nil && utf8.RuneCountInString(*v.Branch) > 200 {
			return fmt.Errorf("%w: branch exceeds 200 characters", ErrInvalid)
		}
	case "session_get":
		if r.SessionGet == nil {
			break
		}
		checks = append(checks, sessionRef(&r.SessionGet.SessionRef))
	case "projects":
		if r.Projects == nil {
			break
		}
		limit(&r.Projects.Limit, 100)
		checks = append(checks, section(r.Projects.Section))
	default:
		return fmt.Errorf("%w: unknown operation", ErrInvalid)
	}
	valid := (r.Operation == "log" && r.Log != nil) || ((r.Operation == "search" || r.Operation == "recall") && r.Search != nil) || (r.Operation == "recent" && r.Recent != nil) || ((r.Operation == "get" || r.Operation == "delete") && r.ID != nil) || (r.Operation == "update" && r.Update != nil) || (r.Operation == "projects" && r.Projects != nil) || (r.Operation == "session" && r.Session != nil) || (r.Operation == "session_get" && r.SessionGet != nil)
	if !valid {
		return fmt.Errorf("%w: mismatched operation input", ErrInvalid)
	}
	for _, e := range checks {
		if e != nil {
			return e
		}
	}
	return nil
}
