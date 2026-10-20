package cully

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The session journal is the semantic record of one coding session: when the
// agent edited, searched, ran checks, saved memory, or failed. Timeline,
// handoff, rescue, loop detection and status are all derived from it.
//
// It is deliberately coarse. It never stores prompts, tool output, file
// contents, diffs, command arguments, environment values or URLs. A repeated
// failure is recognised by a one-way hash of the normalized command.
//
// Replay privacy guarantee. For each tool event the journal may additionally
// store, and only these:
//
//	Path  a project-relative file path from a structured file tool, at most
//	      200 characters. Absolute paths, paths outside the project, and
//	      anything with a URL scheme are dropped. Shell commands do not
//	      contribute paths.
//	To    the project-relative destination of a move, under the same rules.
//	Op    one of read, write, edit, create, delete, move.
//	Cmd   only the program and, for a known tool such as git or go, a
//	      recognized subcommand (for example "go test", "git commit"), when it
//	      matches ^[A-Za-z0-9._/-]+( [A-Za-z0-9._-]+)?$.
//
// Setting CULLY_JOURNAL_PATHS=0 turns off Path, To, Op and Cmd recording; the
// event class and success are still recorded. Old journals without these
// fields still parse. The whole session stays within journalMaxBytes.

// Journal classes. The first four match the single-letter hook counters.
const (
	journalEdit   = "E" // file edit or patch
	journalCheck  = "T" // test, vet or build check
	journalSearch = "S" // search or read
	journalOther  = "O" // any other tool
	journalMemory = "M" // a Cully memory tool call
	journalNote   = "N" // a Cully observation; Note holds a fixed keyword
	journalStart  = "B" // terminal session started
	journalEnd    = "X" // terminal session ended
	journalFileOp = "F" // extra file operation of a multi-file tool call (replay only)
)

// Fixed vocabulary for journalNote events. Free text is never recorded.
const (
	noteLoop              = "loop"
	noteVerifyGap         = "verify-gap"
	noteHistoryTrimmed    = "history-trimmed"
	noteHistoryUnreadable = "history-unreadable"
	noteHistoryInvalid    = "history-invalid"
)

const (
	journalMaxBytes = 256 * 1024
	journalKeep     = 40
	journalMaxAge   = 30 * 24 * time.Hour
)

type journalEvent struct {
	Time    time.Time `json:"t"`
	Agent   string    `json:"a,omitempty"`
	Class   string    `json:"c"`
	Failed  bool      `json:"f,omitempty"`
	Unknown bool      `json:"u,omitempty"` // the agent supplied no outcome
	Tool    string    `json:"tool,omitempty"`
	Sig     string    `json:"sig,omitempty"`
	Note    string    `json:"n,omitempty"`
	// Replay fields; see the privacy guarantee above.
	Path string `json:"p,omitempty"`
	To   string `json:"to,omitempty"`
	Op   string `json:"op,omitempty"`
	Cmd  string `json:"cmd,omitempty"`
}

func journalDir() string { return filepath.Join(cullyDir(), "journals") }

// projectKey identifies a project directory without storing its path.
func projectKey(cwd string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(cwd)))
	return hex.EncodeToString(sum[:6])
}

func journalPath(cwd, session string) string {
	return filepath.Join(journalDir(), projectKey(cwd)+"-"+safeSession(session)+".jsonl")
}

// appendJournal records one event. It never fails the caller: hooks must stay
// silent and fast. The file is locked so a concurrent hook cannot append while
// an over-size journal is rewritten.
func appendJournal(cwd, session string, event journalEvent) {
	if session == "" || cwd == "" {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(journalDir(), 0o700); err != nil {
		return
	}
	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	line = append(line, '\n')
	f, err := os.OpenFile(journalPath(cwd, session), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if lockFile(f) != nil {
		return
	}
	defer unlockFile(f)
	info, err := f.Stat()
	if err != nil {
		return
	}
	if info.Size() >= journalMaxBytes {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return
		}
		kept := keepHalfJournal(data)
		kept = append(kept, line...)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return
		}
		if _, err := f.Write(kept); err != nil {
			return
		}
		_ = f.Truncate(int64(len(kept)))
		return
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return
	}
	_, _ = f.Write(line)
}

