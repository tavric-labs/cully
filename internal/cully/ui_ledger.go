package cully

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// The change ledger is an audit trail for what shapes how Cully works for you: agents, background
// jobs, the workflow rules file and the tool policy. Every create, update, delete, approve, pause or
// resume is written down with what it looked like before and after, who asked for it (a proposal, you,
// or a schedule), and when. Any single entry can be rolled back, which puts the "before" back, but
// only when the thing still looks like the entry's "after": if you changed it since, Cully refuses
// unless you say force. Nothing here deletes your data beyond the 500-entry cap on the ledger itself.
//
// Snapshots never hold an agent's notes or run history, and any token value is redacted from them.
// A snapshot that redaction had to change cannot be restored exactly, so its entry is marked
// lossy and refuses to roll back.

const uiLedgerMax = 500

type uiLedgerEntry struct {
	ID           string          `json:"id"`
	Time         time.Time       `json:"time"`
	Kind         string          `json:"kind"`    // agent, job, rule, policy
	Subject      string          `json:"subject"` // the name shown to the user
	SubjectID    string          `json:"subjectId,omitempty"`
	Action       string          `json:"action"` // create, update, delete, approve, pause, resume, rollback
	Source       string          `json:"source"` // proposal:<id>, user, scheduled, rollback:<id>
	Before       json.RawMessage `json:"before,omitempty"`
	After        json.RawMessage `json:"after,omitempty"`
	RolledBack   bool            `json:"rolledBack,omitempty"`
	RolledBackAt time.Time       `json:"rolledBackAt,omitzero"`
	Lossy        bool            `json:"lossy,omitempty"`
}

// uiFieldChange is one line of a plain diff: a field, what it was, what it becomes.
type uiFieldChange struct {
	Field string `json:"field"`
	Old   string `json:"old,omitempty"`
	New   string `json:"new,omitempty"`
}

// uiLedgerView is an entry as the History page shows it.
type uiLedgerView struct {
	ID           string          `json:"id"`
	Time         time.Time       `json:"time"`
	Kind         string          `json:"kind"`
	Subject      string          `json:"subject"`
	Action       string          `json:"action"`
	Source       string          `json:"source"`
	Summary      string          `json:"summary"`
	Changes      []uiFieldChange `json:"changes"`
	RolledBack   bool            `json:"rolledBack"`
	CanRollback  bool            `json:"canRollback"`
	WhyNot       string          `json:"whyNot,omitempty"`
	RolledBackAt time.Time       `json:"rolledBackAt,omitzero"`
}

var (
	uiLedgerMu      sync.Mutex // serialises rollbacks; taken before any agent or rules lock
	uiLedgerWriteMu sync.Mutex // guards the stored entries; taken last, so a writer may hold other locks
	errLedgerChange = errors.New("changed since")
)

func uiLedgerSource(proposal string) string {
	if proposal = strings.TrimSpace(proposal); proposal != "" {
		return "proposal:" + uiClip(proposal, 40)
	}
	return "user"
}

// ---- snapshots

type uiAgentSnap struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Role         string          `json:"role"`
	Instructions string          `json:"instructions"`
	Scope        string          `json:"scope"`
	Folder       string          `json:"folder"`
	Groups       map[string]bool `json:"groups"`
	Mode         string          `json:"mode"`
	Schedule     string          `json:"schedule"`
	Approved     bool            `json:"approved"`
	Enabled      bool            `json:"enabled"`
	Created      time.Time       `json:"created,omitzero"`
}

// uiAgentSnapOf is what the ledger keeps of an agent: its settings, never its notes or run history.
func uiAgentSnapOf(id *uiIdentity) *uiAgentSnap {
	if id == nil {
		return nil
	}
	g := map[string]bool{}
	for k, v := range id.Groups {
		if v {
			g[k] = true
		}
	}
	return &uiAgentSnap{ID: id.ID, Name: id.Name, Role: id.Role, Instructions: id.Instructions, Scope: id.Scope, Folder: id.Folder, Groups: g,
		Mode: id.Mode, Schedule: id.Schedule, Approved: id.Approved, Enabled: id.Enabled, Created: id.Created}
}

