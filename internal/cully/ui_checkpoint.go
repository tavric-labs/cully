package cully

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Checkpoints are Cully's own per-turn snapshots of a task's worktree. They live under
// refs/cully/checkpoints/<task>/<n>, built with git plumbing against a private index file, so the
// task's branch, its commits and its staged state are never touched. Untracked files are included
// and ignored files are not. Every harness gets them, because only one harness restores files itself.

const (
	uiCheckpointMax     = 100 // per task; the oldest refs are pruned
	uiCheckpointFiles   = 200 // changed files recorded per checkpoint
	uiCheckpointNoGit   = "This is a named agent's own folder, which Cully does not snapshot."
	uiShadowMaxFiles    = 20000 // a folder with more files than this is not snapshotted
	uiGitEmptyTree      = "4b825dc642cb6eb9a060e54bf8d69288fbf2c15"
	uiCheckpointRefBase = "refs/cully/checkpoints/"
)

var uiCheckpointIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type uiCheckpoint struct {
	N      int       `json:"n"`
	Time   time.Time `json:"time"`
	Turn   int       `json:"turn"`
	Tree   string    `json:"tree"`
	Prev   string    `json:"prev"`            // tree of the checkpoint before it (or of HEAD for the first)
	Label  string    `json:"label,omitempty"` // first line of the agent's last message
	Files  []string  `json:"files"`           // name-status lines since the previous checkpoint, e.g. "M\tmain.go"
	Total  int       `json:"total"`           // how many files changed, even if Files was capped
	Safety bool      `json:"safety,omitempty"`
}

type uiCheckpointList struct {
	Available   bool           `json:"available"`
	Shadow      bool           `json:"shadow,omitempty"` // the folder has no git: snapshots are kept in a private repository under Cully's own folder
	Reason      string         `json:"reason,omitempty"`
	CanRevert   bool           `json:"canRevert"`
	RevertBlock string         `json:"revertBlock,omitempty"`
	Checkpoints []uiCheckpoint `json:"checkpoints"`
}

// uiGitDirs remembers the git directory of each worktree the first time Cully snapshots it, which is
// before the task's first turn. Later calls use that one, so a task that rewrites the worktree's
// .git file cannot point Cully's git at another repository.
var uiGitDirs sync.Map

// uiGitSafe returns the settings that keep git from running commands the task could have planted in
// its worktree's config: no file-system monitor, no hooks, and every configured filter driver
// replaced by a plain pass-through. It reads the config with the same git directory and environment
// the real command will use, and with -z so a name cannot be mis-split. Reading the config runs nothing.
func uiGitSafe(dir string, env []string) []string {
	args := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false", "-c", "core.attributesFile=/dev/null", "-c", "diff.external=", "-c", "core.pager=cat", "-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-c", "gpg.program=false", "-c", "gpg.ssh.program=false", "-c", "gpg.x509.program=false"}
	cmd := exec.Command("git", "config", "-z", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return args
	}
	seen := map[string]bool{}
	for _, entry := range strings.Split(string(out), "\x00") {
		key, _, _ := strings.Cut(entry, "\n")
		if i := strings.LastIndex(key, "."); i > len("filter.") {
			seen[key[:i]] = true
		}
	}
	for name := range seen {
		args = append(args, "-c", name+".clean=cat", "-c", name+".smudge=cat", "-c", name+".process=", "-c", name+".required=false")
	}
	return args
}

// uiGitDirFor finds the git directory of a worktree once and keeps it. It fails closed: with no
// directory it can trust, nothing runs.
func uiGitDirFor(dir string) (string, error) {
	if gd, ok := uiGitDirs.Load(dir); ok {
		return gd.(string), nil
	}
	cmd := exec.Command("git", "rev-parse", "--absolute-git-dir")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.Output()
	gd := strings.TrimSpace(string(out))
	if err != nil || gd == "" || !filepath.IsAbs(gd) {
		return "", fmt.Errorf("git: the git directory of %s cannot be determined", dir)
	}
	if st, err := os.Stat(gd); err != nil || !st.IsDir() {
		return "", fmt.Errorf("git: the git directory of %s does not exist", dir)
	}
	uiGitDirs.Store(dir, gd)
	return gd, nil
}

