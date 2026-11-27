package cully

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Past sessions: what the coding agent itself recorded on this machine, read
// from its own folders (Claude keeps transcripts under its config folder's
// projects/, Codex under sessions/). Only a few facts are read from each file,
// never the conversation.

const (
	uiHistoryMax    = 200
	uiHistoryChunk  = 128 << 10
	uiOpenWindow    = 15 * time.Minute
	uiHistoryAllMax = 3650
)

var uiSessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type uiPastSession struct {
	ID      string    `json:"id"`
	Agent   string    `json:"agent"`
	Cwd     string    `json:"cwd"`
	Name    string    `json:"name"`
	Branch  string    `json:"branch,omitempty"`
	Title   string    `json:"title,omitempty"` // the agent's own title, when it recorded one
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
	Open    bool      `json:"open"`   // a session is running in this folder and was active moments ago
	Exists  bool      `json:"exists"` // the folder is still there
	Resume  string    `json:"resume"` // terminal command that continues this session
	Fork    string    `json:"fork,omitempty"`
	// Origin says where the session was started: a terminal, an editor, a script, and so on.
	Origin string `json:"origin,omitempty"`
	// From the session library, when it has this session.
	Description string `json:"description,omitempty"`
	Different   string `json:"different,omitempty"`
	ActiveSec   int    `json:"activeSec,omitempty"`
	// Facts and verdict from the session library, when it has this session.
	Library *uiLibSummary `json:"library,omitempty"`
}

type uiLibSummary struct {
	Task      string          `json:"task,omitempty"`
	Completed string          `json:"completed,omitempty"`
	OnTask    string          `json:"onTask,omitempty"`
	Drift     string          `json:"drift,omitempty"`
	Turns     int             `json:"turns"`
	Tools     int             `json:"tools"`
	Files     int             `json:"files"`
	Checks    int             `json:"checks"`
	Errors    int             `json:"errors"`
	ToolNames map[string]int  `json:"toolNames,omitempty"`
	Named     bool            `json:"named"`
	Steps     []uiSessionStep `json:"steps,omitempty"`
}

type uiHistory struct {
	Agent    string          `json:"agent"`
	Days     int             `json:"days"`
	Older    bool            `json:"older"` // more sessions exist beyond this window
	Sessions []uiPastSession `json:"sessions"`
	Note     string          `json:"note,omitempty"`
}

func uiShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func uiCodexSessionsDir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return filepath.Join(d, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
}

type uiHistFile struct {
	path string
	mod  time.Time
}

// uiReadEnds returns the first and last chunk of a file as lines. The first
// line of the tail is dropped because it may be cut.
func uiReadEnds(path string) (head, tail [][]byte) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil
	}
	buf := make([]byte, uiHistoryChunk)
	n, _ := io.ReadFull(f, buf)
	head = bytes.Split(buf[:n], []byte("\n"))
	if st.Size() > int64(n) {
		head = head[:len(head)-1] // last line is probably cut
		off := st.Size() - uiHistoryChunk
		if off < int64(n) {
			off = int64(n)
		}
		tb := make([]byte, st.Size()-off)
		m, _ := f.ReadAt(tb, off)
		lines := bytes.Split(tb[:m], []byte("\n"))
		if len(lines) > 1 {
			tail = lines[1:]
		}
	}
	return head, tail
}

