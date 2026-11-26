package cully

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Data behind the Dashboard's Overview, Setup, Savings and Activity views.
// Each is shaped for a person: plain labels, no hook names, no tool names, and
// counts where a list of names would only be noise.

// ---- file groups ----

type uiFileGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

const uiGroupsMax = 8

func uiGroupName(file string) string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(file), "/"), "/")
	switch {
	case parts[0] == "" || strings.HasPrefix(parts[0], ".") || (len(parts) == 1 && parts[0] == "env"):
		return "Local settings"
	case len(parts) == 1:
		return "Top level"
	case len(parts) >= 3:
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// uiGroupFiles counts changed files by folder, biggest first, folding the
// long tail into "Other".
func uiGroupFiles(files []string) []uiFileGroup {
	counts := map[string]int{}
	for _, f := range files {
		counts[uiGroupName(f)]++
	}
	groups := make([]uiFileGroup, 0, len(counts))
	for n, c := range counts {
		groups = append(groups, uiFileGroup{n, c})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})
	if len(groups) > uiGroupsMax {
		other := 0
		for _, g := range groups[uiGroupsMax:] {
			other += g.Count
		}
		groups = append(groups[:uiGroupsMax], uiFileGroup{"Other", other})
	}
	return groups
}

// uiPorcelainFiles lists every changed and untracked file in a repository.
func uiPorcelainFiles(repo string) []string {
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain", "-uall").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		name := line[3:]
		if i := strings.Index(name, " -> "); i >= 0 {
			name = name[i+4:]
		}
		files = append(files, strings.Trim(name, `"`))
		if len(files) >= 5000 {
			break
		}
	}
	return files
}

// ---- setup ----

type uiSetupItem struct {
	Label string `json:"label"`
	OK    bool   `json:"ok"`
	Hint  string `json:"hint,omitempty"` // what to do, in plain words, when it is not OK
}

type uiSetupAgent struct {
	Agent     string        `json:"agent"`
	Installed bool          `json:"installed"`
	Items     []uiSetupItem `json:"items"`
	Ready     bool          `json:"ready"`
}

type uiSetupView struct {
	Agents         []uiSetupAgent `json:"agents"`
	AdvisorRunning bool           `json:"advisorRunning"`
	AdvisorHint    string         `json:"advisorHint,omitempty"`
	Tools          struct {
		MCP, Skills, Agents int
	} `json:"tools"` // counts only; the names stay out of the page
	AllReady bool `json:"allReady"`
}

func uiSetup() uiSetupView { return uiSetupFor(func(string) bool { return true }) }

