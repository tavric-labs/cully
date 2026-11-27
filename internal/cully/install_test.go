package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	starterskills "github.com/mcp-runtime/cully/skills/starter"
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

func TestSetupInstallsTheSessionsAndStarterSkillsAndUninstallRemovesThem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	if err := installMemorySkill("claude"); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"cully-sessions"}, starterskills.Names...)
	for _, name := range want {
		p := filepath.Join(runtimeAgentDir("claude"), "skills", name, "SKILL.md")
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(b), "cully:skill:managed") {
			t.Fatalf("%s not installed: %v", name, err)
		}
	}
	// A skill the user edited is theirs: setup leaves it, and uninstall does not delete it.
	edited := filepath.Join(runtimeAgentDir("claude"), "skills", "cully-spec", "SKILL.md")
	if err := os.WriteFile(edited, []byte("---\nname: cully-spec\n---\nmy own version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installMemorySkill("claude"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(edited); !strings.Contains(string(b), "my own version") {
		t.Fatalf("setup overwrote an edited skill: %s", b)
	}
	if err := removeManagedMemorySkill("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeAgentDir("claude"), "skills", "cully-verify", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("an untouched skill was left behind: %v", err)
	}
	if _, err := os.Stat(edited); err != nil {
		t.Fatalf("uninstall deleted a skill the user edited: %v", err)
	}
}
