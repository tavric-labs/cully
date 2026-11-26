package cully

import (
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
