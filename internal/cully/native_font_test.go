package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFontChecksumFailureCannotInstallFont(t *testing.T) {
	home := t.TempDir()
	if _, err := installNativeFont(home, "darwin", func() ([]byte, error) { return []byte("corrupt download"), nil }); err == nil {
		t.Fatal("accepted invalid font")
	}
	if _, err := os.Stat(filepath.Join(home, "Library")); !os.IsNotExist(err) {
		t.Fatal("checksum failure wrote to user fonts")
	}
}

func TestNativeFontAppliesPresetSizeAndCapturesRollback(t *testing.T) {
	for _, program := range []string{"gnome-terminal", "ptyxis", "gnome-console"} {
		t.Run(program, func(t *testing.T) {
			run := func(args ...string) (string, error) {
				if args[0] != "gsettings" || args[1] != "get" {
					t.Fatalf("inspection mutated settings: %v", args)
				}
				switch args[len(args)-1] {
				case "default":
					return "'b1dcc9dd-5262-4d8d-a863-c897e6d979b9'", nil
				case "use-system-font":
					return "true", nil
				case "monospace-font-name":
					return "'Monospace 15'", nil
				default:
					return "'Custom Font 10'", nil
				}
			}
			settings, err := nativeFontSettings(terminalProfile{OS: "linux", Program: program}, run)
			if err != nil {
				t.Fatal(err)
			}
			if len(settings) != 2 || settings[0].Before != "'Custom Font 10'" || settings[0].After != "'MesloLGS Nerd Font Mono 12'" || settings[1].After != "false" || settings[1].Before != "true" {
				t.Fatalf("%+v", settings)
			}
		})
	}
}

func TestMacFontProfilesAndArgumentsArePreserved(t *testing.T) {
	name := "My \"profile\""
	run := func(args ...string) (string, error) {
		if args[0] == "tty" {
			return "/dev/ttys123", nil
		}
		if args[2] != macFontInspect || args[3] != "/dev/ttys123" {
			t.Fatalf("unexpected inspection: %v", args)
		}
		return name + "\tMenlo-Regular\t11\n" + name + "\tMenlo-Regular\t11\n", nil
	}
	settings, err := nativeFontSettings(terminalProfile{OS: "darwin", Program: "Apple_Terminal"}, run)
	if err != nil || len(settings) != 2 || settings[0].Target != name {
		t.Fatalf("%+v: %v", settings, err)
	}
	if settings[1].Key != "font-size" || settings[1].Before != "11" || settings[1].After != "14" {
		t.Fatalf("font size must apply the preset and retain rollback: %+v", settings[1])
	}
	err = writeFontSetting("darwin", settings[0], nativeFontPostScript, func(args ...string) (string, error) {
		if args[2] != macFontSet || args[3] != name || args[4] != "font" || args[5] != nativeFontPostScript || strings.Contains(args[2], name) {
			t.Fatal("profile name interpolated into executable script")
		}
		return nativeFontPostScript, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNativeFontNeverTouchesSSHClientPreferences(t *testing.T) {
	_, err := nativeFontSettings(terminalProfile{OS: "linux", Connection: "ssh"}, func(...string) (string, error) {
		t.Fatal("ran client font command on SSH host")
		return "", fmt.Errorf("unreachable")
	})
	if err == nil {
		t.Fatal("SSH client settings should require local setup")
	}
}
