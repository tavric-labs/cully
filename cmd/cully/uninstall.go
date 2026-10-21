package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mcp-runtime/cully/internal/cully"
)

func runUninstall(args []string) error {
	if len(args) > 0 && args[0] != "--purge-data" && args[0] != "--help" && args[0] != "-h" {
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("usage: cully uninstall [--purge-data] or cully uninstall [claude|codex|cursor|all]")
			}
		}
		return cully.Uninstall(args...)
	}
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purgeData := fs.Bool("purge-data", false, "delete local Docker volumes, memory, and self-hosted configuration")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Println("Usage: cully uninstall [--purge-data] or cully uninstall [claude|codex|cursor|all]")
		return nil
	} else if err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: cully uninstall [--purge-data] or cully uninstall [claude|codex|cursor|all]")
	}
	if err := stopSelfHostedStack(*purgeData); err != nil {
		return err
	}
	if err := cully.Uninstall("all"); err != nil {
		return err
	}
	base, err := selfHostedBase()
	if err != nil {
		return err
	}
	endpoints, err := selfHostedEndpoints(base)
	if err != nil {
		return err
	}
	for _, agent := range []string{"claude", "codex", "cursor"} {
		if err := cully.RemoveMCPIfMatching(agent, endpoints); err != nil {
			return err
		}
	}
	if *purgeData {
		if err := purgeSelfHostedData(); err != nil {
			return err
		}
		fmt.Println("Removed local Cully memory volumes and self-hosted configuration.")
	} else {
		fmt.Println("Kept local Cully memory volumes and self-hosted configuration. Use cully uninstall --purge-data to delete them.")
	}
	removed, err := removeCurrentInstallerBinary()
	if err != nil {
		return err
	}
	if removed {
		fmt.Println("Removed the installed Cully CLI binary.")
	} else {
		fmt.Println("The Cully CLI binary was not in a standard installer location; remove it manually if no longer needed.")
	}
	return nil
}

func removeCurrentInstallerBinary() (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	canonical := filepath.Join(home, ".local", "bin", "cully")
	resolved, resolveErr := filepath.EvalSymlinks(executable)
	resolvedCanonical, canonicalErr := filepath.EvalSymlinks(canonical)
	if resolveErr == nil && canonicalErr == nil && resolved == resolvedCanonical {
		if err := removeInstallerLinks(home, canonical); err != nil {
			return false, err
		}
		if err := os.Remove(canonical); err != nil {
			return false, err
		}
		return true, nil
	}
	for _, candidate := range []string{
		filepath.Join(cully.ConfigDir(), "bin", "cully"),
		filepath.Join(cully.CodexConfigDir(), "bin", "cully"),
		filepath.Join(cully.CursorConfigDir(), "bin", "cully"),
		filepath.Join(home, ".local", "bin", "cully"),
	} {
		if filepath.Clean(executable) == filepath.Clean(candidate) {
			if err := os.Remove(candidate); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

func selfHostedBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cully", "self-hosted"), nil
}

func installedComposeDir(base string) (string, error) {
	releases := filepath.Join(base, "releases")
	entries, err := os.ReadDir(releases)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		setupDir := filepath.Join(releases, entry.Name(), "deploy", "self-hosted")
		if regularFile(filepath.Join(setupDir, "compose.yaml")) && regularFile(filepath.Join(setupDir, "setup.sh")) {
			candidates = append(candidates, setupDir)
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Strings(candidates)
	return candidates[len(candidates)-1], nil
}

func stopSelfHostedStack(purgeData bool) error {
	base, err := selfHostedBase()
	if err != nil {
		return err
	}
	setupDir, err := installedComposeDir(base)
	if err != nil || setupDir == "" {
		return err
	}
	args := []string{"compose"}
	if envFile := filepath.Join(base, "config", ".env"); regularFile(envFile) {
		args = append(args, "--env-file", envFile)
	}
	args = append(args, "-f", filepath.Join(setupDir, "compose.yaml"))
	if custom := filepath.Join(setupDir, "compose.custom-auth.yaml"); regularFile(custom) {
		args = append(args, "-f", custom)
	}
	args = append(args, "--profile", "oauth", "--profile", "mcp-auth", "down")
	if purgeData {
		args = append(args, "--volumes")
	}
	command := exec.Command("docker", args...)
	command.Dir = setupDir
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	// Compose evaluates required variables even for `down`. No secret value is
	// needed to stop a stack, so use non-secret placeholders in the child only.
	command.Env = append(os.Environ(),
		"CULLY_DB_NAME=unused", "CULLY_DB_USER=unused", "CULLY_DB_PASSWORD=unused",
		"CULLY_DATABASE_URL=unused", "CULLY_DATA_API_TOKEN=unused",
		"CULLY_MEM0_DB_PASSWORD=unused", "CULLY_MEM0_API_KEY=unused", "CULLY_MEM0_JWT_SECRET=unused",
		"CULLY_MCP_AUTH_IMAGE=unused:unused", "CULLY_MCP_AUTH_CONFIG_DIR="+setupDir,
		"CULLY_MCP_AUTH_ENV_FILE="+os.DevNull,
		"CULLY_AUTH_ISSUER=https://unused.invalid", "CULLY_AUTH_RESOURCE=https://unused.invalid/mcp",
	)
	if err := command.Run(); err != nil {
		return fmt.Errorf("stop Cully Docker stack: %w", err)
	}
	fmt.Println("Stopped the local Cully Docker stack.")
	return nil
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func selfHostedEndpoints(base string) ([]string, error) {
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	values, err := readSelfHostEnv(filepath.Join(base, "config", ".env"))
	if err != nil {
		return nil, err
	}
	port := strings.TrimSpace(values["CULLY_MCP_PORT"])
	if port == "" {
		port = "8080"
	}
	endpoints := []string{"http://127.0.0.1:" + port + "/mcp"}
	if host := strings.TrimSpace(values["CULLY_MCP_HOST"]); host != "" && !strings.Contains(host, "example.com") {
		endpoints = append(endpoints, "https://"+host+"/mcp")
	}
	return endpoints, nil
}

func purgeSelfHostedData() error {
	base, err := selfHostedBase()
	if err != nil {
		return err
	}
	configPath, err := selfHostConfigPath()
	if err != nil {
		return err
	}
	document, err := readSelfHostDocument(configPath)
	if err != nil {
		return err
	}
	if _, ok := document["self_hosted"]; !ok {
		return os.RemoveAll(base)
	}
	delete(document, "self_hosted")
	if len(document) == 0 {
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := saveSelfHostDocument(configPath, document); err != nil {
		return err
	}
	return os.RemoveAll(base)
}

// Remove only links that point to the canonical installer binary.
func removeInstallerLinks(home, canonical string) error {
	for _, candidate := range []string{
		filepath.Join(cully.ConfigDir(), "bin", "cully"),
		filepath.Join(cully.CodexConfigDir(), "bin", "cully"),
		filepath.Join(cully.CursorConfigDir(), "bin", "cully"),
		filepath.Join(home, ".claude", "bin", "cully"),
		filepath.Join(home, ".codex", "bin", "cully"),
		filepath.Join(home, ".cursor", "bin", "cully"),
	} {
		target, err := os.Readlink(candidate)
		if err != nil || target != canonical {
			continue
		}
		if err := os.Remove(candidate); err != nil {
			return err
		}
	}
	return nil
}
