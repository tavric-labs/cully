//go:build !windows

package cully

// paneDivider remembers the requested height across terminal resizes. Automatic
// growth stops after a drag, so refreshing advice cannot undo the users choice.
type paneDivider struct {
	Height                  int
	Manual, Dragging, Hover bool
}

// gripState selects how strongly the divider is drawn: always visible at rest,
// brighter under the pointer, and solid while it is being dragged.
type gripState int

const (
	gripIdle gripState = iota
	gripHover
	gripDrag
)

func (d *paneDivider) grip() gripState {
	switch {
	case d.Dragging:
		return gripDrag
	case d.Hover:
		return gripHover
	}
	return gripIdle
}

// hover tracks a button-less pointer move and reports whether the highlight
// changed. Such motion never reaches the agent, which did not ask for it.
func (d *paneDivider) hover(mouse *panelMouse, rows, top int) (motion, changed bool) {
	// Once a drag has begun, every motion belongs to it. Some terminals report
	// held-button motion with the button-less code, which would otherwise be
	// swallowed here as a hover and never move the divider.
	if d.Dragging || mouse.Release || mouse.Button&32 == 0 || mouse.Button&3 != 3 {
		return false, false
	}
	over := !d.Dragging && rows >= 12 && top < rows && mouse.Y == top+1
	changed = over != d.Hover
	d.Hover = over
	return true, changed
}

func (d *paneDivider) top(rows, contentRows int) int {
	if !d.Manual {
		return paneTop(rows, contentRows)
	}
	if rows < 12 {
		return rows
	}
	minimum := 8
	if rows >= 24 {
		minimum = 12
	}
	return rows - min(max(1, d.Height), rows-minimum)
}

// handle consumes only a left-button gesture beginning on the divider. Drag
// motion and release remain owned by the divider even outside the panel.
func (d *paneDivider) handle(mouse *panelMouse, rows, top int) bool {
	if d.Dragging {
		if mouse.Release || mouse.Button&32 != 0 {
			d.Height = rows - mouse.Y + 1
			d.Manual = true
			if mouse.Release {
				d.Dragging = false
			}
		}
		return true
	}
	if rows >= 12 && top < rows && !mouse.Release && mouse.Button == 0 && mouse.Y == top+1 {
		d.Dragging, d.Hover = true, true
		return true
	}
	return false
}
