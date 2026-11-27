package cully

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Checkpoints are Cully's own per-turn snapshots of a task's worktree. They live under
// refs/cully/checkpoints/<task>/<n>, built with git plumbing against a private index file, so the
// task's branch, its commits and its staged state are never touched. Untracked files are included
// and ignored files are not. Every harness gets them, because only one harness restores files itself.

const (
	uiCheckpointMax     = 100 // per task; the oldest refs are pruned
	uiCheckpointFiles   = 200 // changed files recorded per checkpoint
	uiCheckpointNoGit   = "This task works in a folder with no git, so Cully cannot take checkpoints or revert files there."
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
	Reason      string         `json:"reason,omitempty"`
	CanRevert   bool           `json:"canRevert"`
	RevertBlock string         `json:"revertBlock,omitempty"`
	Checkpoints []uiCheckpoint `json:"checkpoints"`
}

func uiGitRun(dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
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

func (a *uiAgent) checkpointsAvailable() (bool, string) {
	if a.req.inPlaceDir != "" {
		return false, uiCheckpointNoGit
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
	_, _ = uiGitRun(a.worktree, env, "", "read-tree", "HEAD") // an unborn HEAD just means an empty start
	if _, err := uiGitRun(a.worktree, env, "", "add", "-A"); err != nil {
		return "", err
	}
	out, err := uiGitRun(a.worktree, env, "", "write-tree")
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
		out, err := uiGitRun(a.worktree, nil, "", "rev-parse", "HEAD^{tree}")
		prev = strings.TrimSpace(out)
		if err != nil || prev == "" {
			prev = uiGitEmptyTree
		}
	}
	idEnv := []string{"GIT_AUTHOR_NAME=Cully", "GIT_AUTHOR_EMAIL=cully@localhost", "GIT_COMMITTER_NAME=Cully", "GIT_COMMITTER_EMAIL=cully@localhost"}
	msg := fmt.Sprintf("Cully checkpoint %d of task %s", n, a.taskID())
	commit, err := uiGitRun(a.worktree, idEnv, "", "commit-tree", tree, "-m", msg)
	if err != nil {
		return uiCheckpoint{}, false, err
	}
	commit = strings.TrimSpace(commit)
	if _, err := uiGitRun(a.worktree, nil, "", "update-ref", uiCheckpointRefBase+a.taskID()+"/"+strconv.Itoa(n), commit); err != nil {
		return uiCheckpoint{}, false, err
	}
	changed, _ := uiGitRun(a.worktree, nil, "", "diff-tree", "-r", "--name-status", "--no-renames", prev, tree)
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
		_, _ = uiGitRun(a.worktree, nil, "", "update-ref", "-d", uiCheckpointRefBase+a.taskID()+"/"+strconv.Itoa(d.N))
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
	out := uiCheckpointList{Checkpoints: []uiCheckpoint{}}
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
	if stat, _ := uiGitRun(a.worktree, nil, "", "diff", "--stat", cp.Prev, cp.Tree); strings.TrimSpace(stat) != "" {
		b.WriteString(stat + "\n")
	}
	full, _ := uiGitRun(a.worktree, nil, "", "diff", cp.Prev, cp.Tree)
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
	out, err := uiGitRun(a.worktree, nil, "", "diff-tree", "-r", "-z", "--name-status", "--no-renames", safety.Tree, target.Tree)
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
		if _, err := uiGitRun(a.worktree, env, "", "read-tree", target.Tree); err != nil {
			return res, http.StatusInternalServerError, err
		}
		for _, p := range restore {
			if info, err := os.Lstat(filepath.Join(a.worktree, p)); err == nil && info.IsDir() {
				_ = os.RemoveAll(filepath.Join(a.worktree, p)) // a folder where the checkpoint had a file
			}
		}
		if _, err := uiGitRun(a.worktree, env, strings.Join(restore, "\x00")+"\x00", "checkout-index", "-f", "-q", "--stdin", "-z"); err != nil {
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
