package cully

import (
	"context"
	"encoding/json"
)

type codexMCPScopeKey struct{}
type codexMCPScope struct{ Session, Origin string }

func withCodexMCPScope(ctx context.Context, session, origin string) context.Context {
	return context.WithValue(ctx, codexMCPScopeKey{}, codexMCPScope{session, origin})
}

func cullySemantic(raw json.RawMessage) bool {
	var input struct {
		Mode string `json:"mode"`
	}
	return json.Unmarshal(raw, &input) == nil && input.Mode == "semantic"
}
