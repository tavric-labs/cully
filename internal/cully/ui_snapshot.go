package cully

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// A live picture of a session, for any harness: how full its context window is
// and what it has used. Each harness records different things, so every
// snapshot says what it could and could not read:
//
//	Claude: the transcript has the tokens in the context after each call; the
//	        window size is not in it, so it is estimated from the model name.
//	Codex:  the rollout has token_count events with the real window and, when
//	        the plan reports them, usage limits.
//	Cursor: the transcript has requests and tool calls only: no tokens, no
//	        limits.
type uiSnapContext struct {
	Used      int  `json:"used"`
	Window    int  `json:"window"`
	Left      int  `json:"left"`
	Percent   int  `json:"percent"`
	Near      bool `json:"near"`
	Estimated bool `json:"estimated"` // the window size is a guess from the model name
}

type uiSnapshot struct {
	Agent     string         `json:"agent"`
	Available bool           `json:"available"`
	Note      string         `json:"note,omitempty"` // what this harness does not record
	Context   *uiSnapContext `json:"context,omitempty"`
	Model     string         `json:"model,omitempty"`
	Tokens    int            `json:"tokens,omitempty"` // total used by the session, when recorded
	Limits    *uiLimits      `json:"limits,omitempty"`
}

func uiSnapContextOf(used, window int, estimated bool) *uiSnapContext {
	if used <= 0 || window <= 0 {
		return nil
	}
	p := used * 100 / window
	if p > 100 {
		p = 100
	}
	left := window - used
	if left < 0 {
		left = 0
	}
	return &uiSnapContext{Used: used, Window: window, Left: left, Percent: p, Near: p >= uiNearFull, Estimated: estimated}
}

