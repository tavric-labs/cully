package cully

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Local recall. With the Cully MCP, recall finds prior work by meaning (Mem0).
// Without it, Cully searches the named sessions by keyword in the local
// database (SQLite FTS5) and puts the few best matches in the task's prompt.
// It costs no model call, but it matches words, not meaning, so it is a
// weaker substitute: the page says so.

const uiRecallHits = 5

var uiRecallWord = regexp.MustCompile(`[A-Za-z0-9_]{3,}`)

// refreshRecall rewrites the search index from the named sessions.
func (m *uiManager) refreshRecall() {
	st := uiDB()
	if !st.ok() || !st.fts {
		return
	}
	tx, err := st.db.Begin()
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM recall`); err != nil {
		return
	}
	for _, s := range m.sessionLibrary().Sessions {
		if s.Source != "ai" || uiHidden(s.Cwd, m.cfg.HideSessionsIn) {
			continue
		}
		text := strings.Join([]string{s.Title, s.Task, s.Description, s.Different, strings.Join(s.Criteria, " "), strings.Join(s.Signals, " ")}, " ")
		if _, err := tx.Exec(`INSERT INTO recall(id, text) VALUES(?, ?)`, s.ID, m.cfg.Tokens.redact(text)); err != nil {
			return
		}
	}
	_ = tx.Commit()
}

// localRecall returns up to a few lines about past sessions that match the task.
func (m *uiManager) localRecall(task string) string {
	st := uiDB()
	if !st.ok() || !st.fts {
		return ""
	}
	seen := map[string]bool{}
	var terms []string
	for _, w := range uiRecallWord.FindAllString(strings.ToLower(task), -1) {
		if !seen[w] && !uiTopicStop[w] && len(terms) < 12 {
			seen[w] = true
			terms = append(terms, `"`+w+`"`)
		}
	}
	if len(terms) == 0 {
		return ""
	}
	m.refreshRecall()
	rows, err := st.db.Query(`SELECT id FROM recall WHERE recall MATCH ? ORDER BY rank LIMIT ?`, strings.Join(terms, " OR "), uiRecallHits)
	if err != nil {
		return ""
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	var lines []string
	byID := map[string]uiIndexed{}
	for _, s := range m.sessionLibrary().Sessions {
		byID[s.ID] = s
	}
	for _, id := range ids {
		s, ok := byID[id]
		if !ok {
			continue
		}
		line := fmt.Sprintf("- %s (%s, %s)", uiClipWords(s.Title, 80), s.Name, s.Updated.Format(time.DateOnly))
		if s.Task != "" {
			line += ": " + uiClipWords(s.Task, 140)
		}
		if s.Different != "" {
			line += " Would do differently: " + uiClipWords(s.Different, 140)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// useLocalRecall says whether recall should come from the local database: the
// user pinned local mode, or the Cully MCP is not connected for Claude.
func (m *uiManager) useLocalRecall() bool {
	switch uiMemoryOverride() {
	case "local":
		return true
	case "mcp":
		return false
	}
	return m.mcp == nil || m.mcp.agentState(context.Background(), "claude") != "connected"
}
