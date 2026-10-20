//go:build !windows

package cully

import "testing"

func TestDividerDragShrinksExpandsAndSurvivesRefresh(t *testing.T) {
	var d paneDivider
	top := d.top(40, 10)
	if d.handle(&panelMouse{Button: 0, X: 10, Y: top}, 40, top) {
		t.Fatal("agent click captured")
	}
	if !d.handle(&panelMouse{Button: 0, X: 10, Y: top + 1}, 40, top) {
		t.Fatal("divider press missed")
	}
	if !d.handle(&panelMouse{Button: 32, X: 10, Y: 16}, 40, top) {
		t.Fatal("drag lost")
	}
	if got := d.top(40, 10); got != 15 {
		t.Fatalf("expand top=%d", got)
	}
	d.handle(&panelMouse{Button: 0, X: 10, Y: 39, Release: true}, 40, 15)
	if d.Dragging || !d.Manual || d.top(40, 100) != 38 {
		t.Fatal("release/refresh undid manual size")
	}
	// Small terminals clamp the viewport without forgetting the requested height.
	d.Height = 25
	if d.top(20, 100) != 8 || d.top(10, 100) != 10 || d.top(40, 100) != 15 {
		t.Fatal("terminal resize lost height or minimum agent space")
	}
}

func TestDividerCollapseAndBounds(t *testing.T) {
	for _, tc := range []struct{ y, want int }{{1, 12}, {40, 39}, {400, 39}} {
		var d paneDivider
		d.handle(&panelMouse{Button: 0, Y: 30}, 40, 29)
		d.handle(&panelMouse{Button: 32, Y: tc.y}, 40, 29)
		if got := d.top(40, 100); got != tc.want {
			t.Errorf("y=%d: top=%d want=%d", tc.y, got, tc.want)
		}
	}
}

func TestDividerIgnoresWheelRightClickAndRelease(t *testing.T) {
	var d paneDivider
	for _, mouse := range []*panelMouse{{Button: 64, Y: 30}, {Button: 2, Y: 30}, {Button: 0, Y: 30, Release: true}, {Button: 32, Y: 30}} {
		if d.handle(mouse, 40, 29) {
			t.Fatalf("unexpected gesture captured: %+v", mouse)
		}
	}
	if d.Manual || d.Dragging {
		t.Fatal("unrelated mouse changed size")
	}
}

func TestDividerHoverHighlightsOnlyOverTheBar(t *testing.T) {
	var d paneDivider
	top := d.top(40, 10)
	if motion, changed := d.hover(&panelMouse{Button: 35, Y: top + 1}, 40, top); !motion || !changed || d.grip() != gripHover {
		t.Fatalf("hover over bar: motion=%v changed=%v grip=%v", motion, changed, d.grip())
	}
	if _, changed := d.hover(&panelMouse{Button: 35, Y: top + 1}, 40, top); changed {
		t.Fatal("repeated hover should not repaint")
	}
	if motion, changed := d.hover(&panelMouse{Button: 35, Y: 5}, 40, top); !motion || !changed || d.grip() != gripIdle {
		t.Fatal("moving away should clear the highlight")
	}
	for _, m := range []*panelMouse{{Button: 32, Y: top + 1}, {Button: 0, Y: top + 1}, {Button: 35, Y: top + 1, Release: true}} {
		if motion, _ := d.hover(m, 40, top); motion {
			t.Fatalf("not button-less motion: %+v", m)
		}
	}
}

func TestDividerGripStrengthensWhenPickedUp(t *testing.T) {
	var d paneDivider
	top := d.top(40, 10)
	d.hover(&panelMouse{Button: 35, Y: top + 1}, 40, top)
	d.handle(&panelMouse{Button: 0, Y: top + 1}, 40, top)
	if d.grip() != gripDrag {
		t.Fatalf("grip=%v want drag", d.grip())
	}
	d.handle(&panelMouse{Button: 0, Y: top + 1, Release: true}, 40, top)
	if d.grip() == gripDrag {
		t.Fatal("release should drop the drag grip")
	}
}
