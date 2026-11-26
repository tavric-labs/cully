package cully

import (
	"sort"
	"strings"
	"time"
)

// Finding work that repeats. Cully proposes an agent only when the sessions
// show something an agent would really take over, because a worker that is not
// needed costs tokens and attention. The tests are deliberately plain:
//   - the same kind of goal, in the same folder, at least three times on at
//     least two different days; or
//   - changes left untested in at least three sessions of one folder.
// This follows what the harness research advises: add a worker only in response
// to an observed failure mode or a task that clearly repeats, and stay with one
// agent until a concrete reason says otherwise.

const (
	uiRepeatMin       = 3
	uiRepeatMinDays   = 2
	uiRepeatSimilar   = 0.5 // share of words two goals must have in common
	uiAgentCheckEvery = 24 * time.Hour
	uiAgentCheckNew   = 5 // named sessions that must have appeared since the last check
)

var uiStopWords = map[string]bool{"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true, "in": true, "on": true, "for": true, "with": true, "is": true, "it": true, "this": true, "that": true, "from": true, "by": true, "as": true, "at": true, "be": true, "are": true, "my": true, "me": true, "our": true, "we": true, "you": true, "your": true, "run": false}

func uiGoalWords(task string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(task), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }) {
		if len(w) > 2 && !uiStopWords[w] {
			out[w] = true
		}
	}
	return out
}

func uiSimilar(a, b map[string]bool) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	both := 0
	for w := range a {
		if b[w] {
			both++
		}
	}
	small := len(a)
	if len(b) < small {
		small = len(b)
	}
	return float64(both)/float64(small) >= uiRepeatSimilar
}

type uiRepeat struct {
	Kind     string // "goal" or "untested"
	Folder   string // folder name
	Path     string
	Count    int
	Days     int
	Examples []string
}

// uiRepeatedWork returns the patterns that justify proposing an agent.
func uiRepeatedWork(sessions []uiIndexed) []uiRepeat {
	byFolder := map[string][]uiIndexed{}
	for _, s := range sessions {
		if s.Source == "ai" && s.Task != "" {
			byFolder[s.Cwd] = append(byFolder[s.Cwd], s)
		}
	}
	var out []uiRepeat
	for cwd, list := range byFolder {
		sort.Slice(list, func(i, j int) bool { return list[i].Updated.After(list[j].Updated) })
		used := make([]bool, len(list))
		for i := range list {
			if used[i] {
				continue
			}
			wi := uiGoalWords(list[i].Task)
			group := []int{i}
			for j := i + 1; j < len(list); j++ {
				if !used[j] && uiSimilar(wi, uiGoalWords(list[j].Task)) {
					group = append(group, j)
				}
			}
			days := map[string]bool{}
			for _, k := range group {
				days[list[k].Updated.Format("2006-01-02")] = true
			}
			if len(group) >= uiRepeatMin && len(days) >= uiRepeatMinDays {
				r := uiRepeat{Kind: "goal", Folder: list[i].Name, Path: cwd, Count: len(group), Days: len(days)}
				for _, k := range group {
					used[k] = true
					if len(r.Examples) < 3 {
						r.Examples = append(r.Examples, uiClipWords(list[k].Task, 100))
					}
				}
				out = append(out, r)
			}
		}
		untested := 0
		var ex []string
		for _, s := range list {
			if s.Unverified {
				untested++
				if len(ex) < 3 {
					ex = append(ex, uiClipWords(s.Task, 100))
				}
			}
		}
		if untested >= uiRepeatMin {
			out = append(out, uiRepeat{Kind: "untested", Folder: list[0].Name, Path: cwd, Count: untested, Examples: ex})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func (r uiRepeat) line() string {
	switch r.Kind {
	case "untested":
		return r.Folder + ": " + uiItoa(r.Count) + " sessions changed files and never ran the tests. For example: " + strings.Join(r.Examples, " | ")
	}
	return r.Folder + ": the same kind of task " + uiItoa(r.Count) + " times over " + uiItoa(r.Days) + " days. For example: " + strings.Join(r.Examples, " | ")
}

// agentSuggestIfDue checks for repeated work at most once a day, and only when
// at least a few new sessions have been named since the last check.
func (m *uiManager) agentSuggestIfDue() {
	lib := m.sessionLibrary()
	if !lib.Analysis.On || lib.Totals.Analyzed < uiCoachMin {
		return
	}
	m.idxMu.Lock()
	m.loadIndexLocked()
	last, based := m.index.LastAgentCheck, m.index.AgentCheckBased
	m.idxMu.Unlock()
	if !last.IsZero() && (time.Since(last) < uiAgentCheckEvery || lib.Totals.Analyzed < based+uiAgentCheckNew) {
		return
	}
	_, _ = m.agentSuggestRun()
}
