package cully

import (
	"reflect"
	"testing"
)

func strVal(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestParseFooterRows(t *testing.T) {
	rows := []string{
		"some conversation text",
		"GPT-6.1-Sol medium · Context 73% left · 1.2K in · 240 out · 5h 82% left · Weekly 91% left · Fast off",
	}
	upd, match := parseFooterRows(rows, 17)
	if !match.Found || match.Row != 18 {
		t.Fatalf("match = %+v", match)
	}
	if upd.Source != sourceFooter || strVal(upd.Model) != "GPT-6.1-Sol medium" ||
		upd.ContextLeft == nil || *upd.ContextLeft != 73 ||
		strVal(upd.Input) != "1.2K" || strVal(upd.Output) != "240" ||
		strVal(upd.FiveHour) != "5h 82% left" || strVal(upd.Weekly) != "Weekly 91% left" ||
		strVal(upd.Fast) != "Fast off" {
		t.Fatalf("update = %+v", upd)
	}
	clipped := []string{"GPT-6.1-Sol medium · Context 70% left · 1.4K in"}
	upd, match = parseFooterRows(clipped, 0)
	if !match.Found || match.Row != 0 {
		t.Fatalf("clipped match = %+v", match)
	}
	if upd.Input == nil || upd.Output != nil || upd.FiveHour != nil {
		t.Fatal("clipped row must only carry what it shows")
	}
	for _, tc := range [][]string{
		{"plain text"},
		{"Context 8% left"},
		{"Model · Context 101% left"},
		{},
	} {
		upd, match := parseFooterRows(tc, 5)
		if match.Found || match.Row != -1 {
			t.Fatalf("rows %q matched: %+v", tc, match)
		}
		var empty sessionView
		empty.mergeUpdate(upd)
		if !reflect.DeepEqual(empty, sessionView{}) {
			t.Fatalf("empty update changed the view: %+v", empty)
		}
	}
}

func TestMergeUpdateRetainsAndOverwrites(t *testing.T) {
	var view sessionView
	first := InstrumentUpdate{Source: sourceFooter}
	model, input := "M", "1K"
	left := 70
	first.Model, first.ContextLeft, first.Input = &model, &left, &input
	view.mergeUpdate(first)
	second := InstrumentUpdate{Source: sourceFooter}
	model2, left2 := "M2", 68
	second.Model, second.ContextLeft = &model2, &left2
	view.mergeUpdate(second)
	if view.Model != "M2" || view.ContextLeft != 68 || !view.ContextKnown || view.Input != "1K" {
		t.Fatalf("merge = %+v", view)
	}
}

// TestSessionSnapshotFixtureDecodesAcrossSaves pins the persisted snapshot
// shape: a saved fixture must decode the same before and after a save/load
// round trip, so a Go rename can never silently change stored state.
