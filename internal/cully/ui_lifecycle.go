package cully

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Lifecycle for agents and proposals. Both move through active, stale and archived:
//
//   - An agent that has not run for 30 days is stale. A proposal nobody acted on for 14 days is stale and
//     is archived on its own.
//   - Archived things are hidden by default and never deleted; you can bring one back.
//   - A pinned agent or proposal is never stale and is never archived on its own.

const (
	uiStaleAgentDays    = 30
	uiStaleProposalDays = 14
)

func (id uiIdentity) lifecycle(now time.Time) string {
	switch {
	case id.Archived:
		return "archived"
	case id.Pinned || !id.Approved:
		return "active"
	}
	last := id.LastRun
	if last.IsZero() {
		last = id.Created
	}
	if !last.IsZero() && now.Sub(last) > uiStaleAgentDays*24*time.Hour {
		return "stale"
	}
	return "active"
}

// uiIdentitiesByLifecycle keeps the archived agents or, by default, everything else.
func uiIdentitiesByLifecycle(in []uiIdentity, archived bool) []uiIdentity {
	out := []uiIdentity{}
	for _, a := range in {
		if a.Archived == archived {
			out = append(out, a)
		}
	}
	return out
}

// identitySetLifecycle pins or archives an agent. Archiving drops the pin and stops its schedule from
// starting runs; nothing is deleted. Pinning an archived agent brings it back.
func (m *uiManager) identitySetLifecycle(id, what string, on bool) (uiIdentity, error) {
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	for _, x := range m.loadIdentitiesLocked() {
		if x.ID != id {
			continue
		}
		switch what {
		case "pin":
			x.Pinned = on
			if on {
				x.Archived = false
			}
		case "archive":
			x.Archived = on
			if on {
				x.Pinned = false
			}
		default:
			return uiIdentity{}, errors.New("unknown change")
		}
		m.saveIdentitiesLocked()
		c := x.clone()
		c.Lifecycle = c.lifecycle(time.Now())
		return c, nil
	}
	return uiIdentity{}, errors.New("no such agent")
}

// proposalsCurate archives proposals nobody acted on for 14 days, unless pinned. It returns how many.
func (m *uiManager) proposalsCurate() int {
	return m.proposalsCurateAt(time.Now())
}

func (m *uiManager) proposalsCurateAt(now time.Time) int {
	cutoff := now.Add(-uiStaleProposalDays * 24 * time.Hour)
	n := 0
	uiSuggestMu.Lock()
	for _, s := range uiReadAgentSuggestions() {
		if s.State == "new" && !s.Pinned && !s.Created.IsZero() && s.Created.Before(cutoff) {
			s.State = "archived"
			uiDB().put("agentsuggest", s.ID, s)
			n++
		}
	}
	uiSuggestMu.Unlock()
	uiJobSuggestMu.Lock()
	for _, s := range uiReadJobSuggestions() {
		if s.State == "new" && !s.Pinned && !s.Created.IsZero() && s.Created.Before(cutoff) {
			s.State = "archived"
			uiDB().put("jobsuggest", s.ID, s)
			n++
		}
	}
	uiJobSuggestMu.Unlock()
	return n
}

// proposalSetLifecycle pins, archives or restores one agent or job proposal.
func (m *uiManager) proposalSetLifecycle(id, what string, on bool) error {
	apply := func(state *string, pinned *bool, created *time.Time) error {
		switch what {
		case "pin":
			*pinned = on
			if on && *state == "archived" {
				*state = "new"
			}
		case "archive":
			if on {
				if *state != "new" {
					return errors.New("only a proposal that is still waiting can be archived")
				}
				*state, *pinned = "archived", false
			} else if *state == "archived" {
				*state, *created = "new", time.Now().UTC() // a fresh 14 days
			}
		default:
			return errors.New("unknown change")
		}
		return nil
	}
	uiSuggestMu.Lock()
	for _, s := range uiReadAgentSuggestions() {
		if s.ID == id {
			err := apply(&s.State, &s.Pinned, &s.Created)
			if err == nil {
				uiDB().put("agentsuggest", s.ID, s)
			}
			uiSuggestMu.Unlock()
			return err
		}
	}
	uiSuggestMu.Unlock()
	uiJobSuggestMu.Lock()
	defer uiJobSuggestMu.Unlock()
	for _, s := range uiReadJobSuggestions() {
		if s.ID == id {
			err := apply(&s.State, &s.Pinned, &s.Created)
			if err == nil {
				uiDB().put("jobsuggest", s.ID, s)
			}
			return err
		}
	}
	return errors.New("no such suggestion")
}