// uiShadowSkip are the folders a snapshot never includes: they are large, rebuilt by a tool, and not work
// the agent is doing.
var uiShadowSkip = map[string]bool{"node_modules": true, ".venv": true, "venv": true, "__pycache__": true, "dist": true, "build": true, "target": true, ".next": true, ".cache": true, ".gradle": true, ".tox": true}

var errShadowTooBig = fmt.Errorf("it has more than %d files, so a snapshot would be slow and large; start the task in a smaller folder, or use git there", uiShadowMaxFiles)

// uiShadowGitDir returns the private git repository that snapshots a folder which has no git. The repository
// lives under Cully's config folder and is pointed at the folder with GIT_DIR and GIT_WORK_TREE, which is how
// every checkpoint command already runs, so the checkpoint code works on it unchanged. It refuses folders where
// a snapshot would be unwise: the root, your home folder or a folder above it, a path near the top of the disk,
// and a folder with a very large number of files. It fails closed.
func uiShadowGitDir(dir string) (string, error) {
	if gd, ok := uiGitDirs.Load(dir); ok {
		return gd.(string), nil
	}
	clean := filepath.Clean(dir)
	sep := string(os.PathSeparator)
	home, _ := os.UserHomeDir()
	switch {
	case !filepath.IsAbs(clean) || strings.Count(clean, sep) < 3:
		return "", fmt.Errorf("%s is too close to the top of the disk to snapshot", clean)
	case home != "" && (clean == home || strings.HasPrefix(home+sep, clean+sep)):
		return "", fmt.Errorf("a home folder or a folder above it is too broad to snapshot")
	case strings.HasPrefix(clean+sep, filepath.Clean(ConfigDir())+sep):
		return "", fmt.Errorf("Cully's own folder is not snapshotted")
	}
	sum := sha256.Sum256([]byte(clean))
	gd := filepath.Join(ConfigDir(), "ui-shadow", hex.EncodeToString(sum[:8])+".git")
	if st, err := os.Stat(filepath.Join(gd, "HEAD")); err == nil && !st.IsDir() {
		uiGitDirs.Store(dir, gd)
		return gd, nil
	}
	files := 0
	walkErr := filepath.WalkDir(clean, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != clean && (uiShadowSkip[d.Name()] || d.Name() == ".git") {
				return fs.SkipDir
			}
			return nil
		}
		if files++; files > uiShadowMaxFiles {
			return errShadowTooBig
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if err := os.MkdirAll(filepath.Dir(gd), 0o700); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "init", "--bare", "-q", gd)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init: %v: %s", err, strings.TrimSpace(string(out)))
	}
	var ex strings.Builder
	ex.WriteString("# Cully's snapshots skip these.\n")
	for name := range uiShadowSkip {
		ex.WriteString(name + "/\n")
	}
	ex.WriteString("*.pyc\n.DS_Store\n")
	if err := os.WriteFile(filepath.Join(gd, "info", "exclude"), []byte(ex.String()), 0o600); err != nil {
		return "", err
	}
	uiGitDirs.Store(dir, gd)
	return gd, nil
}

