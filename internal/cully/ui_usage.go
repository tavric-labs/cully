package cully

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"time"
)

// A record of what each finished task used, kept so the Savings page can show
// real numbers over time: tokens handled, how much was served from the prompt
// cache, and whether the task loaded every connected MCP server or only the
// ones chosen. Only counts are stored, never prompts or replies.

const uiUsageKeep = 2000

type uiUsageRec struct {
	Time        time.Time `json:"time"`
	TaskID      string    `json:"taskId"`
	Title       string    `json:"title"`
	Repo        string    `json:"repo"`
	Turns       int       `json:"turns"`
	Fresh       int       `json:"fresh"`
	Cached      int       `json:"cached"`
	Output      int       `json:"output"`
	McpFiltered bool      `json:"mcpFiltered"`
	Named       bool      `json:"named"` // started as a named agent's run
}

func (m *uiManager) recordUsage(a *uiAgent) {
	a.mu.Lock()
	if a.usageRecorded || a.usage.Total() == 0 {
		a.usageRecorded = true
		a.mu.Unlock()
		return
	}
	a.usageRecorded = true
	u, turns := a.usage, a.turns
	rec := uiUsageRec{Time: time.Now().UTC(), TaskID: a.id, Title: a.title, Repo: a.repo, Turns: turns,
		Fresh: u.Input + u.CacheWrite, Cached: u.CacheRead, Output: u.Output, McpFiltered: a.mcpFiltered, Named: a.req.appendSystem != ""}
	a.mu.Unlock()
	uiDB().logAdd("usage", rec, uiUsageKeep)
}

func uiReadUsage() []uiUsageRec {
	var out []uiUsageRec
	for _, raw := range uiDB().logAll("usage") {
		var rec uiUsageRec
		if json.Unmarshal(raw, &rec) == nil {
			out = append(out, rec)
		}
	}
	return out
}

type uiUsageDay struct {
	Date   string `json:"date"`
	Tokens int    `json:"tokens"`
	Saved  int    `json:"saved"`
	Tasks  int    `json:"tasks"`
}

type uiUsageFolder struct {
	Name   string `json:"name"`
	Tokens int    `json:"tokens"`
	Tasks  int    `json:"tasks"`
}

type uiUsageTask struct {
	Title       string    `json:"title"`
	Tokens      int       `json:"tokens"`
	CachedShare int       `json:"cachedShare"`
	Time        time.Time `json:"time"`
	TaskID      string    `json:"taskId"`
}

type uiUsageSummary struct {
	Days        int             `json:"days"`
	Tasks       int             `json:"tasks"`
	Tokens      int             `json:"tokens"` // everything handled
	Fresh       int             `json:"fresh"`
	Cached      int             `json:"cached"`
	Output      int             `json:"output"`
	CachedShare int             `json:"cachedShare"` // percent of input from the cache
	Saved       int             `json:"saved"`       // cost saved by the cache, in tokens at full price
	Daily       []uiUsageDay    `json:"daily"`
	Folders     []uiUsageFolder `json:"folders"`
	Top         []uiUsageTask   `json:"top"`
	// Input per turn for tasks that loaded every connected server and for those limited to chosen ones.
	AllPerTurn      int `json:"allPerTurn"`
	FilteredPerTurn int `json:"filteredPerTurn"`
	AllTasks        int `json:"allTasks"`
	FilteredTasks   int `json:"filteredTasks"`
	// MCPSaved is what limiting servers saved: the difference per turn times the turns of filtered tasks.
	MCPSaved int `json:"mcpSaved"`
}

func (m *uiManager) usageSummary(days int) uiUsageSummary {
	if days <= 0 || days > 365 {
		days = 14
	}
	s := uiUsageSummary{Days: days, Daily: []uiUsageDay{}, Folders: []uiUsageFolder{}, Top: []uiUsageTask{}}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	byDay := map[string]*uiUsageDay{}
	for i := days - 1; i >= 0; i-- {
		d := time.Now().AddDate(0, 0, -i).UTC().Format("2006-01-02")
		byDay[d] = &uiUsageDay{Date: d}
		s.Daily = append(s.Daily, uiUsageDay{Date: d})
	}
	byFolder := map[string]*uiUsageFolder{}
	var allIn, allTurns, filtIn, filtTurns int
	recs := uiReadUsage()
	var tasks []uiUsageTask
	for _, r := range recs {
		if r.Time.Before(cut) || uiHidden(r.Repo, m.cfg.HideSessionsIn) {
			continue
		}
		total := r.Fresh + r.Cached + r.Output
		s.Tasks++
		s.Tokens += total
		s.Fresh += r.Fresh
		s.Cached += r.Cached
		s.Output += r.Output
		if d := byDay[r.Time.Format("2006-01-02")]; d != nil {
			d.Tokens += total
			d.Saved += r.Cached * 9 / 10
			d.Tasks++
		}
		name := filepath.Base(r.Repo)
		f := byFolder[name]
		if f == nil {
			f = &uiUsageFolder{Name: name}
			byFolder[name] = f
		}
		f.Tokens += total
		f.Tasks++
		share := 0
		if in := r.Fresh + r.Cached; in > 0 {
			share = r.Cached * 100 / in
		}
		tasks = append(tasks, uiUsageTask{Title: r.Title, Tokens: total, CachedShare: share, Time: r.Time, TaskID: r.TaskID})
		if r.Turns > 0 {
			if r.McpFiltered {
				filtIn += r.Fresh + r.Cached
				filtTurns += r.Turns
				s.FilteredTasks++
			} else {
				allIn += r.Fresh + r.Cached
				allTurns += r.Turns
				s.AllTasks++
			}
		}
	}
	if in := s.Fresh + s.Cached; in > 0 {
		s.CachedShare = s.Cached * 100 / in
	}
	s.Saved = s.Cached * 9 / 10
	for i := range s.Daily {
		if d := byDay[s.Daily[i].Date]; d != nil {
			s.Daily[i] = *d
		}
	}
	for _, f := range byFolder {
		s.Folders = append(s.Folders, *f)
	}
	sort.Slice(s.Folders, func(i, j int) bool { return s.Folders[i].Tokens > s.Folders[j].Tokens })
	if len(s.Folders) > 8 {
		s.Folders = s.Folders[:8]
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Tokens > tasks[j].Tokens })
	if len(tasks) > 5 {
		tasks = tasks[:5]
	}
	s.Top = append(s.Top, tasks...)
	if allTurns > 0 {
		s.AllPerTurn = allIn / allTurns
	}
	if filtTurns > 0 {
		s.FilteredPerTurn = filtIn / filtTurns
	}
	if s.AllPerTurn > s.FilteredPerTurn && s.FilteredPerTurn > 0 {
		s.MCPSaved = (s.AllPerTurn - s.FilteredPerTurn) * filtTurns
	}
	return s
}
