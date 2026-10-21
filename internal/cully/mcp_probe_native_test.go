package cully

import (
	"context"
	"os"
	"testing"
	"time"
)

// Explicit opt-in: discover tools through the installed CLI, without a model
// turn, memory call, configuration mutation or credential output.
func TestNativeCodexConnectionProbe(t *testing.T) {
	if os.Getenv("CULLY_NATIVE_MCP_PROBE") != "1" {
		t.Skip("native CLI check not requested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	h, a, found := probeCodexMCP(ctx)
	if !found || h != "healthy" || a != "authenticated" {
		t.Fatalf("connection discovery: health=%s auth=%s found=%t", h, a, found)
	}
}