// keepHalfJournal returns the newer half of a journal's lines.
func keepHalfJournal(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	kept = kept[len(kept)/2:]
	if len(kept) == 0 {
		return nil
	}
	// Keep a fixed, content-free marker through every subsequent rotation.
	var first journalEvent
	_ = json.Unmarshal([]byte(kept[0]), &first)
	marker, _ := json.Marshal(journalEvent{Time: first.Time, Agent: first.Agent, Class: journalNote, Note: noteHistoryTrimmed})
	return append(append(marker, '\n'), []byte(strings.Join(kept, "\n")+"\n")...)
}

func readJournal(path string) []journalEvent {
	f, err := os.Open(path)
	if err != nil {
		return []journalEvent{{Class: journalNote, Note: noteHistoryUnreadable}}
	}
	defer f.Close()
	var events []journalEvent
	invalid := false
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var event journalEvent
		if json.Unmarshal(scan.Bytes(), &event) == nil && validJournalClass(event.Class) {
			events = append(events, event)
		} else {
			invalid = true
		}
	}
	if invalid || scan.Err() != nil || len(events) == 0 {
		var stamp time.Time
		if len(events) > 0 {
			stamp = events[len(events)-1].Time
		}
		events = append(events, journalEvent{Time: stamp, Class: journalNote, Note: noteHistoryInvalid})
	}
	return events
}

func validJournalClass(class string) bool {
	switch class {
	case journalEdit, journalCheck, journalSearch, journalOther, journalMemory, journalNote, journalStart, journalEnd, journalFileOp, "A", "W", "G":
		return true
	}
	return false
}

type journalFile struct {
	Path     string
	Session  string
	Modified time.Time
}

// projectJournals lists a project's journals, newest first.
func projectJournals(cwd string) []journalFile {
	prefix := projectKey(cwd) + "-"
	entries, _ := os.ReadDir(journalDir())
	var files []journalFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, journalFile{
			Path:     filepath.Join(journalDir(), name),
			Session:  strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".jsonl"),
			Modified: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Modified.After(files[j].Modified) })
	return files
}

// latestJournal returns the newest journal for a project.
func latestJournal(cwd string) (journalFile, []journalEvent, bool) {
	files := projectJournals(cwd)
	if len(files) == 0 {
		return journalFile{}, nil, false
	}
	return files[0], readJournal(files[0].Path), true
}

// pruneJournals removes old journals. Each project keeps its newest few, and
// anything older than journalMaxAge is removed.
func pruneJournals() {
	entries, _ := os.ReadDir(journalDir())
	type item struct {
		path     string
		modified time.Time
	}
	groups := map[string][]item{}
	for _, entry := range entries {
		info, err := entry.Info()
		if entry.IsDir() || err != nil || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		name := entry.Name()
		project := name
		if i := strings.IndexByte(name, '-'); i > 0 {
			project = name[:i]
		}
		groups[project] = append(groups[project], item{filepath.Join(journalDir(), name), info.ModTime()})
	}
	for _, all := range groups {
		sort.Slice(all, func(i, j int) bool { return all[i].modified.After(all[j].modified) })
		for i, it := range all {
			if i >= journalKeep || time.Since(it.modified) > journalMaxAge {
				_ = os.Remove(it.path)
			}
		}
	}
}

var commandWhitespace = regexp.MustCompile(`\s+`)

// commandSig returns a short one-way hash of a shell command after collapsing
// whitespace and case. Two runs of the same command share a signature; the
// command itself is never stored.
func commandSig(command string) string {
	command = strings.ToLower(strings.TrimSpace(commandWhitespace.ReplaceAllString(command, " ")))
	if command == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:4])
}
