package cully

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// `cully run --ui AGENT` starts (or reuses) a browser UI for the current
// repository and prints its link before the agent starts. One background
// server runs per repository: it records its address and pid in a small file
// under Cully's log directory, so later runs in the same repository print the
// same link instead of starting another server. The sign-in token stays in the
// separate ui-token file and is never written to that record or to the log.

const uiSpawnWait = 6 * time.Second

type uiInfo struct {
	Addr    string    `json:"addr"`
	PID     int       `json:"pid"`
	Repo    string    `json:"repo"`
	Started time.Time `json:"started"`
}

// uiInfoPath is the record of the running background UI. There is one per
// user, not one per folder: it lists every session on the machine.
func uiInfoPath() string {
	return filepath.Join(cullyDir(), "ui.json")
}

func readUIInfo() (uiInfo, bool) {
	b, err := os.ReadFile(uiInfoPath())
	if err != nil {
		return uiInfo{}, false
	}
	var info uiInfo
	if json.Unmarshal(b, &info) != nil || info.Addr == "" {
		return uiInfo{}, false
	}
	return info, true
}

func writeUIInfo(info uiInfo) error {
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(uiInfoPath(), b, 0o600)
}

func uiReachable(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func uiAlive(info uiInfo) bool {
	return info.PID > 0 && syscall.Kill(info.PID, 0) == nil && uiReachable(info.Addr)
}

func uiRepoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("this directory is not inside a git repository, and the UI runs agents in git worktrees")
	}
	return canonicalDir(strings.TrimSpace(string(out)))
}

// uiSpawn starts `cully ui` for a repository in the background and returns its
// address and pid. It is a variable so tests need no real process.
var uiSpawn = func(repo string) (string, int, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", 0, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", 0, err
	}
	addr := ln.Addr().String()
	ln.Close()
	logPath := filepath.Join(cullyDir(), "ui.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", 0, err
	}
	defer logFile.Close()
	args := []string{"ui", "--quiet", "--addr", addr}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "CULLY_PANE_SESSION", "CULLY_SESSION", "CULLY_AGENT", "MODEL_HINT_GUARD":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	if err := cmd.Start(); err != nil {
		return "", 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	deadline := time.Now().Add(uiSpawnWait)
	for time.Now().Before(deadline) {
		if uiReachable(addr) {
			return addr, pid, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	return "", 0, fmt.Errorf("the UI did not start; see %s", logPath)
}

// uiEndpoint is a running UI server and the token that signs in to it.
type uiEndpoint struct{ Addr, Token string }

func (e uiEndpoint) link() string { return "http://" + e.Addr + "/?token=" + e.Token }

// ensureUI finds the running UI or starts one. It works from any folder: the
// UI shows every session on the machine, so the folder only decides which
// repository is added first.
func ensureUI(dir string) (uiEndpoint, bool, error) {
	root, _ := uiRepoRoot(dir) // empty when dir is not in a git repository
	token, err := loadOrCreateUIToken()
	if err != nil {
		return uiEndpoint{}, false, err
	}
	if info, ok := readUIInfo(); ok && uiAlive(info) {
		return uiEndpoint{info.Addr, token}, false, nil
	}
	addr, pid, err := uiSpawn(root)
	if err != nil {
		return uiEndpoint{}, false, err
	}
	if err := writeUIInfo(uiInfo{Addr: addr, PID: pid, Repo: root, Started: time.Now().UTC()}); err != nil {
		return uiEndpoint{}, false, err
	}
	return uiEndpoint{addr, token}, true, nil
}

// EnsureUI returns the sign-in link of the UI, starting the background server
// if none is running.
func EnsureUI(dir string) (link string, started bool, err error) {
	ep, started, err := ensureUI(dir)
	if err != nil {
		return "", false, err
	}
	return ep.link(), started, nil
}

// registerAgentInUse tells a running UI which agent launched it, so the page
// focuses on that agent instead of asking about all three. It is best effort:
// an older server or a slow reply never delays the agent.
func registerAgentInUse(ep uiEndpoint, agent string) {
	uiPost(ep, "/api/mcp/in-use", map[string]string{"agent": agent})
}

// registerRepo lets the running UI start agents in the folder you launched from.
func registerRepo(ep uiEndpoint, path string) {
	uiPost(ep, "/api/repos", map[string]string{"path": path})
}

func uiPost(ep uiEndpoint, path string, payload map[string]string) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+ep.Addr+path, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	req.Header.Set("X-Cully-UI", "1")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// UIWanted reports whether the UI link should be shown: the --ui flag, or
// CULLY_UI=1 to make it the default.
func UIWanted(flag bool) bool {
	if flag {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CULLY_UI"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// AnnounceUI prints the UI link for the current repository. Utility commands
// (version, help, mcp and so on) are skipped, and a failure is only reported:
// it never stops the agent from starting.
func AnnounceUI(w io.Writer, agent string, args []string) {
	a, err := lookupPaneAgent(agent)
	if err != nil || agentUtilityCommand(a.Name, args) {
		return
	}
	ep, started, err := ensureUI(currentDir())
	if err != nil {
		fmt.Fprintf(w, "Cully UI unavailable: %v\n", err)
		return
	}
	registerAgentInUse(ep, a.Name)
	if root, err := uiRepoRoot(currentDir()); err == nil {
		registerRepo(ep, root)
	}
	link := ep.link()
	state := "ready"
	if started {
		state = "started"
	}
	fmt.Fprintf(w, "Cully UI %s: %s\n", state, link)
	if os.Getenv("SSH_CONNECTION") != "" {
		if u, perr := parseUILinkPort(link); perr == nil {
			fmt.Fprintf(w, "Over SSH, forward the port first: ssh -L %s:127.0.0.1:%s <this-host>\n", u, u)
		}
	}
}

func parseUILinkPort(link string) (string, error) {
	rest := strings.TrimPrefix(link, "http://")
	hostport, _, _ := strings.Cut(rest, "/")
	_, port, err := net.SplitHostPort(hostport)
	return port, err
}

// MaybeAnnounceUI is AnnounceUI for `cully run`: only when asked for, only in
// an interactive terminal, and written to stderr so piped output stays clean.
func MaybeAnnounceUI(flag bool, agent string, args []string, in, out *os.File) {
	if !UIWanted(flag) || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return
	}
	AnnounceUI(os.Stderr, agent, args)
}