type uiClaudeRec struct {
	Type        string `json:"type"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	Timestamp   string `json:"timestamp"`
	AITitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	Sidechain   bool   `json:"isSidechain"`
	Entry       string `json:"entrypoint"`
}

func uiParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func uiClaudeSession(f uiHistFile) (uiPastSession, bool) {
	id := strings.TrimSuffix(filepath.Base(f.path), ".jsonl")
	if !uiSessionID.MatchString(id) {
		return uiPastSession{}, false
	}
	head, tail := uiReadEnds(f.path)
	s := uiPastSession{ID: id, Agent: "claude", Updated: f.mod}
	var customTitle string
	for _, line := range head {
		var r uiClaudeRec
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if r.Sidechain {
			continue
		}
		if s.Cwd == "" && r.Cwd != "" {
			s.Cwd, s.Branch = r.Cwd, r.GitBranch
		}
		if s.Origin == "" && r.Entry != "" {
			s.Origin = uiOriginLabel("claude", r.Entry)
		}
		if s.Started.IsZero() && r.Timestamp != "" {
			s.Started = uiParseTime(r.Timestamp)
		}
		if r.AITitle != "" {
			s.Title = r.AITitle
		}
		if r.CustomTitle != "" {
			customTitle = r.CustomTitle
		}
	}
	for _, line := range tail {
		var r uiClaudeRec
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if r.AITitle != "" {
			s.Title = r.AITitle
		}
		if r.CustomTitle != "" {
			customTitle = r.CustomTitle
		}
		if t := uiParseTime(r.Timestamp); !t.IsZero() && t.After(s.Updated) {
			s.Updated = t
		}
	}
	if s.Cwd == "" {
		return uiPastSession{}, false
	}
	if s.Started.IsZero() {
		s.Started = f.mod
	}
	s.Title = strings.TrimSpace(strings.Join(strings.Fields(firstNonEmpty(customTitle, s.Title)), " "))
	if r := []rune(s.Title); len(r) > 100 {
		s.Title = string(r[:100]) + "…"
	}
	q := uiShellQuote(s.Cwd)
	s.Resume = "cd " + q + " && claude --resume " + id
	s.Fork = "cd " + q + " && claude --resume " + id + " --fork-session"
	return s, true
}

func uiCodexSession(f uiHistFile) (uiPastSession, bool) {
	head, _ := uiReadEnds(f.path)
	if len(head) == 0 {
		return uiPastSession{}, false
	}
	var r struct {
		Type    string `json:"type"`
		Payload struct {
			ID         string          `json:"id"`
			Cwd        string          `json:"cwd"`
			Timestamp  string          `json:"timestamp"`
			Originator string          `json:"originator"`
			Source     json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(head[0], &r) != nil || r.Type != "session_meta" || !uiSessionID.MatchString(r.Payload.ID) || r.Payload.Cwd == "" {
		return uiPastSession{}, false
	}
	src := strings.Trim(string(r.Payload.Source), `"`)
	if strings.HasPrefix(src, "{") && strings.Contains(src, "subagent") {
		src = "subagent"
	}
	s := uiPastSession{ID: r.Payload.ID, Agent: "codex", Origin: uiOriginLabel("codex", src), Cwd: r.Payload.Cwd, Started: uiParseTime(r.Payload.Timestamp), Updated: f.mod}
	if s.Started.IsZero() {
		s.Started = f.mod
	}
	q := uiShellQuote(s.Cwd)
	s.Resume = "cd " + q + " && codex resume " + s.ID
	s.Fork = "cd " + q + " && codex fork " + s.ID
	return s, true
}

