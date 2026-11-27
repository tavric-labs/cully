package cully

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// Token use per session, read from the harness's own transcript on this
// machine. A session's tokens are what the model was billed for new work:
// uncached input, cache writes and output. Cache reads are left out because
// they repeat the same context on every turn and would swamp the number.
// Cursor keeps no token counts in its transcripts, so its sessions count as
// zero and the page says so. Results are cached by file modification time.

const setupTokenBudget = 4 * time.Second

type setupTokenCache struct {
	Mod    int64 `json:"mod"`
	Tokens int64 `json:"tokens"`
}

// setupSessionTokens returns a session's tokens and whether the figure is known.
func setupSessionTokens(s uiIndexed) (int64, bool) {
	if s.File == "" || s.Agent == "cursor" {
		return 0, false
	}
	var c setupTokenCache
	if uiDB().get("setuptokens", s.ID, &c) && c.Mod == s.Mod {
		return c.Tokens, true
	}
	n, err := scanTranscriptTokens(s.File, s.Agent)
	if err != nil {
		return 0, false
	}
	uiDB().put("setuptokens", s.ID, setupTokenCache{Mod: s.Mod, Tokens: n})
	return n, true
}

// setupTokensFor totals tokens over sessions within a time budget. partial is
// true when the budget ran out or a transcript could not be read.
func setupTokensFor(sessions []uiIndexed) (per map[string]int64, partial bool) {
	per = map[string]int64{}
	deadline := time.Now().Add(setupTokenBudget)
	for _, s := range sessions {
		if s.Agent == "cursor" {
			continue
		}
		var c setupTokenCache
		cached := uiDB().get("setuptokens", s.ID, &c) && c.Mod == s.Mod
		if !cached && time.Now().After(deadline) {
			partial = true
			continue
		}
		n, ok := setupSessionTokens(s)
		if !ok {
			partial = true
			continue
		}
		per[s.ID] = n
	}
	return per, partial
}

func scanTranscriptTokens(path, agent string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	byMsg := map[string]int64{} // Claude repeats a message while it streams: keep the last figure per message
	var codexLast int64
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if agent == "codex" {
				if v, ok := codexTokenLine(line); ok {
					codexLast = v
				}
			} else if id, v, ok := claudeTokenLine(line); ok {
				byMsg[id] = v
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, err
		}
	}
	if agent == "codex" {
		return codexLast, nil
	}
	var total int64
	for _, v := range byMsg {
		total += v
	}
	return total, nil
}

func claudeTokenLine(line []byte) (string, int64, bool) {
	if !bytes.Contains(line, []byte(`"usage"`)) {
		return "", 0, false
	}
	var e struct {
		RequestID string `json:"requestId"`
		UUID      string `json:"uuid"`
		Message   struct {
			ID    string `json:"id"`
			Usage struct {
				In      int64 `json:"input_tokens"`
				Out     int64 `json:"output_tokens"`
				CacheIn int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil {
		return "", 0, false
	}
	u := e.Message.Usage
	v := u.In + u.Out + u.CacheIn
	if v == 0 {
		return "", 0, false
	}
	id := e.Message.ID + "|" + e.RequestID
	if id == "|" {
		id = e.UUID
	}
	return id, v, true
}

func codexTokenLine(line []byte) (int64, bool) {
	if !bytes.Contains(line, []byte("total_token_usage")) {
		return 0, false
	}
	var e struct {
		Payload struct {
			Type string `json:"type"`
			Info struct {
				Total struct {
					In     int64 `json:"input_tokens"`
					Cached int64 `json:"cached_input_tokens"`
					Out    int64 `json:"output_tokens"`
				} `json:"total_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil || e.Payload.Type != "token_count" {
		return 0, false
	}
	t := e.Payload.Info.Total
	v := t.In - t.Cached + t.Out
	if v < 0 {
		v = t.Out
	}
	return v, true
}
