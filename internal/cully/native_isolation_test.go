package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Running claude, codex or cursor-agent directly must stay a native session
// even after `cully setup`: every Cully hook is gated on `cully run`, and no
// skill, instruction, or status line is placed where the agent discovers it.
func TestSetupLeavesDirectNativeSessionsUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	for _, install := range []func() error{installClaude, installCodex, installCursor} {
		if err := install(); err != nil {
			t.Fatal(err)
		}
	}
	for _, agent := range []string{"claude", "codex", "cursor"} {
		if err := installMemorySkill(agent); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "hooks.json"),
		filepath.Join(home, ".cursor", "hooks.json"),
	} {
		hooks := 0
		walkHookCommands(readJSON(t, path), func(command string) {
			hooks++
			if !strings.HasPrefix(command, scopedHookCommand("")[:len(`if [ "${CULLY_RUN_ACTIVE:-}" = "1" ]; then `)]) {
				t.Errorf("%s: hook runs outside cully run: %s", path, command)
			}
		})
		if hooks == 0 {
			t.Errorf("%s has no Cully hooks to check", path)
		}
	}
	settings := readJSON(t, filepath.Join(home, ".claude", "settings.json"))
	if _, ok := settings["statusLine"]; ok {
		t.Error("setup registered a status line")
	}
	for _, dir := range []string{".claude", ".codex", ".cursor"} {
		for _, name := range []string{"cully", "dev-workflow-rules"} {
			if _, err := os.Lstat(filepath.Join(home, dir, "skills", name)); !os.IsNotExist(err) {
				t.Errorf("%s/skills/%s is visible to native sessions: %v", dir, name, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("Codex instructions were written: %v", err)
	}
}

func walkHookCommands(config map[string]any, visit func(string)) {
	events, _ := config["hooks"].(map[string]any)
	for _, groups := range events {
		for _, group := range toList(groups) {
			entry, _ := group.(map[string]any)
			if command, _ := entry["command"].(string); command != "" {
				visit(command)
			}
			for _, hook := range toList(entry["hooks"]) {
				if command, _ := hook.(map[string]any)["command"].(string); command != "" {
					visit(command)
				}
			}
		}
	}
}

func TestSidebarHitAndRender(t *testing.T) {
	names := []string{"1 a", "2 b", "3 c"}
	lines := sidebarLines(names, 1, sidebarState{Selected: 1}, 10)
	if len(lines) != 10 || !strings.Contains(lines[0], "Sessions") || !strings.Contains(lines[1], "[+ New]") || !strings.Contains(lines[3], "▸ 2 b") {
		t.Fatalf("sidebar = %q", lines)
	}
	for _, c := range []struct {
		x, y int
		kind sidebarHitKind
		idx  int
	}{
		{3, 2, sidebarHitNew, 0}, {12, 2, sidebarHitRename, 0}, {5, 3, sidebarHitSession, 0}, {5, 5, sidebarHitSession, 2},
		{5, 6, sidebarNoHit, 0}, {10, 2, sidebarNoHit, 0},
	} {
		if kind, idx := sidebarHit(c.x, c.y, len(names), 1, 10); kind != c.kind || idx != c.idx {
			t.Errorf("hit(%d,%d) = %v,%d want %v,%d", c.x, c.y, kind, idx, c.kind, c.idx)
		}
	}
	if got := cleanSessionName([]rune("  \x1bwork\x07  ")); got != "work" {
		t.Errorf("name = %q", got)
	}
}
