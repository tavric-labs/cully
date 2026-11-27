package cully

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Scheduled jobs let the Cully daemon start a command you configure, once per
// day, the way it already starts advisor workers: as a detached, low-priority
// child of the daemon. Cully adds no launchd, cron or systemd entry.
//
// Jobs come only from ~/.cully/jobs.json, a file you write yourself. Cully and
// the agents it supervises never create or edit it, `cully apply` cannot touch
// it, and a file that group or others can write is refused. A job is an exact
// argument vector, not a shell line. The daemon runs only while a Cully session
// has started it, so a job whose time passes while nothing is running simply
// starts at the next opportunity.

const (
	jobClaimGrace          = 2 * time.Minute
	jobsDefaultMinHours    = 20.0
	jobsDefaultTimeoutMin  = 240
	jobsMaxTimeoutMin      = 24 * 60
	jobsDefaultMaxAttempts = 2
	jobsMaxAttempts        = 10
	jobsDefaultRetryMin    = 60
	jobsMaxMinHours        = 24 * 365
	jobLogMax              = 1 << 20
	jobsTick               = 5 * time.Minute
	jobsFirstDelay         = 2 * time.Minute
	jobsMaxRuns            = 1000
	jobHistoryKeep         = 20
	jobTailMax             = 600
)

var (
	jobNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	errJobRunning  = errors.New("job is already running")
	// jobKillGrace is how long a timed-out job gets after SIGTERM before SIGKILL.
	jobKillGrace = 15 * time.Second
	jobInflight  sync.Map // job names started by this process
	jobStateMu   sync.Mutex
	// jobsNow is the clock; tests replace it.
	jobsNow = time.Now
)

// cullyUserDir is ~/.cully, or "" when the home directory is unknown (so a
// relative path is never used for the config or state).
func cullyUserDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cully")
}

func jobsConfigFile() string        { return filepath.Join(cullyUserDir(), "jobs.json") }
func jobsRunDir() string            { return filepath.Join(cullyUserDir(), "jobs") }
func jobsStateFile() string         { return filepath.Join(jobsRunDir(), "state.json") }
func jobLogFile(name string) string { return filepath.Join(jobsRunDir(), name+".log") }

type scheduledJob struct {
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	After          string   `json:"after"`
	MinHours       float64  `json:"min_hours,omitempty"`
	TimeoutMinutes int      `json:"timeout_minutes,omitempty"`
	MaxAttempts    int      `json:"max_attempts,omitempty"`
	RetryMinutes   int      `json:"retry_minutes,omitempty"`
	// Schedule is "" or "daily" (once a day after `after`), "weekdays" (the same, not on
	// Saturday or Sunday), "hourly", "every 15m" to "every 168h", or "in 30m" (one run).
	Schedule string `json:"schedule,omitempty"`
	// MaxRuns ends the job after that many scheduled runs; 0 means it never ends.
	MaxRuns int `json:"max_runs,omitempty"`
	// RunAt is when a one-shot "in" job runs. Cully sets it when the job is saved.
	RunAt time.Time `json:"run_at,omitempty"`

	timeoutOverride time.Duration // tests only
}

func (j scheduledJob) timeout() time.Duration {
	if j.timeoutOverride > 0 {
		return j.timeoutOverride
	}
	return time.Duration(j.TimeoutMinutes) * time.Minute
}