// uiSetupFor describes only the agents the page was opened for. Nothing about
// the others is computed or sent.
func uiSetupFor(keep func(agent string) bool) uiSetupView {
	cwd, _ := os.Getwd()
	settings, _ := loadSettings(filepath.Join(ConfigDir(), "settings.json"))
	codexHooks, _ := loadHookConfig(codexHooksPath())
	cursorHooks, _ := loadHookConfig(cursorHooksPath())
	setupHint := func(name string) string {
		return "Cully is not set up for " + name + " yet."
	}
	item := func(label string, ok bool, name string) uiSetupItem {
		it := uiSetupItem{Label: label, OK: ok}
		if !ok {
			it.Hint = setupHint(name)
		}
		return it
	}
	cursorAll := func(subs [][2]string) bool {
		for _, s := range subs {
			if !cursorHookConfigured(cursorHooks, s[0], s[1]) {
				return false
			}
		}
		return true
	}
	agents := []uiSetupAgent{
		{Agent: "claude", Items: []uiSetupItem{
			item("Session tracking", hookConfigured(settings, "Stop", "analyze") && hookConfigured(settings, "SessionStart", "continuity claude start") &&
				hookConfigured(settings, "Stop", "continuity claude stop") && hookConfigured(settings, "SessionEnd", "cleanup"), "Claude"),
			item("Live status in the Cully terminal", hookConfigured(settings, "PostToolUse", "pane-signal claude"), "Claude"),
		}},
		{Agent: "codex", Items: []uiSetupItem{
			item("Session tracking", hookConfigured(codexHooks, "SessionStart", "continuity codex start"), "Codex"),
			item("Live status in the Cully terminal", hookConfigured(codexHooks, "PostToolUse", "codex-signal"), "Codex"),
			item("Cully command inside Codex", fileExists(codexPromptPath()), "Codex"),
		}},
		{Agent: "cursor", Items: []uiSetupItem{
			item("Session tracking", cursorAll([][2]string{{"sessionStart", "continuity cursor start"}, {"afterAgentResponse", "continuity cursor response"}, {"stop", "continuity cursor stop"}}), "Cursor"),
			item("Live status in the Cully terminal", cursorAll([][2]string{{"afterShellExecution", cursorSignalSub}, {"afterFileEdit", cursorSignalSub}, {"afterMCPExecution", cursorSignalSub}}), "Cursor"),
			item("Cully command inside Cursor", fileExists(cursorCommandPath()), "Cursor"),
		}},
	}
	kept := agents[:0:0]
	for _, a := range agents {
		if keep(a.Agent) {
			kept = append(kept, a)
		}
	}
	agents = kept
	v := uiSetupView{AdvisorRunning: isDaemonRunning()}
	if !v.AdvisorRunning {
		v.AdvisorHint = "The background advisor for terminal sessions is not running."
	}
	v.AllReady = v.AdvisorRunning
	anyInstalled := false
	for i := range agents {
		a := &agents[i]
		a.Installed = uiAgentInstalled(a.Agent)
		a.Ready = a.Installed
		for _, it := range a.Items {
			a.Ready = a.Ready && it.OK
		}
		if a.Installed {
			anyInstalled = true
			v.AllReady = v.AllReady && a.Ready
		}
	}
	v.AllReady = v.AllReady && anyInstalled
	v.Agents = agents
	v.Tools.MCP = len(strings.Fields(listMCPServers(cwd)))
	v.Tools.Skills = len(strings.Fields(listSkills(cwd)))
	v.Tools.Agents = len(strings.Fields(listAgents(cwd)))
	return v
}

// ---- savings ----

type uiDay struct {
	Date  string `json:"date"`
	Saved int64  `json:"saved"`
}

type uiImpactView struct {
	Days            int     `json:"days"`
	Sessions        int     `json:"sessions"`
	Saved           int64   `json:"saved"`
	Cost            int64   `json:"cost"`
	Net             int64   `json:"net"`
	MemoryHits      int     `json:"memoryHits"`
	AnalysesSkipped int     `json:"analysesSkipped"`
	LoopsCaught     int     `json:"loopsCaught"`
	WasteSessions   int     `json:"wasteSessions"`
	WasteTokens     int64   `json:"wasteTokens"`
	Basis           string  `json:"basis,omitempty"`
	Daily           []uiDay `json:"daily"`
}

func uiImpact(days int) uiImpactView {
	if days <= 0 {
		days = 14
	}
	if days > 90 {
		days = 90
	}
	now := time.Now()
	events := readImpactEvents(impactLedgerFile(), now.AddDate(0, 0, -days))
	im := SummarizeImpact(events, days, 0)
	v := uiImpactView{
		Days: days, Sessions: im.Sessions, Saved: im.TotalSaved, Cost: im.TotalCost, Net: im.Net,
		MemoryHits: im.MemoryHits, AnalysesSkipped: im.AnalysesSkipped, LoopsCaught: im.LoopsCaught,
		WasteSessions: im.WasteSessions, WasteTokens: im.WasteTokensFlagged, Basis: im.SavedBasis,
	}
	byDay := map[string]int64{}
	for _, e := range events {
		if e.Kind == impactAnalysisSkipped {
			byDay[e.At.Local().Format("2006-01-02")] += e.Tokens
		}
	}
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		v.Daily = append(v.Daily, uiDay{Date: d, Saved: byDay[d]})
	}
	return v
}

// ---- activity ----

