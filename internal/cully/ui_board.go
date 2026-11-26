package cully

import (
	"fmt"
	"sort"
	"strings"
)

// The board is the shared view across agents working in the same repository:
// who is doing what, which files each has edited, and where two agents have
// touched the same file. It is derived from in-memory agent state only.

const uiBoardFiles = 50

type uiOverlap struct {
	File   string   `json:"file"`
	Agents []string `json:"agents"` // agent ids
}

type uiBoard struct {
	Agents   []uiAgentView `json:"agents"`
	Overlaps []uiOverlap   `json:"overlaps"`
}

func (a *uiAgent) sortedFilesLocked() []string {
	out := make([]string, 0, len(a.files))
	for f := range a.files {
		out = append(out, f)
	}
	sort.Strings(out)
	if len(out) > uiBoardFiles {
		out = out[:uiBoardFiles]
	}
	return out
}

func uiLive(state string) bool {
	return state == uiPreparing || state == uiWorking || state == uiWaiting
}

func (m *uiManager) board() uiBoard {
	var live []uiAgentView
	for _, v := range m.list() {
		if uiLive(v.State) {
			live = append(live, v)
		}
	}
	byFile := map[string][]string{}
	for _, v := range live {
		for _, f := range v.Files {
			byFile[f] = append(byFile[f], v.ID)
		}
	}
	board := uiBoard{Agents: live, Overlaps: []uiOverlap{}}
	if board.Agents == nil {
		board.Agents = []uiAgentView{}
	}
	for f, ids := range byFile {
		if len(ids) > 1 {
			board.Overlaps = append(board.Overlaps, uiOverlap{File: f, Agents: ids})
		}
	}
	sort.Slice(board.Overlaps, func(i, j int) bool { return board.Overlaps[i].File < board.Overlaps[j].File })
	return board
}

// peersText describes the other live agents in a's repository so a new agent
// can stay out of their way. Empty when there are none.
func (m *uiManager) peersText(self *uiAgent) string {
	var lines []string
	for _, v := range m.list() {
		if v.ID == self.id || v.Repo != self.repo || !uiLive(v.State) {
			continue
		}
		files := "no files yet"
		if len(v.Files) > 0 {
			files = strings.Join(v.Files, ", ")
		}
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", v.Title, v.Branch, files))
	}
	return strings.Join(lines, "\n")
}
