package cully

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// advisorJob is queued for the persistent cully daemon (ECAM computer).
// Cwd travels with the job so the worker stamps the resulting report with the
// project it came from — the daemon serves every session, so a job must never
// lose track of whose suggestions it is producing.
type advisorJob struct {
	Session     string `json:"session"`
	SignalsPath string `json:"signals_path"`
	Cwd         string `json:"cwd"`
}

func daemonPIDFile() string { return filepath.Join(cullyDir(), ".cully-daemon.pid") }
func daemonLogFile() string { return filepath.Join(cullyDir(), ".cully-daemon.log") }
func jobDir() string        { return filepath.Join(cullyDir(), "cully-jobs") }

func isDaemonRunning() bool {
	b, err := os.ReadFile(daemonPIDFile())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	return processAlive(pid)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// RunDaemonStatus prints whether the advisor daemon is running.
func RunDaemonStatus(w io.Writer) {
	if isDaemonRunning() {
		b, _ := os.ReadFile(daemonPIDFile())
		fmt.Fprintf(w, "cully advisor daemon running (pid %s)\n", strings.TrimSpace(string(b)))
		pending, _ := filepath.Glob(filepath.Join(jobDir(), "*.job"))
		if len(pending) > 0 {
			fmt.Fprintf(w, "  queued jobs: %d\n", len(pending))
		}
		return
	}
	fmt.Fprintln(w, "cully advisor daemon not running")
	fmt.Fprintln(w, "  rerun cully setup with your original options to start the advisor")
}

// StartDaemonDetached launches the long-running advisor daemon in the background.
func StartDaemonDetached() error {
	if os.Getenv("CULLY_ANALYZE_DISABLE") == "1" {
		return fmt.Errorf("CULLY_ANALYZE_DISABLE=1 disables the advisor; unset it to start the daemon")
	}
	if isDaemonRunning() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "_internal", "daemon")
	cmd.Env = append(os.Environ(), "MODEL_HINT_GUARD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	if err := cmd.Start(); err != nil {
		return err
	}
	// wait briefly for pid file
	for i := 0; i < 20; i++ {
		if isDaemonRunning() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon started but pid file not ready")
}

// StopDaemon sends SIGTERM to the advisor daemon.
func StopDaemon() error {
	if !isDaemonRunning() {
		_ = os.Remove(daemonPIDFile())
		return nil
	}
	b, _ := os.ReadFile(daemonPIDFile())
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 30; i++ {
			if !processAlive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Remove(daemonPIDFile())
	return nil
}

// RunDaemon runs the foreground advisor daemon (internal: cully _internal daemon).
func RunDaemon() {
	if os.Getenv("CULLY_ANALYZE_DISABLE") == "1" {
		fmt.Fprintln(os.Stderr, "daemon: CULLY_ANALYZE_DISABLE is set")
		os.Exit(1)
	}
	if err := os.MkdirAll(cullyDir(), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "daemon:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(jobDir(), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "daemon:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(daemonPIDFile(), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "daemon:", err)
		os.Exit(1)
	}
	defer os.Remove(daemonPIDFile())

	daemonLog("daemon: start pid=%d (acquisition + advisor LRU)", os.Getpid())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go acquisitionLoop(ctx)
	go selfImproveLoop(ctx)
	advisorDone := make(chan struct{})
	go func() { defer close(advisorDone); advisorLoop(ctx) }()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	<-sigc
	daemonLog("daemon: shutdown")
	cancel()
	<-advisorDone
}

func daemonLog(format string, args ...any) {
	msg := fmt.Sprintf("%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
	_ = os.MkdirAll(cullyDir(), 0o755)
	f, err := os.OpenFile(daemonLogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(msg)
}

// acquisitionLoop is the data-acquisition LRU — keeps instrument snapshots
// fresh. Only account-wide rate limits are fanned out to every live session's
// snapshot; context fill is per-session and owned by that session's statusline,
// so it is deliberately NOT patched here (doing so used to leak one session's
// context pressure into another's alert classification).
func acquisitionLoop(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			st, ok := readState()
			if !ok {
				continue
			}
			for _, session := range liveSessionSnapshots() {
				snap := readSnapshot(session)
				snap.Rate5hPct = st.FiveH
				snap.Rate7dPct = st.SevenD
				writeSnapshot(session, snap)
			}
		}
	}
}

// liveSessionSnapshots returns the session ids of snapshot files fresh enough
// to belong to a session that may still be running.
func liveSessionSnapshots() []string {
	matches, _ := filepath.Glob(filepath.Join(cullyDir(), "*.snapshot"))
	var out []string
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil || time.Since(info.ModTime()) > hintMaxAge {
			continue
		}
		if st, ok := readSessionStamp(p); ok && st.Session != "" {
			out = append(out, st.Session)
		}
	}
	return out
}

// Sessions have independent advisor slots. One session is always serialized;
// its pending refreshes coalesce so an old backlog cannot monopolize the pool.
const advisorConcurrency = 4

func advisorLoop(ctx context.Context) {
	runAdvisorQueue(ctx, 400*time.Millisecond, runWorkerContext)
}

func runAdvisorQueue(ctx context.Context, interval time.Duration, run func(context.Context, string, string, string)) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	active := map[string]bool{}
	finished := make(chan string, advisorConcurrency)
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		for ctx.Err() == nil && len(active) < advisorConcurrency {
			job, path, ok := nextAdvisorJob(active)
			if !ok {
				break
			}
			active[job.Session] = true
			workers.Add(1)
			go func() {
				defer workers.Done()
				daemonLog("daemon: advisor job session=%s", job.Session)
				run(ctx, job.SignalsPath, job.Session, job.Cwd)
				// Interrupted jobs stay queued for the next daemon start.
				if ctx.Err() == nil {
					_ = os.Remove(path)
				}
				finished <- job.Session
			}()
		}
		select {
		case <-ctx.Done():
			return
		case session := <-finished:
			delete(active, session)
		case <-tick.C:
		}
	}
}

