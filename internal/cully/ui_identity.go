package cully

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A named agent: a worker with a role, its own instructions, a set of tools, a
// folder to work in, and notes it keeps between runs. The shape follows what
// coding-agent harnesses use for agent definitions (Claude Code's subagents:
// description, prompt, tools, mode, isolation) and for memory (a small bounded
// note file the agent maintains, read at the start of each run). A run is an
// ordinary task, started on request or on a schedule.

const (
	uiIdentityMax      = 20
	uiIdentityMemory   = 2200 // characters of notes kept; the newest win
	uiIdentityRunsKeep = 20
	uiIdentityRunLimit = 15 * time.Minute
)

var uiIdentitySchedules = map[string]time.Duration{"": 0, "hourly": time.Hour, "daily": 24 * time.Hour}

type uiIdentityRun struct {
	ID      string    `json:"id"`
	Started time.Time `json:"started"`
	Status  string    `json:"status"` // running, done, failed
	Summary string    `json:"summary,omitempty"`
	TaskID  string    `json:"taskId,omitempty"`
	Seconds int       `json:"seconds,omitempty"`
}

type uiIdentity struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Role         string          `json:"role"`         // what it does, in a sentence
	Instructions string          `json:"instructions"` // how it works and what it must not do
	Folder       string          `json:"folder"`
	Groups       map[string]bool `json:"groups"` // tool groups it may use
	Mode         string          `json:"mode"`   // edit or plan
	Schedule     string          `json:"schedule"`
	Enabled      bool            `json:"enabled"` // the schedule is on
	Memory       string          `json:"memory"`
	Created      time.Time       `json:"created"`
	LastRun      time.Time       `json:"lastRun,omitempty"`
	Runs         []uiIdentityRun `json:"runs"`
}

type uiIdentityInput struct {
	Name         string          `json:"name"`
	Role         string          `json:"role"`
	Instructions string          `json:"instructions"`
	Folder       string          `json:"folder"`
	Groups       map[string]bool `json:"groups"`
	Mode         string          `json:"mode"`
	Schedule     string          `json:"schedule"`
	Enabled      bool            `json:"enabled"`
	ClearMemory  bool            `json:"clearMemory"`
}