// archivedSuggestions lists archived agent and job proposals for the inbox's Archived filter.
func (m *uiManager) archivedSuggestions() []uiSuggestion {
	out := []uiSuggestion{}
	uiSuggestMu.Lock()
	for _, s := range uiReadAgentSuggestions() {
		if s.State == "archived" {
			out = append(out, uiSuggestion{ID: s.ID, Kind: "agent", Title: "Agent: " + s.Name, Why: s.Why, Detail: s.Role, At: s.Created, Href: "#/dashboard/agents", Evidence: s.Evidence, Lifecycle: "archived"})
		}
	}
	uiSuggestMu.Unlock()
	uiJobSuggestMu.Lock()
	for _, s := range uiReadJobSuggestions() {
		if s.State == "archived" {
			out = append(out, uiSuggestion{ID: s.ID, Kind: "job", Title: "Background job: " + s.Name, Why: s.Why, Command: uiShowArgv(s.Command), After: s.After, At: s.Created, Href: "#/dashboard/jobs", Evidence: s.Evidence, Lifecycle: "archived"})
		}
	}
	uiJobSuggestMu.Unlock()
	return out
}

// ---- dry run: what approving a proposal would change

type uiChangePreview struct {
	Action  string          `json:"action"` // create, update or none
	Summary string          `json:"summary"`
	Changes []uiFieldChange `json:"changes"`
}

// agentPreview computes the change from the same snapshots the ledger will record when the proposal is approved.
func (m *uiManager) agentPreview(s uiAgentSuggestion) *uiChangePreview {
	groups := map[string]bool{}
	for k, v := range s.Groups {
		if v {
			groups[k] = true
		}
	}
	after := &uiAgentSnap{Name: s.Name, Role: s.Role, Instructions: s.Instructions, Scope: "project", Folder: s.Folder, Groups: groups, Mode: "edit",
		Schedule: s.Schedule, Approved: true, Enabled: s.Schedule != ""}
	var before *uiAgentSnap
	if s.Replaces != "" {
		for _, a := range m.identityList() {
			if a.ID == s.Replaces {
				a := a
				before = uiAgentSnapOf(&a)
				n := *before
				n.Role, n.Instructions, n.Groups, n.Schedule, n.Enabled = after.Role, after.Instructions, after.Groups, after.Schedule, before.Enabled && after.Schedule != ""
				if before.Schedule != after.Schedule {
					n.Enabled = after.Schedule != ""
				}
				if after.Folder != "" {
					n.Folder = after.Folder
				}
				after = &n
			}
		}
	}
	return uiMakePreview("agent", s.Name, uiSnapJSON(before), uiSnapJSON(after))
}

func (m *uiManager) jobPreview(s uiJobSuggestion) *uiChangePreview {
	after := &uiJobSnap{Name: s.Name, Command: s.Command, After: s.After}
	var before *uiJobSnap
	if s.Updates != "" {
		if b := uiJobSnapshot(s.Name); b != nil {
			before = b
			n := *b
			n.Command, n.After = s.Command, s.After
			after = &n
		}
	}
	return uiMakePreview("job", s.Name, uiSnapJSON(before), uiSnapJSON(after))
}

func uiMakePreview(kind, name string, before, after []byte) *uiChangePreview {
	p := &uiChangePreview{Changes: uiSnapDiff(before, after)}
	what := map[string]string{"agent": "agent", "job": "background job"}[kind]
	switch {
	case len(before) == 0:
		p.Action, p.Summary = "create", "Adds the "+what+" "+name+"."
	case len(p.Changes) == 0:
		p.Action, p.Summary = "none", "Nothing about "+name+" would change."
	default:
		p.Action, p.Summary = "update", "Changes the "+what+" "+name+": "+strconv.Itoa(len(p.Changes))+" "+map[bool]string{true: "field", false: "fields"}[len(p.Changes) == 1]+"."
	}
	return p
}