// normalized applies defaults and validates one job.
func (j scheduledJob) normalized() (scheduledJob, error) {
	if !jobNamePattern.MatchString(j.Name) {
		return j, fmt.Errorf("name must be lowercase letters, digits and dashes (got %q)", j.Name)
	}
	if len(j.Command) == 0 || strings.TrimSpace(j.Command[0]) == "" {
		return j, errors.New("command must be a non-empty argument list, for example [\"python3\", \"~/script.py\"]")
	}
	home, _ := os.UserHomeDir()
	cmd := make([]string, len(j.Command))
	for i, a := range j.Command {
		if strings.HasPrefix(a, "~/") {
			a = filepath.Join(home, a[2:])
		}
		cmd[i] = a
	}
	j.Command = cmd
	if prog := cmd[0]; strings.Contains(prog, "/") && !filepath.IsAbs(prog) {
		return j, fmt.Errorf("command[0] %q must be an absolute path, ~/path, or a bare program name found on PATH; a relative path would depend on where the daemon was started", prog)
	}
	kind, _, err := jobSchedule(j.Schedule)
	if err != nil {
		return j, err
	}
	j.Schedule = strings.ToLower(strings.TrimSpace(j.Schedule))
	if kind == jobDaily || kind == jobWeekdays || j.After != "" {
		if _, err := time.Parse("15:04", j.After); err != nil {
			return j, fmt.Errorf("after must be a 24-hour local time such as \"22:30\" (got %q)", j.After)
		}
	}
	if kind == jobOnce && j.RunAt.IsZero() {
		return j, errors.New("a one-shot job needs run_at, which Cully sets when it saves the job")
	}
	if j.MaxRuns < 0 || j.MaxRuns > jobsMaxRuns {
		return j, fmt.Errorf("max_runs must be between 1 and %d", jobsMaxRuns)
	}
	if j.MinHours < 0 || j.MinHours > jobsMaxMinHours {
		return j, errors.New("min_hours must be between 0 and 8760")
	}
	if j.MinHours == 0 {
		j.MinHours = jobsDefaultMinHours
	}
	if j.TimeoutMinutes < 0 || j.TimeoutMinutes > jobsMaxTimeoutMin {
		return j, fmt.Errorf("timeout_minutes must be between 1 and %d", jobsMaxTimeoutMin)
	}
	if j.TimeoutMinutes == 0 {
		j.TimeoutMinutes = jobsDefaultTimeoutMin
	}
	if j.MaxAttempts < 0 || j.MaxAttempts > jobsMaxAttempts {
		return j, fmt.Errorf("max_attempts must be between 1 and %d", jobsMaxAttempts)
	}
	if j.MaxAttempts == 0 {
		j.MaxAttempts = jobsDefaultMaxAttempts
	}
	if j.RetryMinutes < 0 {
		return j, errors.New("retry_minutes must not be negative")
	}
	if j.RetryMinutes == 0 {
		j.RetryMinutes = jobsDefaultRetryMin
	}
	return j, nil
}

