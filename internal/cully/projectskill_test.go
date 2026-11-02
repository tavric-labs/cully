package cully

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	if remote != "" {
		gitIn(t, dir, "remote", "add", "origin", remote)
	}
	return dir
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:tavric-labs/Cully.git":                    "github.com/tavric-labs/cully",
		"https://github.com/tavric-labs/cully.git":                "github.com/tavric-labs/cully",
		"https://x-access-token:ghs_secret123@github.com/o/r.git": "github.com/o/r",
		"ssh://git@github.com/o/r/":                               "github.com/o/r",
		"  https://github.com/o/r  ":                              "github.com/o/r",
		"":                                                        "",
	} {
		got := normalizeRemote(in)
		if got != want {
			t.Errorf("normalizeRemote(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(got, "secret") || strings.Contains(got, "@") {
			t.Errorf("credentials survived normalisation: %q", got)
		}
	}
}

func TestProjectSkillRootIsStableAcrossWorktreesAndCwd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t, "git@github.com:tavric-labs/demo.git")
	first, err := projectSkillRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(first), "demo_") || !strings.HasPrefix(first, projectSkillsRoot()) {
		t.Fatalf("unexpected project directory %s", first)
	}
	// A subdirectory and a linked worktree are the same project.
	sub := filepath.Join(repo, "pkg", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := projectSkillRoot(sub); got != first {
		t.Fatalf("subdirectory got %s, want %s", got, first)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "side", wt)
	if got, _ := projectSkillRoot(wt); got != first {
		t.Fatalf("worktree got %s, want %s", got, first)
	}
}

func TestProjectSkillRootSurvivesAnOrganisationRename(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t, "git@github.com:mcp-runtime/demo.git")
	first, err := projectSkillRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	// The repository is renamed to another organisation; the same checkout still
	// resolves to the same directory (matched by path), and the new remote is learned.
	gitIn(t, repo, "remote", "set-url", "origin", "git@github.com:tavric-labs/demo.git")
	renamed, _ := projectSkillRoot(repo)
	if renamed != first {
		t.Fatalf("rename split the project: %s vs %s", renamed, first)
	}
	// A fresh clone elsewhere that only knows the new remote now matches as well.
	clone := newRepo(t, "https://github.com/tavric-labs/demo.git")
	if got, _ := projectSkillRoot(clone); got != first {
		t.Fatalf("clone by the learned remote got %s, want %s", got, first)
	}
}

func TestDifferentProjectsGetDifferentDirectories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := projectSkillRoot(newRepo(t, "git@github.com:o/alpha.git"))
	b, _ := projectSkillRoot(newRepo(t, "git@github.com:o/beta.git"))
	if a == "" || a == b {
		t.Fatalf("projects collided: %q %q", a, b)
	}
}

func TestProjectSkillRootWithoutGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "My Project!")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := projectSkillRoot(dir)
	if err != nil || !strings.HasPrefix(filepath.Base(got), "my-project_") {
		t.Fatalf("got %q err=%v", got, err)
	}
	if again, _ := projectSkillRoot(dir); again != got {
		t.Fatal("a non-Git directory was not stable")
	}
}

func TestFindProjectSkillRootNeverCreates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := newRepo(t, "git@github.com:o/demo.git")
	if got := findProjectSkillRoot(repo); got != "" {
		t.Fatalf("found a directory that was never made: %s", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".cully")); !os.IsNotExist(err) {
		t.Fatal("a read-only lookup created Cully files")
	}
	made, _ := projectSkillRoot(repo)
	if got := findProjectSkillRoot(repo); got != made {
		t.Fatalf("lookup got %q, want %q", got, made)
	}
}

func TestProjectRegistryHoldsNoCredentialsAndIsPrivate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t, "https://x-access-token:ghs_secret123@github.com/o/demo.git")
	if _, err := projectSkillRoot(repo); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(projectSkillsRoot(), "index.json")
	b, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "ghs_secret123") || strings.Contains(string(b), "x-access-token") {
		t.Fatalf("credentials stored in the registry: %s", b)
	}
	if info, _ := os.Stat(index); info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode %v", info.Mode().Perm())
	}
	if info, _ := os.Stat(projectSkillsRoot()); info.Mode().Perm() != 0o700 {
		t.Fatalf("projects directory mode %v", info.Mode().Perm())
	}
}
