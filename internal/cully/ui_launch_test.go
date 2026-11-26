package cully

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// uiFakeSpawn replaces the real background launch with an in-process listener.
type uiFakeSpawn struct {
	calls atomic.Int32
	ln    net.Listener
}

func (f *uiFakeSpawn) install(t *testing.T) {
	t.Helper()
	old := uiSpawn
	uiSpawn = func(repo string) (string, int, error) {
		f.calls.Add(1)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", 0, err
		}
		f.ln = ln
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		return ln.Addr().String(), os.Getpid(), nil
	}
	t.Cleanup(func() {
		uiSpawn = old
		if f.ln != nil {
			f.ln.Close()
		}
	})
}

func uiChdir(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func TestEnsureUISpawnsOnceAndReuses(t *testing.T) {
	repo := uiNewTestRepo(t)
	var f uiFakeSpawn
	f.install(t)
	link, started, err := EnsureUI(repo)
	if err != nil || !started {
		t.Fatalf("first call: started=%v err=%v", started, err)
	}
	token, _ := loadOrCreateUIToken()
	if want := "http://" + f.ln.Addr().String() + "/?token=" + token; link != want {
		t.Fatalf("link = %q, want %q", link, want)
	}
	info, err := os.Stat(uiInfoPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info file: %v %v", err, info)
	}
	link2, started2, err := EnsureUI(repo)
	if err != nil || started2 || link2 != link {
		t.Fatalf("second call: %q started=%v err=%v", link2, started2, err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("spawned %d times, want 1", f.calls.Load())
	}
}

func TestEnsureUIRespawnsWhenServerGone(t *testing.T) {
	repo := uiNewTestRepo(t)
	var f uiFakeSpawn
	f.install(t)
	if _, _, err := EnsureUI(repo); err != nil {
		t.Fatal(err)
	}
	f.ln.Close() // the background server died
	_, started, err := EnsureUI(repo)
	if err != nil || !started || f.calls.Load() != 2 {
		t.Fatalf("started=%v err=%v spawns=%d", started, err, f.calls.Load())
	}
}

func TestAnnounceUISkipsUtilityCommands(t *testing.T) {
	repo := uiNewTestRepo(t)
	uiChdir(t, repo)
	var f uiFakeSpawn
	f.install(t)
	var out bytes.Buffer
	AnnounceUI(&out, "claude", []string{"--version"})
	AnnounceUI(&out, "claude", []string{"mcp", "list"})
	if out.Len() != 0 || f.calls.Load() != 0 {
		t.Fatalf("announced for a utility command: %q spawns=%d", out.String(), f.calls.Load())
	}
}

func TestAnnounceUIPrintsLinkAndSSHHint(t *testing.T) {
	repo := uiNewTestRepo(t)
	uiChdir(t, repo)
	var f uiFakeSpawn
	f.install(t)
	t.Setenv("SSH_CONNECTION", "")
	var plain bytes.Buffer
	AnnounceUI(&plain, "codex", nil)
	if !strings.Contains(plain.String(), "http://127.0.0.1:") || !strings.Contains(plain.String(), "?token=") {
		t.Fatalf("no link: %q", plain.String())
	}
	if strings.Contains(plain.String(), "ssh -L") {
		t.Fatalf("ssh hint outside ssh: %q", plain.String())
	}
	t.Setenv("SSH_CONNECTION", "10.0.0.2 51000 10.0.0.9 22")
	var viaSSH bytes.Buffer
	AnnounceUI(&viaSSH, "codex", nil)
	_, port, _ := net.SplitHostPort(f.ln.Addr().String())
	if want := "ssh -L " + port + ":127.0.0.1:" + port; !strings.Contains(viaSSH.String(), want) {
		t.Fatalf("missing %q in %q", want, viaSSH.String())
	}
}

func TestUIWanted(t *testing.T) {
	t.Setenv("CULLY_UI", "")
	if UIWanted(false) || !UIWanted(true) {
		t.Fatal("flag handling wrong")
	}
	t.Setenv("CULLY_UI", "1")
	if !UIWanted(false) {
		t.Fatal("CULLY_UI=1 ignored")
	}
	t.Setenv("CULLY_UI", "0")
	if UIWanted(false) {
		t.Fatal("CULLY_UI=0 enabled the UI")
	}
}

func TestUIURLCommandPrintsTheLink(t *testing.T) {
	repo := uiNewTestRepo(t)
	uiChdir(t, repo)
	var f uiFakeSpawn
	f.install(t)
	var out bytes.Buffer
	if err := RunUI(&out, []string{"url"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "http://127.0.0.1:") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("output = %q", out.String())
	}
}

func TestAnnounceUIRegistersTheLaunchingAgent(t *testing.T) {
	repo := uiNewTestRepo(t)
	uiChdir(t, repo)
	var gotAgent, gotAuth, gotHeader string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mcp/in-use" && r.Method == http.MethodPost {
			var b struct{ Agent string }
			json.NewDecoder(r.Body).Decode(&b)
			gotAgent, gotAuth, gotHeader = b.Agent, r.Header.Get("Authorization"), r.Header.Get("X-Cully-UI")
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(hs.Close)
	old := uiSpawn
	uiSpawn = func(string) (string, int, error) { return strings.TrimPrefix(hs.URL, "http://"), os.Getpid(), nil }
	t.Cleanup(func() { uiSpawn = old })
	var out bytes.Buffer
	AnnounceUI(&out, "codex", nil)
	token, _ := loadOrCreateUIToken()
	if gotAgent != "codex" || gotAuth != "Bearer "+token || gotHeader != "1" {
		t.Fatalf("registered agent=%q auth ok=%v header=%q", gotAgent, gotAuth == "Bearer "+token, gotHeader)
	}
	if !strings.Contains(out.String(), "Cully UI") {
		t.Fatalf("link not printed: %q", out.String())
	}
}

func TestAnnounceUIStillPrintsWhenRegistrationFails(t *testing.T) {
	repo := uiNewTestRepo(t)
	uiChdir(t, repo)
	var f uiFakeSpawn // its listener drops connections, so registration fails
	f.install(t)
	var out bytes.Buffer
	AnnounceUI(&out, "cursor", nil)
	if !strings.Contains(out.String(), "?token=") {
		t.Fatalf("link missing: %q", out.String())
	}
}

func TestEnsureUIIsSharedAcrossFolders(t *testing.T) {
	a := uiNewTestRepo(t)
	b := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(b, 0o755)
	exec.Command("git", "-C", b, "init", "-q").Run()
	var f uiFakeSpawn
	f.install(t)
	first, started, err := EnsureUI(a)
	if err != nil || !started {
		t.Fatalf("first: started=%v err=%v", started, err)
	}
	second, started, err := EnsureUI(b)
	if err != nil || started || second != first || f.calls.Load() != 1 {
		t.Fatalf("a second folder started its own server: started=%v spawns=%d", started, f.calls.Load())
	}
}

func TestEnsureUIWorksFromAnyFolder(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var f uiFakeSpawn
	f.install(t)
	var gotRepo string
	old := uiSpawn
	inner := uiSpawn
	uiSpawn = func(repo string) (string, int, error) { gotRepo = repo; return inner(repo) }
	t.Cleanup(func() { uiSpawn = old })
	if _, started, err := EnsureUI(t.TempDir()); err != nil || !started {
		t.Fatalf("a folder that is not a git repository must not block the UI: started=%v err=%v", started, err)
	}
	if gotRepo != "" {
		t.Fatalf("spawned with repo %q for a non-git folder", gotRepo)
	}
}

func TestAnnounceUIFailureNeverBlocksTheAgent(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	uiChdir(t, t.TempDir())
	old := uiSpawn
	uiSpawn = func(string) (string, int, error) { return "", 0, errors.New("boom") }
	t.Cleanup(func() { uiSpawn = old })
	var out bytes.Buffer
	AnnounceUI(&out, "cursor", nil)
	if !strings.Contains(out.String(), "Cully UI unavailable") || !strings.Contains(out.String(), "boom") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestAnnounceUIRegistersTheFolderToo(t *testing.T) {
	repo := uiNewTestRepo(t)
	uiChdir(t, repo)
	var gotPath string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/repos" && r.Method == http.MethodPost {
			var b struct{ Path string }
			json.NewDecoder(r.Body).Decode(&b)
			gotPath = b.Path
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(hs.Close)
	old := uiSpawn
	uiSpawn = func(string) (string, int, error) { return strings.TrimPrefix(hs.URL, "http://"), os.Getpid(), nil }
	t.Cleanup(func() { uiSpawn = old })
	var out bytes.Buffer
	AnnounceUI(&out, "claude", nil)
	want, _ := canonicalDir(repo)
	if gotPath != want {
		t.Fatalf("registered folder %q, want %q", gotPath, want)
	}
}
