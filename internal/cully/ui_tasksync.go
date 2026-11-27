package cully

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Saving tasks to Cully memory. Each task is kept locally (a task record with every session it has had).
// When the Cully MCP is connected, Cully also saves what happened to PostgreSQL, where Mem0 indexes it by
// meaning, so a later session on any machine can find it: every session of the task is registered with
// cully_session (its harness, its own session id, the branch and the task name), which is how memory
// counts the task's sessions, and one summary note is written with cully_log. The summary is made from
// what Cully already knows: the task's goal and ending and the analysed summary of each session. It never
// holds a prompt, a file's contents, a folder path or a token. Only a settled task is summarised, so a
// task still being worked on is not saved half-finished; the sync waits and runs again.

const (
	uiTaskSyncPerRun = 3
	uiTaskSettle     = 5 * time.Minute  // quiet this long before a finished task is summarised
	uiTaskIdle       = 30 * time.Minute // a task waiting on you this long is treated as settled
)

var uiTaskSecretLike = regexp.MustCompile(`(?i)\b(?:password|secret|api[_-]?key|access[_-]?token)\s*[:=]\s*\S{8,}|\bgh[pousr]_[A-Za-z0-9_]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`)

// cleanOut prepares any text that leaves this machine for Cully memory: token values and anything shaped
// like a credential are removed, and it is shortened. Every outbound field goes through it.
func (m *uiManager) cleanOut(s string, n int) string {
	return uiClipWords(uiTaskSecretLike.ReplaceAllString(m.cfg.Tokens.redact(s), "[removed]"), n)
}

// outboundOK is the last check before a call: no argument may still hold a token value or a credential.
// It refuses the whole call rather than send it.
func (m *uiManager) outboundOK(argsJSON string) bool {
	if uiTaskSecretLike.MatchString(argsJSON) {
		return false
	}
	return m.cfg.Tokens.redact(argsJSON) == argsJSON
}

// uiSessionRef is the stable reference memory uses for one session of one task.
func uiSessionRef(taskID, sessionID string) string {
	sum := sha256.Sum256([]byte(taskID + "\x00" + sessionID))
	return "ui-" + hex.EncodeToString(sum[:])[:16]
}

