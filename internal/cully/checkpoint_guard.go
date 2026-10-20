package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	guardExplorationCalls = 20
	guardUnverifiedEdits  = 12
)

type checkpointNudge struct{ reason, epoch, text string }

func guardTaskEvents(session string, events []journalEvent) []journalEvent {
	info, err := os.Stat(sessionTaskFile(session))
	if err != nil {
		return events
	}
	var current []journalEvent
	for _, event := range events {
		if !event.Time.Before(info.ModTime()) {
			current = append(current, event)
		}
	}
	return current
}

// These are observations, not inferred task progress or scope. No model call
// is needed to notice them. A successful check starts a fresh checkpoint epoch.
func guardCheckpoint(events []journalEvent) checkpointNudge {
	edits, searches := 0, 0
	epoch := "start"
	for _, e := range events {
		switch e.Class {
		case journalEdit:
			searches = 0
			if !e.Failed {
				edits++
			}
		case journalCheck:
			searches = 0
			if !e.Failed && !e.Unknown {
				edits = 0
				epoch = e.Time.UTC().Format(time.RFC3339Nano)
			}
		case journalSearch:
			searches++
		}
	}
	n := checkpointNudge{epoch: epoch}
	loops := detectLoops(events)
	switch {
	case loops.Active():
		n.reason, n.text = "loop", fmt.Sprintf("a failure loop of %d attempts was observed", loops.Loops[0].Attempts)
		if loops.Loops[0].Kind == loopRepeatFailure {
			n.text = fmt.Sprintf("the same command failed %d times", loops.Loops[0].Attempts)
		}
	case edits >= guardUnverifiedEdits:
		n.reason, n.text = "unchecked", fmt.Sprintf("%d edits were observed without a successful check", edits)
	case searches >= guardExplorationCalls:
		n.reason, n.text = "exploration", fmt.Sprintf("%d read/search calls were observed without an edit or check", searches)
	default:
		return checkpointNudge{}
	}
	n.text = "Cully checkpoint: " + n.text + ". Before more work, summarize what you learned, the current blocker and the proposed next step, then ask the user: Are we still on track, or should I change direction? Wait for their answer before continuing this stalled approach unless they already authorized proceeding through this checkpoint. Do not repeat a checkpoint the user already answered for this same evidence."
	return n
}

// Use only a recorded read and file metadata to detect a redundant full read.
// Content and shell arguments are never inspected or persisted by this rule.
func guardReread(cwd, path string, events []journalEvent) (advice, key string) {
	rel := relProjectPath(cwd, path)
	if rel == "" {
		return "", ""
	}
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(cwd, path)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 4096 {
		return "", ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Path != rel || e.Failed {
			continue
		}
		if e.Op != opRead {
			return "", ""
		}
		if e.Time.Before(info.ModTime()) {
			return "", ""
		}
		return "Cully: this file was already read; its current modification time is no newer than that read. Reuse that context, or read only the needed range; explain why another full read is needed if the earlier result was incomplete.", "reread:" + rel + ":" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	}
	return "", ""
}
