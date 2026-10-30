package cully

import (
	"strings"

	"github.com/charmbracelet/x/vt"
)

// Feed sources, precedence and merge contract.
//
// Two feeds fill the panel instruments, and they never run for the same
// agent: the native terminal footer (Codex, gated on NativeFooter) and the
// hook callback (Claude, read from the pane's usage file). Cursor hooks also
// report the selected model and historical pre-compaction context samples.
// Last writer wins between feeds because the agent gates keep them exclusive.
//
// Merge rules, preserved from the original feed code:
//
//   - A matched footer always sets model and context; instruments absent from
//     a clipped row are retained, never erased.
//   - A callback writes only present native measurements. A complete callback
//     clears absent/null usage instead of retaining stale post-compaction data.
//   - Git owns line changes, changed files and their known flag on its own
//     beat: it resets them first, so a failed Git read zeroes them even when
//     a snapshot seeded values between beats.
//   - Unknown stays unknown. A feed only writes what its source measured;
//     renderers show missing instruments as missing, never guessed.
type instrumentSource string

const (
	sourceFooter   instrumentSource = "footer"
	sourceSnapshot instrumentSource = "snapshot"
)

// InstrumentUpdate is one feed reading with explicit presence: a nil field
// was not measured and must not clear the view. Source names the feed for
// debugging only; merge precedence comes from the agent gates, not the tag.
type InstrumentUpdate struct {
	Source                   instrumentSource
	Model                    *string
	ContextLeft              *int
	Input, Output            *string
	FiveHour, Weekly, Fast   *string
	LinesAdded, LinesRemoved *int64
}

// FooterMatch identifies the decoded native footer row so the renderer can
// suppress it. Row is the terminal row index, or -1 when nothing matched.
type FooterMatch struct {
	Row   int
	Found bool
}

// footerRows returns the bounded bottom rows of the terminal plus the index
// of the first one. Only these rows are ever parsed as a native footer.
func footerRows(emulator *vt.Emulator) ([]string, int) {
	base := max(0, emulator.Height()-3)
	rows := make([]string, 0, emulator.Height()-base)
	for y := base; y < emulator.Height(); y++ {
		var text strings.Builder
		for x := 0; x < emulator.Width(); x++ {
			if cell := emulator.CellAt(x, y); cell != nil {
				text.WriteString(cell.Content)
			}
		}
		rows = append(rows, text.String())
	}
	return rows, base
}

// mergeUpdate applies one feed reading to the panel view.
func (v *sessionView) mergeUpdate(u InstrumentUpdate) {
	if u.Model != nil {
		v.Model = *u.Model
	}
	if u.ContextLeft != nil {
		v.ContextLeft, v.ContextKnown = *u.ContextLeft, true
	}
	if u.Input != nil {
		v.Input = *u.Input
	}
	if u.Output != nil {
		v.Output = *u.Output
	}
	if u.FiveHour != nil {
		v.FiveHour = *u.FiveHour
	}
	if u.Weekly != nil {
		v.Weekly = *u.Weekly
	}
	if u.Fast != nil {
		v.Fast = *u.Fast
	}
	if u.LinesAdded != nil {
		v.LinesAdded = *u.LinesAdded
	}
	if u.LinesRemoved != nil {
		v.LinesRemoved = *u.LinesRemoved
	}
}
