package cully

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A task report you can share: one self-contained page with no scripts, made from what Cully already
// knows about the task: its goal, plan, where it ended, the files it changed, whether the checks passed,
// what it cost and which tools it used. It holds no file contents, no secrets (token values are
// removed) and no folder paths beyond the folder's name, so it is safe to attach to a pull request or
// send to a teammate.

type uiReportData struct {
	Title, State, StateClass, Folder, Branch, Harness, Goal, Ended, Checks, ChecksClass string
	When                                                                                string
	Files                                                                               []string
	MoreFiles                                                                           int
	Added, Removed, FileCount                                                           int
	Plan                                                                                []uiPlanItem
	PlanDone                                                                            int
	Tools                                                                               []uiToolCount
	Cost, Tokens, Duration, Cache                                                       string
	Stopped                                                                             string
}

type uiPlanItem struct {
	Text   string
	Mark   string
	Status string
}

type uiToolCount struct {
	Name  string
	Count int
}

var uiReportTmpl = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}: task report</title>
<style>
:root{--bg:#f6f4ec;--card:#fffdf8;--ink:#17231d;--soft:#55645b;--line:#ddd8c8;--ok:#1c6b3e;--okbg:#e3f2e3;--bad:#a3412b;--badbg:#f8e6de;--warn:#8a5a14;--warnbg:#f6e7c8}
@media (prefers-color-scheme:dark){:root{--bg:#141a17;--card:#1c2420;--ink:#e8efe9;--soft:#9aaaa0;--line:#2b3731;--ok:#7fd29c;--okbg:#1d3a28;--bad:#f09a82;--badbg:#3d2018;--warn:#e6c071;--warnbg:#3a2f14}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.55 system-ui,sans-serif}
main{max-width:760px;margin:0 auto;padding:28px 16px 48px}h1{font:600 26px/1.25 Georgia,serif;margin:0 0 6px}
.chip{display:inline-block;border-radius:12px;padding:1px 10px;font-size:13px;font-weight:600;background:var(--line)}
.chip.ok{background:var(--okbg);color:var(--ok)}.chip.bad{background:var(--badbg);color:var(--bad)}.chip.warn{background:var(--warnbg);color:var(--warn)}
.sub{color:var(--soft);font-size:14px;margin:4px 0 18px}section{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px 18px;margin:14px 0}
h2{font-size:13px;margin:0 0 8px;color:var(--soft);font-weight:600}.stats{display:flex;flex-wrap:wrap;gap:22px}.stat b{display:block;font:600 24px Georgia,serif}.stat span{color:var(--soft);font-size:13px}
ul{margin:0;padding-left:20px}li{margin:3px 0}.plan{list-style:none;padding:0}.plan li{display:flex;gap:10px}.done{color:var(--soft);text-decoration:line-through}
code{font:13px ui-monospace,monospace;background:var(--bg);padding:1px 6px;border-radius:5px}p{margin:6px 0;overflow-wrap:anywhere}footer{color:var(--soft);font-size:13px;margin-top:22px}
</style></head><body><main>
<h1>{{.Title}}</h1>
<div class="sub"><span class="chip {{.StateClass}}">{{.State}}</span> &nbsp;{{.Harness}}{{if .Folder}} in <b>{{.Folder}}</b>{{end}}{{if .Branch}}, branch <code>{{.Branch}}</code>{{end}}{{if .When}}, {{.When}}{{end}}</div>
{{if .Stopped}}<section><span class="chip warn">{{.Stopped}}</span></section>{{end}}
<section><h2>Goal</h2><p>{{.Goal}}</p></section>
<section><div class="stats">
<div class="stat"><b>{{.FileCount}}</b><span>files changed</span></div>
<div class="stat"><b>+{{.Added}} −{{.Removed}}</b><span>lines</span></div>
<div class="stat"><b><span class="chip {{.ChecksClass}}">{{.Checks}}</span></b><span>checks</span></div>
{{if .Duration}}<div class="stat"><b>{{.Duration}}</b><span>time</span></div>{{end}}
{{if .Cost}}<div class="stat"><b>{{.Cost}}</b><span>list-price cost</span></div>{{end}}
{{if .Tokens}}<div class="stat"><b>{{.Tokens}}</b><span>tokens{{if .Cache}}, {{.Cache}} from cache{{end}}</span></div>{{end}}
</div></section>
{{if .Plan}}<section><h2>Plan, {{.PlanDone}} of {{len .Plan}} done</h2><ul class="plan">{{range .Plan}}<li class="{{.Status}}"><span>{{.Mark}}</span><span class="{{if eq .Status "completed"}}done{{end}}">{{.Text}}</span></li>{{end}}</ul></section>{{end}}
{{if .Ended}}<section><h2>Where it ended</h2><p>{{.Ended}}</p></section>{{end}}
{{if .Files}}<section><h2>Files changed</h2><ul>{{range .Files}}<li><code>{{.}}</code></li>{{end}}</ul>{{if .MoreFiles}}<p>and {{.MoreFiles}} more</p>{{end}}</section>{{end}}
{{if .Tools}}<section><h2>Tools used</h2><ul>{{range .Tools}}<li>{{.Name}} <span class="chip">{{.Count}}</span></li>{{end}}</ul></section>{{end}}
<footer>Made with Cully. This report holds no file contents, secrets or folder paths beyond the folder's name.</footer>
</main></body></html>
`))

func (a *uiAgent) report() uiReportData {
	v := a.snapshot()
	sum := a.summary()
	d := uiReportData{Title: v.Title, Folder: filepath.Base(v.Repo), Branch: strings.TrimPrefix(v.Branch, "cully/"), Harness: "Claude Code", FileCount: sum.FileCount, Added: sum.Added, Removed: sum.Removed}
	if v.Branch == "no git" {
		d.Branch = ""
	}
	redact := func(s string) string { return a.tokens.redact(s) }
	d.Goal = uiClip(redact(sum.Task), 600)
	d.Ended = uiClip(redact(sum.LastReply), 800)
	d.When = v.Created.Format("Jan 2, 2006")
	switch v.State {
	case uiDone:
		d.State, d.StateClass = "Finished", "ok"
	case uiFailed:
		d.State, d.StateClass = "Failed", "bad"
	case uiStopped:
		d.State, d.StateClass = "Stopped", "warn"
	case uiWaiting:
		d.State, d.StateClass = "Waiting for you", "warn"
	default:
		d.State, d.StateClass = "Working", ""
	}
	switch v.StopReason {
	case "cost_limit":
		d.Stopped = "Stopped at its cost limit"
	case "turn_limit":
		d.Stopped = "Stopped at its turn limit"
	}
	switch sum.Checks.Last {
	case "passed":
		d.Checks, d.ChecksClass = "passed", "ok"
		if sum.EditsSinceCheck > 0 {
			d.Checks, d.ChecksClass = "passed, then "+strconv.Itoa(sum.EditsSinceCheck)+" edits", "warn"
		}
	case "failed":
		d.Checks, d.ChecksClass = "failed", "bad"
	default:
		d.Checks, d.ChecksClass = "not run", "warn"
	}
	for i, f := range sum.Files {
		if i >= 12 {
			d.MoreFiles = sum.FileCount - 12
			break
		}
		d.Files = append(d.Files, f)
	}
	if sum.FileCount > len(d.Files) && d.MoreFiles == 0 {
		d.MoreFiles = sum.FileCount - len(d.Files)
	}
	if v.Cost > 0 {
		d.Cost = "$" + strconv.FormatFloat(v.Cost, 'f', 2, 64)
	}
	if v.Usage != nil && v.Usage.Tokens > 0 {
		d.Tokens = uiCompactNum(v.Usage.Tokens)
		if v.Usage.CachedShare > 0 {
			d.Cache = strconv.Itoa(v.Usage.CachedShare) + "%"
		}
	}
	if !v.Updated.IsZero() && !v.Created.IsZero() && v.Updated.After(v.Created) {
		d.Duration = uiShortDuration(v.Updated.Sub(v.Created))
	}
	counts := map[string]int{}
	for _, e := range a.allEvents() {
		switch e.Kind {
		case "tool":
			counts[e.Tool]++
		case "plan":
			var items []struct{ C, S string }
			if json.Unmarshal([]byte(e.Text), &items) == nil {
				d.Plan, d.PlanDone = nil, 0
				for _, it := range items {
					mark := "○"
					switch it.S {
					case "completed":
						mark = "✓"
						d.PlanDone++
					case "in_progress":
						mark = "●"
					}
					d.Plan = append(d.Plan, uiPlanItem{Text: redact(it.C), Mark: mark, Status: it.S})
				}
			}
		}
	}
	for n, c := range counts {
		d.Tools = append(d.Tools, uiToolCount{Name: n, Count: c})
	}
	sort.Slice(d.Tools, func(i, j int) bool {
		return d.Tools[i].Count > d.Tools[j].Count || d.Tools[i].Count == d.Tools[j].Count && d.Tools[i].Name < d.Tools[j].Name
	})
	if len(d.Tools) > 8 {
		d.Tools = d.Tools[:8]
	}
	return d
}

func uiCompactNum(n int) string {
	switch {
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 1000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(n)
}

func uiShortDuration(d time.Duration) string {
	switch {
	case d < 90*time.Second:
		return strconv.Itoa(int(d.Seconds())) + " s"
	case d < 90*time.Minute:
		return strconv.Itoa(int(d.Minutes())) + " min"
	}
	return strconv.FormatFloat(d.Hours(), 'f', 1, 64) + " h"
}

func (s *uiServer) reportGet(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Styles are inline in the report and it has no scripts, so it may carry its own styles but nothing else.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'")
	if r.URL.Query().Get("download") == "1" {
		name := uiSlug(a.snapshot().Title)
		if name == "" {
			name = "task"
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`-report.html"`)
	}
	_ = uiReportTmpl.Execute(w, a.report())
}
