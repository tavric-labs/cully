package cully

import (
	"encoding/json"
	"math"
)

// SessionContext measures how a coding session explored files, using the
// context metrics from ContextBench (Li et al., 2026, arXiv:2602.05892) at
// file granularity. Each Read is one step t that views one file, C^A_t. The
// paper's gold context is human-annotated; a transcript has none, so the files
// the session later edited stand in for it. That makes Recall, Precision and
// AUC-Cov proxies for "did the session find the files it ended up changing",
// not comparable to the paper's numbers. Redundancy needs no gold context and
// is defined exactly as the paper's Eq. 6-7.
type SessionContext struct {
	Steps     int // Read calls, T
	Viewed    int // distinct files viewed
	Edited    int // distinct files edited, the proxy gold context
	Recall    float64
	Precision float64
	AUCCov    float64 // (1/T) * sum_t Recall(A^(t), G)
	Redundant float64 // (1/(T-1)) * sum_{t>=2} |C_t ∩ seen| / |C_t|
}

// measureSessionContext reads transcript lines in order and never keeps their text.
func measureSessionContext(lines [][]byte) SessionContext {
	var views []string
	edited := map[string]bool{}
	for _, line := range lines {
		var e tEntry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		var items []contentItem
		if json.Unmarshal(e.Message.Content, &items) != nil {
			continue
		}
		for _, it := range items {
			if it.Type != "tool_use" || it.Input.FilePath == "" {
				continue
			}
			switch {
			case it.Name == "Read":
				views = append(views, it.Input.FilePath)
			case editTools[it.Name]:
				edited[it.Input.FilePath] = true
			}
		}
	}
	return scoreSessionContext(views, edited)
}

func scoreSessionContext(views []string, gold map[string]bool) SessionContext {
	out := SessionContext{Steps: len(views), Edited: len(gold)}
	seen := map[string]bool{}
	hits, redundant := 0, 0
	for t, file := range views {
		if t > 0 && seen[file] {
			redundant++
		}
		if !seen[file] {
			seen[file] = true
			if gold[file] {
				hits++
			}
		}
		if len(gold) > 0 {
			out.AUCCov += float64(hits) / float64(len(gold))
		}
	}
	out.Viewed = len(seen)
	if out.Steps > 0 && len(gold) > 0 {
		out.AUCCov /= float64(out.Steps)
	}
	if out.Steps > 1 {
		out.Redundant = float64(redundant) / float64(out.Steps-1)
	}
	if len(gold) > 0 {
		out.Recall = float64(hits) / float64(len(gold))
	}
	if out.Viewed > 0 {
		out.Precision = float64(hits) / float64(out.Viewed)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
