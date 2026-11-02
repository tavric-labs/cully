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
	"path/filepath"
	"regexp"
	"sort"
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

func cullyUserDir() string {
	home, _ := os.UserHomeDir()
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
	if _, err := time.Parse("15:04", j.After); err != nil {
		return j, fmt.Errorf("after must be a 24-hour local time such as \"22:30\" (got %q)", j.After)
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
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s is writable by group or others; run: chmod 600 %s", path, path)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("%s is not owned by you", path)
	}
	raw, err := os.ReadFile(path)
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

type jobState struct {
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

// jobDue decides whether to start a job now, and says why not.
func jobDue(j scheduledJob, st jobState, now time.Time) (bool, string) {
	if st.Running {
		return false, "running"
	}
	slot := latestSlot(now, j.After)
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

// ---- state

type jobStates map[string]*jobState

func readJobStatesLocked() jobStates {
	states := jobStates{}
	if raw, err := os.ReadFile(jobsStateFile()); err == nil {
		_ = json.Unmarshal(raw, &states)
	}
	if states == nil {
		states = jobStates{}
	}
	return states
}

func readJobStates() jobStates {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()
	return readJobStatesLocked()
}

func updateJobState(name string, fn func(*jobState)) error {
	jobStateMu.Lock()
	defer jobStateMu.Unlock()
	states := readJobStatesLocked()
	st := states[name]
	if st == nil {
		st = &jobState{}
		states[name] = st
	}
	fn(st)
	raw, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), jobsStateFile())
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
			return nil, fmt.Errorf("job %s: %w", j.Name, err)
		}
		prog = p
	}
	slot := latestSlot(now, j.After).Format(time.RFC3339)
	var claimErr error
	if err := updateJobState(j.Name, func(s *jobState) {
		if s.Running && (s.PID == 0 || pidAlive(s.PID)) {
			claimErr = errJobRunning
			return
		}
		s.Running, s.PID, s.LastStart, s.Detail = true, 0, now, ""
		if !manual {
			if s.Slot == slot {
				s.Attempts++
			} else {
				s.Slot, s.Attempts = slot, 1
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
		return nil, err
	}
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		return fail(err)
	}
	logPath := jobLogFile(j.Name)
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > jobLogMax {
		_ = os.Truncate(logPath, 0)
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(logf, "--- %s start %s\n", now.Format(time.RFC3339), strings.Join(j.Command, " "))
	cmd := exec.Command(prog, j.Command[1:]...)
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
	_ = updateJobState(j.Name, func(s *jobState) { s.PID = pid })

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
			var err error
			select {
			case err = <-waitc:
			case <-time.After(jobKillGrace):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				err = <-waitc
			}
			res.Exit = exitCodeOf(err)
			if res.Exit == 0 {
				res.Exit = -1
			}
			detail = fmt.Sprintf("timed out after %s", j.timeout().Round(time.Second))
		}
		fmt.Fprintf(logf, "--- %s end exit=%d%s\n", jobsNow().Format(time.RFC3339), res.Exit, map[bool]string{true: " (timed out)"}[res.TimedOut])
		finishJob(j.Name, res, detail)
		done <- res
	}()
	return done, nil
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
		if !st.Running {
			continue
		}
		if _, mine := jobInflight.Load(name); mine {
			continue
		}
		limit := timeouts[name]
		if limit == 0 {
			limit = jobsDefaultTimeoutMin * time.Minute
		}
		if pidAlive(st.PID) && now.Sub(st.LastStart) <= 2*limit {
			continue
		}
		_ = updateJobState(name, func(s *jobState) {
			s.Running, s.PID, s.LastEnd, s.LastExit = false, 0, now, -1
			s.Detail = "lost: the daemon restarted before the job finished"
		})
	}
}

// checkScheduledJobs starts every due job once. A job the daemon has not seen
// before is only recorded, so adding one never runs it immediately.
func checkScheduledJobs(now time.Time) {
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil {
		daemonLog("jobs: %v", err)
		return
	}
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
			res := <-done
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
		case due:
			next = "due now"
		case st.FirstSeen.IsZero():
			next = "starts after the daemon first sees it"
		default:
			slot := latestSlot(now, j.After).AddDate(0, 0, 1)
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
		fmt.Fprintf(w, "%-20s %-8s %-18s %-10s %s\n", j.Name, j.After, last, result, next)
	}
	return nil
}