var (
	uiIdentityMu   sync.Mutex
	uiIdentityName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,39}$`)
)

func uiIdentityPath() string { return filepath.Join(cullyDir(), "identities.json") }

func (m *uiManager) loadIdentitiesLocked() []*uiIdentity {
	if m.identities != nil {
		return m.identities
	}
	m.identities = []*uiIdentity{}
	if b, err := os.ReadFile(uiIdentityPath()); err == nil {
		_ = json.Unmarshal(b, &m.identities)
		for _, id := range m.identities {
			for i := range id.Runs {
				if id.Runs[i].Status == "running" {
					id.Runs[i].Status, id.Runs[i].Summary = "failed", "The page's server restarted before this run finished."
				}
			}
		}
	}
	return m.identities
}

func (m *uiManager) saveIdentitiesLocked() {
	b, err := json.MarshalIndent(m.identities, "", "  ")
	if err != nil {
		return
	}
	tmp := uiIdentityPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, uiIdentityPath())
	}
}

// clone copies an agent so a reader never shares its runs or tool groups with a run that is finishing.
func (id *uiIdentity) clone() uiIdentity {
	c := *id
	c.Runs = append([]uiIdentityRun{}, id.Runs...)
	c.Groups = make(map[string]bool, len(id.Groups))
	for k, v := range id.Groups {
		c.Groups[k] = v
	}
	return c
}

func (m *uiManager) identityList() []uiIdentity {
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	out := []uiIdentity{}
	for _, id := range m.loadIdentitiesLocked() {
		if uiHidden(id.Folder, m.cfg.HideSessionsIn) {
			continue
		}
		out = append(out, id.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

func (m *uiManager) validateIdentity(in uiIdentityInput) (uiIdentityInput, error) {
	in.Name, in.Role, in.Instructions = strings.TrimSpace(in.Name), strings.TrimSpace(in.Role), strings.TrimSpace(in.Instructions)
	if !uiIdentityName.MatchString(in.Name) {
		return in, errors.New("give the agent a name of up to 40 letters, numbers, spaces, dots or dashes")
	}
	if in.Role == "" || len([]rune(in.Role)) > 200 {
		return in, errors.New("say what the agent does in one sentence of up to 200 characters")
	}
	if len([]rune(in.Instructions)) > 4000 {
		return in, errors.New("the instructions are too long; keep them under 4000 characters")
	}
	if in.Mode == "" {
		in.Mode = "edit"
	}
	if in.Mode != "edit" && in.Mode != "plan" {
		return in, errors.New(`mode must be "edit" or "plan"`)
	}
	if _, ok := uiIdentitySchedules[in.Schedule]; !ok {
		return in, errors.New(`schedule must be empty, "hourly" or "daily"`)
	}
	valid := map[string]bool{}
	for _, p := range uiToolPresets {
		valid[p.ID] = true
	}
	if in.Groups == nil {
		in.Groups = map[string]bool{}
		for _, p := range uiToolPresets {
			in.Groups[p.ID] = p.Default
		}
	}
	for g := range in.Groups {
		if !valid[g] {
			return in, errors.New("unknown tool group " + g)
		}
	}
	info, err := m.addRepo(in.Folder)
	if err != nil {
		return in, errors.New("the folder cannot be used: " + err.Error())
	}
	in.Folder = info.Path
	return in, nil
}

func (m *uiManager) identityCreate(in uiIdentityInput) (uiIdentity, error) {
	in, err := m.validateIdentity(in)
	if err != nil {
		return uiIdentity{}, err
	}
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	all := m.loadIdentitiesLocked()
	if len(all) >= uiIdentityMax {
		return uiIdentity{}, errors.New("that is the most agents you can keep; remove one first")
	}
	for _, x := range all {
		if strings.EqualFold(x.Name, in.Name) {
			return uiIdentity{}, errors.New("an agent with that name already exists")
		}
	}
	id := &uiIdentity{ID: uiNewID(), Name: in.Name, Role: in.Role, Instructions: in.Instructions, Folder: in.Folder, Groups: in.Groups, Mode: in.Mode,
		Schedule: in.Schedule, Enabled: in.Enabled && in.Schedule != "", Created: time.Now().UTC(), Runs: []uiIdentityRun{}}
	m.identities = append(all, id)
	m.saveIdentitiesLocked()
	return id.clone(), nil
}

func (m *uiManager) identityUpdate(id string, in uiIdentityInput) (uiIdentity, error) {
	in, err := m.validateIdentity(in)
	if err != nil {
		return uiIdentity{}, err
	}
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	all := m.loadIdentitiesLocked()
	var cur *uiIdentity
	for _, x := range all {
		if x.ID == id {
			cur = x
		} else if strings.EqualFold(x.Name, in.Name) {
			return uiIdentity{}, errors.New("an agent with that name already exists")
		}
	}
	if cur == nil {
		return uiIdentity{}, errors.New("no such agent")
	}
	cur.Name, cur.Role, cur.Instructions, cur.Folder, cur.Groups, cur.Mode = in.Name, in.Role, in.Instructions, in.Folder, in.Groups, in.Mode
	cur.Schedule, cur.Enabled = in.Schedule, in.Enabled && in.Schedule != ""
	if in.ClearMemory {
		cur.Memory = ""
	}
	m.saveIdentitiesLocked()
	return cur.clone(), nil
}

func (m *uiManager) identityDelete(id string) error {
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	all := m.loadIdentitiesLocked()
	for i, x := range all {
		if x.ID == id {
			m.identities = append(all[:i:i], all[i+1:]...)
			m.saveIdentitiesLocked()
			return nil
		}
	}
	return errors.New("no such agent")
}

// uiIdentityPrompt is added to the system prompt, so the part that does not
// change between runs stays at the front and can be cached.
func uiIdentityPrompt(id uiIdentity) string {
	var b strings.Builder
	b.WriteString("You are " + id.Name + ", a named agent. What you do: " + id.Role + "\n")
	if id.Instructions != "" {
		b.WriteString("\nHow you work:\n" + id.Instructions + "\n")
	}
	b.WriteString("\nYou keep short notes between runs. When you finish, end your final reply with one line starting MEMORY: followed by up to three short facts worth remembering next time (decisions, where things are, what to avoid). Write nothing sensitive in it.\n")
	if id.Memory != "" {
		b.WriteString("\nYour notes from earlier runs. They may be out of date; treat them as hints, not facts:\n<notes>\n" + id.Memory + "\n</notes>\n")
	}
	return b.String()
}

func uiMergeMemory(old, reply string) string {
	line := ""
	for _, l := range strings.Split(reply, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "MEMORY:") {
			line = strings.TrimSpace(strings.TrimPrefix(t, "MEMORY:"))
		}
	}
	if line == "" {
		return old
	}
	merged := strings.TrimSpace(old + "\n- " + uiClipWords(line, 400))
	if r := []rune(merged); len(r) > uiIdentityMemory {
		merged = string(r[len(r)-uiIdentityMemory:])
		if i := strings.Index(merged, "\n"); i >= 0 {
			merged = merged[i+1:] // do not keep half a note
		}
	}
	return merged
}

func (m *uiManager) identityRun(id string) (uiIdentityRun, error) {
	uiIdentityMu.Lock()
	var cur *uiIdentity
	for _, x := range m.loadIdentitiesLocked() {
		if x.ID == id {
			cur = x
		}
	}
	if cur == nil || uiHidden(cur.Folder, m.cfg.HideSessionsIn) {
		uiIdentityMu.Unlock()
		return uiIdentityRun{}, errors.New("no such agent")
	}
	for _, r := range cur.Runs {
		if r.Status == "running" {
			uiIdentityMu.Unlock()
			return uiIdentityRun{}, errors.New("this agent is already running")
		}
	}
	snap := cur.clone()
	uiIdentityMu.Unlock()

	a, err := m.start(uiStartRequest{Repo: snap.Folder, Title: snap.Name, Prompt: "Do your work now, following how you work. Report what you did and anything that needs a person.",
		Mode: snap.Mode, groups: snap.Groups, appendSystem: uiIdentityPrompt(snap)})
	if err != nil {
		return uiIdentityRun{}, err
	}
	run := uiIdentityRun{ID: uiNewID(), Started: time.Now().UTC(), Status: "running", TaskID: a.id}
	uiIdentityMu.Lock()
	cur.Runs = append([]uiIdentityRun{run}, cur.Runs...)
	if len(cur.Runs) > uiIdentityRunsKeep {
		cur.Runs = cur.Runs[:uiIdentityRunsKeep]
	}
	cur.LastRun = run.Started
	m.saveIdentitiesLocked()
	uiIdentityMu.Unlock()
	go m.finishIdentityRun(id, run.ID, a)
	return run, nil
}

func (m *uiManager) finishIdentityRun(identID, runID string, a *uiAgent) {
	deadline := time.Now().Add(uiIdentityRunLimit)
	for time.Now().Before(deadline) {
		switch a.snapshot().State {
		case uiWaiting, uiDone, uiFailed, uiStopped:
			goto done
		}
		time.Sleep(500 * time.Millisecond)
	}
done:
	state := a.snapshot().State
	lastText, lastResult := "", ""
	for _, ev := range a.allEvents() {
		switch ev.Kind {
		case "text":
			lastText = ev.Text
		case "result":
			lastResult = ev.Text
		}
	}
	last := lastText
	if last == "" {
		last = lastResult
	}
	a.stop()
	status := "done"
	if state == uiFailed || last == "" {
		status = "failed"
	}
	uiIdentityMu.Lock()
	defer uiIdentityMu.Unlock()
	for _, x := range m.loadIdentitiesLocked() {
		if x.ID != identID {
			continue
		}
		for i := range x.Runs {
			if x.Runs[i].ID == runID {
				x.Runs[i].Status = status
				x.Runs[i].Seconds = int(time.Since(x.Runs[i].Started).Seconds())
				var kept []string
				for _, l := range strings.Split(last, "\n") {
					if !strings.HasPrefix(strings.TrimSpace(l), "MEMORY:") {
						kept = append(kept, l)
					}
				}
				x.Runs[i].Summary = uiClip(m.cfg.Tokens.redact(strings.TrimSpace(strings.Join(kept, "\n"))), 1500)
			}
		}
		if status == "done" {
			x.Memory = uiMergeMemory(x.Memory, m.cfg.Tokens.redact(last))
		}
		m.saveIdentitiesLocked()
	}
}

// identitiesDue starts every enabled agent whose schedule has come round, one
// at a time and only when there is room.
func (m *uiManager) identitiesDue() {
	for _, id := range m.identityList() {
		d := uiIdentitySchedules[id.Schedule]
		if !id.Enabled || d == 0 || (!id.LastRun.IsZero() && time.Since(id.LastRun) < d) || !m.hasRoom() {
			continue
		}
		_, _ = m.identityRun(id.ID)
		return
	}
}
