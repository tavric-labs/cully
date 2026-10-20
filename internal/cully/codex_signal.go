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
	"strings"
	"time"
)

// Codex hook events are reduced to single-letter counters. Commands, prompts,
// tool output, and transcript paths are never written to disk.
type toolEvent struct {
	Cwd          string          `json:"cwd"`
	SessionID    string          `json:"session_id"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

type toolStats struct {
	Tools, Errors, Searches, Edits, Checks, EditsSinceCheck int
	Agents, Web, Commits                                    int
	Cully                                                   cullyStats
	Impact                                                  Impact `json:"-"` // loaded by the pane, never persisted
}

var (
	searchCommand = regexp.MustCompile(`(?i)(^|[;&|[:space:]])(rg|grep|find|ls)([[:space:]]|$)`)
	commitCommand = regexp.MustCompile(`(?i)(^|[;&|[:space:]])git[[:space:]]+commit([[:space:]]|$)`)
	checkCommand  = regexp.MustCompile(`(?i)(^|[;&|[:space:]])(go test|go vet|npm test|npm run test|cargo test|pytest)([[:space:]]|$)`)
)

func signalFile(session string) string {
	return filepath.Join(cullyDir(), safeSession(session)+".codex-events")
}

type paneRegistration struct {
	Session string `json:"session"`
	Cwd     string `json:"cwd"`
	PID     int    `json:"pid"`
}

func paneRegistrationFile(session string) string {
	return filepath.Join(cullyDir(), safeSession(session)+".codex-pane")
}

func paneBindingFile(session string) string {
	return filepath.Join(cullyDir(), safeSession(session)+".codex-pane-session")
}

func registerPane(session, cwd string) error {
	b, err := json.Marshal(paneRegistration{Session: session, Cwd: filepath.Clean(cwd), PID: os.Getpid()})
	if err != nil {
		return err
	}
	return os.WriteFile(paneRegistrationFile(session), b, 0o600)
}

// Codex may execute hooks in a long-running app server that predates the pane.
// Its SessionStart hook binds the Codex session to the sole live pane for this
// working directory; later tool hooks use that binding, not inherited env.
func soleActiveCodexPane(cwd string) (paneRegistration, bool) {
	if cwd == "" {
		return paneRegistration{}, false
	}
	entries, err := os.ReadDir(cullyDir())
	if err != nil {
		return paneRegistration{}, false
	}
	var match paneRegistration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".codex-pane") {
			continue
		}
		path := filepath.Join(cullyDir(), entry.Name())
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) > 10*time.Second {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var pane paneRegistration
		if json.Unmarshal(b, &pane) != nil || pane.Session == "" || pane.PID <= 0 ||
			filepath.Clean(pane.Cwd) != filepath.Clean(cwd) || !processAlive(pane.PID) {
			continue
		}
		if match.Session != "" {
			return paneRegistration{}, false
		}
		match = pane
	}
	return match, match.Session != ""
}

func codexSessionKey(id string) string {
	sum := sha256.Sum256([]byte("codex\x00" + id))
	return hex.EncodeToString(sum[:16])
}

func bindPane(cwd, codexSessionID string) {
	if codexSessionID == "" {
		return
	}
	pane, ok := soleActiveCodexPane(cwd)
	if !ok {
		return
	}
	previous := readToolStats(pane.Session)
	f, err := os.OpenFile(paneBindingFile(pane.Session), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return // first SessionStart for this pane keeps its binding
	}
	_, _ = f.WriteString(codexSessionKey(codexSessionID))
	_ = f.Close()
	updateCodexSessionState(pane.Session, func(state *sessionState) {
		// Seed early unbound observations only on a new thread; never replace
		// saved totals when opening a resumed session.
		if _, err := os.Stat(sessionStateFile(pane.Session)); os.IsNotExist(err) {
			state.Stats = previous
		}
	})
}

func activePaneSession(cwd, codexSessionID string) string {
	if codexSessionID == "" {
		return ""
	}
	pane, ok := soleActiveCodexPane(cwd)
	if !ok {
		return ""
	}
	b, err := os.ReadFile(paneBindingFile(pane.Session))
	if err != nil || string(b) != codexSessionKey(codexSessionID) {
		return ""
	}
	return pane.Session
}

// RunSignalHook keeps hooks installed by earlier versions working.
func RunSignalHook(r io.Reader) { RunPaneSignalHook("codex", r) }

// RunPaneSignalHook is installed as an asynchronous PostToolUse hook. It is
// inactive outside the opt-in terminal, so ordinary agent sessions have no
// local advisor artifacts.
func RunPaneSignalHook(agent string, r io.Reader) {
	var event toolEvent
	data, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	if agent == "cursor" {
		var ok bool
		if event, ok = cursorToolEvent(data); !ok {
			return
		}
	} else if json.Unmarshal(data, &event) != nil {
		return
	}
	if event.ToolName == "" {
		return
	}
	if os.Getenv("MODEL_HINT_GUARD") != "" {
		session := os.Getenv("CULLY_MCP_METRICS_SESSION")
		origin := os.Getenv("CULLY_MCP_ORIGIN")
		if session == "" {
			session, origin = os.Getenv("CULLY_MCP_PROBE_SESSION"), "startup"
		}
		if tool := cullyTool(event.ToolName); session != "" && tool != "" {
			health, auth := cullyResponseState(event.ToolResponse)
			recordCodexCullyOriginCall(session, tool, health, auth, origin, cullySemantic(event.ToolInput))
		}
		return
	}
	session := os.Getenv("CULLY_PANE_SESSION")
	if session == "" {
		session = activePaneSession(event.Cwd, event.SessionID)
	}
	if session == "" {
		return
	}
	class := toolClass(event)
	failure := byte('.')
	if toolFailed(event.ToolResponse) {
		failure = '!'
	}
	if tool := cullyTool(event.ToolName); tool != "" {
		health, auth := cullyResponseState(event.ToolResponse)
		if health == "unhealthy" {
			failure = '!'
		}
		recordCodexCullyOriginCall(session, tool, health, auth, "foreground", cullySemantic(event.ToolInput))
		noteCullyLookup(session, tool, event.ToolResponse)
	}
	recordCodexSessionTool(session, class, failure)
	recordJournalTool(agent, session, event, class, failure == '!')
	f, err := os.OpenFile(signalFile(session), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write([]byte{class, failure, '\n'})
	_ = f.Close()
}

func toolClass(event toolEvent) byte {
	// Tool names can include transport namespaces. Recognize the actual tool
	// rather than guessing commands embedded in orchestration source text.
	name := toolBaseName(event.ToolName)
	if name == "apply_patch" || name == "edit" || name == "write" || name == "create_file" || name == "notebookedit" || name == "delete" || name == "str_replace" || name == "strreplace" || name == "multiedit" {
		return 'E'
	}
	if name == "bash" || name == "exec_command" || name == "shell" {
		command := shellCommandOf(event.ToolInput)
		if commitCommand.MatchString(command) {
			return 'G'
		}
		if checkCommand.MatchString(command) {
			return 'T'
		}
		if searchCommand.MatchString(command) {
			return 'S'
		}
		return 'O'
	}
	switch name {
	case "read", "read_file", "readfile", "view", "grep", "glob":
		return 'S'
	case "task", "agent":
		return 'A'
	case "webfetch", "websearch", "web_search":
		return 'W'
	}
	if strings.Contains(name, "search") || strings.Contains(name, "read_file") {
		return 'S'
	}
	return 'O'
}

func toolFailed(raw json.RawMessage) bool {
	var response struct {
		ExitCode      *int `json:"exit_code"`
		ExitCodeCamel *int `json:"exitCode"`
		IsError       bool `json:"isError"`
		IsErrorSnake  bool `json:"is_error"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return false
	}
	return response.IsError || response.IsErrorSnake ||
		(response.ExitCode != nil && *response.ExitCode != 0) ||
		(response.ExitCodeCamel != nil && *response.ExitCodeCamel != 0)
}

