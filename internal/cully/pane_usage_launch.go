package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const codexUsageConfig = `tui.status_line=["model-with-reasoning","context-remaining","total-input-tokens","total-output-tokens","five-hour-limit","weekly-limit","fast-mode"]`

// Request native metrics only for the wrapped child. Explicit launch settings
// and saved custom/disabled Codex footers remain authoritative. Claude's user
// callback is chained; project/CLI status lines are left to Claude's resolver.
func paneUsageArgs(agent string, args []string) []string {
	switch agent {
	case "codex":
		for i, arg := range args {
			if arg == "--" {
				break
			}
			value := ""
			switch {
			case (arg == "-c" || arg == "--config") && i+1 < len(args):
				value = args[i+1]
			case strings.HasPrefix(arg, "--config="):
				value = strings.TrimPrefix(arg, "--config=")
			case strings.HasPrefix(arg, "-c") && len(arg) > 2:
				value = strings.TrimPrefix(arg, "-c")
			}
			key, _, _ := strings.Cut(value, "=")
			if strings.TrimSpace(key) == "tui.status_line" {
				return args
			}
		}
		var config map[string]any
		if b, err := os.ReadFile(codexConfigPath()); err == nil && toml.Unmarshal(b, &config) == nil {
			if tui, ok := config["tui"].(map[string]any); ok {
				if _, custom := tui["status_line"]; custom {
					return args
				}
			}
		}
		// Prefix so any native CLI override retains its normal precedence.
		return append([]string{"-c", codexUsageConfig}, args...)
	case "claude":
		for _, arg := range args {
			if arg == "--settings" || strings.HasPrefix(arg, "--settings=") || arg == "--setting-sources" || strings.HasPrefix(arg, "--setting-sources=") {
				return args
			}
		}
		for _, name := range []string{"settings.json", "settings.local.json"} {
			if settings, err := loadSettings(filepath.Join(currentDir(), ".claude", name)); err != nil {
				return args
			} else if _, configured := settings["statusLine"]; configured {
				return args
			}
		}
		settings, err := loadSettings(filepath.Join(ConfigDir(), "settings.json"))
		if err != nil {
			return args
		}
		exe, err := executablePath()
		if err != nil {
			return args
		}
		status := map[string]any{"type": "command", "command": quote(exe) + " _internal usage"}
		if original, present := settings["statusLine"]; present {
			user, ok := original.(map[string]any)
			if !ok || user["type"] != "command" {
				return args
			}
			command, ok := user["command"].(string)
			if !ok || command == "" {
				return args
			}
			for k, v := range user {
				status[k] = v
			}
			status["command"] = quote(exe) + " _internal usage --forward " + quote(command)
		}
		overlay, _ := json.Marshal(map[string]any{"statusLine": status})
		return append(append([]string{}, args...), "--settings", string(overlay))
	}
	return args
}