func nextAdvisorJob(active map[string]bool) (advisorJob, string, bool) {
	entries, err := os.ReadDir(jobDir())
	if err != nil {
		return advisorJob{}, "", false
	}
	type pendingJob struct {
		job      advisorJob
		path     string
		modified time.Time
	}
	var jobs []pendingJob
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".job") {
			continue
		}
		path := filepath.Join(jobDir(), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var j advisorJob
		if json.Unmarshal(b, &j) != nil || j.Session == "" || j.SignalsPath == "" {
			_ = os.Remove(path)
			continue
		}
		if active[j.Session] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		jobs = append(jobs, pendingJob{j, path, info.ModTime()})
	}
	if len(jobs) == 0 {
		return advisorJob{}, "", false
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].modified.Equal(jobs[j].modified) {
			return jobs[i].path < jobs[j].path
		}
		return jobs[i].modified.Before(jobs[j].modified)
	})
	// Select the oldest waiting session, but run only its newest refresh.
	chosen := jobs[0]
	for _, candidate := range jobs[1:] {
		if candidate.job.Session == chosen.job.Session {
			_ = os.Remove(chosen.path)
			chosen = candidate
		}
	}
	return chosen.job, chosen.path, true
}

func enqueueAdvisorJob(signalsPath, session, cwd string) error {
	if err := os.MkdirAll(jobDir(), 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%d.job", sessionKey(session), time.Now().UnixNano())
	path := filepath.Join(jobDir(), name)
	b, err := json.Marshal(advisorJob{Session: session, SignalsPath: signalsPath, Cwd: cwd})
	if err != nil {
		return err
	}
	return writeAdvisorFile(path, b)
}

// Queue records and signal snapshots must appear whole, even when a refresh
// arrives while a worker is reading. Files contain only reduced metadata.
func writeAdvisorFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".advisor-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}

// dispatchAdvisor sends work to the daemon queue, or spawns a one-shot worker.
func dispatchAdvisor(signals, session, cwd string) {
	if os.Getenv("CULLY_ANALYZE_DISABLE") == "1" {
		return
	}
	signals = advisorTaskContext(signals, readTask(session))
	if !strings.Contains(signals, "terminal_program=") {
		signals += "\n" + detectTerminalProfile().signals()
	}
	if project := advisorProjectURL(cwd); project != "" {
		signals += "\nproject_url=" + project + "\n"
	}
	if err := os.MkdirAll(logDir(), 0o755); err != nil {
		logf(session, "dispatchAdvisor: mkdir logs: %v", err)
		return
	}
	sigPath := sessionSignalsFile(session)
	if err := writeAdvisorFile(sigPath, []byte(signals)); err != nil {
		logf(session, "dispatchAdvisor: write signals: %v", err)
		return
	}
	deferred := admitAdvisorRequest(session, signals, time.Now())
	snap := readSnapshot(session)
	snap.AdvisorDeferred = deferred
	if snap.Cwd == "" {
		snap.Cwd = cwd
	}
	writeSnapshot(session, snap)
	if deferred != "" {
		recordImpact(session, impactAnalysisSkipped, "", int64(approxTokens(len(sharedAdvisorInstructions)+len(signals))))
		return
	}
	if isDaemonRunning() {
		if err := enqueueAdvisorJob(sigPath, session, cwd); err != nil {
			logf(session, "dispatchAdvisor: enqueue failed: %v — fallback worker", err)
			spawnOneShotWorker(sigPath, session, cwd)
			return
		}
		logf(session, "dispatchAdvisor: queued for daemon")
		return
	}
	spawnOneShotWorker(sigPath, session, cwd)
}

func spawnOneShotWorker(sigPath, session, cwd string) {
	exe, err := os.Executable()
	if err != nil {
		logf(session, "spawnWorker: executable: %v", err)
		return
	}
	cmd := exec.Command(exe, "_internal", "worker", sigPath, session, cwd)
	cmd.Env = append(os.Environ(), "MODEL_HINT_GUARD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	}
	if err := cmd.Start(); err != nil {
		logf(session, "spawnWorker: start: %v", err)
	}
}
