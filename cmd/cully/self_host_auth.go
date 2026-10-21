package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	selfhost "github.com/mcp-runtime/cully/deploy/self-hosted"
)

// Store image and paths only. Provider configuration and secrets remain in
// user-owned files, consumed by the selected mcp-auth image.
type mcpAuthOptions struct {
	Image     string `json:"image"`
	ConfigDir string `json:"config_dir"`
	EnvFile   string `json:"env_file"`
}

func (o mcpAuthOptions) specified() bool {
	return o.Image != "" || o.ConfigDir != "" || o.EnvFile != ""
}

func prepareMCPAuth(o mcpAuthOptions, configDir, setupDir string) (mcpAuthOptions, bool, error) {
	settings := filepath.Join(configDir, "mcp-auth.json")
	imageOverride := o.Image
	if o.ConfigDir == "" && o.EnvFile == "" {
		data, err := os.ReadFile(settings)
		if os.IsNotExist(err) {
			if imageOverride != "" {
				return o, false, fmt.Errorf("provide --mcp-auth-config-dir and --mcp-auth-env before using a testing image override")
			}
			return o, false, nil
		}
		if err != nil {
			return o, false, fmt.Errorf("read saved mcp-auth settings: %w", err)
		}
		if json.Unmarshal(data, &o) != nil {
			return o, false, fmt.Errorf("saved mcp-auth settings are invalid")
		}
	}
	if o.ConfigDir == "" || o.EnvFile == "" {
		return o, false, fmt.Errorf("mcp-auth requires a config directory and environment file")
	}
	for _, item := range []struct {
		path      *string
		directory bool
	}{{&o.ConfigDir, true}, {&o.EnvFile, false}} {
		absolute, err := filepath.Abs(*item.path)
		if err != nil {
			return o, false, err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return o, false, fmt.Errorf("mcp-auth input: %w", err)
		}
		if item.directory && !info.IsDir() || !item.directory && !info.Mode().IsRegular() {
			return o, false, fmt.Errorf("mcp-auth config must be a directory and its environment input a regular file")
		}
		*item.path = absolute
	}
	// Production resolves the latest stable release at setup time. A testing
	// override applies to this invocation only, never implicitly to a later run.
	o.Image = imageOverride
	if o.Image == "" {
		var err error
		o.Image, err = latestMCPAuthImage()
		if err != nil {
			return o, false, err
		}
	}
	if strings.ContainsAny(o.Image, " \t\r\n$") || o.Image == "" {
		return o, false, fmt.Errorf("invalid mcp-auth image")
	}
	// All release stacks already provide edge, auth-state and signing_key. A
	// separate service leaves the release's bundled auth service/profile intact.
	if err := os.WriteFile(filepath.Join(setupDir, "compose.custom-auth.yaml"), []byte(selfhost.CustomAuthCompose), 0o600); err != nil {
		return o, false, err
	}
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return o, false, err
	}
	// Never follow an existing link when saving managed settings.
	if info, err := os.Lstat(settings); err == nil && !info.Mode().IsRegular() {
		return o, false, fmt.Errorf("mcp-auth settings must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return o, false, err
	}
	if err := os.WriteFile(settings, append(data, '\n'), 0o600); err != nil {
		return o, false, err
	}
	if err := os.Chmod(settings, 0o600); err != nil {
		return o, false, err
	}
	return o, true, nil
}

var latestMCPAuthImage = func() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return fetchLatestMCPAuthImage(ctx, http.DefaultClient, "https://api.github.com/repos/tavric-labs/mcp-auth/releases/latest")
}

func fetchLatestMCPAuthImage(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cully/"+version)
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest mcp-auth release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve latest mcp-auth release: GitHub returned HTTP %d", response.StatusCode)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release) != nil || release.Draft || release.Prerelease || !releaseVersion.MatchString(release.Tag) || strings.Contains(release.Tag, "-") {
		return "", fmt.Errorf("latest mcp-auth release did not identify a stable version")
	}
	return "docker.io/princekrroshan01/mcp-auth-server:" + strings.TrimPrefix(release.Tag, "v"), nil
}
