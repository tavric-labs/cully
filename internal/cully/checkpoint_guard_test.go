package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func guardEvents(class string, n int) []journalEvent {
	var events []journalEvent
	for i := 0; i < n; i++ {
		events = append(events, journalEvent{Class: class, Time: time.Now().Add(time.Duration(i) * time.Millisecond)})
	}
	return events
}

func TestGuardCheckpointsOnlyOnMeasuredStalls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []journalEvent
		want   string
	}{
		{"ordinary", guardEvents(journalSearch, 19), ""},
		{"exploration", guardEvents(journalSearch, 20), "exploration"},
		{"edits", guardEvents(journalEdit, 12), "unchecked"},
		{"failed check is not verification", append(guardEvents(journalEdit, 12), journalEvent{Class: journalCheck, Failed: true}), "unchecked"},
		{"unknown check is not verification", append(guardEvents(journalEdit, 12), journalEvent{Class: journalCheck, Unknown: true}), "unchecked"},
		{"pass resets", append(guardEvents(journalEdit, 12), journalEvent{Class: journalCheck}), ""},
		{"edit ends exploration", append(guardEvents(journalSearch, 20), journalEvent{Class: journalEdit}), ""},
		{"repeat failures", plantedJournal("FFF"), "loop"},
		{"a recovered loop", plantedJournal("FFFP"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := guardCheckpoint(tc.events)
			if n.reason != tc.want {
				t.Fatalf("got %+v want %s", n, tc.want)
			}
			if n.reason != "" && !strings.Contains(n.text, "Are we still on track") {
				t.Fatal(n.text)
			}
		})
	}
}

func TestGuardCheckpointDoesNotNagAndNewTaskStartsFresh(t *testing.T) {
	cwd := journalTestEnv(t)
	t.Setenv("MODEL_HINT_GUARD", "")
	t.Setenv("CULLY_GUARD", "")
	t.Setenv("CULLY_PANE_SESSION", "checkpoint")
	for _, event := range guardEvents(journalSearch, 20) {
		appendJournal(cwd, "checkpoint", event)
	}
	event := guardEvent(t, "Bash", "native", cwd, map[string]string{"command": "pwd"})
	if out := runGuard("claude", event); !strings.Contains(out, "Are we still on track") {
		t.Fatal(out)
	}
	if out := runGuard("claude", event); out != "" {
		t.Fatalf("duplicate checkpoint: %s", out)
	}
	if err := writeTask("checkpoint", "New task"); err != nil {
		t.Fatal(err)
	}
	// Move the explicit task boundary beyond this planted history.
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(sessionTaskFile("checkpoint"), future, future); err != nil {
		t.Fatal(err)
	}
	if out := runGuard("claude", event); out != "" {
		t.Fatalf("old task history leaked into new task: %s", out)
	}
}

func TestGuardRereadUsesMetadataAndHonorsPartialReads(t *testing.T) {
	cwd := journalTestEnv(t)
	t.Setenv("MODEL_HINT_GUARD", "")
	t.Setenv("CULLY_GUARD", "")
	t.Setenv("CULLY_PANE_SESSION", "reread")
	path := filepath.Join(cwd, "source.go")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 5000)), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now()
	appendJournal(cwd, "reread", journalEvent{Class: journalSearch, Op: opRead, Path: "source.go", Time: stamp})
	read := guardEvent(t, "Read", "native", cwd, map[string]any{"file_path": path})
	if out := runGuard("claude", read); !strings.Contains(out, "already read") {
		t.Fatal(out)
	}
	if out := runGuard("claude", read); out != "" {
		t.Fatalf("repeated reread warning: %s", out)
	}
	if out := runGuard("claude", guardEvent(t, "Read", "native", cwd, map[string]any{"file_path": path, "limit": 100})); out != "" {
		t.Fatal(out)
	}
	future := stamp.Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if advice, _ := guardReread(cwd, path, readSessionJournal(cwd, "reread")); advice != "" {
		t.Fatal("changed file flagged as unchanged")
	}
}
