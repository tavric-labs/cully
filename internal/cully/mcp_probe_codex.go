package cully

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
)

// Codex's mcp list reports configuration. The app server can discover tools
// using Codex's own identity without starting a model turn.
func probeCodexMCP(ctx context.Context) (string, string, bool) {
	cmd := mcpProbeCommand(ctx, "codex", "app-server")
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", "", false
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", false
	}
	if cmd.Start() != nil {
		return "", "", false
	}
	defer func() { _ = cmd.Cancel(); _ = cmd.Wait() }()
	return codexMCPStatusExchange(in, out)
}

func codexMCPStatusExchange(in io.Writer, out io.Reader) (string, string, bool) {
	enc := json.NewEncoder(in)
	send := func(v any) bool { return enc.Encode(v) == nil }
	list := func(cursor string) bool {
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		return send(map[string]any{"id": 2, "method": "mcpServerStatus/list", "params": params})
	}
	if !send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "cully", "version": "1"}}}) {
		return "", "", false
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.ID == 0 {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return "", "", false
		}
		switch msg.ID {
		case 1:
			if !send(map[string]any{"method": "initialized"}) || !list("") {
				return "", "", false
			}
		case 2:
			if health, auth, found := parseCodexMCPStatus(msg.Result); found {
				return health, auth, true
			}
			var page struct {
				NextCursor string `json:"nextCursor"`
			}
			if json.Unmarshal(msg.Result, &page) != nil || page.NextCursor == "" || !list(page.NextCursor) {
				return "", "", false
			}
		}
	}
	return "", "", false
}
