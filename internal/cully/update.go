package cully

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const updateScope = "Updates the local CLI; existing agent links use the same binary.\nFor hooks and skills, rerun cully setup with your original options.\nUpgrade MCP, data, Mem0 and databases using your deployment procedure:\nhttps://docs.cully.net/team-deployment\n"

var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-.][A-Za-z0-9.-]+)?$`)

// RunUpdate replaces the invoked CLI, resolving managed agent symlinks to their
// shared target. It never infers a deployment mode or rewrites configuration.
func RunUpdate(w io.Writer, current string, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(w)
	check := fs.Bool("check", false, "check the release without changing anything")
	tag := fs.String("version", "", "release tag to install (default: latest stable)")
	fs.Usage = func() {
		fmt.Fprintln(w, "Usage: cully update [--check] [--version vX.Y.Z]")
		fs.PrintDefaults()
		fmt.Fprint(w, updateScope)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || (*tag != "" && !releaseTagPattern.MatchString(*tag)) {
		return fmt.Errorf("usage: cully update [--check] [--version vX.Y.Z]")
	}
	if (runtime.GOOS != "darwin" && runtime.GOOS != "linux") || (runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64") {
		return fmt.Errorf("no released CLI for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	u := cliUpdater{client: &http.Client{Timeout: 2 * time.Minute}, api: "https://api.github.com/repos/tavric-labs/cully", releases: "https://github.com/tavric-labs/cully/releases/download"}
	fmt.Fprintln(w, "Checking Cully releases…")
	if *tag == "" {
		data, err := u.get(ctx, u.api+"/releases/latest", 1<<20)
		if err != nil {
			return err
		}
		var release struct {
			Tag string `json:"tag_name"`
		}
		if err := json.Unmarshal(data, &release); err != nil {
			return fmt.Errorf("read latest release: %w", err)
		}
		*tag = release.Tag
	}
	if !releaseTagPattern.MatchString(*tag) {
		return fmt.Errorf("invalid release tag %q", *tag)
	}
	fmt.Fprintf(w, "Installed: %s · Target: %s\n", current, *tag)
	fmt.Fprint(w, updateScope)
	if strings.TrimPrefix(current, "v") == strings.TrimPrefix(*tag, "v") {
		fmt.Fprintln(w, "Already up to date.")
		return nil
	}
	if *check {
		fmt.Fprintf(w, "No changes made. Run cully update --version %s to install this release.\n", *tag)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	asset := fmt.Sprintf("cully_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(w, "Downloading and verifying %s…\n", asset)
	if err := u.install(ctx, exe, *tag, asset); err != nil {
		return fmt.Errorf("CLI update failed; existing binary preserved: %w", err)
	}
	fmt.Fprintf(w, "Updated to %s → %s\n", *tag, exe)
	if isDaemonRunning() {
		fmt.Fprintln(w, "Restarting the local advisor…")
		if err := StopDaemon(); err != nil {
			return fmt.Errorf("CLI updated; stop advisor: %w", err)
		}
		if err := StartDaemonDetached(); err != nil {
			return fmt.Errorf("CLI updated; advisor restart failed: %w; rerun cully setup with your original options", err)
		}
	}
	fmt.Fprintln(w, "Open a new Cully terminal to use the updated wrapper; running sessions keep their current version.")
	return nil
}

type cliUpdater struct {
	client        *http.Client
	api, releases string
}

func (u cliUpdater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cully-update")
	res, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds size limit")
	}
	return data, nil
}

func (u cliUpdater) install(ctx context.Context, destination, tag, asset string) error {
	base := u.releases + "/" + tag + "/"
	sums, err := u.get(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	var expected string
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			expected = fields[0]
			break
		}
	}
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("missing or invalid checksum for %s", asset)
	}
	data, err := u.get(ctx, base+asset, 64<<20)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(data); !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return fmt.Errorf("release archive has no cully binary")
		}
		if err != nil {
			return err
		}
		if header.Name != "cully" && header.Name != "./cully" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 128<<20 {
			return fmt.Errorf("invalid cully binary in archive")
		}
		staged, err := os.CreateTemp(filepath.Dir(destination), ".cully-update-*")
		if err != nil {
			return fmt.Errorf("cannot write installation directory; use its package manager or installer: %w", err)
		}
		defer os.Remove(staged.Name())
		_, copyErr := io.Copy(staged, archive)
		syncErr := staged.Sync()
		closeErr := staged.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return err
		}
		if err := os.Chmod(staged.Name(), 0755); err != nil {
			return err
		}
		verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(verifyCtx, staged.Name(), "version").Output()
		if err != nil || strings.TrimSpace(string(out)) != "cully "+strings.TrimPrefix(tag, "v") {
			return fmt.Errorf("downloaded binary did not report expected version %s", tag)
		}
		return os.Rename(staged.Name(), destination)
	}
}
