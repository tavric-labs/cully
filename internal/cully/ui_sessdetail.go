package cully

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const uiSessionCallPage = 100

type uiSessionCall struct {
	Number int        `json:"number"`
	Name   string     `json:"name"`
	Kind   string     `json:"kind,omitempty"`
	At     *time.Time `json:"at,omitempty"`
	Result string     `json:"result,omitempty"`
	Params string     `json:"params,omitempty"` // the call's arguments, redacted and clipped; read from the harness's own record when the page is opened
}

const uiCallParamsMax = 4000

// Activity is built on demand from the harness's own record; Cully keeps no copy.
// It holds tool names, observed results, file paths and each call's redacted
// arguments, with no tool output or conversation text.
type uiSessionActivity struct {
	Calls []uiSessionCall `json:"calls"`
	Files []string        `json:"files"`
	start int
	count int
	ids   map[string]int
}

func uiActivityCapture(capture []*uiSessionActivity) *uiSessionActivity {
	if len(capture) == 0 {
		return nil
	}
	return capture[0]
}

func (a *uiSessionActivity) call(id, name, stamp, params string) {
	if a == nil {
		return
	}
	n := a.count
	a.count++
	if n < a.start || len(a.Calls) >= uiSessionCallPage {
		return
	}
	call := uiSessionCall{Number: n + 1, Name: name, Params: params}
	if at := uiParseTime(stamp); !at.IsZero() {
		call.At = &at
	}
	if id != "" {
		a.ids[id] = len(a.Calls)
	}
	a.Calls = append(a.Calls, call)
}

func (a *uiSessionActivity) classify(kind string) {
	if a == nil || len(a.Calls) == 0 {
		return
	}
	if last := &a.Calls[len(a.Calls)-1]; last.Number == a.count {
		last.Kind = kind
	}
}

func (a *uiSessionActivity) result(id string, failed bool) {
	if a == nil {
		return
	}
	if i, ok := a.ids[id]; ok {
		a.Calls[i].Result = "returned"
		if failed {
			a.Calls[i].Result = "failed"
		}
	}
}

func (a *uiSessionActivity) setFiles(files map[string]struct{}) {
	if a == nil {
		return
	}
	for p := range files {
		a.Files = append(a.Files, p)
	}
	sort.Strings(a.Files)
}

type uiSessionDetail struct {
	Session  uiIndexed         `json:"session"`
	Activity uiSessionActivity `json:"activity"`
	Snapshot uiSnapshot        `json:"snapshot"`
	Exists   bool              `json:"exists"`
}

func (m *uiManager) sessionDetail(id string, offset int) (uiSessionDetail, error) {
	e, err := m.sessionEntry(id)
	if err != nil {
		return uiSessionDetail{}, err
	}
	activity := uiSessionActivity{Calls: []uiSessionCall{}, Files: []string{}, start: offset, ids: map[string]int{}}
	facts, _, ok := uiScanAgent(e.Agent, e.File, false, &activity)
	if !ok {
		return uiSessionDetail{}, errors.New("the session record is unavailable")
	}
	e.uiSessionFacts = facts
	snap := uiHarnessSnapshot(e.Agent, e.File)
	exists, _ := osStatDir(e.Cwd)
	for i, p := range activity.Files {
		if rel, err := filepath.Rel(e.Cwd, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			p = rel
		}
		activity.Files[i] = m.cfg.Tokens.redact(redactSecrets(p))
	}
	for i := range activity.Calls {
		activity.Calls[i].Name = m.cfg.Tokens.redact(redactSecrets(activity.Calls[i].Name))
		if pr := activity.Calls[i].Params; pr != "" {
			activity.Calls[i].Params = uiClipRaw(m.cfg.Tokens.redact(uiRedactParamsText(uiTaskSecretLike.ReplaceAllString(redactSecrets(pr), "[redacted]"))), uiCallParamsMax)
		}
	}
	return uiSessionDetail{Session: uiSessionView(e), Activity: activity, Snapshot: snap, Exists: exists}, nil
}

func uiSessionView(e uiIndexed) uiIndexed {
	e.File = ""
	e.Health, e.Signals = uiHealthOf(e)
	return e
}

// Use a file name only for single-file work. Multiple edited files do not
// establish which one the session was mainly about.
func uiFocusFile(files map[string]struct{}) string {
	if len(files) == 1 {
		for p := range files {
			return uiClipWords(baseName(p), 64)
		}
	}
	return ""
}

// uiClipRaw cuts s to n runes without collapsing its whitespace, so a command or a diff stays readable.
func uiClipRaw(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "\n… (cut; the full call is in the harness's own record)"
	}
	return s
}

// uiSecretKey matches argument names that hold a credential. Their values are replaced whole, whatever they look like.
var uiSecretKey = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|apikey|authori[sz]ation|auth|credential|private[_-]?key|cookie|bearer|session[_-]?id|signature|access[_-]?key|client[_-]?secret)`)

// uiSecretPair finds a credential key with its value in free text (the key may be quoted); only the value is replaced.
var uiSecretPair = regexp.MustCompile(`(?i)((?:pass(?:word|wd)?|secret|token|api[_-]?key|authori[sz]ation|credential|cookie|access[_-]?key)(?:\\?["'])?\s*[:=]\s*(?:\\?["'])?)([^\s"',;&\\]+)`)

// uiSecretShape finds credentials by their form alone: a Bearer token, a private key block, and common provider key prefixes.
var uiSecretShape = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)|\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|GOCSPX-[A-Za-z0-9_-]{10,}|AIza[0-9A-Za-z_-]{30,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,})`)

// uiRedactValue walks a decoded argument value and blanks the value of every credential-named key, so the
// secret is removed from the structure before it is turned into text.
func uiRedactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if uiSecretKey.MatchString(k) {
				out[k] = "[redacted]"
			} else {
				out[k] = uiRedactValue(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = uiRedactValue(val)
		}
		return out
	}
	return v
}

// uiRedactParamsText is the second pass over the finished text: the structured pass cannot see a secret
// inside a shell command or a file's content.
func uiRedactParamsText(s string) string {
	s = uiSecretShape.ReplaceAllString(s, "[redacted]")
	return uiSecretPair.ReplaceAllStringFunc(s, func(m string) string {
		g := uiSecretPair.FindStringSubmatch(m)
		if strings.HasPrefix(g[2], "[redacted]") {
			return m
		}
		return g[1] + "[redacted]"
	})
}

// uiPrettyParams renders a call's arguments as indented JSON, with credential-named values already removed.
func uiPrettyParams(v any) string {
	b, err := json.MarshalIndent(uiRedactValue(v), "", "  ")
	if err != nil || string(b) == "null" || string(b) == "{}" {
		return ""
	}
	return string(b)
}
