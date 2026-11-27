package cully

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// uiTaskTitle makes a short, readable name for a task from the words it was given: the first
// sentence without the politeness in front, links and long paths shortened to something you can
// read, capitalised, at most eight words. "can you please look at why the gateway times out when
// the upstream is slow?" becomes "Look at why the gateway times out when the".
var (
	uiTitleFiller = regexp.MustCompile(`(?i)^(?:(?:hey|hi|ok|okay|so|now|also|then|and|well|please|pls|kindly|just|can you|could you|would you|will you|can we|could we|i want you to|i want to|i need you to|i need to|i would like you to|i'd like you to|let's|lets|we need to|we should|go ahead and|try to|help me to|help me|you should|you need to)(?:[\s,:]+|$))+`)
	uiTitleURL    = regexp.MustCompile(`https?://\S+`)
	uiTitlePath   = regexp.MustCompile(`(?:~|\.{0,2})?/[\w.\-@]+(?:/[\w.\-@]+)+`)
	uiTitleStop   = regexp.MustCompile(`[.?!;](?:\s|$)`)
	uiTitleMarks  = regexp.MustCompile("[`*#]+|^>+")
)

var uiTitleWeak = map[string]bool{"a": true, "an": true, "the": true, "of": true, "to": true, "and": true, "or": true, "with": true, "in": true, "on": true, "for": true, "that": true, "when": true, "is": true, "so": true, "because": true, "if": true, "my": true, "our": true}

const (
	uiTitleWords = 8
	uiTitleChars = 56
)

func uiTaskTitle(prompt string) string {
	s := strings.TrimSpace(prompt)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	s = uiTitleURL.ReplaceAllString(s, "")
	s = uiTitlePath.ReplaceAllStringFunc(s, func(p string) string { return filepath.Base(p) })
	s = uiTitleMarks.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	for {
		t := strings.TrimSpace(uiTitleFiller.ReplaceAllString(s, ""))
		if t == s {
			break
		}
		s = t
	}
	if loc := uiTitleStop.FindStringIndex(s); loc != nil && loc[0] > 12 { // the first sentence; a dot inside a file name is not a stop
		s = s[:loc[0]]
	}
	words := strings.Fields(s)
	if len(words) > uiTitleWords {
		words = words[:uiTitleWords]
	}
	for len(words) > 3 && uiTitleWeak[strings.ToLower(strings.Trim(words[len(words)-1], ",:;"))] { // do not end on "the" or "with"
		words = words[:len(words)-1]
	}
	s = strings.Join(words, " ")
	s = strings.TrimRight(s, " ,:;-–—(")
	if len([]rune(s)) > uiTitleChars {
		r := []rune(s)[:uiTitleChars]
		s = strings.TrimRight(string(r), " ,:;-–—(")
		if i := strings.LastIndex(s, " "); i > 20 {
			s = s[:i]
		}
	}
	if s == "" {
		return "New task"
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

var uiHexTitle = regexp.MustCompile(`^[0-9a-f]{6,}(?:-[0-9a-f]{3,})*$`)

// uiWeakTaskTitle reports whether a task's name says too little to find it by: empty, one or two words,
// a bare session id, or a "Continue:" handoff title, which names the earlier session and not the work.
func uiWeakTaskTitle(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" || uiHexTitle.MatchString(strings.ToLower(t)) {
		return true
	}
	if strings.HasPrefix(t, "Continue:") {
		return true
	}
	return len(strings.Fields(t)) <= 2
}

// retitleTasks is the background titler. A task is named from the words it was started with, which is often
// a one-word prompt or a handoff. Once one of its sessions has been analysed, that analysis says what the work
// really was, so the task takes its title and summary from the latest analysed session. It costs no tokens: the
// analysis already exists. The new title and summary are saved with the task, so they reach Cully memory
// through the normal task sync (the MCP, or PostgreSQL in self-hosted mode) with no extra store.
func (m *uiManager) retitleTasks() int {
	lib := m.sessionLibrary()
	by := map[string]uiIndexed{}
	for _, s := range lib.Sessions {
		if s.Source == "ai" && s.Title != "" {
			by[s.ID] = s
		}
	}
	if len(by) == 0 {
		return 0
	}
	changed := 0
	uiTaskMu.Lock()
	defer uiTaskMu.Unlock()
	for _, raw := range uiDB().all("task") {
		var r uiTaskRecord
		if json.Unmarshal(raw, &r) != nil || r.ID == "" {
			continue
		}
		var best *uiIndexed
		analysed := 0
		for i := len(r.Sessions) - 1; i >= 0; i-- {
			if s, ok := by[r.Sessions[i].ID]; ok {
				analysed++
				if best == nil {
					c := s
					best = &c
				}
			}
		}
		if best == nil {
			continue
		}
		title, summary := r.Title, best.Description
		if uiWeakTaskTitle(r.Title) {
			title = best.Title
		}
		if len(r.Sessions) > 1 && summary != "" && analysed < len(r.Sessions) {
			summary += " (" + uiItoa(len(r.Sessions)) + " sessions; the latest analysed one is described.)"
		}
		if title == r.Title && summary == r.Summary {
			continue
		}
		r.Title, r.Summary = title, summary
		uiDB().put("task", r.ID, r)
		changed++
	}
	return changed
}
