package cully

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Each message the fake agent receives writes a new file, edits shared.txt and leaves a note file
// that is ignored by git, so every turn changes the worktree.
const fakeEditingClaude = `#!/bin/bash
echo '{"type":"system","subtype":"init"}'
n=0
while IFS= read -r line; do
  n=$((n+1))
  case "$line" in
    *nochange*) ;;
    *) echo "turn $n" > "turn$n.txt"; echo "v$n" > shared.txt; echo junk > ignored.log ;;
  esac
  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Did step '$n'\nsecond line"}]}}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"turn done"}'
done
`

func cpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cpTurn(t *testing.T, a *uiAgent, text string) {
	t.Helper()
	before := a.snapshotTurns()
	if err := a.send(text); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "turn to finish", func() bool { return a.snapshotTurns() > before && a.snapshot().State == "waiting" })
}

func cpStart(t *testing.T) (*uiAgent, *uiManager, string) {
	t.Helper()
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeEditingClaude), "tok-aaa1")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cpGit(t, repo, "add", ".gitignore")
	cpGit(t, repo, "commit", "-q", "-m", "ignore logs")
	a, err := m.start(uiStartRequest{Repo: repo, Title: "cp", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	return a, m, repo
}

func TestCheckpointPerTurnAndSkipWhenUnchanged(t *testing.T) {
	a, _, _ := cpStart(t)
	v := a.checkpointView()
	if !v.Available || len(v.Checkpoints) < 2 {
		t.Fatalf("want a start checkpoint and one for turn 1, got %+v", v)
	}
	if v.Checkpoints[0].Label != "Before the first turn" || v.Checkpoints[0].Turn != 0 {
		t.Fatalf("first = %+v", v.Checkpoints[0])
	}
	last := v.Checkpoints[len(v.Checkpoints)-1]
	if last.Turn != 1 || last.Label != "Did step 1" {
		t.Fatalf("last = %+v", last)
	}
	// untracked file turn1.txt is included, ignored.log is not
	joined := strings.Join(last.Files, "\n")
	if !strings.Contains(joined, "A\tturn1.txt") || strings.Contains(joined, "ignored.log") {
		t.Fatalf("files = %q", joined)
	}
	n := len(v.Checkpoints)
	cpTurn(t, a, "nochange please")
	if got := len(a.checkpointView().Checkpoints); got != n {
		t.Fatalf("a turn with no change added a checkpoint: %d -> %d", n, got)
	}
	cpTurn(t, a, "again")
	if got := len(a.checkpointView().Checkpoints); got != n+1 {
		t.Fatalf("checkpoints = %d, want %d", got, n+1)
	}
	if got := cpGit(t, a.worktree, "for-each-ref", "--format=%(refname)", "refs/cully/checkpoints/"+a.taskID()+"/"); strings.Count(got, "\n")+1 != n+1 {
		t.Fatalf("refs = %q", got)
	}
}

func TestCheckpointLeavesBranchAndIndexAlone(t *testing.T) {
	a, _, _ := cpStart(t)
	head := cpGit(t, a.worktree, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(a.worktree, "staged.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	cpGit(t, a.worktree, "add", "staged.txt")
	statusBefore := cpGit(t, a.worktree, "status", "--porcelain")
	cpTurn(t, a, "more")
	if got := cpGit(t, a.worktree, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD moved %s -> %s", head, got)
	}
	after := cpGit(t, a.worktree, "status", "--porcelain")
	if !strings.Contains(after, "A  staged.txt") || !strings.Contains(statusBefore, "A  staged.txt") {
		t.Fatalf("staged state changed:\n%s\n--\n%s", statusBefore, after)
	}
	if strings.Contains(after, "turn1.txt") && strings.Contains(after, "A  turn1.txt") {
		t.Fatalf("checkpoint staged a file: %s", after)
	}
}

func TestCheckpointRevertRestoresRemovesAndCanBeUndone(t *testing.T) {
	a, _, _ := cpStart(t)
	cpTurn(t, a, "two") // turn 2 adds turn2.txt, shared.txt = v2
	cps := a.checkpointView().Checkpoints
	var after1 uiCheckpoint
	for _, c := range cps {
		if c.Turn == 1 {
			after1 = c
		}
	}
	res, code, err := a.revertTo(after1.N)
	if err != nil || code != http.StatusOK {
		t.Fatalf("revert: %v %d", err, code)
	}
	if res.Removed != 1 || res.Restored != 1 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(a.worktree, "turn2.txt")); !os.IsNotExist(err) {
		t.Fatalf("turn2.txt should be gone: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(a.worktree, "shared.txt")); strings.TrimSpace(string(b)) != "v1" {
		t.Fatalf("shared.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(a.worktree, "turn1.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.worktree, "ignored.log")); err != nil {
		t.Fatalf("ignored file must be left alone: %v", err)
	}
	if res.Safety.Label != "before revert" {
		t.Fatalf("safety = %+v", res.Safety)
	}
	// undo: revert to the safety checkpoint
	if _, _, err := a.revertTo(res.Safety.N); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(a.worktree, "shared.txt")); strings.TrimSpace(string(b)) != "v2" {
		t.Fatalf("undo: shared.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(a.worktree, "turn2.txt")); err != nil {
		t.Fatalf("undo: turn2.txt missing: %v", err)
	}
}

func TestCheckpointRevertRefusedWhileWorking(t *testing.T) {
	a, _, _ := cpStart(t)
	a.mu.Lock()
	a.state = uiWorking
	a.mu.Unlock()
	if v := a.checkpointView(); v.CanRevert || v.RevertBlock == "" {
		t.Fatalf("view = %+v", v)
	}
	if _, code, err := a.revertTo(1); err == nil || code != http.StatusConflict {
		t.Fatalf("revert while working: %v %d", err, code)
	}
	a.mu.Lock()
	a.state = uiWaiting
	a.mu.Unlock()
}

func TestCheckpointAPIAndNonGitFolder(t *testing.T) {
	srv, m, _ := uiTestServer(t, false)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := m.start(uiStartRequest{Repo: dir, Title: "nogit", Prompt: "go", inPlaceDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	res := uiDo(t, "GET", srv.URL+"/api/agents/"+a.id+"/checkpoints", nil, uiAuthed)
	var v uiCheckpointList
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v.Available || v.CanRevert || v.Reason == "" || len(v.Checkpoints) != 0 {
		t.Fatalf("non-git view = %+v", v)
	}
	res = uiDo(t, "POST", srv.URL+"/api/agents/"+a.id+"/checkpoints/1/revert", nil, uiAuthed)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("revert status = %d", res.StatusCode)
	}
}

func TestCheckpointAPIRevertAndDiff(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeEditingClaude), "tok-aaa1")
	srv := httptest.NewServer(newUIServer(m, uiTestToken, false))
	t.Cleanup(srv.Close)
	a, _ := m.start(uiStartRequest{Repo: repo, Title: "api", Prompt: "go"})
	uiWaitState(t, a, "waiting")
	res := uiDo(t, "GET", srv.URL+"/api/agents/"+a.id+"/checkpoints", nil, uiAuthed)
	var v uiCheckpointList
	_ = json.NewDecoder(res.Body).Decode(&v)
	if len(v.Checkpoints) < 2 || !v.CanRevert {
		t.Fatalf("view = %+v", v)
	}
	last := v.Checkpoints[len(v.Checkpoints)-1]
	res = uiDo(t, "GET", srv.URL+"/api/agents/"+a.id+"/checkpoints/"+strconv.Itoa(last.N)+"/diff", nil, uiAuthed)
	raw, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(raw), "turn1.txt") {
		t.Fatalf("diff = %q", raw)
	}
	res = uiDo(t, "POST", srv.URL+"/api/agents/"+a.id+"/checkpoints/"+strconv.Itoa(v.Checkpoints[0].N)+"/revert", nil, uiAuthed)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("revert status = %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(a.worktree, "turn1.txt")); !os.IsNotExist(err) {
		t.Fatalf("turn1.txt should be gone: %v", err)
	}
}

func TestOldTaskRecordsLoadWithoutCheckpoints(t *testing.T) {
	var rec uiTaskRecord
	if err := json.Unmarshal([]byte(`{"id":"x","title":"t","state":"done","files":2,"runs":1,"sessions":[]}`), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Checkpoints != nil || rec.ID != "x" {
		t.Fatalf("rec = %+v", rec)
	}
}

// A task can plant git config in its worktree (a file-system monitor, a filter driver) that runs a
// command when Cully snapshots it. Cully's git must run none of it.
func TestCheckpointGitRunsNoCommandPlantedInTheWorktreeConfig(t *testing.T) {
	repo := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("config", "filter.evil.clean", "touch "+marker+"; cat")
	run("config", "filter.evil.process", "touch "+marker)
	run("config", "core.fsmonitor", "touch "+marker+"; echo")
	idx := filepath.Join(t.TempDir(), "idx")
	if _, err := uiGitRun(repo, []string{"GIT_INDEX_FILE=" + idx}, "", "add", "-A"); err != nil {
		t.Fatalf("add -A: %v", err)
	}
	if _, err := uiGitRun(repo, []string{"GIT_INDEX_FILE=" + idx}, "", "write-tree"); err != nil {
		t.Fatalf("write-tree: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a command planted in the worktree's git config ran")
	}
	// and the pinned git directory is the one found the first time
	if gd, ok := uiGitDirs.Load(repo); !ok || !strings.HasSuffix(gd.(string), ".git") {
		t.Errorf("git dir was not pinned: %v", gd)
	}
}

func TestCheckpointGitFailsClosedAndNeverRunsExternalDiffOrTextconv(t *testing.T) {
	if _, err := uiGitRun(t.TempDir(), nil, "", "status"); err == nil {
		t.Error("git must not run in a folder whose git directory cannot be determined")
	}
	repo := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	_ = os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt diff=evil\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "one")
	t1, _ := uiGitRun(repo, nil, "", "rev-parse", "HEAD^{tree}")
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "two")
	t2, _ := uiGitRun(repo, nil, "", "rev-parse", "HEAD^{tree}")
	run("config", "diff.evil.textconv", "touch "+marker+"; cat")
	run("config", "diff.external", "touch "+marker)
	if _, err := uiGitRun(repo, nil, "", "diff", "--no-ext-diff", "--no-textconv", strings.TrimSpace(t1), strings.TrimSpace(t2)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an external diff or text conversion planted in the config ran")
	}
}

func TestCheckpointCommitNeverRunsASigningProgramFromTheConfig(t *testing.T) {
	repo := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("config", "commit.gpgsign", "true")
	run("config", "gpg.program", "touch "+marker)
	tree := run("write-tree")
	if _, err := uiGitRun(repo, []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}, "", "commit-tree", "--no-gpg-sign", tree, "-m", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a signing program planted in the config ran")
	}
}
