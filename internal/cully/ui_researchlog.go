package cully

import (
	"encoding/json"
	"sort"
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

func uiReadResearch() []uiResearchRec {
	var out []uiResearchRec
	for _, raw := range uiDB().logAll("research") {
		var rec uiResearchRec
		if json.Unmarshal(raw, &rec) == nil {
			out = append(out, rec)
		}
	}
	return out
}

func (m *uiManager) recordResearch(rec uiResearchRec) {
	rec.ID, rec.Time = uiNewID(), time.Now().UTC()
	rec.Text = uiClip(m.cfg.Tokens.redact(rec.Text), uiResearchText)
	rec.Query = uiClip(m.cfg.Tokens.redact(rec.Query), 400)
	uiDB().logAdd("research", rec, uiResearchKeep)
}

func (m *uiManager) researchList(kind string, limit int) []uiResearchRec {
	all := uiReadResearch()
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
