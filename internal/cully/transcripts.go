package cully

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Each coding agent keeps its own session transcripts in its own folder and
// format. A transcriptSource says where they are and how to read tool results
// from one line, so measurements follow the agent that is running instead of
// assuming Claude Code.

type transcriptFile struct {
	Path    string
	Mod     time.Time
	Project string // the agent's own key for the project: a folder name or a directory
}

type transcriptSource struct {
	Agent, Name string
	// Files lists transcripts changed since the time, optionally for one
	// project. root overrides the agent's default transcript folder.
	Files func(root, project string, since time.Time) []transcriptFile
	// Scan feeds one transcript line into the waste counters and returns the
	// number of subagent delegations it records.
	Scan func(w *TokenWaste, line []byte) int
}

var transcriptSources = map[string]transcriptSource{
	"claude": {Agent: "claude", Name: "Claude", Files: claudeTranscripts, Scan: scanWasteLine},
	"codex":  {Agent: "codex", Name: "Codex", Files: codexTranscripts, Scan: scanCodexLine},
}

// transcriptSourceFor returns the source for an agent, if Cully can read its
// transcripts. Cursor's CLI keeps its history in a format that is not read yet.
func transcriptSourceFor(agent string) (transcriptSource, bool) {
	src, ok := transcriptSources[agent]
	return src, ok
}

// claudeTranscripts lists <root>/<project folder>/*.jsonl.
func claudeTranscripts(root, project string, since time.Time) []transcriptFile {
	if root == "" {
		root = filepath.Join(ConfigDir(), "projects")
	}
	var folders []string
	if project != "" {
		folders = []string{project}
	} else {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.IsDir() {
				folders = append(folders, e.Name())
			}
		}
	}
	var out []transcriptFile
	for _, folder := range folders {
		entries, _ := os.ReadDir(filepath.Join(root, folder))
		for _, d := range entries {
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				continue
			}
			if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
				out = append(out, transcriptFile{Path: filepath.Join(root, folder, d.Name()), Mod: info.ModTime(), Project: folder})
			}
		}
	}
	return out
}

// codexTranscripts lists <root>/YYYY/MM/DD/rollout-*.jsonl. Codex records the
// project directory in the first line, so a project is matched by that.
func codexTranscripts(root, project string, since time.Time) []transcriptFile {
	if root == "" {
		root = filepath.Join(CodexConfigDir(), "sessions")
	}
	var out []transcriptFile
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since) {
			return nil
		}
		cwd := codexSessionCwd(path)
		if project != "" && cwd != project {
			return nil
		}
		out = append(out, transcriptFile{Path: path, Mod: info.ModTime(), Project: cwd})
		return nil
	})
	return out
}

// codexSessionCwd reads the working directory from a rollout's first line.
func codexSessionCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 1<<20).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return ""
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &meta) != nil || meta.Type != "session_meta" {
		return ""
	}
	return meta.Payload.Cwd
}

// codexToolName maps Codex tools onto the names the waste counters know:
// every shell or exec tool is Bash, since Codex reads files through the shell.
func codexToolName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case lower == "", strings.Contains(lower, "shell"), strings.Contains(lower, "exec"), strings.Contains(lower, "bash"), strings.Contains(lower, "command"):
		return "Bash"
	case lower == "apply_patch":
		return "Edit"
	}
	return name
}

