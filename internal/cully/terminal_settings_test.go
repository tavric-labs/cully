package cully

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestEnableWarpReportingPreservesPreferences(t *testing.T) {
	for _, data := range []string{
		"# My theme\n[appearance.text]\nfont_size = 17.5\nfont_name = 'custom'\n[terminal]\nmouse_reporting_enabled = false # keep comment\nscroll_reporting_enabled = false\n",
		"[terminal]\nmouse_reporting_enabled = false\n[appearance]\ntheme = 'dark'\n",
		"[terminal]",
		"[\"terminal\"]\n\"mouse_reporting_enabled\" = false\n\"scroll_reporting_enabled\" = false\n",
		"terminal.mouse_reporting_enabled = false\nterminal.scroll_reporting_enabled = false\n",
		"terminal = {mouse_reporting_enabled=false, scroll_reporting_enabled=false}\n",
		"[appearance]\ntext = '''\n[terminal]\nmouse_reporting_enabled = false\n'''\n",
		"",
	} {
		t.Run(data, func(t *testing.T) {
			result, err := enableWarpReporting([]byte(data))
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]any
			if err := toml.Unmarshal([]byte(data), &before); err != nil {
				t.Fatal(err)
			}
			if err := toml.Unmarshal(result, &after); err != nil {
				t.Fatal(err)
			}
			terminal := after["terminal"].(map[string]any)
			if terminal["mouse_reporting_enabled"] != true || terminal["scroll_reporting_enabled"] != true {
				t.Fatal(string(result))
			}
			if strings.Contains(data, "# keep comment") && !strings.Contains(string(result), "# keep comment") {
				t.Fatal("comment lost")
			}
			if data != "" && before["appearance"] != nil {
				a, _ := toml.Marshal(before["appearance"])
				b, _ := toml.Marshal(after["appearance"])
				if !bytes.Equal(a, b) {
					t.Fatal("appearance changed")
				}
			}
			again, err := enableWarpReporting(result)
			if err != nil || !bytes.Equal(again, result) {
				t.Fatalf("not idempotent: %s %v", again, err)
			}
		})
	}
	for _, bad := range []string{"[terminal\n", "terminal = false", "[terminal]\nmouse_reporting_enabled = 'false'", "terminal={mouse_reporting_enabled=false}"} {
		if _, err := enableWarpReporting([]byte(bad)); err == nil {
			t.Fatalf("accepted unsafe layout %q", bad)
		}
	}
}

func TestTerminalConfigurationBackupDryRunAndSSH(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	before := []byte("[terminal]\nmouse_reporting_enabled=true\nscroll_reporting_enabled=false\n[appearance]\ntheme='mine'\n")
	if err := os.WriteFile(path, before, 0640); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	profile := terminalProfile{Program: "WarpTerminal", Connection: "local"}
	if err := configureTerminalSettings(&output, profile, path, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, before) {
		t.Fatal("dry run modified settings")
	}
	profile.Connection = "ssh"
	if err := configureTerminalSettings(&output, profile, path, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, before) {
		t.Fatal("SSH modified host preferences")
	}
	profile.Connection = "local"
	if err := configureTerminalSettings(&output, profile, path, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if bytes.Equal(data, before) {
		t.Fatal("reporting not enabled")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal("file permissions changed")
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "settings.toml.cully-backup-*"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	saved, _ := os.ReadFile(backups[0])
	if !bytes.Equal(saved, before) {
		t.Fatal("backup differs from original")
	}
	if err := configureTerminalSettings(&output, profile, path, true); err != nil {
		t.Fatal(err)
	}
	backups, _ = filepath.Glob(filepath.Join(filepath.Dir(path), "settings.toml.cully-backup-*"))
	if len(backups) != 1 {
		t.Fatal("repeat setup created duplicate backup")
	}
	if _, err := writeTerminalSettings(path, before, before); err == nil {
		t.Fatal("overwrote concurrently changed settings")
	}
}

func TestWarpSettingsLocations(t *testing.T) {
	for _, test := range []struct{ goos, channel, want string }{
		{"darwin", "stable", "/home/.warp/settings.toml"}, {"darwin", "preview", "/home/.warp-preview/settings.toml"},
		{"linux", "stable", "/config/warp-terminal/settings.toml"}, {"linux", "preview", "/config/warp-terminal-preview/settings.toml"},
		{"windows", "stable", "/local/warp/Warp/config/settings.toml"}, {"windows", "preview", "/local/warp/WarpPreview/config/settings.toml"},
	} {
		path, err := warpSettingsPath("/home", test.goos, "/config", "/local", test.channel)
		if err != nil || filepath.ToSlash(path) != test.want {
			t.Fatalf("%+v: %s %v", test, path, err)
		}
	}
	if _, err := warpSettingsPath("/home", "darwin", "", "", "nightly"); err == nil {
		t.Fatal("accepted unknown channel")
	}
}
