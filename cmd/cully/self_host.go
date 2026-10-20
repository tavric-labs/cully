package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	selfhost "github.com/mcp-runtime/cully/deploy/self-hosted"
)

const maxSourceSize = 200 << 20

var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)

func runSelfHost(agent string, oauth, prepare bool) error {
	if agent != "" && agent != "claude" && agent != "codex" && agent != "cursor" && agent != "all" {
		return fmt.Errorf("choose an agent: claude, codex, cursor, or all")
	}
	fmt.Println("Local setup: download the stack, reuse private settings, start services, configure agents, and verify MCP.")
	fmt.Println("First setup can take several minutes to download images and build Cully and Mem0.")
	ref, err := stackRef(version)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	base := filepath.Join(home, ".cully", "self-hosted")
	fmt.Println("Preparing Cully's local stack and persistent configuration")
	releaseDir, err := ensureStack(ref, base)
	if err != nil {
		return err
	}
	fmt.Printf("Using Cully Docker stack %s\n", ref)
	setupDir := filepath.Join(releaseDir, "deploy", "self-hosted")
	configDir := filepath.Join(base, "config")
	if err := linkStackConfig(setupDir, configDir); err != nil {
		return err
	}
	fmt.Printf("Cully self-hosted configuration: %s\n", configDir)
	if prepare {
		fmt.Println("Edit .env and, for OAuth, connectors.json and .secrets/signing-key.pem; then run cully setup --all (add --oauth for team sign-in).")
		return nil
	}
	// Run the current helper against the matching release's Docker files.
	helper, err := os.CreateTemp(setupDir, ".cully-setup-*.sh")
	if err != nil {
		return err
	}
	defer os.Remove(helper.Name())
	if _, err := helper.WriteString(selfhost.SetupScript); err != nil {
		helper.Close()
		return err
	}
	if err := helper.Close(); err != nil {
		return err
	}
	args := []string{helper.Name()}
	if oauth {
		args = append(args, "--oauth", "mcp-auth")
	}
	if agent != "" {
		args = append(args, agent)
	}
	command := exec.Command("sh", args...)
	command.Dir = setupDir
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	command.Env = append(os.Environ(), "CULLY_CLI_BINARY="+executable, "CULLY_SETUP_CWD="+cwd, "BUILDKIT_PROGRESS=plain")
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("Cully setup incomplete: %w; resolve the error above and rerun cully setup with the same options", err)
	}
	return nil
}

func stackRef(buildVersion string) (string, error) {
	if buildVersion == "" || buildVersion == "dev" {
		return "main", nil
	}
	if !releaseVersion.MatchString(buildVersion) {
		return "", fmt.Errorf("unsupported Cully version %q for self-hosting", buildVersion)
	}
	return "v" + strings.TrimPrefix(buildVersion, "v"), nil
}

func ensureStack(ref, base string) (string, error) {
	releases := filepath.Join(base, "releases")
	if err := os.MkdirAll(releases, 0o700); err != nil {
		return "", err
	}
	destination := filepath.Join(releases, ref)
	setup := filepath.Join(destination, "deploy", "self-hosted", "setup.sh")
	if _, err := os.Stat(setup); err == nil {
		fmt.Printf("Reusing downloaded stack at %s\n", destination)
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if _, err := os.Stat(destination); err == nil {
		return "", fmt.Errorf("incomplete Cully stack at %s; move it aside before retrying", destination)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temporary, err := os.MkdirTemp(releases, ".download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	url := "https://github.com/tavric-labs/cully/archive/refs/tags/" + ref + ".tar.gz"
	if ref == "main" {
		url = "https://github.com/tavric-labs/cully/archive/refs/heads/main.tar.gz"
	}
	fmt.Printf("Downloading Cully Docker stack from %s (%s); timeout: 3 minutes\n", ref, url)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Cully stack download failed: HTTP %d", response.StatusCode)
	}
	fmt.Println("Extracting the managed Docker stack.")
	progress := &stackDownloadProgress{last: time.Now()}
	if err := extractStack(io.TeeReader(io.LimitReader(response.Body, maxSourceSize), progress), temporary); err != nil {
		return "", err
	}
	fmt.Printf("Downloaded and extracted %.1f MiB.\n", float64(progress.bytes)/(1<<20))
	if _, err := os.Stat(filepath.Join(temporary, "deploy", "self-hosted", "setup.sh")); err != nil {
		return "", fmt.Errorf("Cully source archive has no Docker setup: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func extractStack(archive io.Reader, destination string) error {
	zipped, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	var root string
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if path.IsAbs(header.Name) || strings.Contains(header.Name, "\\") {
			return fmt.Errorf("invalid path in Cully source archive: %q", header.Name)
		}
		parts := strings.Split(header.Name, "/")
		for _, part := range parts {
			if part == ".." {
				return fmt.Errorf("invalid path in Cully source archive: %q", header.Name)
			}
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return fmt.Errorf("multiple roots in Cully source archive: %q and %q", root, parts[0])
		}
		if len(parts) < 2 {
			continue
		}
		relative := path.Clean(strings.Join(parts[1:], "/"))
		if relative == "." {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size < 0 || total > maxSourceSize {
				return fmt.Errorf("Cully source archive is too large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o755
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			written, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if written != header.Size {
				return fmt.Errorf("truncated Cully source archive")
			}
		default:
			return fmt.Errorf("unsupported entry in Cully source archive: %q", header.Name)
		}
	}
}

func linkStackConfig(setupDir, configDir string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(configDir, 0o700); err != nil {
		return err
	}
	envFile := filepath.Join(configDir, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		contents, readErr := os.ReadFile(filepath.Join(setupDir, ".env.example"))
		if readErr != nil {
			return readErr
		}
		if writeErr := os.WriteFile(envFile, contents, 0o600); writeErr != nil {
			return writeErr
		}
	} else if err != nil {
		return err
	}
	info, err := os.Lstat(envFile)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("self-hosted configuration must be a regular file: %s", envFile)
	}
	if err := os.Chmod(envFile, 0o600); err != nil {
		return err
	}
	connectorExample := filepath.Join(configDir, "connectors.keycloak.example.json")
	if _, err := os.Stat(connectorExample); os.IsNotExist(err) {
		contents, readErr := os.ReadFile(filepath.Join(setupDir, "connectors.keycloak.example.json"))
		if readErr != nil {
			return readErr
		}
		if writeErr := os.WriteFile(connectorExample, contents, 0o600); writeErr != nil {
			return writeErr
		}
	} else if err != nil {
		return err
	}
	secretsDir := filepath.Join(configDir, ".secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{".env", "connectors.json", ".secrets"} {
		link := filepath.Join(setupDir, name)
		target := filepath.Join(configDir, name)
		if existing, err := os.Readlink(link); err == nil {
			if existing == target {
				continue
			}
			return fmt.Errorf("refusing to replace existing self-hosted config link %s", link)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("refusing to replace existing self-hosted config %s: %w", link, err)
		}
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	return nil
}

// Report compressed bytes as they arrive without exposing archive contents.
type stackDownloadProgress struct {
	bytes int64
	last  time.Time
}

func (p *stackDownloadProgress) Write(data []byte) (int, error) {
	p.bytes += int64(len(data))
	if time.Since(p.last) >= 2*time.Second {
		fmt.Printf("Downloading and extracting: %.1f MiB received.\n", float64(p.bytes)/(1<<20))
		p.last = time.Now()
	}
	return len(data), nil
}
