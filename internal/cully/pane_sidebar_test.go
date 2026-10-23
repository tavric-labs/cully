package cully

import (
	"strings"
	"testing"
)

// Renames must preserve the unique names creation guarantees: a duplicate
// would leave two identical rows in the numbered list while switching still
// works by index, silently confusing the pointer and keyboard paths.
func TestRenameSessionName(t *testing.T) {
	names := []string{"codex", "codex 2", "work"}
	for _, c := range []struct {
		index int
		input string
		want  string
		ok    bool
	}{
		{0, "  research  ", "research", true},
		{0, "codex", "codex", true},     // keeping its own name is fine
		{1, "codex 2", "codex 2", true}, // keeping its own name is fine
		{2, "codex", "", false},         // another session's name
		{2, "codex 2", "", false},       // another session's name
		{0, "codex 2 ", "", false},      // cleaned before comparing
		{1, "", "", false},              // blank keeps the old name
		{1, "   ", "", false},           // blank keeps the old name
		{1, "Codex", "Codex", true},     // display names match exactly
		{0, strings.Repeat("x", 40), strings.Repeat("x", sidebarMaxName), true},
		{0, "a\x00b\x07c", "abc", true}, // control bytes never reach the list
	} {
		if got, ok := renameSessionName(names, c.index, []rune(c.input)); got != c.want || ok != c.ok {
			t.Errorf("renameSessionName(%q from %d) = %q,%v want %q,%v", c.input, c.index, got, ok, c.want, c.ok)
		}
	}
}
