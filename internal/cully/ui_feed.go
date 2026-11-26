package cully

import (
	"net/http"
	"regexp"
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
	Body   string    `json:"body,omitempty"` // the full text, Markdown, shown when the card is opened
	Meta   []uiMeta  `json:"meta,omitempty"` // the facts behind it: who, where, how long
}

type uiMeta struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

const uiFeedBody = 6000

func uiMetaList(kv ...string) []uiMeta {
	var out []uiMeta
	for i := 0; i+1 < len(kv); i += 2 {
		if strings.TrimSpace(kv[i+1]) != "" {
			out = append(out, uiMeta{kv[i], kv[i+1]})
		}
	}
	return out
}

var uiFeedKinds = map[string]bool{"task": true, "agent": true, "eval": true, "scout": true, "advice": true}

func (m *uiManager) feed(kind string, limit int) []uiFeedItem {
	var items []uiFeedItem
	add := func(it uiFeedItem) {
		if !it.At.IsZero() && (kind == "" || kind == it.Kind) && !uiHidden(it.Repo, m.cfg.HideSessionsIn) {
			it.Detail = uiClipWords(it.Detail, 220)
			it.Body = uiClip(it.Body, uiFeedBody)
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
		meta := uiMetaList("Folder", a.Repo, "Branch", a.Branch, "Started", a.Created.Format("Jan 2, 15:04"), "Files changed", uiNonZero(len(a.Files)))
		if a.Usage != nil {
			meta = append(meta, uiMeta{"Tokens", strconv.Itoa(a.Usage.Tokens) + ", " + strconv.Itoa(a.Usage.CachedShare) + "% from the cache"})
		}
		add(uiFeedItem{ID: "task-" + a.ID, Kind: "task", Status: st, Title: a.Title + " " + word, Detail: a.Last, Body: a.Last, Meta: meta, At: a.Updated, Href: "#/task/" + a.ID, Repo: a.Repo})
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
			add(uiFeedItem{ID: "run-" + r.ID, Kind: "agent", Status: st, Title: id.Name + " " + word, Detail: r.Summary, Body: r.Summary, At: r.Started, Href: href, Repo: id.Folder,
				Meta: uiMetaList("Agent", id.Name, "Role", uiClipWords(id.Role, 140), "Took", uiNonZeroDur(r.Seconds), "Schedule", id.Schedule, "Folder", id.Folder)})
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
			var lines []string
			for _, x := range run.Results {
				name := x.CaseID
				for _, c := range ev.Cases {
					if c.ID == x.CaseID {
						name = c.Name
					}
				}
				lines = append(lines, "- **"+name+"**: "+x.Status+". "+x.Reason)
			}
			add(uiFeedItem{ID: "aeval-" + run.ID, Kind: "eval", Status: st, Title: id.Name + ": " + strconv.Itoa(pass) + " of " + strconv.Itoa(run.Total) + " eval cases passed", Body: strings.Join(lines, "\n"), At: run.Started, Href: "#/dashboard/evals", Repo: id.Folder,
				Meta: uiMetaList("Agent", id.Name, "Cases", strconv.Itoa(run.Total), "Passed", strconv.Itoa(pass))})
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
		add(uiFeedItem{ID: "eval-" + r.ID, Kind: "eval", Status: st, Title: "Eval " + r.Status + ": " + r.Task, Detail: r.Reason, Body: r.Reason, At: r.Started, Href: "#/dashboard/evals",
			Meta: uiMetaList("Result", r.Status, "Took", uiNonZeroDur(r.Seconds), "Files changed", uiNonZero(r.Files))})
	}
	for _, r := range m.researchList("", uiResearchKeep) {
		switch r.Kind {
		case "scout":
			add(uiFeedItem{ID: "r-" + r.ID, Kind: "scout", Status: "info", Title: "Scout: " + r.Query, Detail: uiPlain(r.Text), Body: r.Text, At: r.Time, Href: "#/dashboard/scout", Repo: r.Repo,
				Meta: uiMetaList("Searched for", r.Query, "Started by", r.Source, "Folder", r.Repo)})
		case "digest":
			add(uiFeedItem{ID: "r-" + r.ID, Kind: "scout", Status: "good", Title: "Suggestions from the last 12 hours", Detail: uiPlain(r.Text), Body: r.Text, At: r.Time, Href: "#/dashboard/scout", Repo: r.Repo, Meta: uiMetaList("Made from", r.Source)})
		case "advice":
			st := "info"
			if r.Level == "warn" {
				st = "wait"
			}
			add(uiFeedItem{ID: "r-" + r.ID, Kind: "advice", Status: st, Title: "Advice: " + uiClipWords(uiPlain(r.Text), 90), Detail: uiPlain(r.Text), Body: r.Text, At: r.Time, Href: "#/dashboard/advisor", Repo: r.Repo,
				Meta: uiMetaList("For", r.Source, "Level", r.Level, "Suggested action", r.Action, "Folder", r.Repo)})
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

func uiNonZero(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func uiNonZeroDur(sec int) string {
	switch {
	case sec <= 0:
		return ""
	case sec < 90:
		return strconv.Itoa(sec) + " seconds"
	}
	return strconv.Itoa(sec/60) + " minutes"
}

var uiMDMarks = regexp.MustCompile("(?m)\\*\\*|__|`|^\\s*[-*#]+\\s+")

var uiMDLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

// uiPlain strips Markdown markers so a preview reads as plain words.
func uiPlain(s string) string {
	s = uiMDLink.ReplaceAllString(s, "$1")
	s = uiMDMarks.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}
