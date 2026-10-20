package cully

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyProjectFixture(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{
		filepath.Join(root, "AGENTS.md"):                       "# Project rules\n\n" + managedStart("codex") + "\n" + legacyCodexAgentInstructions() + "\n" + managedEnd("codex") + "\n\nKeep this rule.\n",
		sharedSkillPath(root, "cully"):                         cullySkill(),
		filepath.Join(root, ".cursor", "commands", "cully.md"): cursorCullyCommand,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestProjectMigrationOnlyRemovesUnchangedGeneratedFiles(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	legacyProjectFixture(t, root)
	untouched := legacyProjectFixture(t, other)
	custom := filepath.Join(root, ".cully", "keep.txt")
	if err := os.WriteFile(custom, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for i := 0; i < 2; i++ {
		if err := MigrateProjectIntegration(root, &out); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(b) != "# Project rules\n\n\n\nKeep this rule.\n" {
		t.Fatalf("changed surrounding project instructions: %q, %v", b, err)
	}
	for _, path := range []string{sharedSkillPath(root, "cully"), filepath.Join(root, ".cursor")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy path remains: %s: %v", path, err)
		}
	}
	if b, err := os.ReadFile(custom); err != nil || string(b) != "mine" {
		t.Fatal("removed custom project data")
	}
	for path, content := range untouched {
		if b, err := os.ReadFile(path); err != nil || string(b) != content {
			t.Fatalf("changed unselected project: %s", path)
		}
	}
}

func TestProjectMigrationPreservesModifiedFiles(t *testing.T) {
	root := t.TempDir()
	files := legacyProjectFixture(t, root)
	for path, content := range files {
		if strings.HasSuffix(path, "AGENTS.md") {
			content = strings.Replace(content, "## Cully", "## My Cully instructions", 1)
		} else {
			content += "\nMy custom instructions.\n"
		}
		files[path] = content
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := MigrateProjectIntegration(root, &out); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		if b, err := os.ReadFile(path); err != nil || string(b) != content {
			t.Fatalf("modified custom file: %s", path)
		}
	}
	if strings.Count(out.String(), "Preserved modified") != 3 {
		t.Fatalf("missing preservation report: %s", &out)
	}
}

func TestProjectMigrationDoesNotFollowSymlinks(t *testing.T) {
	for _, linkDir := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[linkDir], func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			files := legacyProjectFixture(t, outside)
			link, target := filepath.Join(root, "AGENTS.md"), filepath.Join(outside, "AGENTS.md")
			if linkDir {
				link, target = filepath.Join(root, ".cully"), filepath.Join(outside, ".cully")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := MigrateProjectIntegration(root, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Preserved unsafe or linked") {
				t.Fatalf("missing linked-path warning: %s", &out)
			}
			for path, content := range files {
				if b, err := os.ReadFile(path); err != nil || string(b) != content {
					t.Fatalf("modified linked file: %s", path)
				}
			}
			rootLink := filepath.Join(t.TempDir(), "project")
			if err := os.Symlink(outside, rootLink); err != nil {
				t.Fatal(err)
			}
			if err := MigrateProjectIntegration(rootLink, &out); err == nil {
				t.Fatal("accepted linked project directory")
			}
		})
	}
}

func TestUserSetupPreservesLegacyProjects(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	files := legacyProjectFixture(t, root)
	t.Chdir(root)
	if err := installCodex(); err != nil {
		t.Fatal(err)
	}
	if err := installCursor(); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall("codex", "cursor"); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		if b, err := os.ReadFile(path); err != nil || string(b) != content {
			t.Fatalf("setup or uninstall modified legacy file: %s", path)
		}
	}
}

func TestCodexSetupUsesExistingGlobalOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "AGENTS.override.md")
	const original = "# My global override\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installCodex(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(b), original) || !strings.Contains(string(b), managedStart("codex")) {
		t.Fatalf("override not respected: %s, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(home, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("wrote ignored global instructions")
	}
	for _, item := range auditHarness(harnessDirs{Codex: home}) {
		if item.Component == "Instructions" && !item.Present() {
			t.Fatal("harness missed global override instructions")
		}
	}
	if err := uninstallCodex(); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil || string(b) != original {
		t.Fatalf("lost global override: %q, %v", b, err)
	}
}

func TestManagedInstructionsReadFailureIsNotOverwritten(t *testing.T) {
	path := t.TempDir()
	if err := upsertManagedSection(path, "codex", "new instructions"); err == nil {
		t.Fatal("ignored instruction read error")
	}
}