// loadJobs reads and validates jobs.json. A missing file means no jobs. A file
// that others can write, or that another user owns, is refused: the daemon runs
// what it lists.
func loadJobs(path string) ([]scheduledJob, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("cannot locate ~/.cully/jobs.json: the home directory is unknown")
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat() // the file actually opened, not a path that could change
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s is writable by group or others; run: chmod 600 %s", path, path)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("%s is not owned by you", path)
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Jobs []scheduledJob `json:"jobs"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s: unexpected data after the top-level object", path)
	}
	seen := map[string]bool{}
	var out []scheduledJob
	for i, j := range cfg.Jobs {
		n, err := j.normalized()
		if err != nil {
			return nil, fmt.Errorf("%s: job %d: %w", path, i+1, err)
		}
		if seen[n.Name] {
			return nil, fmt.Errorf("%s: duplicate job name %q", path, n.Name)
		}
		seen[n.Name] = true
		out = append(out, n)
	}
	return out, nil
}

// ---- schedule

// latestSlot is the most recent occurrence of the local clock time after at or
// before now.
func latestSlot(now time.Time, after string) time.Time {
	t, _ := time.Parse("15:04", after)
	slot := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if slot.After(now) {
		slot = time.Date(now.Year(), now.Month(), now.Day()-1, t.Hour(), t.Minute(), 0, 0, now.Location())
	}
	return slot
}

const (
	jobDaily    = "daily"
	jobWeekdays = "weekdays"
	jobInterval = "interval"
	jobOnce     = "once"
)

var jobInRe = regexp.MustCompile(`^in ([0-9]{1,3})(m|h)$`)

// jobSchedule reads a job's schedule. The forms are those of an agent ("hourly", "daily",
// "weekdays", "every 15m" to "every 168h", see uiScheduleInterval) plus "in 30m" (one run, 1 minute
// to 168 hours from when the job is saved). An empty schedule is "daily", at the `after` time.
func jobSchedule(s string) (kind string, every time.Duration, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if m := jobInRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		d := time.Duration(n) * time.Minute
		if m[2] == "h" {
			d = time.Duration(n) * time.Hour
		}
		if d < time.Minute || d > 7*24*time.Hour {
			return "", 0, errors.New("in must be between 1m and 168h")
		}
		return jobOnce, d, nil
	}
	switch s {
	case "", "daily":
		return jobDaily, 24 * time.Hour, nil
	case "weekdays":
		return jobWeekdays, 24 * time.Hour, nil
	}
	d, ok := uiScheduleInterval(s)
	if !ok || d <= 0 {
		return "", 0, fmt.Errorf("schedule must be daily, weekdays, hourly, every 15m to every 168h, or in 30m (got %q)", s)
	}
	return jobInterval, d, nil
}

// jobScheduleKind is the kind of a job that has already been validated.
func jobScheduleKind(j scheduledJob) (string, time.Duration) {
	k, d, _ := jobSchedule(j.Schedule)
	return k, d
}

// jobLatestSlot is the latest daily slot, stepping back over a weekend for "weekdays".
func jobLatestSlot(j scheduledJob, now time.Time) time.Time {
	slot := latestSlot(now, j.After)
	if j.Schedule == jobWeekdays {
		for slot.Weekday() == time.Saturday || slot.Weekday() == time.Sunday {
			slot = time.Date(slot.Year(), slot.Month(), slot.Day()-1, slot.Hour(), slot.Minute(), 0, 0, slot.Location())
		}
	}
	return slot
}

type jobState struct {
	Paused      bool      `json:"paused,omitempty"`
	Runs        int       `json:"runs,omitempty"` // scheduled runs started, for max_runs
	FirstSeen   time.Time `json:"first_seen,omitempty"`
	LastStart   time.Time `json:"last_start,omitempty"`
	LastEnd     time.Time `json:"last_end,omitempty"`
	LastSuccess time.Time `json:"last_success,omitempty"`
	LastExit    int       `json:"last_exit"`
	Running     bool      `json:"running,omitempty"`
	PID         int       `json:"pid,omitempty"`
	Slot        string    `json:"slot,omitempty"`
	Attempts    int       `json:"attempts,omitempty"`
	Detail      string    `json:"detail,omitempty"`
}

// jobDue decides whether to start a job now, and says why not. A job that is paused, still
// running, or past its run limit is never due. Whatever the schedule, a long downtime gives one
// catch-up run, not one per missed time.
func jobDue(j scheduledJob, st jobState, now time.Time) (bool, string) {
	if st.Paused {
		return false, "paused"
	}
	if st.Running {
		return false, "running" // skip this tick; the earlier run is still going
	}
	if j.MaxRuns > 0 && st.Runs >= j.MaxRuns {
		return false, "run limit reached"
	}
	// A stored time in the future means the clock was wrong once. Never let it
	// block a job: ignore a future success, and bound the others to now.
	if st.LastSuccess.After(now) {
		st.LastSuccess = time.Time{}
	}
	if st.FirstSeen.After(now) {
		st.FirstSeen = now
	}
	if st.LastEnd.After(now) {
		st.LastEnd = now
	}
	if st.LastStart.After(now) {
		st.LastStart = now
	}
	retryReady := func() (bool, string) {
		if st.Attempts >= j.MaxAttempts {
			return false, "attempt limit reached for this slot"
		}
		if now.Sub(st.LastEnd) < time.Duration(j.RetryMinutes)*time.Minute {
			return false, "waiting to retry after a failed attempt"
		}
		return true, "due"
	}
	kind, every := jobScheduleKind(j)
	switch kind {
	case jobOnce:
		if !st.LastSuccess.IsZero() {
			return false, "finished"
		}
		if now.Before(j.RunAt) {
			return false, "waiting for its time"
		}
		if st.Slot == "once" && st.Attempts > 0 {
			return retryReady()
		}
		return true, "due"
	case jobInterval:
		if st.Slot != "" && strings.HasPrefix(st.Slot, "i:") && st.LastExit != 0 && !st.LastEnd.IsZero() && st.Attempts > 0 && st.Attempts < j.MaxAttempts {
			return retryReady()
		}
		anchor := st.LastStart
		if anchor.IsZero() {
			anchor = st.FirstSeen
		}
		if anchor.IsZero() {
			return false, "waiting for the daemon to see it"
		}
		if now.Sub(anchor) < every {
			return false, "not yet time for the next run"
		}
		return true, "due"
	}
	slot := jobLatestSlot(j, now)
	if st.LastSuccess.IsZero() {
		if st.FirstSeen.IsZero() || !slot.After(st.FirstSeen) {
			return false, "waiting for the first slot after it was added"
		}
	} else {
		if !st.LastSuccess.Before(slot) {
			return false, "already succeeded since the latest slot"
		}
		if now.Sub(st.LastSuccess) < time.Duration(j.MinHours*float64(time.Hour)) {
			return false, "minimum spacing since the last success"
		}
	}
	if st.Slot == slot.Format(time.RFC3339) {
		if st.Attempts >= j.MaxAttempts {
			return false, "attempt limit reached for this slot"
		}
		if st.Attempts > 0 && now.Sub(st.LastEnd) < time.Duration(j.RetryMinutes)*time.Minute {
			return false, "waiting to retry after a failed attempt"
		}
	}
	return true, "due"
}

// jobNext says when a job next runs, for a person: a time, or why it does not.
func jobNext(j scheduledJob, st jobState, now time.Time) time.Time {
	if st.Paused || st.Running {
		return time.Time{}
	}
	if j.MaxRuns > 0 && st.Runs >= j.MaxRuns {
		return time.Time{}
	}
	kind, every := jobScheduleKind(j)
	switch kind {
	case jobOnce:
		if !st.LastSuccess.IsZero() {
			return time.Time{}
		}
		return j.RunAt
	case jobInterval:
		anchor := st.LastStart
		if anchor.IsZero() {
			anchor = st.FirstSeen
		}
		if anchor.IsZero() {
			return time.Time{}
		}
		if n := anchor.Add(every); n.After(now) {
			return n
		}
		return now
	}
	if due, _ := jobDue(j, st, now); due {
		return now
	}
	next := jobLatestSlot(j, now).AddDate(0, 0, 1)
	for !next.After(now) || (j.Schedule == jobWeekdays && (next.Weekday() == time.Saturday || next.Weekday() == time.Sunday)) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// ---- state

type jobStates map[string]*jobState

// readJobStatesLocked reads the state file. One that does not parse is moved
// aside, never overwritten, so a corrupt file cannot silently erase what ran.
func readJobStatesLocked() jobStates {
	raw, err := os.ReadFile(jobsStateFile())
	if err != nil {
		return jobStates{}
	}
	states := jobStates{}
	if err := json.Unmarshal(raw, &states); err != nil || states == nil {
		_ = os.Rename(jobsStateFile(), jobsStateFile()+".corrupt-"+time.Now().Format("20060102T150405.000000"))
		return jobStates{}
	}
	for name, st := range states {
		if st == nil {
			delete(states, name)
		}
	}
	return states
}

// lockJobStateFile takes an exclusive advisory lock shared with every other
// Cully process (the daemon and `cully jobs run`) that updates the state.
func lockJobStateFile() (func(), error) {
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(jobsRunDir(), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func readJobStates() jobStates {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()
	return readJobStatesLocked()
}

func updateJobState(name string, fn func(*jobState)) error {
	return mutateJobStates(func(states jobStates) {
		st := states[name]
		if st == nil {
			st = &jobState{}
			states[name] = st
		}
		fn(st)
	})
}

// mutateJobStates applies fn to the whole state under both the in-process mutex
// and the cross-process file lock, then replaces the file atomically.
func mutateJobStates(fn func(jobStates)) error {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()
	unlock, err := lockJobStateFile()
	if err != nil {
		return err
	}
	defer unlock()
	states := readJobStatesLocked()
	fn(states)
	raw, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(jobsRunDir(), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), jobsStateFile())
}

// pruneJobStates forgets jobs that are no longer configured (and not running),
// so removing and re-adding a job starts it fresh.
func pruneJobStates(keep map[string]bool) {
	_ = mutateJobStates(func(states jobStates) {
		for name, st := range states {
			if !keep[name] && !st.Running {
				delete(states, name)
			}
		}
	})
}

// ---- running

type jobResult struct {
	Exit     int
	TimedOut bool
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// groupAlive reports whether any process is left in the job's process group.
func groupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		return ee.ExitCode()
	}
	return -1
}

func finishJob(name string, res jobResult, detail string) {
	_ = updateJobState(name, func(s *jobState) {
		s.Running, s.PID = false, 0
		s.LastEnd, s.LastExit, s.Detail = jobsNow(), res.Exit, detail
		if res.Exit == 0 && !res.TimedOut {
			s.LastSuccess = s.LastEnd
		}
	})
}

// startJob claims the job and starts it detached. manual runs (cully jobs run)
// do not count against the slot's attempt limit. tee, when set, also receives
// the job's output. The channel yields the result when the job ends.
func startJob(j scheduledJob, now time.Time, manual bool, tee io.Writer) (<-chan jobResult, error) {
	prog := j.Command[0]
	if !filepath.IsAbs(prog) {
		p, err := exec.LookPath(prog)
		if err != nil {
			err = fmt.Errorf("job %s: %w", j.Name, err)
			_ = updateJobState(j.Name, func(s *jobState) { s.Detail = err.Error() })
			return nil, err
		}
		prog = p
	}
	kind, _ := jobScheduleKind(j)
	var claimErr error
	if err := updateJobState(j.Name, func(s *jobState) {
		if s.Running && ((s.PID == 0 && now.Sub(s.LastStart) < jobClaimGrace) || pidAlive(s.PID)) {
			claimErr = errJobRunning
			return
		}
		s.Running, s.PID, s.LastStart, s.Detail = true, 0, now, ""
		if !manual {
			var slot string
			switch kind {
			case jobOnce:
				slot = "once"
			case jobInterval:
				slot = "i:" + now.Format(time.RFC3339)
				if strings.HasPrefix(s.Slot, "i:") && s.LastExit != 0 && !s.LastEnd.IsZero() && s.Attempts > 0 && s.Attempts < j.MaxAttempts {
					slot = s.Slot // a retry of the run that failed
				}
			default:
				slot = jobLatestSlot(j, now).Format(time.RFC3339)
			}
			if s.Slot == slot {
				s.Attempts++
			} else {
				s.Slot, s.Attempts = slot, 1
				s.Runs++
			}
		}
	}); err != nil {
		return nil, err
	}
	if claimErr != nil {
		return nil, claimErr
	}
	fail := func(err error) (<-chan jobResult, error) {
		finishJob(j.Name, jobResult{Exit: -1}, err.Error())
		recordJobRun(j.Name, jobRunRec{Start: now, End: jobsNow(), Status: "failed", Exit: -1, Tail: err.Error(), Manual: manual})
		return nil, err
	}
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		return fail(err)
	}
	logPath := jobLogFile(j.Name)
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > jobLogMax {
		_ = os.Rename(logPath, logPath+".1") // keep the previous run's output
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fail(err)
	}
	logFrom := int64(0)
	if fi, err := logf.Stat(); err == nil {
		logFrom = fi.Size()
	}
	fmt.Fprintf(logf, "--- %s start %s\n", now.Format(time.RFC3339), strings.Join(j.Command, " "))
	cmd := exec.Command(prog, j.Command[1:]...)
	cmd.Dir, _ = os.UserHomeDir() // a fixed directory, not whichever session started the daemon
	cmd.Env = append(os.Environ(), "CULLY_JOB="+j.Name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out io.Writer = logf
	if tee != nil {
		out = io.MultiWriter(logf, tee)
	}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		logf.Close()
		return fail(err)
	}
	pid := cmd.Process.Pid
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 10)
	jobInflight.Store(j.Name, true)
	_ = updateJobState(j.Name, func(s *jobState) { s.Running, s.PID = true, pid })

	done := make(chan jobResult, 1)
	go func() {
		defer logf.Close()
		defer jobInflight.Delete(j.Name)
		waitc := make(chan error, 1)
		go func() { waitc <- cmd.Wait() }()
		var res jobResult
		detail := ""
		select {
		case err := <-waitc:
			res.Exit = exitCodeOf(err)
		case <-time.After(j.timeout()):
			res.TimedOut = true
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			// Give the group a moment to leave, then kill whatever is still in
			// it, including a descendant that ignored SIGTERM after the leader
			// exited.
			for deadline := time.Now().Add(jobKillGrace); time.Now().Before(deadline) && groupAlive(pid); {
				time.Sleep(50 * time.Millisecond)
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			err := <-waitc
			res.Exit = exitCodeOf(err)
			if res.Exit == 0 {
				res.Exit = -1
			}
			detail = fmt.Sprintf("timed out after %s", j.timeout().Round(time.Second))
		}
		fmt.Fprintf(logf, "--- %s end exit=%d%s\n", jobsNow().Format(time.RFC3339), res.Exit, map[bool]string{true: " (timed out)"}[res.TimedOut])
		finishJob(j.Name, res, detail)
		status := "ok"
		if res.TimedOut {
			status = "timeout"
		} else if res.Exit != 0 {
			status = "failed"
		}
		tail := jobLogTail(logPath, logFrom)
		if tail == "" {
			tail = detail
		}
		recordJobRun(j.Name, jobRunRec{Start: now, End: jobsNow(), Status: status, Exit: res.Exit, Tail: tail, Manual: manual})
		done <- res
	}()
	return done, nil
}

// ---- run history

// jobRunRec is one run in a job's history, kept in the database: the last jobHistoryKeep per job.
type jobRunRec struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Status string    `json:"status"` // ok, failed or timeout
	Exit   int       `json:"exit"`
	Tail   string    `json:"tail,omitempty"` // the end of the output, with secrets redacted
	Manual bool      `json:"manual,omitempty"`
}

func jobHistoryNS(name string) string { return "jobruns:" + name }

func recordJobRun(name string, r jobRunRec) {
	uiDB().logAdd(jobHistoryNS(name), r, jobHistoryKeep)
}

// jobHistory returns a job's runs, newest first.
func jobHistory(name string) []jobRunRec {
	var out []jobRunRec
	raw := uiDB().logAll(jobHistoryNS(name))
	for i := len(raw) - 1; i >= 0; i-- {
		var r jobRunRec
		if json.Unmarshal(raw[i], &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// jobFailureStreak counts the failed runs in a row, newest first.
func jobFailureStreak(runs []jobRunRec) int {
	n := 0
	for _, r := range runs {
		if r.Status == "ok" {
			break
		}
		n++
	}
	return n
}

// jobLogTail is the end of what a run wrote to the log from offset from, without the marker lines
// and with secrets redacted.
func jobLogTail(path string, from int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() <= from {
		return ""
	}
	if fi.Size()-from > 8192 {
		from = fi.Size() - 8192
	}
	buf := make([]byte, fi.Size()-from)
	n, _ := f.ReadAt(buf, from)
	var keep []string
	for _, ln := range strings.Split(string(buf[:n]), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" && !strings.HasPrefix(ln, "--- ") {
			keep = append(keep, ln)
		}
	}
	out := redactSecrets(strings.ToValidUTF8(strings.Join(keep, "\n"), ""))
	if r := []rune(out); len(r) > jobTailMax {
		out = "…" + string(r[len(r)-jobTailMax:])
	}
	return out
}

// setJobPaused pauses or resumes a job without deleting it.
func setJobPaused(name string, paused bool) error {
	return updateJobState(name, func(s *jobState) {
		s.Paused = paused
		if !paused && s.FirstSeen.IsZero() {
			s.FirstSeen = jobsNow()
		}
	})
}

// reconcileJobs repairs bookkeeping for jobs this process did not start (a job
// the daemon launched before it restarted). A job whose process is gone is
// marked failed. One that has outlived twice its timeout is treated as lost
// too, which also covers a recycled process id; it is never killed from here.
func reconcileJobs(jobs []scheduledJob, now time.Time) {
	timeouts := map[string]time.Duration{}
	for _, j := range jobs {
		timeouts[j.Name] = j.timeout()
	}
	for name, st := range readJobStates() {
		if st == nil || !st.Running {
			continue
		}
		if _, mine := jobInflight.Load(name); mine {
			continue
		}
		if st.PID == 0 && now.Sub(st.LastStart) < jobClaimGrace {
			continue // another process is starting this job right now
		}
		limit := timeouts[name]
		if limit == 0 {
			limit = jobsDefaultTimeoutMin * time.Minute
		}
		if pidAlive(st.PID) && now.Sub(st.LastStart) <= 2*limit {
			continue
		}
		recordJobRun(name, jobRunRec{Start: st.LastStart, End: now, Status: "failed", Exit: -1, Tail: "lost: the daemon restarted before the job finished"})
		_ = updateJobState(name, func(s *jobState) {
			s.Running, s.PID, s.LastEnd, s.LastExit = false, 0, now, -1
			s.Detail = "lost: the daemon restarted before the job finished"
		})
	}
}

// checkScheduledJobs starts every due job once. A job the daemon has not seen
// before is only recorded, so adding one never runs it immediately.
func checkScheduledJobs(now time.Time) {
	if uiPaused() {
		return
	}
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil {
		daemonLog("jobs: %v", err)
		return
	}
	keep := map[string]bool{}
	for _, j := range jobs {
		keep[j.Name] = true
	}
	pruneJobStates(keep)
	reconcileJobs(jobs, now)
	states := readJobStates()
	for _, j := range jobs {
		st := states[j.Name]
		if st == nil || st.FirstSeen.IsZero() {
			_ = updateJobState(j.Name, func(s *jobState) { s.FirstSeen = now })
			continue
		}
		if due, _ := jobDue(j, *st, now); !due {
			continue
		}
		if _, err := startJob(j, now, false, nil); err != nil {
			daemonLog("jobs: %s: %v", j.Name, err)
			continue
		}
		daemonLog("jobs: started %s", j.Name)
	}
}

// scheduledJobLoop starts due jobs. It mirrors selfImproveLoop: a short delay,
// then a periodic check that survives daemon restarts through the state file.
func scheduledJobLoop(ctx context.Context) {
	if os.Getenv("CULLY_JOBS_DISABLE") == "1" {
		return
	}
	check := func() { checkScheduledJobs(jobsNow()) }
	select {
	case <-ctx.Done():
		return
	case <-time.After(jobsFirstDelay):
	}
	check()
	tick := time.NewTicker(jobsTick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			check()
		}
	}
}

// ---- cully jobs

const jobsHelp = `No scheduled jobs. Create ~/.cully/jobs.json yourself (chmod 600):

  {"jobs": [{"name": "nightly-audit", "command": ["python3", "~/nightly-audit/run.py"], "after": "22:30"}]}

after is a local 24-hour time. Optional: min_hours (20), timeout_minutes (240),
max_attempts per slot (2), retry_minutes (60). The daemon starts a due job
while a Cully session is running; "cully jobs run NAME" starts one now.`

// RunJobs implements `cully jobs [list|run NAME]`.
func RunJobs(w io.Writer, args []string) error {
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "run" {
		if len(args) != 2 {
			return errors.New("usage: cully jobs run NAME")
		}
		for _, j := range jobs {
			if j.Name != args[1] {
				continue
			}
			done, err := startJob(j, jobsNow(), true, w)
			if err != nil {
				return err
			}
			sigc := make(chan os.Signal, 1)
			signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(sigc)
			var res jobResult
			select {
			case res = <-done:
			case <-sigc: // stop the job with us instead of orphaning it
				if st := readJobStates()[j.Name]; st != nil && st.PID > 0 {
					_ = syscall.Kill(-st.PID, syscall.SIGTERM)
				}
				res = <-done
			}
			if res.TimedOut {
				return fmt.Errorf("job %s timed out", j.Name)
			}
			if res.Exit != 0 {
				return fmt.Errorf("job %s exited with %d", j.Name, res.Exit)
			}
			fmt.Fprintf(w, "job %s finished\n", j.Name)
			return nil
		}
		return fmt.Errorf("no job named %q in %s", args[1], jobsConfigFile())
	}
	if len(args) > 0 && args[0] != "list" {
		return errors.New("usage: cully jobs [list|run NAME]")
	}
	if len(jobs) == 0 {
		fmt.Fprintln(w, jobsHelp)
		return nil
	}
	sort.Slice(jobs, func(a, b int) bool { return jobs[a].Name < jobs[b].Name })
	states := readJobStates()
	now := jobsNow()
	fmt.Fprintf(w, "%-20s %-8s %-18s %-10s %s\n", "NAME", "AFTER", "LAST RUN", "RESULT", "NEXT")
	for _, j := range jobs {
		st := states[j.Name]
		if st == nil {
			st = &jobState{}
		}
		last, result := "never", "-"
		if !st.LastEnd.IsZero() {
			last = st.LastEnd.Format("2006-01-02 15:04")
			result = "ok"
			if st.LastExit != 0 {
				result = fmt.Sprintf("exit %d", st.LastExit)
			}
		}
		next := ""
		switch due, why := jobDue(j, *st, now); {
		case st.Running:
			next, result = "running", "running"
		case st.Paused:
			next = "paused"
		case why == "run limit reached" || why == "finished":
			next = why
		case due:
			next = "due now"
		case st.FirstSeen.IsZero():
			next = "starts after the daemon first sees it"
		case strings.HasPrefix(why, "waiting to retry"):
			next = st.LastEnd.Add(time.Duration(j.RetryMinutes)*time.Minute).Format("2006-01-02 15:04") + " (retry)"
		case strings.HasPrefix(why, "attempt limit"):
			next = latestSlot(now, j.After).AddDate(0, 0, 1).Format("2006-01-02 15:04") + " (attempt limit reached)"
		case func() bool { k, _ := jobScheduleKind(j); return k == jobInterval || k == jobOnce }():
			next = jobNext(j, *st, now).Format("2006-01-02 15:04") + " (" + why + ")"
		default:
			slot := jobLatestSlot(j, now).AddDate(0, 0, 1)
			if !st.LastSuccess.IsZero() {
				if m := st.LastSuccess.Add(time.Duration(j.MinHours * float64(time.Hour))); m.After(slot) {
					slot = latestSlot(m, j.After)
					if slot.Before(m) {
						slot = slot.AddDate(0, 0, 1)
					}
				}
			}
			next = slot.Format("2006-01-02 15:04") + " (" + why + ")"
		}
		when := j.After
		if j.Schedule != "" && j.Schedule != jobDaily && j.Schedule != jobWeekdays {
			when = j.Schedule
		}
		fmt.Fprintf(w, "%-20s %-8s %-18s %-10s %s\n", j.Name, when, last, result, next)
	}
	return nil
}
