package cully

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// A permanent record of what scouts found and what the advisor advised, so it
// survives a restart of the page's server. One JSON line per item in the
// user's Cully folder. Disk mode keeps it there; saving it to the Cully MCP
// is a later step, which is what Synced is reserved for.

const (
	uiResearchKeep = 500
	uiResearchText = 6000
)

type uiResearchRec struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"` // scout, recall or advice
	Time   time.Time `json:"time"`
	Source string    `json:"source"` // the agent or page action it came from
	Repo   string    `json:"repo"`
	Query  string    `json:"query,omitempty"`
	Text   string    `json:"text"`
	Level  string    `json:"level,omitempty"`
	Action string    `json:"action,omitempty"`
	Synced bool      `json:"synced"`
}

var uiResearchMu sync.Mutex

func uiResearchPath() string { return filepath.Join(cullyDir(), "research-log.jsonl") }

func uiReadResearch() []uiResearchRec {
	var out []uiResearchRec
	f, err := os.Open(uiResearchPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec uiResearchRec
			if json.Unmarshal(line, &rec) == nil {
				out = append(out, rec)
			}
		}
		if err != nil {
			break
		}
	}
	return out
}

func (m *uiManager) recordResearch(rec uiResearchRec) {
	rec.ID, rec.Time = uiNewID(), time.Now().UTC()
	rec.Text = uiClip(m.cfg.Tokens.redact(rec.Text), uiResearchText)
	rec.Query = uiClip(m.cfg.Tokens.redact(rec.Query), 400)
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	uiResearchMu.Lock()
	defer uiResearchMu.Unlock()
	f, err := os.OpenFile(uiResearchPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
	if all := uiReadResearch(); len(all) > uiResearchKeep+100 {
		all = all[len(all)-uiResearchKeep:]
		tmp := uiResearchPath() + ".tmp"
		if g, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
			w := bufio.NewWriter(g)
			for _, r := range all {
				if l, err := json.Marshal(r); err == nil {
					_, _ = w.Write(append(l, '\n'))
				}
			}
			_ = w.Flush()
			_ = g.Close()
			_ = os.Rename(tmp, uiResearchPath())
		}
	}
}

func (m *uiManager) researchList(kind string, limit int) []uiResearchRec {
	uiResearchMu.Lock()
	all := uiReadResearch()
	uiResearchMu.Unlock()
	out := []uiResearchRec{}
	for _, r := range all {
		if kind != "" && r.Kind != kind || uiHidden(r.Repo, m.cfg.HideSessionsIn) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
