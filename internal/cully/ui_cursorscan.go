package cully

import (
	"bufio"
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

// Cursor keeps a plain JSONL transcript of each agent chat under its projects
// folder: ~/.cursor/projects/<project>/agent-transcripts/<id>/<id>.jsonl. It
// records requests and tool calls but no timestamps and no results, so time
// worked and tool errors are not available for Cursor. The project folder is
// named after the path with its slashes turned into dashes, which is turned
// back by looking for the folders that exist.

func uiCursorProjectsDir() string {
	if d := os.Getenv("CURSOR_HOME"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor", "projects")
}

func uiCursorFiles() []uiHistFile {
	var out []uiHistFile
	dirs, _ := filepath.Glob(filepath.Join(uiCursorProjectsDir(), "*", "agent-transcripts", "*"))
	for _, d := range dirs {
		id := filepath.Base(d)
		if !uiSessionID.MatchString(id) {
			continue
		}
		p := filepath.Join(d, id+".jsonl")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, uiHistFile{p, info.ModTime()})
		}
	}
	return out
}

// uiCursorSlugPath turns a Cursor project folder name back into the path it
// stands for, preferring the longest folder names that exist.
func uiCursorSlugPath(slug string) string {
	parts := strings.Split(slug, "-")
	var walk func(dir string, i int) string
	walk = func(dir string, i int) string {
		if i == len(parts) {
			return dir
		}
		for j := len(parts); j > i; j-- {
			next := filepath.Join(dir, strings.Join(parts[i:j], "-"))
			if st, err := os.Stat(next); err == nil && st.IsDir() {
				if got := walk(next, j); got != "" {
					return got
				}
			}
		}
		return ""
	}
	return walk("/", 0)
}

var uiCursorStamp = regexp.MustCompile(`<timestamp>([^<]+)</timestamp>`)

type uiCursorRec struct {
	Role    string `json:"role"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type uiCursorBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func uiCursorBlocks(raw json.RawMessage) []uiCursorBlock {
	var blocks []uiCursorBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return blocks
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return []uiCursorBlock{{Type: "text", Text: s}}
	}
	return nil
}

func uiCursorUserText(t string) string {
	if i := strings.Index(t, "<user_query>"); i >= 0 {
		t = t[i+len("<user_query>"):]
		if j := strings.Index(t, "</user_query>"); j >= 0 {
			t = t[:j]
		}
	} else {
		t = uiCursorStamp.ReplaceAllString(t, "")
	}
	return strings.TrimSpace(t)
}

func scanCursor(path string, withDigest bool) (uiSessionFacts, uiDigest, bool) {
	f, err := os.Open(path)
	if err != nil {
		return uiSessionFacts{}, uiDigest{}, false
	}
	defer f.Close()
	var facts uiSessionFacts
	var dg uiDigest
	files := map[string]struct{}{}
	last, run, sinceCheck := "", 0, false
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec uiCursorRec
			if json.Unmarshal(line, &rec) == nil {
				for _, b := range uiCursorBlocks(rec.Message.Content) {
					switch {
					case rec.Role == "user" && b.Type == "text":
						if t := uiCursorUserText(b.Text); t != "" {
							facts.Turns++
							if withDigest && dg.First == "" {
								dg.First = t
							}
						}
					case rec.Role == "assistant" && b.Type == "text":
						if withDigest && strings.TrimSpace(b.Text) != "" {
							dg.Last = b.Text
						}
					case b.Type == "tool_use":
						facts.Tools++
						if facts.ToolNames == nil {
							facts.ToolNames = map[string]int{}
						}
						facts.ToolNames[b.Name]++
						in, _ := json.Marshal(b.Input)
						hash := b.Name + string(in)
						if hash == last {
							run++
						} else {
							last, run = hash, 1
						}
						if run > facts.MaxRepeat {
							facts.MaxRepeat = run
						}
						switch b.Name {
						case "StrReplace", "Write", "EditNotebook", "Delete":
							facts.Edits++
							sinceCheck = true
							if p, ok := b.Input["path"].(string); ok {
								files[p] = struct{}{}
							}
						case "Shell":
							if cmd, ok := b.Input["command"].(string); ok && uiCheckCommand.MatchString(cmd) {
								facts.Checks++
								sinceCheck = false
							}
						}
					}
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return facts, dg, false
			}
			break
		}
	}
	facts.Files = len(files)
	facts.Unverified = facts.Edits > 0 && sinceCheck
	if withDigest {
		for p := range files {
			dg.Files = append(dg.Files, p)
		}
		sort.Strings(dg.Files)
		if len(dg.Files) > 15 {
			dg.Files = dg.Files[:15]
		}
	}
	return facts, dg, true
}

func uiCursorSession(f uiHistFile) (uiPastSession, bool) {
	id := uiSessionIDFromFile(f.path)
	if !uiSessionID.MatchString(id) {
		return uiPastSession{}, false
	}
	slug := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(f.path))))
	cwd := uiCursorSlugPath(slug)
	if cwd == "" {
		return uiPastSession{}, false
	}
	s := uiPastSession{ID: id, Agent: "cursor", Cwd: cwd, Origin: "Cursor", Started: f.mod, Updated: f.mod}
	head, _ := uiReadEnds(f.path)
	for _, line := range head {
		var rec uiCursorRec
		if json.Unmarshal(line, &rec) != nil || rec.Role != "user" {
			continue
		}
		for _, b := range uiCursorBlocks(rec.Message.Content) {
			if m := uiCursorStamp.FindStringSubmatch(b.Text); m != nil {
				if t, err := time.Parse("Monday, Jan 2, 2006, 3:04 PM", strings.TrimSpace(strings.TrimSuffix(m[1], "(UTC)"))); err == nil {
					s.Started = t
				}
			}
		}
		break
	}
	q := uiShellQuote(cwd)
	s.Resume = "cd " + q + " && cursor-agent --resume " + id
	return s, true
}
