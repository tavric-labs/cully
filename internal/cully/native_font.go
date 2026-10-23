package cully

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const nativeFontFamily = "MesloLGS Nerd Font Mono"
const nativeFontPostScript = "MesloLGSNFM-Regular"
const nativeFontFile = "MesloLGSNerdFontMono-Regular.ttf"
const nativeFontURL = "https://raw.githubusercontent.com/ryanoasis/nerd-fonts/v3.4.0/patched-fonts/Meslo/S/Regular/" + nativeFontFile
const nativeFontSHA256 = "3cb52e923ca3981cecca9ea7307186e424e8521cbc5643fd0b5b5b1d7daa53d9"

// Values, never executable commands, form the private rollback record.
type nativeFontSetting struct {
	Target string
	Key    string
	Before string
	After  string
}
type nativeFontBackup struct {
	Version  int
	OS       string
	Settings []nativeFontSetting
}

type nativeCommand func(...string) (string, error)

func nativeCommands(input, output *os.File) nativeCommand {
	return func(args ...string) (string, error) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin, cmd.Stderr = input, output
		data, err := cmd.Output()
		return strings.TrimSpace(string(data)), err
	}
}

// Arguments carry profile names and fonts, keeping them out of AppleScript.
const macFontInspect = `on run argv
 tell application "Terminal"
  set profiles to {default settings, startup settings}
  repeat with w in windows
   repeat with t in tabs of w
    if tty of t is item 1 of argv then set end of profiles to current settings of t
   end repeat
  end repeat
  set resultText to ""
  repeat with p in profiles
   set resultText to resultText & name of p & tab & font name of p & tab & (font size of p as text) & linefeed
  end repeat
  return resultText
 end tell
end run`
const macFontSet = `on run argv
 tell application "Terminal"
  set p to settings set (item 1 of argv)
  if item 2 of argv is "font-size" then
   set font size of p to (item 3 of argv as integer)
   return font size of p as text
  end if
  set font name of p to item 3 of argv
  return font name of p
 end tell
end run`

func nativeFontSettings(profile terminalProfile, run nativeCommand) ([]nativeFontSetting, error) {
	if profile.Connection == "ssh" {
		return nil, fmt.Errorf("configure the native terminal font on the client's local machine, outside SSH")
	}
	if profile.OS == "darwin" && profile.Program == "Apple_Terminal" {
		tty, err := run("tty")
		if err != nil {
			return nil, fmt.Errorf("identify the current Terminal tab: %w", err)
		}
		value, err := run("osascript", "-e", macFontInspect, tty)
		if err != nil {
			return nil, fmt.Errorf("read Terminal profiles (allow Terminal automation if macOS asks): %w", err)
		}
		var settings []nativeFontSetting
		seen := map[string]bool{}
		for _, line := range strings.Split(strings.TrimRight(value, "\r\n"), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 3 || fields[0] == "" {
				return nil, fmt.Errorf("cannot read Terminal profile font safely")
			}
			if seen[fields[0]] {
				continue
			}
			seen[fields[0]] = true
			settings = append(settings, nativeFontSetting{fields[0], "font", fields[1], nativeFontPostScript}, nativeFontSetting{fields[0], "font-size", fields[2], "14"})
		}
		return settings, nil
	}
	// Ghostty embeds Nerd Font symbols, so its default font already renders
	// Cully's icons. Only a configured non-Nerd family needs a manual fix;
	// Cully never rewrites Ghostty's config file.
	if strings.EqualFold(profile.Program, "ghostty") {
		return ghosttyFontSettings()
	}
	// These terminals speak Cully's protocols but own their font settings
	// (native UI or text configs Cully must not rewrite). Point at the manual
	// step instead of failing setup with an unrelated terminal's instructions.
	if guidance, ok := manualFontTerminals[strings.ToLower(profile.Program)]; ok {
		return nil, fmt.Errorf("select a Nerd Font in %s, then rerun cully terminal configure; preferences unchanged", guidance)
	}
	if profile.OS != "linux" {
		return nil, fmt.Errorf("open macOS Terminal to configure its font")
	}
	var schema, key string
	switch strings.ToLower(profile.Program) {
	case "gnome-terminal":
		id := os.Getenv("GNOME_TERMINAL_PROFILE")
		if id == "" {
			value, err := run("gsettings", "get", "org.gnome.Terminal.ProfilesList", "default")
			if err != nil {
				return nil, fmt.Errorf("read GNOME Terminal's default profile: %w", err)
			}
			id = strings.Trim(value, "'")
		}
		if !regexp.MustCompile(`^[a-fA-F0-9-]{36}$`).MatchString(id) {
			return nil, fmt.Errorf("invalid GNOME Terminal profile identifier")
		}
		schema, key = "org.gnome.Terminal.Legacy.Profile:/org/gnome/terminal/legacy/profiles:/:"+id+"/", "font"
	case "ptyxis":
		schema, key = "org.gnome.Ptyxis", "font-name"
	case "gnome-console", "kgx":
		schema, key = "org.gnome.Console", "custom-font"
	default:
		return nil, fmt.Errorf("identify your Linux native terminal with --terminal gnome-terminal, ptyxis or gnome-console; no preferences changed")
	}
	font, err := run("gsettings", "get", schema, key)
	if err != nil {
		return nil, fmt.Errorf("read native terminal font: %w", err)
	}
	useSystem, err := run("gsettings", "get", schema, "use-system-font")
	if err != nil {
		return nil, err
	}
	settings := []nativeFontSetting{{schema, key, font, "'" + nativeFontFamily + " 12'"}, {schema, "use-system-font", useSystem, "false"}}
	return settings, nil
}

