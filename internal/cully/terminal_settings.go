package cully

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"golang.org/x/term"
)

// Host preferences have no cross-terminal API. Keep adapters limited to known
// settings required by Cully; fonts, colors, padding and keymaps stay untouched.
func warpSettingsPath(home, goos, configHome, localAppData, channel string) (string, error) {
	if channel != "stable" && channel != "preview" {
		return "", fmt.Errorf("terminal channel must be stable or preview")
	}
	switch goos {
	case "darwin":
		dir := ".warp"
		if channel == "preview" {
			dir += "-preview"
		}
		return filepath.Join(home, dir, "settings.toml"), nil
	case "linux":
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		dir := "warp-terminal"
		if channel == "preview" {
			dir += "-preview"
		}
		return filepath.Join(configHome, dir, "settings.toml"), nil
	case "windows":
		if localAppData == "" {
			return "", fmt.Errorf("LOCALAPPDATA is required for Warp settings")
		}
		dir := "Warp"
		if channel == "preview" {
			dir += "Preview"
		}
		return filepath.Join(localAppData, "warp", dir, "config", "settings.toml"), nil
	default:
		return "", fmt.Errorf("no Warp settings adapter for %s", goos)
	}
}

type terminalEdit struct {
	start, end int
	text       string
}

// Patch only boolean tokens and missing keys. Parsing first and validating the
// result protects unrelated preferences, comments, quoted keys and strings.
func enableWarpReporting(data []byte) ([]byte, error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid Warp settings; no changes made: %w", err)
	}
	if value, ok := doc["terminal"]; ok {
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("Warp terminal settings must be a table")
		}
	}
	var parser unstable.Parser
	parser.Reset(data)
	var table []string
	insertAt := -1
	var edits []terminalEdit
	found := map[string]bool{}
	keys := func(node *unstable.Node) []string {
		var parts []string
		iter := node.Key()
		for iter.Next() {
			parts = append(parts, string(iter.Node().Data))
		}
		return parts
	}
	var visit func(*unstable.Node, []string) error
	visit = func(node *unstable.Node, prefix []string) error {
		path := append(append([]string(nil), prefix...), keys(node)...)
		value := node.Value()
		if len(path) == 2 && path[0] == "terminal" && (path[1] == "mouse_reporting_enabled" || path[1] == "scroll_reporting_enabled") {
			if value.Kind != unstable.Bool {
				return fmt.Errorf("terminal.%s must be a boolean; no changes made", path[1])
			}
			found[path[1]] = true
			if string(value.Data) != "true" {
				r := parser.Range(value.Data)
				edits = append(edits, terminalEdit{int(r.Offset), int(r.Offset + r.Length), "true"})
			}
		}
		if value.Kind == unstable.InlineTable {
			iter := value.Children()
			for iter.Next() {
				if err := visit(iter.Node(), path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = keys(node)
			if node.Kind == unstable.Table && len(table) == 1 && table[0] == "terminal" {
				offset := int(node.Child().Raw.Offset)
				if next := bytes.IndexByte(data[offset:], '\n'); next >= 0 {
					insertAt = offset + next + 1
				} else {
					insertAt = len(data)
				}
			}
		case unstable.KeyValue:
			if err := visit(node, table); err != nil {
				return nil, err
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	// Root dotted keys avoid moving or reserializing existing tables. TOML
	// validation rejects incompatible inline-table layouts without writing.
	var prefix strings.Builder
	for _, key := range []string{"mouse_reporting_enabled", "scroll_reporting_enabled"} {
		if !found[key] {
			if insertAt >= 0 {
				fmt.Fprintf(&prefix, "%s = true\n", key)
			} else {
				fmt.Fprintf(&prefix, "terminal.%s = true\n", key)
			}
		}
	}
	if prefix.Len() > 0 {
		if insertAt < 0 {
			insertAt = 0
		}
		text := prefix.String()
		if insertAt > 0 && data[insertAt-1] != '\n' {
			text = "\n" + text
		}
		edits = append(edits, terminalEdit{insertAt, insertAt, text})
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	result := bytes.Clone(data)
	for _, edit := range edits {
		result = append(append(append([]byte(nil), result[:edit.start]...), []byte(edit.text)...), result[edit.end:]...)
	}
	if err := toml.Unmarshal(result, &doc); err != nil {
		return nil, fmt.Errorf("cannot preserve this Warp settings layout automatically; enable Mouse Reporting and Scroll Reporting in Warp Settings: %w", err)
	}
	return result, nil
}

// Back up before replacing, preserve permissions and symlinks, and refuse to
// overwrite a file changed since inspection. Backups are private local files.
func writeTerminalSettings(path string, before, after []byte) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if !bytes.Equal(current, before) {
		return "", fmt.Errorf("terminal settings changed during configuration; retry")
	}
	mode := os.FileMode(0600)
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("terminal settings are not a regular file")
		}
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	backup := ""
	if current != nil {
		file, err := os.CreateTemp(filepath.Dir(path), "settings.toml.cully-backup-*")
		if err != nil {
			return "", err
		}
		backup = file.Name()
		_, err = file.Write(before)
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".cully-terminal-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(after)
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return backup, nil
}

func configureTerminalSettings(w io.Writer, profile terminalProfile, path string, apply bool) error {
	fmt.Fprintf(w, "Terminal: %s\n", profile.label())
	if profile.Connection == "ssh" {
		fmt.Fprintln(w, "Host preferences: not changed over SSH; configure the terminal on its local machine.")
		return nil
	}
	if !profile.isWarp() {
		fmt.Fprintln(w, "Mouse and wheel: standard SGR/legacy protocols requested at launch. No host-preference adapter required or available for this terminal.")
		return nil
	}
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	after, err := enableWarpReporting(before)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		fmt.Fprintln(w, "Mouse Reporting: enabled\nScroll Reporting: enabled")
		return nil
	}
	if !apply {
		fmt.Fprintln(w, "Mouse/scroll reporting: configuration needed. Run cully terminal configure (or cully setup --terminal-only).")
		return nil
	}
	backup, err := writeTerminalSettings(path, before, after)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "Enabled Warp Mouse Reporting and Scroll Reporting. Appearance and keymaps preserved.")
	if backup != "" {
		fmt.Fprintf(w, "Backup: %s\nRestore with: cully terminal restore --backup %q\n", backup, backup)
	}
	return nil
}

func RunTerminal(input, output *os.File, args []string) error {
	command := "check"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command != "check" && command != "configure" && command != "test" && command != "restore" {
		return fmt.Errorf("usage: cully terminal check|configure|test|restore [--terminal PROGRAM] [--channel stable|preview] [--dry-run] [--backup PATH]")
	}
	fs := flag.NewFlagSet("terminal "+command, flag.ContinueOnError)
	fs.SetOutput(output)
	program := fs.String("terminal", "", "terminal program; default: TERM_PROGRAM")
	channel := fs.String("channel", "stable", "Warp release channel: stable or preview")
	dry := fs.Bool("dry-run", false, "inspect required changes without writing")
	backup := fs.String("backup", "", "backup created by Cully, for restore")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("terminal: unexpected arguments")
	}
	profile := detectTerminalProfile()
	if *program != "" {
		profile.Program = terminalField(*program)
	}
	if command == "test" {
		return RunTerminalExercise(input, output)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path, err := warpSettingsPath(home, runtime.GOOS, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("LOCALAPPDATA"), *channel)
	if err != nil && profile.isWarp() {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if command == "restore" {
		if !profile.isWarp() || profile.Connection == "ssh" || *backup == "" {
			return fmt.Errorf("restore requires a local Warp terminal and --backup PATH")
		}
		if filepath.Dir(*backup) != filepath.Dir(path) || !strings.HasPrefix(filepath.Base(*backup), "settings.toml.cully-backup-") {
			return fmt.Errorf("backup must be a Cully settings backup beside %s", path)
		}
		data, err := os.ReadFile(*backup)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := toml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("invalid backup: %w", err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if *dry {
			fmt.Fprintf(output, "Would restore %s from %s\n", path, *backup)
			return nil
		}
		saved, err := writeTerminalSettings(path, before, data)
		if err == nil {
			fmt.Fprintf(output, "Restored terminal preferences. Previous settings backed up to %s\n", saved)
		}
		return err
	}
	return configureTerminalSettings(output, profile, path, command == "configure" && !*dry)
}

// Setup owns persistent configuration. Ordinary agent launches never rewrite
// terminal preferences; a live exercise is explicit and skipped when headless.
func SetupTerminal(input, output *os.File, skipLive bool) error {
	if err := RunTerminal(input, output, []string{"configure"}); err != nil {
		return err
	}
	if skipLive || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		fmt.Fprintln(output, "Live wheel/divider/advisor checks: not tested. Run cully terminal test in your terminal.")
		return nil
	}
	return RunTerminalExercise(input, output)
}
