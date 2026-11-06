package cully

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallClaudeRemovesOnlyTheRemovedCullyStatusLine(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(ConfigDir(), "settings.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		m, err := loadSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	write(`{"statusLine":{"type":"command","command":"'/home/u/.local/bin/cully' _internal statusline","padding":0}}`)
	if err := installClaude(); err != nil {
		t.Fatal(err)
	}
	if _, present := read()["statusLine"]; present {
		t.Fatal("setup left the status line for the removed command")
	}

	write(`{"statusLine":{"type":"command","command":"~/bin/my-statusline"}}`)
	if err := installClaude(); err != nil {
		t.Fatal(err)
	}
	if _, present := read()["statusLine"]; !present {
		t.Fatal("setup removed the user's own status line")
	}
}
