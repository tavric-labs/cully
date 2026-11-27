package cully

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Sessions are the Claude, Codex and Cursor sessions open on this machine,
// whichever folder they were started in and however they were started. They
// are found by looking at the user's own running processes. Nothing is read
// from them beyond the command line, the working folder and whether Cully's
// terminal started them; environment values are never read or returned.

type uiProc struct {
	PID, PPID int
	Cmd       []string
	Cwd       string
	Started   time.Time
	InCully   bool // started inside the Cully terminal
	// Interactive means the process is attached to a live terminal. Detached
	// means it is still running but its terminal window is gone.
	Interactive, Detached bool
}

type uiSession struct {
	PID       int       `json:"pid"`
	SessionID string    `json:"sessionId,omitempty"`
	Agent     string    `json:"agent"` // claude, codex or cursor
	Cwd       string    `json:"cwd"`
	Repo      string    `json:"repo,omitempty"` // git root, when the folder is in a repository
	Name      string    `json:"name"`
	Branch    string    `json:"branch,omitempty"`
	Remote    string    `json:"remote,omitempty"`
	Via       string    `json:"via"`                // cully or terminal
	Detached  bool      `json:"detached,omitempty"` // running, but its terminal window is gone
	InUse     bool      `json:"inUse"`              // an agent you launch Cully with
	Started   time.Time `json:"started"`
	// What the harness recorded about this session's context and usage, when it did.
	Snapshot *uiSnapshot `json:"snapshot,omitempty"`
}

var uiAgentBinaries = map[string]string{"claude": "claude", "codex": "codex", "cursor-agent": "cursor"}

// uiAgentOf names the coding agent a command line starts, or "".
func uiAgentOf(cmd []string) (agent string, args []string) {
	if len(cmd) == 0 {
		return "", nil
	}
	first := filepath.Base(cmd[0])
	if a, ok := uiAgentBinaries[first]; ok {
		return a, cmd[1:]
	}
	if first == "agent" {
		// Cursor's command-line tool is installed as `agent`; the name alone is too common.
		for i := 1; i < len(cmd); i++ {
			if strings.Contains(cmd[i], "cursor-agent") {
				return "cursor", cmd[i+1:]
			}
		}
		return "", nil
	}
	switch first {
	case "node", "nodejs", "bun", "python", "python3", "bash", "sh", "zsh":
		for i := 1; i < len(cmd); i++ {
			if strings.HasPrefix(cmd[i], "-") {
				continue
			}
			if a, ok := uiAgentBinaries[filepath.Base(cmd[i])]; ok {
				return a, cmd[i+1:]
			}
			if strings.Contains(cmd[i], "cursor-agent") {
				return "cursor", cmd[i+1:]
			}
			break
		}
	}
	return "", nil
}

// A resume argument identifies the transcript explicitly. A folder can
// have several open sessions, so it does not identify one on its own.
func uiExplicitSessionID(cmd []string) string {
	_, args := uiAgentOf(cmd)
	for i, arg := range args {
		switch arg {
		case "resume", "--resume", "--session-id":
			if i+1 < len(args) && uiSessionID.MatchString(args[i+1]) {
				return strings.ToLower(args[i+1])
			}
		}
	}
	return ""
}

type uiFolder struct {
	root, branch, remote string
	at                   time.Time
}

var (
	uiFolderMu    sync.Mutex
	uiFolderCache = map[string]uiFolder{}
)

// uiFolderInfo describes a working folder, caching briefly so a page that
// polls does not run git for every session every time.
func uiFolderInfo(cwd string) uiFolder {
	uiFolderMu.Lock()
	if c, ok := uiFolderCache[cwd]; ok && time.Since(c.at) < 8*time.Second {
		uiFolderMu.Unlock()
		return c
	}
	uiFolderMu.Unlock()
	f := uiFolder{at: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
		if real, err := canonicalDir(strings.TrimSpace(string(out))); err == nil {
			f.root = real
			if b, err := exec.CommandContext(ctx, "git", "-C", real, "branch", "--show-current").Output(); err == nil {
				f.branch = strings.TrimSpace(string(b))
			}
			f.remote = uiRepoRemote(real)
		}
	}
	uiFolderMu.Lock()
	uiFolderCache[cwd] = f
	uiFolderMu.Unlock()
	return f
}

func uiHidden(cwd string, hidden []string) bool {
	for _, h := range hidden {
		h = strings.TrimRight(h, "/")
		if h != "" && (cwd == h || strings.HasPrefix(cwd, h+"/")) {
			return true
		}
	}
	return false
}

