package cully

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tev(min int, class string, mod func(*journalEvent)) journalEvent {
	e := journalEvent{Time: time.Date(2026, 1, 2, 10, min, 0, 0, time.UTC), Agent: "claude", Class: class}
	if mod != nil {
		mod(&e)
	}
	return e
}

func sampleSession() []journalEvent {
	fail := func(e *journalEvent) { e.Failed, e.Sig = true, "abcd1234" }
	return []journalEvent{
		tev(31, journalStart, nil),
		tev(34, journalEdit, nil), tev(35, journalEdit, nil), tev(36, journalSearch, nil), tev(36, journalEdit, nil),
		tev(37, journalCheck, fail), tev(38, journalCheck, fail),
		tev(42, journalNote, func(e *journalEvent) { e.Note = noteLoop }),
		tev(46, journalEdit, nil),
		tev(47, journalCheck, nil),
		tev(48, journalMemory, func(e *journalEvent) { e.Tool = "cully_log" }),
		tev(52, journalEnd, nil),
	}
}

func TestRenderTimelineGroupsAndSummarizes(t *testing.T) {
	out := renderTimeline(sampleSession(), false)
	for _, want := range []string{
		"Claude Code started", "Edited ×3", "Check failed ×2 (same command)",
		"Cully noticed a loop: the same check failed repeatedly", "Edited ×1", "Check passed",
		"Saved to Cully memory (cully_log)", "Claude Code ended",
		"Summary: 21m, 4 edits, checks 1 passed / 2 failed, 1 loop, 1 memory save",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("unexpected color: %q", out)
	}
	if c := renderTimeline(sampleSession(), true); !strings.Contains(c, "\x1b[") {
		t.Errorf("expected color")
	}
}

func TestGroupingDifferentSigsAndAgents(t *testing.T) {
	events := []journalEvent{
		tev(1, journalCheck, func(e *journalEvent) { e.Failed, e.Sig = true, "a" }),
		tev(2, journalCheck, func(e *journalEvent) { e.Failed, e.Sig = true, "b" }),
		{Time: time.Now(), Agent: "codex", Class: journalStart},
		{Time: time.Now(), Agent: "weird", Class: journalEnd},
	}
	out := renderTimeline(events, false)
	if !strings.Contains(out, "Check failed ×2\n") || strings.Contains(out, "same command") {
		t.Errorf("out:\n%s", out)
	}
	if !strings.Contains(out, "Codex started") || !strings.Contains(out, "weird ended") {
		t.Errorf("names:\n%s", out)
	}
}

func TestEmptyTimeline(t *testing.T) {
	if got := renderTimeline(nil, false); !strings.Contains(got, "No session recorded for this project yet. Run your agent with `cully run AGENT`.") {
		t.Fatal(got)
	}
	var b bytes.Buffer
	if err := RunTimeline(&b, []string{"--cwd", journalTestEnv(t)}); err != nil || !strings.Contains(b.String(), "No session recorded") {
		t.Fatalf("%v %q", err, b.String())
	}
}

func TestSummaryMath(t *testing.T) {
	s := computeStats(sampleSession())
	if s.Edits != 4 || s.ChecksPassed != 1 || s.ChecksFailed != 2 || s.Loops != 1 || s.MemorySaves != 1 || s.Duration != 21*time.Minute || !s.Ended {
		t.Fatalf("%+v", s)
	}
}

func TestTimelineAllTable(t *testing.T) {
	cwd := journalTestEnv(t)
	for _, ev := range sampleSession() {
		appendJournal(cwd, "sess-one", ev)
	}
	appendJournal(cwd, "sess-two", journalEvent{Agent: "codex", Class: journalStart})
	var b bytes.Buffer
	if err := RunTimeline(&b, []string{"--all", "--cwd", cwd}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"SESSION", "sess-one", "sess-two", "Claude Code", "Codex", "ended, last check passed, 1 loop", "open, no checks"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	b.Reset()
	if err := RunTimeline(&b, []string{"--session", "sess-", "--cwd", cwd}); err == nil {
		t.Fatal("ambiguous prefix should fail")
	}
	b.Reset()
	if err := RunTimeline(&b, []string{"--session", "sess-one", "--cwd", cwd}); err != nil || !strings.Contains(b.String(), "Edited ×3") {
		t.Fatalf("%v %q", err, b.String())
	}
}

func TestHandoffSections(t *testing.T) {
	full := buildHandoff(handoffInput{
		Branch: "feat/x", DiffStat: "2 files changed, 5 insertions(+)",
		Files: []string{"a.go", "b.go"}, TotalFiles: 2, Events: append(sampleSession(), tev(53, journalEdit, nil)),
	})
	for _, want := range []string{"## Task", "see Cully memory", "focused task query", "## Completed work", "5 edits",
		"## Current state", "Branch: feat/x", "- a.go", "## Verification", "unverified", "## Open problems", "1 loop",
		"## Remaining work", "1 edit not yet", "## Known dead ends", "## For the next agent", "cully_context", "cully_log"} {
		if !strings.Contains(full, want) {
			t.Errorf("missing %q:\n%s", want, full)
		}
	}
	empty := buildHandoff(handoffInput{})
	for _, absent := range []string{"Completed work", "Current state", "Verification", "Open problems", "Remaining work", "Known dead ends"} {
		if strings.Contains(empty, absent) {
			t.Errorf("unexpected %q:\n%s", absent, empty)
		}
	}
	if !strings.Contains(empty, "## Task") || !strings.Contains(empty, "## For the next agent") {
		t.Errorf("fixed sections missing:\n%s", empty)
	}
}

func TestHandoffFileNameCap(t *testing.T) {
	cwd := journalTestEnv(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	run("init", "-q")
	for i := 0; i < 35; i++ {
		if err := os.WriteFile(filepath.Join(cwd, "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, _, files, total := gitLive(cwd)
	if total != 35 || len(files) != handoffMaxFiles {
		t.Fatalf("total=%d files=%d", total, len(files))
	}
	out := buildHandoff(handoffInput{Files: files, TotalFiles: total})
	if !strings.Contains(out, "... and 5 more") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestOutputsNeverLeakCommandsOrPaths(t *testing.T) {
	cwd := journalTestEnv(t)
	cmdText := "go test ./secret/pkg -run TestPrivateThing"
	for _, ev := range []journalEvent{
		{Agent: "claude", Class: journalStart},
		{Agent: "claude", Class: journalEdit},
		{Agent: "claude", Class: journalCheck, Failed: true, Sig: commandSig(cmdText)},
		{Agent: "claude", Class: journalCheck, Failed: true, Sig: commandSig(cmdText)},
	} {
		appendJournal(cwd, "s1", ev)
	}
	var tl bytes.Buffer
	if err := RunTimeline(&tl, []string{"--cwd", cwd}); err != nil {
		t.Fatal(err)
	}
	_, events, _ := latestJournal(cwd)
	ho := buildHandoff(handoffInput{Events: events})
	for _, out := range []string{tl.String(), ho} {
		for _, bad := range []string{cmdText, "secret", "TestPrivateThing", cwd, commandSig(cmdText)} {
			if strings.Contains(out, bad) {
				t.Errorf("output leaked %q:\n%s", bad, out)
			}
		}
	}
	if !strings.Contains(ho, "1 check failed repeatedly") {
		t.Errorf("dead end count missing:\n%s", ho)
	}
}
