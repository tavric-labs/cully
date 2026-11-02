package cully

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Cully keeps what it learns about a project in its own directory, never in the
// project's repository: ~/.cully/skills/projects/<name>_<id>/. Repository skills
// and instruction files belong to the user and are usually checked into Git, so
// Cully does not write them. After an update it names the directory and leaves
// copying anything into the repository to the user.
//
// The <id> is minted once and recorded in index.json beside the directories.
// A project is recognised by its repository's common Git directory (so every
// worktree of one clone resolves to the same project) and by its normalised
// remote (so a second clone does, and so does the same repository after its
// owner or organisation is renamed).

const projectSkillFile = "SKILL.md"

func projectSkillsRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cully", "skills", "projects")
}

type projectRecord struct {
	Paths   []string `json:"paths,omitempty"`
	Remotes []string `json:"remotes,omitempty"`
}

type projectRegistry struct {
	Projects map[string]projectRecord `json:"projects"`
}

var unsafeProjectName = regexp.MustCompile(`[^a-z0-9._-]+`)

func projectDirName(name string) string {
	name = strings.Trim(unsafeProjectName.ReplaceAllString(strings.ToLower(name), "-"), "-.")
	if name == "" {
		return "project"
	}
	if len(name) > 48 {
		name = strings.Trim(name[:48], "-.")
	}
	return name
}

// normalizeRemote reduces a Git remote URL to host/owner/repo in lower case. It
// drops the scheme, any credentials, a trailing .git and an scp-style colon, so
// two spellings of one remote compare equal and no token is ever stored.
func normalizeRemote(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if at := strings.LastIndex(u, "@"); at >= 0 {
		u = u[at+1:]
	}
	if i := strings.Index(u, ":"); i >= 0 && !strings.Contains(u[:i], "/") {
		u = u[:i] + "/" + strings.TrimLeft(u[i+1:], "/")
	}
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	return strings.ToLower(u)
}

func gitOutput(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// projectIdentity returns the project's display name and the paths and remotes
// that identify it. Outside a Git repository the directory itself is the project.
func projectIdentity(cwd string) (name string, paths, remotes []string) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = filepath.Clean(cwd)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	root := abs
	if common := gitOutput(abs, "rev-parse", "--git-common-dir"); common != "" {
		if !filepath.IsAbs(common) {
			common = filepath.Join(abs, common)
		}
		common = filepath.Clean(common)
		if resolved, err := filepath.EvalSymlinks(common); err == nil {
			common = resolved
		}
		root = common
		if filepath.Base(common) == ".git" {
			root = filepath.Dir(common)
		}
	}
	paths = []string{root}
	name = filepath.Base(root)
	if remote := normalizeRemote(gitOutput(abs, "config", "--get", "remote.origin.url")); remote != "" {
		remotes = []string{remote}
		if base := filepath.Base(remote); base != "." && base != "/" {
			name = base
		}
	}
	return projectDirName(name), paths, remotes
}

func (r projectRecord) shares(paths, remotes []string) bool {
	for _, p := range paths {
		if slices.Contains(r.Paths, p) {
			return true
		}
	}
	for _, m := range remotes {
		if slices.Contains(r.Remotes, m) {
			return true
		}
	}
	return false
}

func mergeUnique(list []string, add []string) []string {
	for _, v := range add {
		if !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

func loadProjectRegistry(path string) projectRegistry {
	reg := projectRegistry{Projects: map[string]projectRecord{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &reg)
	}
	if reg.Projects == nil {
		reg.Projects = map[string]projectRecord{}
	}
	return reg
}

func saveProjectRegistry(path string, reg projectRegistry) error {
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func lockProjectRegistry(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".index.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = unlockFile(f); _ = f.Close() }, nil
}

// findProjectSkillRoot returns the project's skill directory when one already
// exists. It never creates or registers anything, so a read-only caller such as
// a session-start hook leaves no trace.
func findProjectSkillRoot(cwd string) string {
	_, paths, remotes := projectIdentity(cwd)
	root := projectSkillsRoot()
	reg := loadProjectRegistry(filepath.Join(root, "index.json"))
	for dir, rec := range reg.Projects {
		if rec.shares(paths, remotes) {
			if info, err := os.Stat(filepath.Join(root, dir)); err == nil && info.IsDir() {
				return filepath.Join(root, dir)
			}
		}
	}
	return ""
}

// projectSkillRoot returns the project's private skill directory, registering
// the project and creating the directory on first use.
func projectSkillRoot(cwd string) (string, error) {
	name, paths, remotes := projectIdentity(cwd)
	root := projectSkillsRoot()
	unlock, err := lockProjectRegistry(root)
	if err != nil {
		return "", err
	}
	defer unlock()
	index := filepath.Join(root, "index.json")
	reg := loadProjectRegistry(index)
	dirName := ""
	for dir, rec := range reg.Projects {
		if rec.shares(paths, remotes) {
			dirName = dir
			rec.Paths = mergeUnique(rec.Paths, paths)
			rec.Remotes = mergeUnique(rec.Remotes, remotes)
			reg.Projects[dir] = rec
			break
		}
	}
	if dirName == "" {
		id := make([]byte, 4)
		if _, err := rand.Read(id); err != nil {
			return "", err
		}
		dirName = name + "_" + hex.EncodeToString(id)
		reg.Projects[dirName] = projectRecord{Paths: paths, Remotes: remotes}
	}
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := saveProjectRegistry(index, reg); err != nil {
		return "", err
	}
	return dir, nil
}