func uiTaskFingerprint(r uiTaskRecord) string {
	b, _ := json.Marshal([]any{r.State, len(r.Sessions), r.Files, r.Last, r.Closed, r.Runs})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// uiTaskSettled reports whether a task is quiet enough to summarise.
func uiTaskSettled(r uiTaskRecord, now time.Time) bool {
	if r.Live != "" {
		return false
	}
	quiet := now.Sub(r.Updated)
	switch {
	case r.Closed:
		return quiet >= uiTaskSettle
	case r.State == uiDone || r.State == uiFailed || r.State == uiStopped:
		return quiet >= uiTaskSettle
	case r.State == uiWaiting:
		return quiet >= uiTaskIdle
	}
	return false
}

// taskSummary is the note saved to memory: what the task was, how it went and what each session did.
func (m *uiManager) taskSummary(r uiTaskRecord) (summary, outcome, next string) {
	lib := m.sessionLibrary()
	by := map[string]uiIndexed{}
	for _, s := range lib.Sessions {
		by[s.ID] = s
	}
	clean := func(s string, n int) string {
		return uiClipWords(uiTaskSecretLike.ReplaceAllString(m.cfg.Tokens.redact(s), "[removed]"), n)
	}
	var b strings.Builder
	b.WriteString("Task: " + clean(r.Title, 120) + ". ")
	if r.Prompt != "" {
		b.WriteString("Asked: " + clean(r.Prompt, 220) + ". ")
	}
	harnesses := map[string]bool{}
	for _, s := range r.Sessions {
		harnesses[s.Agent] = true
	}
	var names []string
	for _, a := range []string{"claude", "codex", "cursor"} {
		if harnesses[a] {
			names = append(names, uiHarnessName(a))
		}
	}
	b.WriteString(strconv.Itoa(len(r.Sessions)) + " session" + map[bool]string{true: "", false: "s"}[len(r.Sessions) == 1])
	if len(names) > 0 {
		b.WriteString(" (" + strings.Join(names, ", ") + ")")
	}
	b.WriteString(", " + strconv.Itoa(r.Files) + " files changed.")
	lines := 0
	for _, s := range r.Sessions {
		e, ok := by[s.ID]
		if !ok || (e.Title == "" && e.Description == "") || lines >= 6 {
			continue
		}
		lines++
		b.WriteString("\n- " + uiHarnessName(s.Agent) + ": " + clean(firstNonEmpty(e.Title, e.Task), 80))
		if e.Description != "" {
			b.WriteString(". " + clean(e.Description, 160))
		}
	}
	outcome = map[string]string{uiDone: "Finished.", uiFailed: "Failed.", uiStopped: "Stopped.", uiWaiting: "Waiting for the user."}[r.State]
	if r.Closed {
		outcome = "Closed by the user."
	}
	if r.Last != "" {
		outcome += " " + clean(r.Last, 300)
	}
	if r.State != uiDone && !r.Closed {
		next = "Continue the task from Cully's task list."
	}
	return b.String(), strings.TrimSpace(outcome), next
}

// syncTasks saves the settled tasks that changed since they were last saved. It returns how many.
func (m *uiManager) syncTasks() int {
	now := time.Now()
	var todo []uiTaskRecord
	for _, r := range m.taskList("all") {
		if uiTaskSettled(r, now) && r.Synced != uiTaskFingerprint(r) {
			todo = append(todo, r)
		}
		if len(todo) >= uiTaskSyncPerRun {
			break
		}
	}
	if len(todo) == 0 || !uiCullyMCPConnected(m) {
		return 0
	}
	cwd := ConfigDir()
	if repos := m.repoList(); len(repos) > 0 {
		cwd = repos[0].Path
	}
	saved := 0
	for _, r := range todo {
		ok := true
		last := ""
		for _, s := range r.Sessions {
			if s.ID == "" || !uiAgentSessionRE.MatchString(s.ID) {
				continue
			}
			ref := uiSessionRef(r.ID, s.ID)
			last = ref
			args := map[string]any{"session_ref": ref, "assistant": s.Agent, "agent_session_id": s.ID, "task": m.cleanOut(r.Title, 120)}
			if r.Branch != "" && r.Branch != "no git" {
				args["branch"] = m.cleanOut(r.Branch, 120)
			}
			b, _ := json.Marshal(args)
			if !m.outboundOK(string(b)) || uiCullyToolCall(cwd, "cully_session", string(b)) != nil {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		summary, outcome, next := m.taskSummary(r)
		args := map[string]any{"summary": summary, "assistant": "claude", "entry_type": "work", "outcome": outcome, "occurred_at": r.Updated.UTC().Format(time.RFC3339)}
		if next != "" {
			args["next_steps"] = next
		}
		if last != "" {
			args["session_ref"] = last
		}
		b, _ := json.Marshal(args)
		if !m.outboundOK(string(b)) || uiCullyToolCall(cwd, "cully_log", string(b)) != nil {
			continue
		}
		uiTaskMu.Lock()
		var cur uiTaskRecord
		if uiDB().get("task", r.ID, &cur) {
			cur.Synced, cur.SyncedAt, cur.Entries = uiTaskFingerprint(r), time.Now().UTC(), cur.Entries+1
			uiDB().put("task", r.ID, cur)
		}
		uiTaskMu.Unlock()
		saved++
	}
	return saved
}

var uiAgentSessionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// syncTasksIfDue runs the save at most every ten minutes.
func (m *uiManager) syncTasksIfDue() {
	m.syncMu.Lock()
	if time.Since(m.taskSyncAt) < uiAgentSyncEvery {
		m.syncMu.Unlock()
		return
	}
	m.taskSyncAt = time.Now()
	m.syncMu.Unlock()
	m.syncTasks()
	m.syncSessions()
}

// syncSessions drips the analysed summaries of sessions that belong to no task into Cully memory, a few
// at a time, newest first, so past work is findable by meaning from any machine. A session that is part
// of a task is covered by that task's summary. One note per session, written once; the session is marked
// saved afterwards, and nothing is sent when the MCP is not connected.
func (m *uiManager) syncSessions() int {
	inTask := map[string]bool{}
	for _, r := range m.taskList("all") {
		for _, s := range r.Sessions {
			inTask[s.ID] = true
		}
	}
	lib := m.sessionLibrary()
	var todo []uiIndexed
	for _, s := range lib.Sessions {
		if s.Source == "ai" && s.Completed != "" && !s.Synced && !inTask[s.ID] && uiAgentSessionRE.MatchString(s.ID) && time.Since(s.Updated) > uiTaskSettle {
			todo = append(todo, s)
		}
	}
	sortUpdatedDesc := func(l []uiIndexed) {
		for i := 1; i < len(l); i++ {
			for j := i; j > 0 && l[j].Updated.After(l[j-1].Updated); j-- {
				l[j], l[j-1] = l[j-1], l[j]
			}
		}
	}
	sortUpdatedDesc(todo)
	if len(todo) > uiTaskSyncPerRun {
		todo = todo[:uiTaskSyncPerRun]
	}
	if len(todo) == 0 || !uiCullyMCPConnected(m) {
		return 0
	}
	cwd := ConfigDir()
	if repos := m.repoList(); len(repos) > 0 {
		cwd = repos[0].Path
	}
	clean := func(s string, n int) string {
		return uiClipWords(uiTaskSecretLike.ReplaceAllString(m.cfg.Tokens.redact(s), "[removed]"), n)
	}
	saved := 0
	for _, s := range todo {
		var b strings.Builder
		b.WriteString(uiHarnessName(s.Agent) + " session in " + clean(s.Name, 60) + ": " + clean(firstNonEmpty(s.Title, s.Task), 100) + ".")
		if s.Description != "" {
			b.WriteString(" " + clean(s.Description, 300))
		}
		if s.Task != "" {
			b.WriteString(" Goal: " + clean(s.Task, 160) + ".")
		}
		b.WriteString(" Finished the job: " + s.Completed + ". " + strconv.Itoa(s.Files) + " files changed, " + strconv.Itoa(s.Checks) + " test runs.")
		args := map[string]any{"summary": b.String(), "assistant": s.Agent, "agent_session_id": s.ID, "entry_type": "work", "occurred_at": s.Updated.UTC().Format(time.RFC3339)}
		if s.Different != "" && s.Different != "Nothing notable." {
			args["learning"] = clean(s.Different, 300)
		}
		a, _ := json.Marshal(args)
		if !m.outboundOK(string(a)) || uiCullyToolCall(cwd, "cully_log", string(a)) != nil {
			continue
		}
		m.idxMu.Lock()
		m.loadIndexLocked()
		if e := m.index.Sessions[s.ID]; e != nil {
			e.Synced = true
			m.saveIndexLocked()
		}
		m.idxMu.Unlock()
		saved++
	}
	return saved
}