func readToolStats(session string) toolStats {
	if path := sessionStateFile(session); path != "" {
		if _, err := os.Stat(path); err == nil {
			return readCodexSessionState(path).Stats
		}
	}
	stats := toolStats{Cully: readCodexCullyStats(session)}
	f, err := os.Open(signalFile(session))
	if err != nil {
		return stats
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return stats
	}
	// The pane uses a bounded recent window even during a long session.
	reader := bufio.NewReader(f)
	if info.Size() > 8192 {
		_, _ = f.Seek(info.Size()-8192, io.SeekStart)
		reader.Reset(f)
		_, _ = reader.ReadBytes('\n') // discard a potentially partial first record
	}
	scan := bufio.NewScanner(reader)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) != 2 {
			continue
		}
		stats.Tools++
		switch line[0] {
		case 'S':
			stats.Searches++
		case 'A':
			stats.Agents++
		case 'W':
			stats.Web++
		case 'G':
			stats.Commits++
		case 'E':
			stats.Edits++
			stats.EditsSinceCheck++
		case 'T':
			stats.Checks++
			if line[1] != '!' {
				stats.EditsSinceCheck = 0
			}
		}
		if line[1] == '!' {
			stats.Errors++
		}
	}
	return stats
}

func statsAdvice(stats toolStats) []string {
	var lines []string
	if stats.Errors >= 3 {
		lines = append(lines, "CAUT|⚠️ Several tool calls failed. Inspect the first failure before retrying.")
	}
	if stats.Searches >= 10 {
		lines = append(lines, "ADV|🔎 Many searches this session. Narrow the path or query before continuing.")
	}
	if stats.EditsSinceCheck > 0 {
		lines = append(lines, "ADV|✓ Files changed. Run a focused check before finishing.")
	}
	if len(lines) == 0 {
		if stats.Tools == 0 {
			return []string{"MEMO|Awaiting Codex tool signals. Workflow checks are unavailable until a tool event arrives."}
		}
		return []string{"MEMO|No workflow warning in the observed tool signals."}
	}
	return lines
}