type uiFacts struct {
	Edits           int      `json:"edits"`
	Checks          uiChecks `json:"checks"`
	EditsSinceCheck int      `json:"editsSinceCheck"`
	Loops           int      `json:"loops"`
}

type uiActivityEvent struct {
	Time   time.Time `json:"t"`
	Kind   string    `json:"kind"` // start, end, edit, read, search, check, command, memory, note
	Label  string    `json:"label,omitempty"`
	Path   string    `json:"path,omitempty"`
	Failed bool      `json:"failed,omitempty"`
}

type uiActivityView struct {
	Recorded bool              `json:"recorded"`
	Sessions int               `json:"sessions"`
	Facts    uiFacts           `json:"facts"`
	Events   []uiActivityEvent `json:"events"`
}

const uiActivityMax = 300

var uiNoteWords = map[string]string{
	noteLoop:      "The same command failed repeatedly",
	noteVerifyGap: "Edits went in without a check",
}

// uiActivity turns the latest recorded session for a folder into plain
// events: what the agent did, never what was said. The journal holds only
// event kinds, relative paths and a short command label.
func uiActivity(repo string) uiActivityView {
	var events []journalEvent
	if _, ev, ok := latestJournal(repo); ok {
		events = ev
	}
	return uiActivityFrom(events, len(projectJournals(repo)))
}

// activity is uiActivity plus the agents this page started in the folder,
// whose journals live under their worktrees.
func (m *uiManager) activity(repo string) uiActivityView {
	var events []journalEvent
	if _, ev, ok := latestJournal(repo); ok {
		events = ev
	}
	sessions := len(projectJournals(repo))
	for _, v := range m.list() {
		if v.Repo != repo {
			continue
		}
		if ev := readJournal(journalPath(v.Worktree, "ui-"+v.ID)); len(ev) > 0 {
			events = append(events, ev...)
			sessions++
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	return uiActivityFrom(events, sessions)
}

func uiActivityFrom(events []journalEvent, sessions int) uiActivityView {
	v := uiActivityView{Events: []uiActivityEvent{}, Sessions: sessions}
	if len(events) == 0 {
		return v
	}
	v.Recorded = true
	f := handoffFactsOf(events)
	v.Facts = uiFacts{Edits: f.Edits, EditsSinceCheck: f.EditsSinceCheck, Loops: f.Loops,
		Checks: uiChecks{Passed: f.ChecksPassed, Failed: f.ChecksFailed, Last: "none"}}
	if f.LastCheck == "passed" || f.LastCheck == "failed" {
		v.Facts.Checks.Last = f.LastCheck
	}
	for _, e := range events {
		ev := uiActivityEvent{Time: e.Time, Failed: e.Failed, Path: e.Path}
		first := func(a, b string) string {
			if a != "" {
				return a
			}
			return b
		}
		switch e.Class {
		case journalStart:
			ev.Kind, ev.Label = "start", first(e.Agent, "agent")
		case journalEnd:
			ev.Kind = "end"
		case journalEdit:
			ev.Kind, ev.Label = "edit", first(e.Path, e.Tool)
		case journalSearch:
			ev.Kind, ev.Label = "search", first(e.Path, first(e.Cmd, e.Tool))
			if e.Op == opRead {
				ev.Kind = "read"
			}
		case journalCheck:
			ev.Kind, ev.Label = "check", e.Cmd
		case journalOther:
			ev.Kind, ev.Label = "command", first(e.Cmd, e.Tool)
		case journalMemory:
			ev.Kind, ev.Label = "memory", "Cully memory"
		case journalNote:
			ev.Kind, ev.Label = "note", first(uiNoteWords[e.Note], e.Note)
		case journalFileOp:
			ev.Kind, ev.Label = "edit", e.Path
			if e.Op == opRead {
				ev.Kind = "read"
			}
		default:
			continue
		}
		v.Events = append(v.Events, ev)
	}
	if len(v.Events) > uiActivityMax {
		v.Events = v.Events[len(v.Events)-uiActivityMax:]
	}
	return v
}