func uiGitRun(dir string, env []string, stdin string, args ...string) (string, error) {
	gd, err := uiGitDirFor(dir)
	if err != nil {
		return "", err
	}
	base := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_DIR="+gd, "GIT_WORK_TREE="+dir)
	pre := uiGitSafe(dir, base)
	cmd := exec.Command("git", append(pre, args...)...)
	cmd.Dir = dir
	cmd.Env = append(base, env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// gitRun runs one git command for this task's checkpoints. For a task in a folder with no git it first pins the
// private shadow repository, so git is never asked which repository the folder belongs to: a folder nested in
// someone else's repository, or one that later gains a .git, must not have its snapshots written there.
func (a *uiAgent) gitRun(env []string, stdin string, args ...string) (string, error) {
	if a.req.inPlaceDir != "" && a.req.identityName == "" {
		if _, err := uiShadowGitDir(a.worktree); err != nil {
			return "", err
		}
	}
	return uiGitRun(a.worktree, env, stdin, args...)
}

func (a *uiAgent) checkpointsAvailable() (bool, string) {
	if a.req.inPlaceDir != "" {
		if a.req.identityName != "" {
			return false, uiCheckpointNoGit
		}
		// A folder with no git: snapshots go in a private repository under Cully's own folder, so nothing
		// is added to the folder itself.
		if _, err := uiShadowGitDir(a.worktree); err != nil {
			return false, "Cully cannot take checkpoints in this folder: " + err.Error()
		}
	}
	if a.worktree == "" || !uiCheckpointIDRe.MatchString(a.taskID()) {
		return false, "Checkpoints are not available for this task."
	}
	return true, ""
}

func (a *uiAgent) taskRecord() (uiTaskRecord, bool) {
	var rec uiTaskRecord
	uiTaskMu.Lock()
	defer uiTaskMu.Unlock()
	return rec, uiDB().get("task", a.taskID(), &rec)
}

// snapshotTree writes the worktree's current content (tracked, staged-or-not, untracked, minus
// ignored) as a tree object, using a throwaway index so the real one is never read or written.
func (a *uiAgent) snapshotTree() (string, error) {
	dir, err := os.MkdirTemp("", "cully-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
	_, _ = a.gitRun(env, "", "read-tree", "HEAD") // an unborn HEAD just means an empty start
	if _, err := a.gitRun(env, "", "add", "-A"); err != nil {
		return "", err
	}
	out, err := a.gitRun(env, "", "write-tree")
	return strings.TrimSpace(out), err
}

func uiParseNameStatus(out string) []string {
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			files = append(files, l)
		}
	}
	return files
}

// checkpoint takes a snapshot unless nothing changed since the last one (force always takes it).
// It returns the checkpoint that now stands for the current state and whether it is new.
func (a *uiAgent) checkpoint(label string, turn int, force bool) (uiCheckpoint, bool, error) {
	if ok, why := a.checkpointsAvailable(); !ok {
		return uiCheckpoint{}, false, fmt.Errorf("%s", why)
	}
	a.cpMu.Lock()
	defer a.cpMu.Unlock()
	if a.mgr != nil {
		a.mgr.saveTask(a) // make sure the task record exists
	}
	rec, ok := a.taskRecord()
	if !ok {
		return uiCheckpoint{}, false, fmt.Errorf("task record not found")
	}
	tree, err := a.snapshotTree()
	if err != nil {
		return uiCheckpoint{}, false, err
	}
	prev, n := "", 1
	if k := len(rec.Checkpoints); k > 0 {
		last := rec.Checkpoints[k-1]
		if last.Tree == tree && !force {
			return last, false, nil
		}
		prev, n = last.Tree, last.N+1
	} else {
		out, err := a.gitRun(nil, "", "rev-parse", "HEAD^{tree}")
		prev = strings.TrimSpace(out)
		if err != nil || prev == "" {
			prev = uiGitEmptyTree
		}
	}
	idEnv := []string{"GIT_AUTHOR_NAME=Cully", "GIT_AUTHOR_EMAIL=cully@localhost", "GIT_COMMITTER_NAME=Cully", "GIT_COMMITTER_EMAIL=cully@localhost"}
	msg := fmt.Sprintf("Cully checkpoint %d of task %s", n, a.taskID())
	commit, err := a.gitRun(idEnv, "", "commit-tree", "--no-gpg-sign", tree, "-m", msg)
	if err != nil {
		return uiCheckpoint{}, false, err
	}
	commit = strings.TrimSpace(commit)
	if _, err := a.gitRun(nil, "", "update-ref", uiCheckpointRefBase+a.taskID()+"/"+strconv.Itoa(n), commit); err != nil {
		return uiCheckpoint{}, false, err
	}
	changed, _ := a.gitRun(nil, "", "diff-tree", "-r", "--name-status", "--no-renames", prev, tree)
	files := uiParseNameStatus(changed)
	cp := uiCheckpoint{N: n, Time: time.Now().UTC(), Turn: turn, Tree: tree, Prev: prev, Label: label, Total: len(files), Safety: force}
	if len(files) > uiCheckpointFiles {
		files = files[:uiCheckpointFiles]
	}
	cp.Files = files
	var dropped []uiCheckpoint
	uiTaskMu.Lock()
	var cur uiTaskRecord
	if uiDB().get("task", a.taskID(), &cur) {
		cur.Checkpoints = append(cur.Checkpoints, cp)
		if over := len(cur.Checkpoints) - uiCheckpointMax; over > 0 {
			dropped = append(dropped, cur.Checkpoints[:over]...)
			cur.Checkpoints = append([]uiCheckpoint(nil), cur.Checkpoints[over:]...)
		}
		uiDB().put("task", a.taskID(), cur)
	}
	uiTaskMu.Unlock()
	for _, d := range dropped {
		_, _ = a.gitRun(nil, "", "update-ref", "-d", uiCheckpointRefBase+a.taskID()+"/"+strconv.Itoa(d.N))
	}
	return cp, true, nil
}

// lastMessageLabel is the first line of the agent's latest message, for a checkpoint's label.
func (a *uiAgent) lastMessageLabel() string {
	evs := a.allEvents()
	for _, kind := range []string{"text", "result"} { // the agent's words first, the turn's result line as a fallback
		for i := len(evs) - 1; i >= 0; i-- {
			if evs[i].Kind != kind {
				continue
			}
			line, _, _ := strings.Cut(strings.TrimSpace(evs[i].Text), "\n")
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if a.mgr != nil {
				line = a.mgr.cfg.Tokens.redact(line)
			}
			return uiClip(line, 120)
		}
	}
	return ""
}

// checkpointTurn runs at the end of a turn. A failure is noted in the task's events, never fatal.
func (a *uiAgent) checkpointTurn(turn int) {
	if ok, _ := a.checkpointsAvailable(); !ok {
		return
	}
	if _, _, err := a.checkpoint(a.lastMessageLabel(), turn, false); err != nil {
		a.add(uiEvent{Kind: "note", Text: "Could not take a checkpoint: " + err.Error()})
	}
}

func (a *uiAgent) checkpointView() uiCheckpointList {
	out := uiCheckpointList{Checkpoints: []uiCheckpoint{}, Shadow: a.req.inPlaceDir != "" && a.req.identityName == ""}
	ok, why := a.checkpointsAvailable()
	if !ok {
		out.Reason = why
		return out
	}
	out.Available = true
	if rec, found := a.taskRecord(); found && rec.Checkpoints != nil {
		out.Checkpoints = rec.Checkpoints
	}
	switch a.snapshot().State {
	case uiWorking, uiPreparing:
		out.RevertBlock = "The task is working. Stop it first, then revert its files."
	default:
		out.CanRevert = true
	}
	return out
}

func (a *uiAgent) findCheckpoint(n int) (uiCheckpoint, bool) {
	rec, _ := a.taskRecord()
	for _, c := range rec.Checkpoints {
		if c.N == n {
			return c, true
		}
	}
	return uiCheckpoint{}, false
}

// checkpointDiff shows what one turn changed.
func (a *uiAgent) checkpointDiff(cp uiCheckpoint) string {
	var b strings.Builder
	if stat, _ := a.gitRun(nil, "", "diff", "--no-ext-diff", "--no-textconv", "--stat", cp.Prev, cp.Tree); strings.TrimSpace(stat) != "" {
		b.WriteString(stat + "\n")
	}
	full, _ := a.gitRun(nil, "", "diff", "--no-ext-diff", "--no-textconv", cp.Prev, cp.Tree)
	if len(full) > uiDiffMax {
		full = full[:uiDiffMax] + "\n... diff cut here\n"
	}
	b.WriteString(full)
	if b.Len() == 0 {
		return "No file changes in this checkpoint."
	}
	return b.String()
}

type uiRevertResult struct {
	Reverted int          `json:"reverted"`
	Restored int          `json:"restored"`
	Removed  int          `json:"removed"`
	Safety   uiCheckpoint `json:"safety"`
}

// revertTo restores the worktree's files to a checkpoint: files that differ are written back, files
// that did not exist then are removed. A safety checkpoint of the current state is taken first.
func (a *uiAgent) revertTo(n int) (uiRevertResult, int, error) {
	var res uiRevertResult
	if ok, why := a.checkpointsAvailable(); !ok {
		return res, http.StatusConflict, fmt.Errorf("%s", why)
	}
	switch a.snapshot().State {
	case uiWorking, uiPreparing:
		return res, http.StatusConflict, fmt.Errorf("the task is working: stop it first, then revert its files")
	}
	target, ok := a.findCheckpoint(n)
	if !ok {
		return res, http.StatusNotFound, fmt.Errorf("no such checkpoint")
	}
	safety, _, err := a.checkpoint("before revert", a.snapshotTurns(), true)
	if err != nil {
		return res, http.StatusInternalServerError, fmt.Errorf("could not take the safety checkpoint, so nothing was changed: %v", err)
	}
	res.Safety, res.Reverted = safety, n
	out, err := a.gitRun(nil, "", "diff-tree", "-r", "-z", "--name-status", "--no-renames", safety.Tree, target.Tree)
	if err != nil {
		return res, http.StatusInternalServerError, err
	}
	// -z output: status NUL path NUL ... ; status is from the current state to the target.
	parts := strings.Split(out, "\x00")
	var restore, remove []string
	for i := 0; i+1 < len(parts); i += 2 {
		path := parts[i+1]
		clean := filepath.Clean(path)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			continue
		}
		if strings.HasPrefix(parts[i], "D") { // present now, absent at the checkpoint
			remove = append(remove, path)
		} else {
			restore = append(restore, path)
		}
	}
	for _, p := range remove {
		full := filepath.Join(a.worktree, p)
		if err := os.RemoveAll(full); err != nil {
			return res, http.StatusInternalServerError, err
		}
		for d := filepath.Dir(full); d != a.worktree && strings.HasPrefix(d, a.worktree); d = filepath.Dir(d) {
			if os.Remove(d) != nil { // stops at the first folder that is not empty
				break
			}
		}
	}
	if len(restore) > 0 {
		dir, err := os.MkdirTemp("", "cully-index-")
		if err != nil {
			return res, http.StatusInternalServerError, err
		}
		defer os.RemoveAll(dir)
		env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
		if _, err := a.gitRun(env, "", "read-tree", target.Tree); err != nil {
			return res, http.StatusInternalServerError, err
		}
		for _, p := range restore {
			if info, err := os.Lstat(filepath.Join(a.worktree, p)); err == nil && info.IsDir() {
				_ = os.RemoveAll(filepath.Join(a.worktree, p)) // a folder where the checkpoint had a file
			}
		}
		if _, err := a.gitRun(env, strings.Join(restore, "\x00")+"\x00", "checkout-index", "-f", "-q", "--stdin", "-z"); err != nil {
			return res, http.StatusInternalServerError, err
		}
	}
	res.Restored, res.Removed = len(restore), len(remove)
	a.add(uiEvent{Kind: "note", Text: fmt.Sprintf("Files reverted to checkpoint %d (turn %d). A safety checkpoint, number %d, holds the state from before.", n, target.Turn, safety.N)})
	return res, http.StatusOK, nil
}

func (a *uiAgent) snapshotTurns() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.doneTurns
}

func (s *uiServer) checkpointList(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.agent(w, r); ok {
		uiJSON(w, http.StatusOK, a.checkpointView())
	}
}

func (s *uiServer) checkpointDiffGet(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		uiError(w, http.StatusBadRequest, "bad checkpoint number")
		return
	}
	cp, found := a.findCheckpoint(n)
	if !found {
		uiError(w, http.StatusNotFound, "no such checkpoint")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(a.checkpointDiff(cp)))
}

func (s *uiServer) checkpointRevert(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		uiError(w, http.StatusBadRequest, "bad checkpoint number")
		return
	}
	res, code, err := a.revertTo(n)
	if err != nil {
		uiError(w, code, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, res)
}

// checkpointBefore takes the starting checkpoint, before the first turn of this run.
func (a *uiAgent) checkpointBefore() {
	if ok, _ := a.checkpointsAvailable(); !ok {
		return
	}
	if _, _, err := a.checkpoint("Before the first turn", 0, false); err != nil {
		a.add(uiEvent{Kind: "note", Text: "Could not take a checkpoint: " + err.Error()})
	}
}