func (s *uiAgentSnap) input() uiIdentityInput {
	g := map[string]bool{}
	for k, v := range s.Groups {
		g[k] = v
	}
	return uiIdentityInput{Name: s.Name, Role: s.Role, Instructions: s.Instructions, Scope: s.Scope, Folder: s.Folder, Groups: g, Mode: s.Mode, Schedule: s.Schedule, Enabled: s.Enabled, Approve: s.Approved}
}

type uiJobSnap struct {
	Name           string    `json:"name"`
	Command        []string  `json:"command"`
	After          string    `json:"after,omitempty"`
	MinHours       float64   `json:"min_hours,omitempty"`
	TimeoutMinutes int       `json:"timeout_minutes,omitempty"`
	MaxAttempts    int       `json:"max_attempts,omitempty"`
	RetryMinutes   int       `json:"retry_minutes,omitempty"`
	Schedule       string    `json:"schedule,omitempty"`
	MaxRuns        int       `json:"max_runs,omitempty"`
	RunAt          time.Time `json:"run_at,omitzero"`
	Paused         bool      `json:"paused,omitempty"`
}

var uiJobKeys = []string{"command", "after", "min_hours", "timeout_minutes", "max_attempts", "retry_minutes", "schedule", "max_runs", "run_at"}

func (s *uiJobSnap) scheduled() scheduledJob {
	return scheduledJob{Name: s.Name, Command: s.Command, After: s.After, MinHours: s.MinHours, TimeoutMinutes: s.TimeoutMinutes, MaxAttempts: s.MaxAttempts,
		RetryMinutes: s.RetryMinutes, Schedule: s.Schedule, MaxRuns: s.MaxRuns, RunAt: s.RunAt}
}

// uiJobSnapshot reads one job from jobs.json with its paused state, or nil when there is none.
func uiJobSnapshot(name string) *uiJobSnap {
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil {
		return nil
	}
	for _, j := range jobs {
		if j.Name == name {
			return &uiJobSnap{Name: j.Name, Command: j.Command, After: j.After, MinHours: j.MinHours, TimeoutMinutes: j.TimeoutMinutes, MaxAttempts: j.MaxAttempts,
				RetryMinutes: j.RetryMinutes, Schedule: j.Schedule, MaxRuns: j.MaxRuns, RunAt: j.RunAt, Paused: readJobStates()[name] != nil && readJobStates()[name].Paused}
		}
	}
	return nil
}

type uiRuleSnap struct {
	Text string `json:"text"`
}

func uiSnapJSON(v any) json.RawMessage {
	if v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()) {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func uiSnapEqual(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// ---- the plain diff

var uiFieldLabels = map[string]string{
	"name": "Name", "role": "What it does", "instructions": "How it works", "scope": "Kind", "folder": "Folder", "groups": "Tools", "mode": "Mode",
	"schedule": "Schedule", "approved": "Approved", "enabled": "Schedule on", "command": "Command", "after": "Runs after", "min_hours": "Least hours between runs",
	"timeout_minutes": "Timeout in minutes", "max_attempts": "Attempts", "retry_minutes": "Minutes before a retry", "max_runs": "Stops after runs", "run_at": "Runs at",
	"paused": "Paused", "text": "Workflow rules", "presets": "Tool groups", "allow": "Always allowed", "deny": "Always blocked", "mcpOnly": "Connected tools", "askRisky": "Ask before risky calls",
}

func uiShowValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", x), "0"), ".")
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = uiShowValue(e)
		}
		return strings.Join(parts, " ")
	case map[string]any:
		var on []string
		for k, e := range x {
			if b, ok := e.(bool); ok {
				if b {
					on = append(on, k)
				} else {
					on = append(on, k+" off")
				}
			} else {
				on = append(on, k+"="+uiShowValue(e))
			}
		}
		sort.Strings(on)
		return strings.Join(on, ", ")
	}
	return fmt.Sprint(v)
}

func uiShortValue(s string) string { return uiClip(strings.ReplaceAll(s, "\n", " / "), 160) }

func uiEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// uiSnapDiff compares two snapshots field by field. A missing before is a creation (every set field is
// new); a missing after is a removal. A long multi-line text, such as the rules file, is summarised by
// how many lines were added and removed.
func uiSnapDiff(before, after json.RawMessage) []uiFieldChange {
	var b, a map[string]any
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range a {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		if k != "id" && k != "created" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	out := []uiFieldChange{}
	for _, k := range names {
		ov, nv := b[k], a[k]
		if uiEmptyValue(ov) && uiEmptyValue(nv) || reflect.DeepEqual(ov, nv) {
			continue
		}
		label := uiFieldLabels[k]
		if label == "" {
			label = k
		}
		os, ns := uiShowValue(ov), uiShowValue(nv)
		if len(os) > 160 || len(ns) > 160 || strings.Contains(os, "\n") || strings.Contains(ns, "\n") {
			if strings.Count(os, "\n") > 2 || strings.Count(ns, "\n") > 2 {
				add, del := 0, 0
				for _, l := range uiLineDiff(os, ns) {
					switch l.Op {
					case "+":
						add++
					case "-":
						del++
					}
				}
				out = append(out, uiFieldChange{Field: label, Old: uiItoa(strings.Count(os, "\n")+boolInt(os != "")) + " lines", New: uiItoa(add) + " added, " + uiItoa(del) + " removed"})
				continue
			}
		}
		out = append(out, uiFieldChange{Field: label, Old: uiShortValue(os), New: uiShortValue(ns)})
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func uiLedgerSummary(e uiLedgerEntry) string {
	what := map[string]string{"agent": "agent", "job": "background job", "rule": "workflow rules", "policy": "tool policy"}[e.Kind]
	verb := map[string]string{"create": "Created", "update": "Changed", "delete": "Deleted", "approve": "Approved", "pause": "Paused", "resume": "Resumed", "rollback": "Rolled back a change to"}[e.Action]
	if e.Kind == "rule" || e.Kind == "policy" {
		return verb + " the " + what
	}
	return verb + " " + what + " " + e.Subject
}

// ---- writing and reading

func uiLedgerAll() []uiLedgerEntry {
	var out []uiLedgerEntry
	for _, raw := range uiDB().all("ledger") {
		var e uiLedgerEntry
		if json.Unmarshal(raw, &e) == nil && e.ID != "" {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID }) // oldest first
	return out
}

// ledgerAdd stores one entry and forgets the oldest beyond the cap.
func (m *uiManager) ledgerAdd(e uiLedgerEntry) {
	uiLedgerWriteMu.Lock()
	defer uiLedgerWriteMu.Unlock()
	e.ID = fmt.Sprintf("%016x-%s", time.Now().UnixNano(), uiNewID())
	e.Time = time.Now().UTC()
	// Redact token values from free text. When that changes a snapshot the entry cannot restore it exactly.
	for _, p := range []*json.RawMessage{&e.Before, &e.After} {
		if len(*p) == 0 {
			continue
		}
		red := m.cfg.Tokens.redact(string(*p))
		if red != string(*p) {
			e.Lossy = true
			if json.Valid([]byte(red)) {
				*p = json.RawMessage(red)
			} else {
				*p = json.RawMessage(`{"redacted":true}`)
			}
		}
	}
	e.Subject = uiClip(m.cfg.Tokens.redact(e.Subject), 120)
	db := uiDB()
	db.put("ledger", e.ID, e)
	if all := uiLedgerAll(); len(all) > uiLedgerMax {
		for _, old := range all[:len(all)-uiLedgerMax] {
			db.del("ledger", old.ID)
		}
	}
}

// ledgerRecord writes one change. It skips a change that left the snapshot as it was.
func (m *uiManager) ledgerRecord(kind, subject, subjectID, action, source string, before, after json.RawMessage) {
	if action != "delete" && action != "create" && uiSnapEqual(before, after) {
		return
	}
	m.ledgerAdd(uiLedgerEntry{Kind: kind, Subject: subject, SubjectID: subjectID, Action: action, Source: source, Before: before, After: after})
}

func uiAgentAction(b, a *uiAgentSnap) string {
	switch {
	case b == nil:
		return "create"
	case a == nil:
		return "delete"
	case !b.Approved && a.Approved:
		return "approve"
	}
	c := *b
	c.Enabled = a.Enabled
	if a.Enabled != b.Enabled && uiSnapEqual(uiSnapJSON(&c), uiSnapJSON(a)) {
		if a.Enabled {
			return "resume"
		}
		return "pause"
	}
	return "update"
}

func (m *uiManager) ledgerAgent(b, a *uiAgentSnap, source string) {
	s := a
	if s == nil {
		s = b
	}
	if s == nil {
		return
	}
	m.ledgerRecord("agent", s.Name, s.ID, uiAgentAction(b, a), source, uiSnapJSON(b), uiSnapJSON(a))
}

func (m *uiManager) ledgerJob(b, a *uiJobSnap, source string) {
	s := a
	if s == nil {
		s = b
	}
	if s == nil {
		return
	}
	action := "update"
	switch {
	case b == nil:
		action = "create"
	case a == nil:
		action = "delete"
	case b.Paused != a.Paused:
		action = "pause"
		if !a.Paused {
			action = "resume"
		}
	}
	m.ledgerRecord("job", s.Name, s.Name, action, source, uiSnapJSON(b), uiSnapJSON(a))
}

func (m *uiManager) ledgerRules(before, after, source string) {
	m.ledgerRecord("rule", "Workflow rules", "rules", "update", source, uiSnapJSON(&uiRuleSnap{before}), uiSnapJSON(&uiRuleSnap{after}))
}

func (m *uiManager) policySnap() json.RawMessage {
	m.policyMu.Lock()
	m.loadPolicyLocked()
	p := m.policy
	m.policyMu.Unlock()
	return uiSnapJSON(&p)
}

func (m *uiManager) ledgerPolicy(before json.RawMessage, source string) {
	m.ledgerRecord("policy", "Tool policy", "policy", "update", source, before, m.policySnap())
}

func (e uiLedgerEntry) rollbackable() (bool, string) {
	switch {
	case e.RolledBack:
		return false, "Already rolled back."
	case e.Action == "rollback":
		return false, "A rollback is not rolled back again; make the change by hand."
	case e.Lossy:
		return false, "A secret was removed from this record, so it cannot be restored exactly."
	}
	return true, ""
}

// ledgerList returns entries newest first, optionally only one kind.
func (m *uiManager) ledgerList(kind string, limit int) []uiLedgerView {
	if limit <= 0 || limit > uiLedgerMax {
		limit = uiLedgerMax
	}
	uiLedgerWriteMu.Lock()
	all := uiLedgerAll()
	uiLedgerWriteMu.Unlock()
	out := []uiLedgerView{}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		if kind != "" && e.Kind != kind {
			continue
		}
		ok, why := e.rollbackable()
		out = append(out, uiLedgerView{ID: e.ID, Time: e.Time, Kind: e.Kind, Subject: e.Subject, Action: e.Action, Source: e.Source, Summary: uiLedgerSummary(e),
			Changes: uiSnapDiff(e.Before, e.After), RolledBack: e.RolledBack, CanRollback: ok, WhyNot: why, RolledBackAt: e.RolledBackAt})
	}
	return out
}

// ---- rollback

// ledgerRollback puts one entry's "before" back. Unless force is set it refuses when the thing no longer
// looks like the entry's "after". The restored settings go through the same checks as a new one: an agent
// through validateIdentity, a job through its command allowlist.
func (m *uiManager) ledgerRollback(id string, force bool) (uiLedgerView, error) {
	uiLedgerMu.Lock()
	defer uiLedgerMu.Unlock()
	var e *uiLedgerEntry
	uiLedgerWriteMu.Lock()
	entries := uiLedgerAll()
	uiLedgerWriteMu.Unlock()
	for _, x := range entries {
		if x.ID == id {
			x := x
			e = &x
		}
	}
	if e == nil {
		return uiLedgerView{}, errors.New("no such entry")
	}
	if ok, why := e.rollbackable(); !ok {
		return uiLedgerView{}, errors.New(why)
	}
	var cur, restored json.RawMessage
	var err error
	switch e.Kind {
	case "agent":
		cur, restored, err = m.rollbackAgent(e, force)
	case "job":
		cur, restored, err = m.rollbackJob(e, force)
	case "rule":
		cur, restored, err = rollbackRules(e, force)
	case "policy":
		cur, restored, err = m.rollbackPolicy(e, force)
	default:
		err = errors.New("unknown kind")
	}
	if err != nil {
		return uiLedgerView{}, err
	}
	e.RolledBack, e.RolledBackAt = true, time.Now().UTC()
	uiLedgerWriteMu.Lock()
	uiDB().put("ledger", e.ID, e)
	uiLedgerWriteMu.Unlock()
	m.ledgerAdd(uiLedgerEntry{Kind: e.Kind, Subject: e.Subject, SubjectID: e.SubjectID, Action: "rollback", Source: "rollback:" + e.ID, Before: cur, After: restored})
	ok, why := e.rollbackable()
	return uiLedgerView{ID: e.ID, Time: e.Time, Kind: e.Kind, Subject: e.Subject, Action: e.Action, Source: e.Source, Summary: uiLedgerSummary(*e), Changes: uiSnapDiff(e.Before, e.After), RolledBack: true, CanRollback: ok, WhyNot: why}, nil
}

func uiChangedErr(what string) error {
	return fmt.Errorf("%w: %s has been changed since this entry, so rolling it back would undo your later edits. Use force to roll back anyway", errLedgerChange, what)
}

func (m *uiManager) rollbackAgent(e *uiLedgerEntry, force bool) (cur, restored json.RawMessage, err error) {
	var before *uiAgentSnap
	if len(e.Before) > 0 {
		before = &uiAgentSnap{}
		if json.Unmarshal(e.Before, before) != nil {
			return nil, nil, errors.New("this entry is unreadable")
		}
	}
	var in uiIdentityInput
	if before != nil {
		if in, err = m.validateIdentity(before.input()); err != nil {
			return nil, nil, err
		}
	}
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	all := m.loadIdentitiesLocked()
	var now *uiIdentity
	for _, x := range all {
		if x.ID == e.SubjectID {
			now = x
		}
	}
	cur = uiSnapJSON(uiAgentSnapOf(now))
	if !force && !uiSnapEqual(cur, e.After) {
		return nil, nil, uiChangedErr("the agent " + e.Subject)
	}
	switch {
	case before == nil: // it was created: remove it
		if now == nil {
			return nil, nil, errors.New("the agent is already gone")
		}
		uiDB().del("agent", now.ID)
		uiDB().del("agenteval", now.ID)
		kept := []*uiIdentity{}
		for _, x := range all {
			if x != now {
				kept = append(kept, x)
			}
		}
		m.identities = kept
		return cur, nil, nil
	}
	for _, x := range all {
		if x != now && strings.EqualFold(x.Name, in.Name) {
			return nil, nil, errors.New("another agent already has the name " + in.Name)
		}
	}
	if now == nil { // it was deleted: bring it back, without its notes or run history
		if len(all) >= uiIdentityMax {
			return nil, nil, errors.New("that is the most agents you can keep; remove one first")
		}
		created := before.Created
		if created.IsZero() {
			created = time.Now().UTC()
		}
		now = &uiIdentity{ID: before.ID, Created: created, Runs: []uiIdentityRun{}}
		m.identities = append(all, now)
	}
	now.Name, now.Role, now.Instructions, now.Scope, now.Folder, now.Groups, now.Mode = in.Name, in.Role, in.Instructions, in.Scope, in.Folder, in.Groups, in.Mode
	now.Schedule, now.Approved = in.Schedule, before.Approved
	now.Enabled = before.Enabled && in.Schedule != "" && now.Approved
	now.Synced = false
	m.saveIdentitiesLocked()
	return cur, uiSnapJSON(uiAgentSnapOf(now)), nil
}

func (m *uiManager) rollbackJob(e *uiLedgerEntry, force bool) (cur, restored json.RawMessage, err error) {
	name := e.Subject
	var before *uiJobSnap
	if len(e.Before) > 0 {
		before = &uiJobSnap{}
		if json.Unmarshal(e.Before, before) != nil {
			return nil, nil, errors.New("this entry is unreadable")
		}
		if err := uiJobCommandOK(before.Command, uiJobFolderArg(before.Command)); err != nil {
			return nil, nil, errors.New("the earlier command can no longer be restored: " + err.Error())
		}
		if _, err := before.scheduled().normalized(); err != nil {
			return nil, nil, err
		}
	}
	now := uiJobSnapshot(name)
	cur = uiSnapJSON(now)
	if !force && !uiSnapEqual(cur, e.After) {
		return nil, nil, uiChangedErr("the job " + name)
	}
	if before == nil { // it was created: remove it
		if now == nil {
			return nil, nil, errors.New("the job is already gone")
		}
		err := uiEditJobsFile(func(_ []scheduledJob, raw []json.RawMessage) ([]json.RawMessage, error) {
			kept := raw[:0:0]
			for _, r := range raw {
				if n, _ := uiJobEntryName(r); n != name {
					kept = append(kept, r)
				}
			}
			return kept, nil
		})
		if err != nil {
			return nil, nil, err
		}
		keep := map[string]bool{}
		if jobs, err := loadJobs(jobsConfigFile()); err == nil {
			for _, j := range jobs {
				keep[j.Name] = true
			}
		}
		pruneJobStates(keep)
		uiDB().logDel(jobHistoryNS(name))
		return cur, nil, nil
	}
	fields := map[string]json.RawMessage{}
	b, _ := json.Marshal(before)
	_ = json.Unmarshal(b, &fields)
	err = uiEditJobsFile(func(_ []scheduledJob, raw []json.RawMessage) ([]json.RawMessage, error) {
		for i, r := range raw {
			if n, mm := uiJobEntryName(r); mm != nil && n == name {
				for _, k := range uiJobKeys {
					if v, ok := fields[k]; ok {
						mm[k] = v
					} else {
						delete(mm, k)
					}
				}
				raw[i], _ = json.Marshal(mm)
				return raw, nil
			}
		}
		entry := map[string]json.RawMessage{"name": fields["name"]}
		for _, k := range uiJobKeys {
			if v, ok := fields[k]; ok {
				entry[k] = v
			}
		}
		out, _ := json.Marshal(entry)
		return append(raw, out), nil
	})
	if err != nil {
		return nil, nil, err
	}
	if before.Paused != (now != nil && now.Paused) {
		_ = setJobPaused(name, before.Paused)
	}
	return cur, uiSnapJSON(uiJobSnapshot(name)), nil
}

func rollbackRules(e *uiLedgerEntry, force bool) (cur, restored json.RawMessage, err error) {
	var before, after uiRuleSnap
	_ = json.Unmarshal(e.Before, &before)
	_ = json.Unmarshal(e.After, &after)
	uiRulesMu.Lock()
	defer uiRulesMu.Unlock()
	text := uiRulesCurrent()
	cur = uiSnapJSON(&uiRuleSnap{text})
	if !force && text != after.Text {
		return nil, nil, uiChangedErr("the workflow rules file")
	}
	if before.Text != "" {
		if err := uiValidRules(before.Text); err != nil {
			return nil, nil, err
		}
	}
	wrote := 0
	for _, t := range uiRulesTargets() {
		old, rerr := os.ReadFile(t.Path)
		if before.Text == "" { // there was no file before: remove the ones that hold what was written
			if rerr == nil && (force || string(old) == after.Text) && os.Remove(t.Path) == nil {
				wrote++
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
			continue
		}
		if rerr == nil {
			_ = os.WriteFile(t.Path+".prev", old, 0o644)
		}
		tmp := t.Path + ".tmp"
		if os.WriteFile(tmp, []byte(before.Text), 0o644) == nil && os.Rename(tmp, t.Path) == nil {
			wrote++
		}
	}
	if wrote == 0 {
		return nil, nil, errors.New("could not write the file")
	}
	return cur, uiSnapJSON(&uiRuleSnap{before.Text}), nil
}

func (m *uiManager) rollbackPolicy(e *uiLedgerEntry, force bool) (cur, restored json.RawMessage, err error) {
	var before uiPolicy
	if json.Unmarshal(e.Before, &before) != nil {
		return nil, nil, errors.New("this entry is unreadable")
	}
	cur = m.policySnap()
	if !force && !uiSnapEqual(cur, e.After) {
		return nil, nil, uiChangedErr("the tool policy")
	}
	uiDB().put("settings", "policy", before)
	m.policyMu.Lock()
	m.policy, m.policyLoaded = before, true
	m.policyMu.Unlock()
	return cur, m.policySnap(), nil
}