func uiHistoryFiles(agent string) []uiHistFile {
	var out []uiHistFile
	add := func(path string, d os.DirEntry) {
		if info, err := d.Info(); err == nil {
			out = append(out, uiHistFile{path, info.ModTime()})
		}
	}
	switch agent {
	case "claude":
		root := filepath.Join(ConfigDir(), "projects")
		dirs, _ := os.ReadDir(root)
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			files, _ := os.ReadDir(filepath.Join(root, d.Name()))
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".jsonl") {
					add(filepath.Join(root, d.Name(), f.Name()), f)
				}
			}
		}
	case "cursor":
		out = append(out, uiCursorFiles()...)
	case "codex":
		_ = filepath.WalkDir(uiCodexSessionsDir(), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
				add(p, d)
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

// history lists past sessions of one agent from the last `days` days (0 means
// all), newest first, skipping folders the user chose to hide and the
// worktrees Cully's own agents use.
func (m *uiManager) history(agent string, days int) uiHistory {
	if agent == "all" {
		out := uiHistory{Agent: "all", Days: days, Sessions: []uiPastSession{}}
		for _, a := range []string{"claude", "codex", "cursor"} {
			part := m.history(a, days)
			out.Sessions = append(out.Sessions, part.Sessions...)
			out.Older = out.Older || part.Older
			if part.Note != "" {
				out.Note = part.Note
			}
		}
		sort.SliceStable(out.Sessions, func(i, j int) bool { return out.Sessions[i].Updated.After(out.Sessions[j].Updated) })
		return out
	}
	h := uiHistory{Agent: agent, Days: days, Sessions: []uiPastSession{}}
	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	own := filepath.Join(ConfigDir(), "ui-worktrees")
	openCwd := map[string]bool{}
	for _, s := range m.sessions() {
		if s.Agent == agent {
			openCwd[s.Cwd] = true
		}
	}
	for _, f := range uiHistoryFiles(agent) {
		if !cutoff.IsZero() && f.mod.Before(cutoff) {
			h.Older = true
			break
		}
		if len(h.Sessions) >= uiHistoryMax {
			h.Older = true
			break
		}
		var s uiPastSession
		var ok bool
		switch agent {
		case "claude":
			s, ok = uiClaudeSession(f)
		case "cursor":
			s, ok = uiCursorSession(f)
		default:
			s, ok = uiCodexSession(f)
		}
		if !ok || uiHidden(s.Cwd, m.cfg.HideSessionsIn) || uiHidden(s.Cwd, []string{own}) {
			continue
		}
		s.Name = filepath.Base(s.Cwd)
		if st, err := os.Stat(s.Cwd); err == nil && st.IsDir() {
			s.Exists = true
		}
		s.Open = openCwd[s.Cwd] && time.Since(s.Updated) < uiOpenWindow
		m.idxMu.Lock()
		m.loadIndexLocked()
		if e := m.index.Sessions[s.ID]; e != nil {
			if e.Title != "" {
				s.Title = e.Title
			}
			s.Description, s.Different, s.ActiveSec = e.Description, e.Different, e.ActiveSec
			if s.Origin == "" {
				s.Origin = e.Origin
			}
			s.Library = &uiLibSummary{Task: e.Task, Completed: e.Completed, OnTask: e.OnTask, Drift: e.Drift, Turns: e.Turns, Tools: e.Tools, Files: e.Files, Checks: e.Checks, Errors: e.Errors, ToolNames: e.ToolNames, Named: e.Source == "ai", Steps: e.Steps}
		}
		m.idxMu.Unlock()
		h.Sessions = append(h.Sessions, s)
	}
	return h
}

// findPast looks up one past session by id, from the agent's own folder only.
func (m *uiManager) findPast(agent, id string) (uiPastSession, bool) {
	if !uiSessionID.MatchString(id) {
		return uiPastSession{}, false
	}
	for _, f := range uiHistoryFiles(agent) {
		if agent == "claude" && strings.TrimSuffix(filepath.Base(f.path), ".jsonl") == id {
			s, ok := uiClaudeSession(f)
			if !ok {
				return s, false
			}
			s.Name = filepath.Base(s.Cwd)
			if st, err := os.Stat(s.Cwd); err == nil && st.IsDir() {
				s.Exists = true
			}
			for _, o := range m.sessions() {
				if o.Agent == agent && o.Cwd == s.Cwd && time.Since(s.Updated) < uiOpenWindow {
					s.Open = true
				}
			}
			return s, true
		}
	}
	return uiPastSession{}, false
}

// resumeSession continues a past Claude session as an agent this page runs,
// in the folder it started in. With fork the original session is left as it
// is and the agent works in a copy of its history.
func (m *uiManager) resumeSession(id, prompt string, fork bool) (*uiAgent, error) {
	return m.resumeSessionFor(id, prompt, fork, "")
}

// resumeSessionFor resumes a Claude session; taskRef, when set, is the task the new run belongs to.
func (m *uiManager) resumeSessionFor(id, prompt string, fork bool, taskRef string) (*uiAgent, error) {
	s, ok := m.findPast("claude", id)
	if !ok {
		return nil, errors.New("no such session")
	}
	if uiHidden(s.Cwd, m.cfg.HideSessionsIn) {
		return nil, errors.New("no such session")
	}
	if !s.Exists {
		return nil, errors.New("the folder this session worked in is gone")
	}
	if s.Open && !fork {
		return nil, errors.New("this session is open in a terminal; fork it instead so the two do not write to the same session")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "Continue where we left off."
	}
	m.mu.Lock()
	for _, a := range m.agents {
		if !fork && a.req.resumeID == id && !a.req.fork && uiLive(a.snapshot().State) {
			m.mu.Unlock()
			return nil, errors.New("this session is already running here")
		}
	}
	m.mu.Unlock()
	req := uiStartRequest{Prompt: prompt, resumeID: id, resumeDir: s.Cwd, fork: fork, taskRef: taskRef}
	if info, err := m.addRepo(s.Cwd); err == nil {
		req.Repo = info.Path
	} else if _, gitErr := uiRepoRoot(s.Cwd); gitErr != nil && !strings.Contains(err.Error(), "dubious ownership") {
		// A folder with no git cannot have a worktree, but a resumed session runs where it
		// started anyway, so it simply runs there.
		if !uiWritable(s.Cwd) {
			return nil, errors.New("you do not have write access to that folder")
		}
		req.inPlaceDir = s.Cwd
	} else {
		return nil, err
	}
	what := "Continue"
	if fork {
		what = "Fork"
	}
	title := s.Title
	if title == "" {
		title = s.Name
	}
	req.Title = what + ": " + title
	return m.start(req)
}

// uiOriginLabel turns what an agent records about how a session started into
// plain words. A value it does not know is shown as it was recorded.
func uiOriginLabel(agent, raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	switch agent {
	case "claude":
		switch {
		case raw == "cli":
			return "Terminal"
		case raw == "sdk-cli":
			return "Started by a tool"
		case raw == "sdk-py" || raw == "sdk-ts" || strings.HasPrefix(raw, "sdk"):
			return "Started by an app (SDK)"
		case strings.Contains(raw, "desktop"):
			return "Claude desktop"
		case strings.Contains(raw, "vscode"):
			return "VS Code"
		case strings.Contains(raw, "jetbrains"):
			return "JetBrains"
		}
	case "codex":
		switch raw {
		case "cli":
			return "Terminal"
		case "vscode":
			return "Editor or desktop app"
		case "exec":
			return "Started by a tool"
		case "subagent":
			return "Helper of another session"
		}
	}
	return raw
}