// toolCommand returns the shell command of a shell tool call, or "".
// An argv-style command is joined so the hash matches the string form.
func toolCommand(event toolEvent) string {
	name := toolBaseName(event.ToolName)
	if name != "bash" && name != "exec_command" && name != "shell" {
		return ""
	}
	return shellCommandOf(event.ToolInput)
}

// recordJournalTool adds one hook event to the session journal. It stores the
// tool class, success, a one-way hash of a shell command, and, unless
// CULLY_JOURNAL_PATHS=0, structured file operations and a command label.
// The hash input is the command text; the command itself is not stored.
func recordJournalTool(agent, session string, event toolEvent, class byte, failed bool) {
	entry := journalEvent{Time: time.Now().UTC(), Agent: agent, Class: string(class), Failed: failed}
	var extra []fileOp
	if cullyTool(event.ToolName) != "" {
		entry.Class, entry.Tool = journalMemory, cullyTool(event.ToolName)
	} else {
		if class == 'T' || failed {
			entry.Sig = commandSig(toolCommand(event))
		}
		if journalPathsEnabled() {
			ops, cmd := extractToolActivity(event)
			entry.Cmd = cmd
			if len(ops) > 0 {
				entry.Path, entry.Op, entry.To = ops[0].Path, ops[0].Op, ops[0].To
				extra = ops[1:]
			}
		}
	}
	appendJournal(event.Cwd, session, entry)
	for _, o := range extra {
		appendJournal(event.Cwd, session, journalEvent{Time: entry.Time, Agent: agent, Class: journalFileOp, Failed: failed, Path: o.Path, Op: o.Op, To: o.To})
	}
}
