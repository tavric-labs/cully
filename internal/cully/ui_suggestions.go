package cully

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// One inbox for everything Cully suggests. The Advisor decides what to suggest;
// the other pages (Agents, Background jobs, Workflow rules, Session library) show
// only what you have already approved or what you asked to see. This list
// gathers the pending suggestions from each producer so you answer them in
// one place.

type uiSuggestion struct {
	ID       string       `json:"id"`
	Kind     string       `json:"kind"` // agent, job, coach, rules, digest
	Title    string       `json:"title"`
	Why      string       `json:"why,omitempty"`
	Detail   string       `json:"detail,omitempty"`
	Command  string       `json:"command,omitempty"` // jobs: the exact command that would run
	After    string       `json:"after,omitempty"`   // jobs: the proposed time of day
	At       time.Time    `json:"at"`
	Href     string       `json:"href,omitempty"`
	Evidence []uiEvidence `json:"evidence,omitempty"` // the sessions it is drawn from
	Count    int          `json:"count,omitempty"`
	Days     int          `json:"days,omitempty"`
}

func (m *uiManager) suggestions() []uiSuggestion {
	out := []uiSuggestion{}
	for _, s := range m.agentSuggestionsList() {
		out = append(out, uiSuggestion{ID: s.ID, Kind: "agent", Title: "Agent: " + s.Name, Why: s.Why, Detail: s.Role, At: s.Created, Href: "#/dashboard/agents", Evidence: s.Evidence, Count: s.Count, Days: s.Days})
	}
	for _, s := range m.jobSuggestionsList() {
		out = append(out, uiSuggestion{ID: s.ID, Kind: "job", Title: "Background job: " + s.Name, Why: s.Why, Command: uiShowArgv(s.Command), After: s.After, At: s.Created, Href: "#/dashboard/jobs", Evidence: s.Evidence, Count: s.Count, Days: s.Days})
	}
	if c := m.coachGet(); !c.At.IsZero() {
		for i, it := range c.Items {
			out = append(out, uiSuggestion{ID: "coach-" + uiItoa(i), Kind: "coach", Title: uiClipWords(it.Title, 100), Why: it.Why, Detail: it.How, At: c.At, Href: uiCoachHref(it.Kind)})
		}
	}
	if v := m.rulesView(); v.Proposal != nil && v.Proposal.State == "new" {
		why := "Based on " + uiItoa(v.Proposal.Based) + " analysed sessions."
		if len(v.Proposal.Changes) > 0 {
			why = uiClipWords(v.Proposal.Changes[0], 160)
		}
		out = append(out, uiSuggestion{ID: v.Proposal.ID, Kind: "rules", Title: "Update your workflow rules", Why: why, At: v.Proposal.Created, Href: "#/dashboard/rules"})
	}
	for _, r := range m.researchList("digest", 1) {
		out = append(out, uiSuggestion{ID: r.ID, Kind: "digest", Title: "Suggestions from the scouts", Detail: strings.ReplaceAll(r.Text, "**", ""), At: r.Time, Href: "#/dashboard/scout"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

func (s *uiServer) suggestionsGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.suggestions())
}

func (s *uiServer) jobSuggestRunH(w http.ResponseWriter, r *http.Request) {
	n, err := s.m.jobSuggestRun()
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]int{"added": n})
}

func (s *uiServer) jobSuggestAcceptH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		After string `json:"after"`
	}
	if r.ContentLength > 0 && !uiDecode(w, r, &body) {
		return
	}
	name, err := s.m.jobSuggestionAccept(r.PathValue("id"), body.After)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]string{"job": name})
}

func (s *uiServer) jobSuggestDismissH(w http.ResponseWriter, r *http.Request) {
	if err := s.m.jobSuggestionDismiss(r.PathValue("id")); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// uiShowArgv writes a command the way a shell would need it, quoting any word
// with a space or quote, so what the user reads is exactly the argument list.
func uiShowArgv(cmd []string) string {
	out := make([]string, len(cmd))
	for i, a := range cmd {
		if strings.ContainsAny(a, " \t'\"") {
			a = strconv.Quote(a)
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// uiCoachHref is the page a tip is about, or "" when the tip is a habit that has no page: its full
// text is already on the card.
func uiCoachHref(kind string) string {
	switch kind {
	case "job":
		return "#/dashboard/jobs"
	case "workflow":
		return "#/dashboard/rules"
	case "eval":
		return "#/dashboard/evals"
	case "mcp", "skill":
		return "#/harness"
	}
	return ""
}