// sessions lists the open agent sessions on this machine, newest first.
func (m *uiManager) sessions() []uiSession {
	procs := uiListProcs()
	type found struct {
		p     uiProc
		agent string
	}
	byPID := map[int]found{}
	for _, p := range procs {
		agent, args := uiAgentOf(p.Cmd)
		if agent == "" || agentUtilityCommand(agent, args) {
			continue // not an agent, or a one-off command such as `claude mcp list` or `claude -p`
		}
		if !p.Interactive && !p.Detached {
			continue // a background daemon or a headless run, not a session you can sit at
		}
		byPID[p.PID] = found{p, agent}
	}
	out := []uiSession{}
	for _, f := range byPID {
		if parent, ok := byPID[f.p.PPID]; ok && parent.agent == f.agent {
			continue // a helper process of a session already listed
		}
		cwd := strings.TrimSuffix(f.p.Cwd, " (deleted)")
		if uiHidden(cwd, m.cfg.HideSessionsIn) {
			continue // checked before any git command runs in that folder
		}
		info := uiFolderInfo(cwd)
		s := uiSession{PID: f.p.PID, Agent: f.agent, Cwd: cwd, Repo: info.root, Branch: info.branch, Remote: info.remote, Via: "terminal", Started: f.p.Started, Detached: f.p.Detached, InUse: m.mcp.isInUse(f.agent)}
		s.SessionID = uiExplicitSessionID(f.p.Cmd)
		if f.p.InCully {
			s.Via = "cully"
		}
		if info.root != "" {
			s.Name = filepath.Base(info.root)
		} else {
			s.Name = filepath.Base(cwd)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Detached != out[j].Detached {
			return !out[i].Detached // sessions with a live terminal first
		}
		return out[i].Started.After(out[j].Started)
	})
	return out
}

// uiListProcs returns the current user's agent processes. It is a variable so
// tests need no real processes.
var uiListProcs = func() []uiProc {
	switch runtime.GOOS {
	case "linux":
		return uiListProcsLinux()
	case "darwin":
		return uiListProcsDarwin()
	}
	return nil
}

func uiListProcsLinux() []uiProc {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	uid := os.Getuid()
	var out []uiProc
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := "/proc/" + e.Name()
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
			continue // only the user's own processes
		}
		raw, err := os.ReadFile(dir + "/cmdline")
		if err != nil || len(raw) == 0 {
			continue
		}
		cmd := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if a, _ := uiAgentOf(cmd); a == "" {
			continue
		}
		p := uiProc{PID: pid, Cmd: cmd, Started: info.ModTime()}
		if stat, err := os.ReadFile(dir + "/stat"); err == nil {
			if i := strings.LastIndex(string(stat), ") "); i >= 0 {
				if f := strings.Fields(string(stat)[i+2:]); len(f) > 1 {
					p.PPID, _ = strconv.Atoi(f[1])
				}
			}
		}
		parent := "/proc/" + strconv.Itoa(p.PPID)
		// Some CLIs (Cursor's) hide their own /proc entries; their parent shell
		// is in the folder they were started from and holds the same terminal.
		p.Cwd, _ = os.Readlink(dir + "/cwd")
		if p.Cwd == "" && p.PPID > 0 {
			p.Cwd, _ = os.Readlink(parent + "/cwd")
		}
		if p.Cwd == "" {
			continue
		}
		tty, _ := os.Readlink(dir + "/fd/0")
		if tty == "" && p.PPID > 0 {
			tty, _ = os.Readlink(parent + "/fd/0")
		}
		onTerminal := strings.HasPrefix(tty, "/dev/pts/") || strings.HasPrefix(tty, "/dev/tty")
		p.Detached = onTerminal && strings.HasSuffix(tty, " (deleted)")
		p.Interactive = onTerminal && !p.Detached
		// Only whether the variable exists is checked; its value is never read.
		if env, err := os.ReadFile(dir + "/environ"); err == nil {
			p.InCully = bytes.HasPrefix(env, []byte("CULLY_PANE_SESSION=")) || bytes.Contains(env, []byte("\x00CULLY_PANE_SESSION="))
		}
		if !p.InCully && p.PPID > 0 {
			if comm, err := os.ReadFile("/proc/" + strconv.Itoa(p.PPID) + "/comm"); err == nil {
				p.InCully = strings.HasPrefix(strings.TrimSpace(string(comm)), "cully")
			}
		}
		out = append(out, p)
	}
	return out
}

// uiListProcsDarwin is best effort: macOS has no /proc, so it asks ps and lsof.
func uiListProcsDarwin() []uiProc {
	out, err := exec.Command("ps", "-U", strconv.Itoa(os.Getuid()), "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil
	}
	var procs []uiProc
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		cmd := f[2:]
		if a, _ := uiAgentOf(cmd); a == "" {
			continue
		}
		p := uiProc{PID: pid, PPID: ppid, Cmd: cmd}
		if l, err := exec.Command("lsof", "-a", "-d", "cwd", "-Fn", "-p", strconv.Itoa(pid)).Output(); err == nil {
			for _, ln := range strings.Split(string(l), "\n") {
				if strings.HasPrefix(ln, "n/") {
					p.Cwd = ln[1:]
				}
			}
		}
		if p.Cwd == "" {
			continue
		}
		if comm, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(ppid)).Output(); err == nil {
			p.InCully = strings.HasPrefix(filepath.Base(strings.TrimSpace(string(comm))), "cully")
		}
		procs = append(procs, p)
	}
	return procs
}