// manualFontTerminals configures fonts outside Cully's reach: the terminal
// speaks the needed protocols, but its font lives in its own UI or text
// config, so setup guides instead of rewriting.
var manualFontTerminals = map[string]string{
	"iterm.app":    "iTerm2 (Preferences → Profiles → Text)",
	"kitty":        "Kitty (font_family in kitty.conf)",
	"wezterm":      "WezTerm (font in wezterm.lua)",
	"alacritty":    "Alacritty (font in alacritty.toml)",
	"warpterminal": "Warp (Settings → Appearance → Text)",
	"warp":         "Warp (Settings → Appearance → Text)",
}

// ghosttyFontSettings accepts Ghostty's default font (Nerd symbols are
// embedded) or an explicitly configured Nerd Font, and guides otherwise.
func ghosttyFontSettings() ([]nativeFontSetting, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	paths := []string{filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config")}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "ghostty", "config"))
	}
	paths = append(paths, filepath.Join(home, ".config", "ghostty", "config"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(key) != "font-family" {
				continue
			}
			family := strings.Trim(strings.TrimSpace(value), `"'`)
			if family == "" || strings.Contains(strings.ToLower(family), "nerd") {
				return nil, nil
			}
			return nil, fmt.Errorf("Ghostty uses %s; select a Nerd Font in the Ghostty config, then rerun cully terminal configure; preferences unchanged", family)
		}
		return nil, nil
	}
	return nil, nil
}

func installNativeFont(home, goos string, fetch func() ([]byte, error)) (string, error) {
	dir := filepath.Join(home, ".local", "share", "fonts", "cully")
	if goos == "darwin" {
		dir = filepath.Join(home, "Library", "Fonts")
	}
	path := filepath.Join(dir, nativeFontFile)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != nativeFontSHA256 {
			return "", fmt.Errorf("preserving an existing different font at %s; move it aside before retrying", path)
		}
		return dir, nil
	}
	data, err = fetch()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != nativeFontSHA256 {
		return "", fmt.Errorf("Nerd Font checksum mismatch; font and terminal preferences unchanged")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		if err != nil {
			return "", err
		}
		return "", closeErr
	}
	return dir, nil
}