// scanCodexLine reads one Codex rollout line. Tool calls and their outputs are
// paired by call_id, as Claude's tool_use and tool_result are.
func scanCodexLine(w *TokenWaste, line []byte) int {
	var e struct {
		Type    string `json:"type"`
		Payload struct {
			Type   string          `json:"type"`
			Name   string          `json:"name"`
			CallID string          `json:"call_id"`
			Output json.RawMessage `json:"output"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "response_item" || e.Payload.CallID == "" {
		return 0
	}
	switch e.Payload.Type {
	case "function_call", "custom_tool_call", "local_shell_call":
		w.useCall(e.Payload.CallID, codexToolName(e.Payload.Name), false)
		if e.Payload.Name == "spawn_agent" && w.firstUse(e.Payload.CallID) {
			return 1
		}
	case "function_call_output", "custom_tool_call_output":
		w.result(e.Payload.CallID, codexOutputText(e.Payload.Output))
	}
	return 0
}

// codexOutputText extracts the text of a tool output, which is a string, a list
// of content items, or an object holding either.
func codexOutputText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var items []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &items) == nil {
		var b strings.Builder
		for _, it := range items {
			b.WriteString(it.Text)
		}
		return b.String()
	}
	var object struct {
		Content json.RawMessage `json:"content"`
		Output  json.RawMessage `json:"output"`
	}
	if json.Unmarshal(raw, &object) == nil {
		for _, inner := range []json.RawMessage{object.Output, object.Content} {
			if len(inner) > 0 {
				return codexOutputText(inner)
			}
		}
	}
	return string(raw)
}

// measureTranscripts measures the newest transcripts, at most max.
func measureTranscripts(files []transcriptFile, max int, scan func(*TokenWaste, []byte) int) harnessEvidence {
	sort.Slice(files, func(i, j int) bool { return files[i].Mod.After(files[j].Mod) })
	if len(files) > max {
		files = files[:max]
	}
	var ev harnessEvidence
	for _, p := range files {
		f, err := os.Open(p.Path)
		if err != nil {
			continue
		}
		w := newTokenWaste()
		lines, delegations := 0, 0
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 8<<20)
		for sc.Scan() {
			if len(strings.TrimSpace(string(sc.Bytes()))) == 0 {
				continue
			}
			lines++
			delegations += scan(w, sc.Bytes())
		}
		f.Close()
		if sc.Err() != nil || lines < 20 {
			continue
		}
		ev.Sessions++
		ev.BigTokens += w.BigTokens
		ev.BashTokens += w.BigBashTokens
		ev.ReadTokens += w.FullReadTokens
		ev.Delegations += delegations
	}
	return ev
}

// measureAgentEvidence measures one project's recent sessions for an agent.
func measureAgentEvidence(src transcriptSource, root, project string, days, max int) harnessEvidence {
	if project == "" || days <= 0 || max <= 0 {
		return harnessEvidence{}
	}
	return measureTranscripts(src.Files(root, project, time.Now().AddDate(0, 0, -days)), max, src.Scan)
}

// measureAgentProjects measures every project, grouped by the agent's own key.
func measureAgentProjects(src transcriptSource, root, only string, days, max int) (harnessEvidence, map[string]harnessEvidence) {
	var total harnessEvidence
	by := map[string]harnessEvidence{}
	groups := map[string][]transcriptFile{}
	for _, f := range src.Files(root, "", time.Now().AddDate(0, 0, -days)) {
		if f.Project != "" && strings.Contains(f.Project, only) {
			groups[f.Project] = append(groups[f.Project], f)
		}
	}
	for project, files := range groups {
		ev := measureTranscripts(files, max, src.Scan)
		if ev.Sessions == 0 {
			continue
		}
		by[project] = ev
		total.Sessions += ev.Sessions
		total.BigTokens += ev.BigTokens
		total.BashTokens += ev.BashTokens
		total.ReadTokens += ev.ReadTokens
		total.Delegations += ev.Delegations
	}
	return total, by
}

// detectTranscriptAgent picks the agent whose transcripts for this project
// are newest, so measurements follow the agent you actually use. CULLY_AGENT,
// set inside the Cully terminal, wins when Cully can read that agent.
func detectTranscriptAgent(project, root string) string {
	if agent := os.Getenv("CULLY_AGENT"); agent != "" {
		if _, ok := transcriptSourceFor(agent); ok {
			return agent
		}
	}
	best, bestTime := "claude", time.Time{}
	since := time.Now().AddDate(-1, 0, 0)
	for _, agent := range []string{"claude", "codex"} {
		src := transcriptSources[agent]
		match := project
		if agent == "claude" {
			match = projectFolderMatch(project)
		}
		for _, f := range src.Files(root, match, since) {
			if f.Mod.After(bestTime) {
				best, bestTime = agent, f.Mod
			}
		}
	}
	return best
}
