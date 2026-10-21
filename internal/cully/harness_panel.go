package cully

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// The panel's leftmost column shows the harness: how much of the agent setup is
// in place and what the project's recent sessions actually spent on oversized
// output. The audit is a few file checks, so it refreshes every half minute.
// Transcript measurement is slower, so it runs in the background and the panel
// shows the last result.

type harnessPanelData struct {
	Ready, Total int
	Missing      []string
	Evidence     harnessEvidence
	Measured     bool
	Agent        string // the agent whose sessions are measured
	Unsupported  bool   // Cully cannot read this agent's transcripts yet
}

const (
	harnessAuditTTL   = 30 * time.Second
	harnessMeasureTTL = 5 * time.Minute
	harnessPanelDays  = 30
)

type harnessPanelState struct {
	sync.Mutex
	dirs                harnessDirs
	agent               string
	generation          uint64
	auditAt, measureAt  time.Time
	data                harnessPanelData
	items               []harnessItem
	measuring, hasAudit bool
}

var harnessPanelCache harnessPanelState

func summarizeHarnessPanel(items []harnessItem, ev harnessEvidence, measured bool, agent string) harnessPanelData {
	_, readable := transcriptSourceFor(agent)
	d := harnessPanelData{Total: len(items), Evidence: ev, Measured: measured, Agent: agent, Unsupported: !readable && agent != ""}
	for _, it := range applyEvidence(items, ev) {
		if it.Present() {
			d.Ready++
		} else {
			d.Missing = append(d.Missing, it.Component)
		}
	}
	return d
}

func (c *harnessPanelState) finishMeasurement(generation uint64, ev harnessEvidence) {
	c.Lock()
	defer c.Unlock()
	if c.generation != generation {
		return
	}
	c.data = summarizeHarnessPanel(c.items, ev, true, c.agent)
	c.measureAt, c.measuring = time.Now(), false
}

// harnessPanel returns the harness column's data. The recent-output measurement
// reads the transcripts of the agent that is running, so a Codex session is
// measured from Codex's own history, not Claude's.
func harnessPanel(project, agent string) harnessPanelData {
	c := &harnessPanelCache
	c.Lock()
	defer c.Unlock()
	dirs := harnessDirs{Claude: ConfigDir(), Codex: CodexConfigDir(), Cursor: CursorConfigDir(), Project: project}
	if c.dirs != dirs || c.agent != agent {
		c.dirs, c.agent, c.hasAudit, c.measuring = dirs, agent, false, false
		c.generation++
		c.data, c.auditAt, c.measureAt = harnessPanelData{}, time.Time{}, time.Time{}
	}
	now := time.Now()
	if !c.hasAudit || now.Sub(c.auditAt) > harnessAuditTTL {
		c.items = auditHarness(dirs)
		c.data = summarizeHarnessPanel(c.items, c.data.Evidence, c.data.Measured, agent)
		c.hasAudit, c.auditAt = true, now
	}
	src, readable := transcriptSourceFor(agent)
	if project != "" && readable && !c.measuring && now.Sub(c.measureAt) > harnessMeasureTTL {
		c.measuring = true
		match := project
		if agent == "claude" {
			match = projectFolderMatch(project)
		}
		go func(generation uint64) {
			c.finishMeasurement(generation, measureAgentEvidence(src, "", match, harnessPanelDays, 20))
		}(c.generation)
	}
	return c.data
}

func panelTokens(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// harnessPanelRows renders the column's harness lines; the caller adds the
// model and verification state that sit beside them.
func harnessPanelRows(d harnessPanelData) []string {
	ready := green
	if d.Total > 0 && d.Ready < d.Total {
		ready = yellow
	}
	setup := "Setup " + ready + bold + fmt.Sprintf("%d/%d", d.Ready, d.Total) + rst + " ready"
	if len(d.Missing) > 0 {
		setup += " · " + yellow + "missing: " + strings.Join(d.Missing, ", ") + rst
	}
	rows := []string{setup}
	name := agentDisplayName(d.Agent)
	switch {
	case d.Unsupported:
		rows = append(rows, "Waste "+dim+"not measured for "+name+" yet"+rst)
	case !d.Measured:
		rows = append(rows, "Waste "+dim+"measuring…"+rst)
	case d.Evidence.Sessions == 0:
		rows = append(rows, "Waste "+dim+"no recent "+name+" sessions"+rst)
	default:
		waste := d.Evidence.perSession(d.Evidence.BigTokens)
		color := green
		switch {
		case waste >= 20000:
			color = red
		case waste >= 8000:
			color = yellow
		}
		line := fmt.Sprintf("Waste %s%s tok/session%s · Bash %s", color, panelTokens(waste), rst, panelTokens(d.Evidence.perSession(d.Evidence.BashTokens)))
		rows = append(rows, line+dim+fmt.Sprintf(" (%d %s sessions, %dd)", d.Evidence.Sessions, name, harnessPanelDays)+rst)
	}
	return rows
}
