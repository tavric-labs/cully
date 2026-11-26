package cully

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

type uiFakeAdvisor struct {
	mu    sync.Mutex
	calls []bool // research flag per call
	fail  bool
}

func (f *uiFakeAdvisor) run(agent, cwd string, research bool, prompt string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, research)
	f.mu.Unlock()
	if f.fail {
		return "", errors.New("advisor unavailable")
	}
	if research {
		return "scout-notes", nil
	}
	return "recall-notes", nil
}

func firstStdinLine(t *testing.T, out string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "stdin.log"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(string(b), "\n", 2)[0]
}

func stageStatus(a *uiAgent, name string) string {
	for _, s := range a.snapshot().Stages {
		if s.Name == name {
			return s.Status
		}
	}
	return ""
}

func TestComposePromptSections(t *testing.T) {
	got := composeUIPrompt("fix the bug", "PACK-TEXT", "", "SCOUT-TEXT", "")
	if !strings.HasPrefix(got, "fix the bug") {
		t.Fatalf("task is not first: %q", got)
	}
	for _, want := range []string{"PACK-TEXT", "SCOUT-TEXT", "do not push"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "Recalled") {
		t.Fatalf("empty recall section rendered: %q", got)
	}
}

func TestStartRunsStagesAndFeedsPrompt(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	adv := &uiFakeAdvisor{}
	m.cfg.Advisor = adv.run
	a, err := m.start(uiStartRequest{Repo: repo, Prompt: "do x", Context: true, Recall: true, Scout: true})
	if err != nil {
		t.Fatal(err)
	}
	uiWaitState(t, a, "waiting")
	sort.Slice(adv.calls, func(i, j int) bool { return !adv.calls[i] && adv.calls[j] })
	if len(adv.calls) != 2 || adv.calls[0] || !adv.calls[1] {
		t.Fatalf("advisor calls (research flags) = %v, want [false true]", adv.calls)
	}
	for _, s := range []string{"context", "recall", "scout"} {
		if stageStatus(a, s) != "ok" {
			t.Fatalf("stage %s = %q", s, stageStatus(a, s))
		}
	}
	first := firstStdinLine(t, out)
	for _, want := range []string{"recall-notes", "scout-notes", "do x"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first prompt lacks %q: %s", want, first)
		}
	}
}

func TestAdvisorFailureDoesNotBlockSpawn(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	m.cfg.Advisor = (&uiFakeAdvisor{fail: true}).run
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "do x", Recall: true, Scout: true})
	uiWaitState(t, a, "waiting")
	if stageStatus(a, "recall") != "failed" || stageStatus(a, "scout") != "failed" {
		t.Fatalf("stages = %+v", a.snapshot().Stages)
	}
	if !strings.Contains(firstStdinLine(t, out), "do x") {
		t.Fatal("task missing from prompt after advisor failure")
	}
}

func TestStagesSkippedWhenDisabled(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	adv := &uiFakeAdvisor{}
	m.cfg.Advisor = adv.run
	a, _ := m.start(uiStartRequest{Repo: repo, Prompt: "do x"})
	uiWaitState(t, a, "waiting")
	if len(adv.calls) != 0 {
		t.Fatalf("advisor called %d times with all stages off", len(adv.calls))
	}
	for _, s := range []string{"context", "recall", "scout"} {
		if stageStatus(a, s) != "skipped" {
			t.Fatalf("stage %s = %q", s, stageStatus(a, s))
		}
	}
	if strings.Contains(firstStdinLine(t, out), "Context from Cully") {
		t.Fatal("context section present with context off")
	}
}

func TestComposePromptIncludesPeers(t *testing.T) {
	got := composeUIPrompt("task", "", "", "", "- Fix login (cully/fix-login-ab12): a.txt")
	if !strings.Contains(got, "Other agents working in this repository") || !strings.Contains(got, "cully/fix-login-ab12") {
		t.Fatalf("peers section missing: %q", got)
	}
}

func TestNewAgentIsToldAboutPeers(t *testing.T) {
	m, repo, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	a1, _ := m.start(uiStartRequest{Repo: repo, Title: "Fix login", Prompt: "one"})
	uiWaitState(t, a1, "waiting")
	a2, _ := m.start(uiStartRequest{Repo: repo, Title: "Add docs", Prompt: "two"})
	uiWaitState(t, a2, "waiting")
	b, _ := os.ReadFile(filepath.Join(out, "stdin.log"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		t.Fatalf("stdin lines = %d", len(lines))
	}
	second := lines[1]
	for _, want := range []string{"Other agents working in this repository", "Fix login", "a.txt"} {
		if !strings.Contains(second, want) {
			t.Fatalf("second agent's prompt lacks %q: %s", want, second)
		}
	}
	if strings.Contains(lines[0], "Other agents working") {
		t.Fatal("first agent was told about peers that did not exist")
	}
}

func TestBoardReportsOverlappingFiles(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1", "tok-bbb2")
	a1, _ := m.start(uiStartRequest{Repo: repo, Prompt: "one"})
	uiWaitState(t, a1, "waiting")
	a2, _ := m.start(uiStartRequest{Repo: repo, Prompt: "two"})
	uiWaitState(t, a2, "waiting")
	board := m.board()
	if len(board.Agents) != 2 {
		t.Fatalf("board agents = %d", len(board.Agents))
	}
	if len(board.Overlaps) != 1 || board.Overlaps[0].File != "a.txt" || len(board.Overlaps[0].Agents) != 2 {
		t.Fatalf("overlaps = %+v", board.Overlaps)
	}
	if len(board.Agents[0].Files) != 1 || board.Agents[0].Files[0] != "a.txt" {
		t.Fatalf("files = %+v", board.Agents[0].Files)
	}
}