func fetchNativeFont() ([]byte, error) {
	client := http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(nativeFontURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("font download returned HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 8<<20))
}

func writeFontSetting(goos string, setting nativeFontSetting, value string, run nativeCommand) error {
	if goos == "darwin" {
		got, err := run("osascript", "-e", macFontSet, setting.Target, setting.Key, value)
		if err != nil {
			return err
		}
		if got != value {
			return fmt.Errorf("Terminal did not select %s (reported %s)", value, got)
		}
		return nil
	}
	_, err := run("gsettings", "set", setting.Target, setting.Key, value)
	if err != nil {
		return err
	}
	got, err := run("gsettings", "get", setting.Target, setting.Key)
	if err != nil {
		return err
	}
	if got != value {
		return fmt.Errorf("terminal font setting did not persist")
	}
	return nil
}

func configureNativeFont(input, output *os.File, profile terminalProfile, dryRun bool) error {
	run := nativeCommands(input, output)
	settings, err := nativeFontSettings(profile, run)
	if err != nil {
		return err
	}
	ready := true
	for _, setting := range settings {
		ready = ready && setting.Before == setting.After
	}
	if ready && profile.OS == "linux" {
		family, err := run("fc-match", "--format=%{family}", nativeFontFamily)
		ready = err == nil && family == nativeFontFamily
	}
	if ready {
		fmt.Fprintln(output, "Nerd Font: already selected and available.")
		return nil
	}
	fmt.Fprintf(output, "Required font setup: install %s (Nerd Fonts v3.4.0, checksum verified) and select it in the current/default terminal profiles. Cully selects 14 pt on macOS or 12 pt on Linux; other preferences are preserved.\n", nativeFontFamily)
	if dryRun {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir, err := installNativeFont(home, profile.OS, fetchNativeFont)
	if err != nil {
		return err
	}
	if profile.OS == "linux" {
		if _, err := run("fc-cache", "-f", dir); err != nil {
			return fmt.Errorf("refresh font cache (fontconfig is required): %w", err)
		}
		family, err := run("fc-match", "--format=%{family}", nativeFontFamily)
		if err != nil || family != nativeFontFamily {
			return fmt.Errorf("the installed Nerd Font is not available to fontconfig; terminal preferences unchanged")
		}
	}
	// A download or OS permission dialog can take time. Do not overwrite a
	// profile the user changed while setup was waiting.
	current, err := nativeFontSettings(profile, run)
	if err != nil {
		return err
	}
	if len(current) != len(settings) {
		return fmt.Errorf("terminal profiles changed during setup; retry")
	}
	for i := range settings {
		if current[i] != settings[i] {
			return fmt.Errorf("terminal font settings changed during setup; retry")
		}
	}
	backupDir := filepath.Join(home, ".cully", "terminal")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(backupDir, "native-font-*.json")
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(nativeFontBackup{1, profile.OS, settings})
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Fprintf(output, "Font settings backup: %s\nRestore with: cully terminal restore --backup %s\n", file.Name(), quote(file.Name()))
	for _, setting := range settings {
		if setting.Before == setting.After {
			continue
		}
		if err := writeFontSetting(profile.OS, setting, setting.After, run); err != nil {
			return fmt.Errorf("font configuration incomplete: %w; restore using the printed backup command", err)
		}
	}
	fmt.Fprintln(output, "Nerd Font selected. Run cully terminal test and confirm that the home icon () and other glyphs are visible.")
	return nil
}

func restoreNativeFont(input, output *os.File, profile terminalProfile, path string, dryRun bool) error {
	if profile.Connection == "ssh" {
		return fmt.Errorf("restore font preferences on the terminal client's local machine")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var backup nativeFontBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return err
	}
	if backup.Version != 1 || backup.OS != profile.OS || len(backup.Settings) == 0 {
		return fmt.Errorf("incompatible native font backup")
	}
	for _, s := range backup.Settings {
		if backup.OS == "darwin" {
			if (s.Key != "font" && s.Key != "font-size") || s.Target == "" {
				return fmt.Errorf("invalid Terminal font backup")
			}
		} else {
			valid := s.Target == "org.gnome.Ptyxis" && (s.Key == "font-name" || s.Key == "use-system-font") || s.Target == "org.gnome.Console" && (s.Key == "custom-font" || s.Key == "use-system-font") || regexp.MustCompile(`^org\.gnome\.Terminal\.Legacy\.Profile:/org/gnome/terminal/legacy/profiles:/:[a-fA-F0-9-]{36}/$`).MatchString(s.Target) && (s.Key == "font" || s.Key == "use-system-font")
			if !valid {
				return fmt.Errorf("invalid native terminal font backup")
			}
		}
	}
	fmt.Fprintf(output, "Restore font settings from %s.\n", path)
	if dryRun {
		return nil
	}
	run := nativeCommands(input, output)
	for _, s := range backup.Settings {
		if err := writeFontSetting(backup.OS, s, s.Before, run); err != nil {
			return err
		}
	}
	fmt.Fprintln(output, "Previous terminal fonts restored. The installed font remains available.")
	return nil
}