// uiHarnessSnapshot reads the end of a session file of any harness.
func uiHarnessSnapshot(agent, path string) uiSnapshot {
	snap := uiSnapshot{Agent: agent}
	switch agent {
	case "cursor":
		snap.Note = "Cursor does not record token use, context size or limits."
		return snap
	case "codex":
		tail := uiEndLines(path)
		for i := len(tail) - 1; i >= 0; i-- {
			if !strings.Contains(string(tail[i]), `"token_count"`) {
				continue
			}
			var rec struct {
				Payload struct {
					Info struct {
						Total struct {
							Total int `json:"total_tokens"`
						} `json:"total_token_usage"`
						Last struct {
							Input int `json:"input_tokens"`
							Total int `json:"total_tokens"`
						} `json:"last_token_usage"`
						Window int `json:"model_context_window"`
					} `json:"info"`
					Limits *struct {
						Primary   *uiCodexWindow `json:"primary"`
						Secondary *uiCodexWindow `json:"secondary"`
					} `json:"rate_limits"`
				} `json:"payload"`
			}
			if json.Unmarshal(tail[i], &rec) != nil {
				continue
			}
			used := rec.Payload.Info.Last.Input
			if used == 0 {
				used = rec.Payload.Info.Last.Total
			}
			snap.Available = true
			snap.Tokens = rec.Payload.Info.Total.Total
			snap.Context = uiSnapContextOf(used, rec.Payload.Info.Window, false)
			if l := rec.Payload.Limits; l != nil && (l.Primary != nil || l.Secondary != nil) {
				snap.Limits = &uiLimits{Seen: time.Now().Unix()}
				if l.Primary != nil {
					snap.Limits.FiveHour = l.Primary.window()
				}
				if l.Secondary != nil {
					snap.Limits.SevenDay = l.Secondary.window()
				}
			}
			return snap
		}
		snap.Note = "No token count has been recorded in this session yet."
		return snap
	}
	// Claude
	tail := uiEndLines(path)
	for i := len(tail) - 1; i >= 0; i-- {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
				Usage struct {
					In    int `json:"input_tokens"`
					Read  int `json:"cache_read_input_tokens"`
					Write int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(tail[i], &rec) != nil || rec.Type != "assistant" {
			continue
		}
		used := rec.Message.Usage.In + rec.Message.Usage.Read + rec.Message.Usage.Write
		if used == 0 {
			continue
		}
		snap.Available, snap.Model = true, rec.Message.Model
		snap.Context = uiSnapContextOf(used, uiContextWindow(rec.Message.Model, 0), true)
		return snap
	}
	snap.Note = "No usage has been recorded in this session yet."
	return snap
}

type uiCodexWindow struct {
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    int64   `json:"resets_at"`
}

func (w *uiCodexWindow) window() uiWindow {
	return uiWindow{Used: int(w.UsedPercent + 0.5), ResetsAt: w.ResetsAt, Status: "allowed"}
}

// latestSessionFile finds the most recently written session file of a harness
// for a folder, if it was written within the last day. It is how an open
// session is matched to its record.
func latestSessionFile(agent, cwd string) (string, bool) {
	var best uiHistFile
	for _, f := range uiHistoryFiles(agent) {
		if time.Since(f.mod) > 24*time.Hour {
			break // newest first
		}
		var s uiPastSession
		var ok bool
		switch agent {
		case "claude":
			s, ok = uiClaudeSession(f)
		case "cursor":
			s, ok = uiCursorSession(f)
		default:
			s, ok = uiCodexSession(f)
		}
		if ok && s.Cwd == cwd {
			best = f
			break
		}
	}
	return best.path, best.path != ""
}

// snapshotFor describes the session of a harness open in a folder.
func (m *uiManager) snapshotFor(agent, cwd string) *uiSnapshot {
	if uiHidden(cwd, m.cfg.HideSessionsIn) {
		return nil
	}
	// The page asks every few seconds; reading session files that often is wasted work.
	key := agent + "|" + cwd
	m.snapMu.Lock()
	if c, ok := m.snapCache[key]; ok && time.Since(c.at) < 15*time.Second {
		m.snapMu.Unlock()
		s := c.snap
		return &s
	}
	m.snapMu.Unlock()
	s := m.snapshotUncached(agent, cwd)
	m.snapMu.Lock()
	if m.snapCache == nil {
		m.snapCache = map[string]struct {
			at   time.Time
			snap uiSnapshot
		}{}
	}
	m.snapCache[key] = struct {
		at   time.Time
		snap uiSnapshot
	}{time.Now(), *s}
	m.snapMu.Unlock()
	return s
}

func (m *uiManager) snapshotUncached(agent, cwd string) *uiSnapshot {
	if agent == "cursor" {
		s := uiHarnessSnapshot("cursor", "")
		return &s
	}
	path, ok := latestSessionFile(agent, cwd)
	if !ok {
		s := uiSnapshot{Agent: agent, Note: "No record of this session was found yet."}
		return &s
	}
	s := uiHarnessSnapshot(agent, path)
	return &s
}

// accountLimitsAll is the limits last heard for each harness, so the page can
// show them side by side.
type uiAccountLimit struct {
	Account string    `json:"account"`
	Limits  *uiLimits `json:"limits"`
}

type uiLimitsView struct {
	Claude []uiAccountLimit `json:"claude"`
	Codex  *uiSnapshot      `json:"codex"`
	Cursor string           `json:"cursor"`
}

func (m *uiManager) limitsAll() uiLimitsView {
	v := uiLimitsView{Claude: []uiAccountLimit{}, Cursor: "Cursor does not record usage limits."}
	m.limitMu.Lock()
	tokens := make([]int, 0, len(m.accountLimits))
	for t := range m.accountLimits {
		tokens = append(tokens, t)
	}
	sort.Ints(tokens)
	for _, t := range tokens {
		l := m.accountLimits[t]
		v.Claude = append(v.Claude, uiAccountLimit{Account: "token " + uiItoa(t), Limits: &l})
	}
	m.limitMu.Unlock()
	// Codex: the newest rollout that has any limits in it.
	for i, f := range uiHistoryFiles("codex") {
		if i >= 5 {
			break
		}
		if s := uiHarnessSnapshot("codex", f.path); s.Limits != nil {
			v.Codex = &s
			break
		}
	}
	return v
}

// uiEndLines is the end of a session file: the last chunk of a large one, or
// all of a small one, which uiReadEnds returns as its head.
func uiEndLines(path string) [][]byte {
	head, tail := uiReadEnds(path)
	if len(tail) > 0 {
		return tail
	}
	return head
}
