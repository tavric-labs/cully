package cully

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	codexCommandMarkerStart  = "# cully:codex-command:start"
	codexCommandMarkerEnd    = "# cully:codex-command:end"
	codexStatusMarkerStart   = "# cully:codex-statusline:start"
	codexStatusMarkerEnd     = "# cully:codex-statusline:end"
	cursorCommandMarkerStart = "<!-- cully:cursor-command:start -->"
	cursorCommandMarkerEnd   = "<!-- cully:cursor-command:end -->"
)

const codexCullyPrompt = `---
description: Run Cully controls
argument-hint: "[status | suggestions | apply <n>]"
# cully:codex-command:start
# cully:codex-command:end
---

Use the Cully executable for this request.

- With no arguments, run cully status.
- If arguments were provided after /prompts:cully, run cully $ARGUMENTS.
- Treat the output as advisory. Do not run cully apply unless the user explicitly requested it.
- Summarize warnings and deferred items first, then explain the useful controls plainly.
`

const cursorCullyCommand = `<!-- cully:cursor-command:start -->
# Cully

Run the Cully executable for this request.

- With no arguments, run cully status.
- If the user supplied arguments after /cully, pass them to cully (for example, /cully status runs cully status).
- Treat the output as advisory. Do not run cully apply unless the user explicitly requested it.
- Summarize warnings and deferred items first, then explain the useful controls plainly.
<!-- cully:cursor-command:end -->
`

func codexInstructionsPath() string {
	override := filepath.Join(CodexConfigDir(), "AGENTS.override.md")
	if _, err := os.Stat(override); !os.IsNotExist(err) {
		return override
	}
	return filepath.Join(CodexConfigDir(), "AGENTS.md")
}

func codexConfigPath() string { return filepath.Join(CodexConfigDir(), "config.toml") }

func codexPromptPath() string {
	return filepath.Join(CodexConfigDir(), "prompts", "cully.md")
}

func cursorCommandPath() string {
	return filepath.Join(CursorConfigDir(), "commands", "cully.md")
}

func writeCodexPrompt() (bool, error) {
	return writeOwnedFile(codexPromptPath(), codexCommandMarkerStart, codexCullyPrompt)
}

func writeCursorCommand() (bool, error) {
	return writeOwnedFile(cursorCommandPath(), cursorCommandMarkerStart, cursorCullyCommand)
}

// writeOwnedFile creates or refreshes a file written by Cully. An
// existing file without our marker is preserved so a user's command or prompt
// with the same name is never overwritten.
func writeOwnedFile(path, marker, content string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && !strings.Contains(string(existing), marker) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}

func removeOwnedFile(path, marker string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), marker) {
		return nil
	}
	return os.Remove(path)
}

// removeCodexStatusLine cleans up the native footer that older Cully releases
// managed. It leaves user-owned status_line settings untouched.
func removeCodexStatusLine(path string) error {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	text, ok := removeMarkedBlock(string(existing), codexStatusMarkerStart, codexStatusMarkerEnd)
	if !ok {
		return nil
	}
	if trimmed := strings.TrimSpace(text); trimmed == "" || trimmed == "[tui]" {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func removeMarkedBlock(text, startMarker, endMarker string) (string, bool) {
	start := strings.Index(text, startMarker)
	if start < 0 {
		return text, false
	}
	relEnd := strings.Index(text[start+len(startMarker):], endMarker)
	if relEnd < 0 {
		return text, false
	}
	end := start + len(startMarker) + relEnd
	endAfter := end + len(endMarker)
	return text[:start] + text[endAfter:], true
}

func tomlTableBounds(text, target string) (start, end int, ok bool) {
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.HasPrefix(trimmed, "[[") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if ok {
				return start, offset, true
			}
			if name == target {
				start = offset + len(line)
				ok = true
			}
		}
		offset += len(line)
	}
	if ok {
		return start, len(text), true
	}
	return 0, 0, false
}
