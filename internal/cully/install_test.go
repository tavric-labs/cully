package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheSetupSkillIsInstalledBesideTheMainSkillAndRemovedWhenUnchanged(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	if err := installMemorySkill("claude"); err != nil {
		t.Fatal(err)
	}
	p := setupSkillPath("claude")
	if filepath.Base(filepath.Dir(p)) != "cully-setup" {
		t.Fatalf("path = %s", p)
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "name: cully-setup") || !strings.Contains(string(b), "<!-- cully:skill:managed -->") {
		t.Fatalf("the setup skill was not installed: %v %q", err, b)
	}
	for _, bad := range []string{"cat ~/.cully", "--local", "docker system prune\n"} {
		if strings.Contains(string(b), bad+"`") {
			t.Errorf("the skill tells the agent to %q", bad)
		}
	}
	// A skill the user edited is left alone on removal; an unchanged one is removed.
	if err := os.WriteFile(p, append(b, []byte("\nmy note\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = removeManagedMemorySkill("claude")
	if _, err := os.Stat(p); err != nil {
		t.Fatal("an edited setup skill was removed")
	}
	_ = os.WriteFile(p, b, 0o644)
	_ = removeManagedMemorySkill("claude")
	if _, err := os.Stat(p); err == nil {
		t.Fatal("an unchanged setup skill was left behind")
	}
}
