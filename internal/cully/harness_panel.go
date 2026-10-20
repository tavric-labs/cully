package cully

import (
	"fmt"
	"path/filepath"
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
}

const (
	harnessAuditTTL   = 30 * time.Second
	harnessMeasureTTL = 5 * time.Minute
	harnessPanelDays  = 30
)

type harnessPanelState struct {
	sync.Mutex
	dirs                harnessDirs
	generation          uint64
	auditAt, measureAt  time.Time
	data                harnessPanelData
	items               []harnessItem
	measuring, hasAudit bool
}

var harnessPanelCache harnessPanelState

func summarizeHarnessPanel(items []harnessItem, ev harnessEvidence, measured bool) harnessPanelData {
	d := harnessPanelData{Total: len(items), Evidence: ev, Measured: measured}
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
	c.data = summarizeHarnessPanel(c.items, ev, true)
	c.measureAt, c.measuring = time.Now(), false
}

func harnessPanel(project string) harnessPanelData {
	c := &harnessPanelCache
	c.Lock()
	defer c.Unlock()
	dirs := harnessDirs{Claude: ConfigDir(), Codex: CodexConfigDir(), Cursor: CursorConfigDir(), Project: project}
	if c.dirs != dirs {
		c.dirs, c.hasAudit, c.measuring = dirs, false, false
		c.generation++
		c.data, c.auditAt, c.measureAt = harnessPanelData{}, time.Time{}, time.Time{}
	}
	now := time.Now()
	if !c.hasAudit || now.Sub(c.auditAt) > harnessAuditTTL {
		c.items = auditHarness(dirs)
		c.data = summarizeHarnessPanel(c.items, c.data.Evidence, c.data.Measured)
		c.hasAudit, c.auditAt = true, now
	}
	if project != "" && !c.measuring && now.Sub(c.measureAt) > harnessMeasureTTL {
		c.measuring = true
		go func(generation uint64) {
			ev := measureHarnessEvidence(filepath.Join(dirs.Claude, "projects"), projectFolderMatch(project), harnessPanelDays, 20)
			c.finishMeasurement(generation, ev)
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
	if d.Total > 0 && d.Ready*2 < d.Total {
		ready = yellow
	}
	rows := []string{"Setup " + ready + bold + fmt.Sprintf("%d/%d", d.Ready, d.Total) + rst + " ready"}
	if len(d.Missing) > 0 {
		rows = append(rows, dim+"Gaps "+strings.Join(d.Missing, ", ")+rst)
	}
	switch {
	case !d.Measured:
		rows = append(rows, "Waste "+dim+"measuring…"+rst)
	case d.Evidence.Sessions == 0:
		rows = append(rows, "Waste "+dim+"no recent Claude sessions"+rst)
	default:
		line := fmt.Sprintf("Waste %s tok/session · Bash %s", panelTokens(d.Evidence.perSession(d.Evidence.BigTokens)), panelTokens(d.Evidence.perSession(d.Evidence.BashTokens)))
		rows = append(rows, line+dim+fmt.Sprintf(" (%d Claude sessions, %dd)", d.Evidence.Sessions, harnessPanelDays)+rst)
	}
	return rows
}