func (m *uiManager) rulesPreview(p *uiRulesProposal, current string) *uiChangePreview {
	a, d := 0, 0
	for _, l := range uiLineDiff(current, p.Text) {
		switch l.Op {
		case "+":
			a++
		case "-":
			d++
		}
	}
	targets := len(uiRulesTargets())
	sum := "Replaces your workflow rules file in " + strconv.Itoa(targets) + " harness " + map[bool]string{true: "folder", false: "folders"}[targets == 1] + ". The old file is kept beside it as SKILL.md.prev."
	if current == "" {
		sum = "Creates your workflow rules file in " + strconv.Itoa(targets) + " harness " + map[bool]string{true: "folder", false: "folders"}[targets == 1] + "."
	}
	return &uiChangePreview{Action: "update", Summary: sum, Changes: []uiFieldChange{{Field: "Workflow rules", Old: strconv.Itoa(len(strings.Split(strings.TrimSpace(current), "\n"))) + " lines", New: strconv.Itoa(a) + " lines added, " + strconv.Itoa(d) + " removed"}}}
}

// uiAgentSuggestionView and uiJobSuggestionView carry the preview with the proposal JSON.
type uiAgentSuggestionView struct {
	uiAgentSuggestion
	Preview *uiChangePreview `json:"preview"`
}

// ---- handlers

func (s *uiServer) ledgerRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/ledger", s.api(s.ledgerGetH))
	mux.Handle("POST /api/ledger/{id}/rollback", s.api(s.ledgerRollbackH))
	mux.Handle("POST /api/identities/{id}/pin", s.api(s.identityLifecycleH("pin")))
	mux.Handle("POST /api/identities/{id}/archive", s.api(s.identityLifecycleH("archive")))
	mux.Handle("POST /api/suggestions/{id}/pin", s.api(s.suggestionLifecycleH("pin")))
	mux.Handle("POST /api/suggestions/{id}/archive", s.api(s.suggestionLifecycleH("archive")))
}

func (s *uiServer) ledgerGetH(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "agent" && kind != "job" && kind != "rule" && kind != "policy" {
		uiError(w, http.StatusBadRequest, "kind must be agent, job, rule or policy")
		return
	}
	uiJSON(w, http.StatusOK, s.m.ledgerList(kind, limit))
}

func (s *uiServer) ledgerRollbackH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Force bool `json:"force"`
	}
	if r.ContentLength > 0 && !uiDecode(w, r, &body) {
		return
	}
	v, err := s.m.ledgerRollback(r.PathValue("id"), body.Force)
	switch {
	case err == nil:
		uiJSON(w, http.StatusOK, v)
	case errors.Is(err, errLedgerChange):
		uiError(w, http.StatusConflict, err.Error())
	case err.Error() == "no such entry":
		uiError(w, http.StatusNotFound, err.Error())
	default:
		uiError(w, http.StatusBadRequest, err.Error())
	}
}

// onBody reads {"on": false}; an empty body means on.
func uiOnBody(w http.ResponseWriter, r *http.Request) (bool, bool) {
	body := struct {
		On *bool `json:"on"`
	}{}
	if r.ContentLength > 0 && !uiDecode(w, r, &body) {
		return false, false
	}
	return body.On == nil || *body.On, true
}

func (s *uiServer) identityLifecycleH(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		on, ok := uiOnBody(w, r)
		if !ok {
			return
		}
		a, err := s.m.identitySetLifecycle(r.PathValue("id"), what, on)
		if err != nil {
			uiError(w, http.StatusNotFound, err.Error())
			return
		}
		uiJSON(w, http.StatusOK, a)
	}
}

func (s *uiServer) suggestionLifecycleH(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		on, ok := uiOnBody(w, r)
		if !ok {
			return
		}
		if err := s.m.proposalSetLifecycle(r.PathValue("id"), what, on); err != nil {
			uiError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
