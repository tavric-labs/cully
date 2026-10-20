//go:build !windows

package cully

import (
	"strings"
	"testing"
)

func TestAdvisorDrawerSelectionAndMouseTargets(t *testing.T) {
	var d advisorDrawer
	d.open([]string{"ADV|Run focused checks.", "ADV|Plan the next task.", "MEMO|More details."})
	d.move(1)
	if d.Selected != 1 {
		t.Fatal(d)
	}
	screen := d.render(80, 24, toolStats{}, sessionView{Project: "project"})
	if !strings.Contains(screen, "▶ Next · Plan the next task.") || !strings.Contains(screen, "Enter · Add to prompt") || !strings.Contains(screen, "you review it before it sends") {
		t.Fatal(screen)
	}
	found := false
	for row, index := range d.HitRows {
		if index == 1 && row > 3 {
			found = true
		}
	}
	if !found || d.AcceptRow <= 3 {
		t.Fatal("missing visible mouse targets", d.HitRows)
	}
	d.move(1)
	if screen = d.render(80, 24, toolStats{}, sessionView{}); !strings.Contains(screen, "Informational tip") || strings.Contains(screen, "Add to prompt") {
		t.Fatal("a tip must not offer to add itself to the prompt", screen)
	}
	d.Details = true
	d.escape()
	if d.Details || !d.Open {
		t.Fatal("Esc must return from instruments to the list")
	}
	d.escape()
	if d.Open {
		t.Fatal("Esc must close list")
	}
}

func TestAdvisorDrawerSelectionVisibleAfterPagingAndResize(t *testing.T) {
	var d advisorDrawer
	d.open([]string{"ADV|one", "ADV|two", "ADV|three", "ADV|four", "ADV|five", "ADV|six", "ADV|seven", "ADV|eight"})
	d.move(10000)
	screen := d.render(40, 12, toolStats{}, sessionView{})
	if !strings.Contains(screen, "▶ Next · eight") {
		t.Fatal("selected item hidden", screen)
	}
	d.Details = true
	d.Scroll = 10000
	_ = d.render(40, 12, toolStats{}, sessionView{})
	d.escape()
	if d.Details || !d.Open {
		t.Fatal(d)
	}
}
