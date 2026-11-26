package cully

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Activity feed: one list, newest first, of what happened across Cully
// that a person would want to know about: tasks that finished or need an
// answer, runs of named agents, evals and their verdicts, and what the scout
// and advisor found. Each item says what it is, how it went, when, and links to
// the place where the detail lives. It reads what is already kept; nothing here
// is stored twice.

type uiFeedItem struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`             // task, agent, eval, scout, advice
	Status string    `json:"status,omitempty"` // good, bad, wait, info
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
	Href   string    `json:"href,omitempty"`
	Repo   string    `json:"repo,omitempty"`
}

var uiFeedKinds = map[string]bool{"task": true, "agent": true, "eval": true, "scout": true, "advice": true}

func (m *uiManager) feed(kind string, limit int) []uiFeedItem {
	var items []uiFeedItem
	add := func(it uiFeedItem) {
		if !it.At.IsZero() && (kind == "" || kind == it.Kind) && !uiHidden(it.Repo, m.cfg.HideSessionsIn) {
			it.Detail = uiClipWords(it.Detail, 220)
			items = append(items, it)
		}
	}
	for _, a := range m.list() {
		st, word := "info", a.State
		switch a.State {
		case uiDone:
			st, word = "good", "finished"
		case uiFailed:
			st, word = "bad", "failed"
		case uiStopped:
			word = "was stopped"
		case uiWaiting:
			st, word = "wait", "needs your answer"
		default:
			word = "is running"
		}
		add(uiFeedItem{ID: "task-" + a.ID, Kind: "task", Status: st, Title: a.Title + " " + word, Detail: a.Last, At: a.Updated, Href: "#/task/" + a.ID, Repo: a.Repo})
	}
	for _, id := range m.identityList() {
		for _, r := range id.Runs {
			st, word := "info", "is running"
			switch r.Status {
			case "done":
				st, word = "good", "ran"
			case "failed":
				st, word = "bad", "failed"
			}
			href := "#/dashboard/agents"
			if r.TaskID != "" {
				href = "#/task/" + r.TaskID
			}
			add(uiFeedItem{ID: "run-" + r.ID, Kind: "agent", Status: st, Title: id.Name + " " + word, Detail: r.Summary, At: r.Started, Href: href, Repo: id.Folder})
		}
		ev := m.readAgentEvals(id.ID)
		for _, run := range ev.Runs {
			if !run.Done {
				continue
			}
			pass := 0
			for _, x := range run.Results {
				if x.Status == "pass" {
					pass++
				}
			}
			st := "good"
			if pass < run.Total {
				st = "bad"
			}
			add(uiFeedItem{ID: "aeval-" + run.ID, Kind: "eval", Status: st, Title: id.Name + ": " + strconv.Itoa(pass) + " of " + strconv.Itoa(run.Total) + " eval cases passed", At: run.Started, Href: "#/dashboard/evals", Repo: id.Folder})
		}
	}
	for _, r := range m.evalRunList() {
		st := "info"
		switch r.Status {
		case "pass":
			st = "good"
		case "fail", "error":
			st = "bad"
		case "partial":
			st = "wait"
		}
		add(uiFeedItem{ID: "eval-" + r.ID, Kind: "eval", Status: st, Title: "Eval " + r.Status + ": " + r.Task, Detail: r.Reason, At: r.Started, Href: "#/dashboard/evals"})
	}
	for _, r := range m.researchList("", uiResearchKeep) {
		switch r.Kind {
		case "scout":
			add(uiFeedItem{ID: "r-" + r.ID, Kind: "scout", Status: "info", Title: "Scout: " + r.Query, Detail: r.Text, At: r.Time, Href: "#/dashboard/advisor", Repo: r.Repo})
		case "digest":
			add(uiFeedItem{ID: "r-" + r.ID, Kind: "scout", Status: "good", Title: "Suggestions from the last 12 hours", Detail: strings.ReplaceAll(strings.ReplaceAll(r.Text, "- **", ""), "**", ""), At: r.Time, Href: "#/dashboard/advisor", Repo: r.Repo})
		case "advice":
			st := "info"
			if r.Level == "warn" {
				st = "wait"
			}
			add(uiFeedItem{ID: "r-" + r.ID, Kind: "advice", Status: st, Title: "Advice: " + uiClipWords(r.Text, 90), At: r.Time, Href: "#/dashboard/advisor", Repo: r.Repo})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []uiFeedItem{}
	}
	return items
}

func (s *uiServer) feedGet(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && !uiFeedKinds[kind] {
		uiError(w, http.StatusBadRequest, "kind must be task, agent, eval, scout or advice")
		return
	}
	uiJSON(w, http.StatusOK, s.m.feed(kind, 150))
}
