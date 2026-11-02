package cully

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func jobsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeJobsFile(t *testing.T, home, body string, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(home, ".cully")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "jobs.json")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func jobAt(day, clock string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-11-"+day+" "+clock, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestLatestSlot(t *testing.T) {
	cases := []struct{ now, want time.Time }{
		{jobAt("02", "17:00"), jobAt("01", "22:30")},
		{jobAt("02", "22:30"), jobAt("02", "22:30")},
		{jobAt("02", "23:59"), jobAt("02", "22:30")},
		{jobAt("03", "00:05"), jobAt("02", "22:30")},
	}
	for _, c := range cases {
		if got := latestSlot(c.now, "22:30"); !got.Equal(c.want) {
			t.Errorf("latestSlot(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}

func testJob() scheduledJob {
	j := scheduledJob{Name: "nightly-audit", Command: []string{"/bin/true"}, After: "22:30"}
	n, err := j.normalized()
	if err != nil {
		panic(err)
	}
	return n
}

func TestJobDue(t *testing.T) {
	j := testJob()
	first := jobAt("02", "09:00")
	cases := []struct {
		name string
		st   jobState
		now  time.Time
		want bool
	}{
		{"running is never due", jobState{FirstSeen: first, Running: true}, jobAt("02", "23:00"), false},
		{"never run waits for the first slot after it was first seen", jobState{FirstSeen: first}, jobAt("02", "10:00"), false},
		{"first slot after first seen is due", jobState{FirstSeen: first}, jobAt("02", "22:31"), true},
		{"already succeeded since this slot", jobState{FirstSeen: first, LastSuccess: jobAt("02", "22:40")}, jobAt("02", "23:30"), false},
		{"missed night catches up on wake", jobState{FirstSeen: first, LastSuccess: jobAt("01", "22:40")}, jobAt("03", "09:00"), true},
		{"minimum spacing holds after an early manual run", jobState{FirstSeen: first, LastSuccess: jobAt("02", "21:00")}, jobAt("02", "22:31"), false},
		{"next slot after spacing elapsed", jobState{FirstSeen: first, LastSuccess: jobAt("02", "21:00")}, jobAt("03", "22:31"), true},
		{"failed attempt waits out the retry cooldown", jobState{FirstSeen: first, LastSuccess: jobAt("01", "22:40"), Slot: jobAt("02", "22:30").Format(time.RFC3339), Attempts: 1, LastEnd: jobAt("02", "22:50"), LastExit: 1}, jobAt("02", "23:10"), false},
		{"failed attempt retried after cooldown", jobState{FirstSeen: first, LastSuccess: jobAt("01", "22:40"), Slot: jobAt("02", "22:30").Format(time.RFC3339), Attempts: 1, LastEnd: jobAt("02", "22:50"), LastExit: 1}, jobAt("02", "23:55"), true},
		{"attempt cap per slot", jobState{FirstSeen: first, LastSuccess: jobAt("01", "22:40"), Slot: jobAt("02", "22:30").Format(time.RFC3339), Attempts: 2, LastEnd: jobAt("02", "23:00"), LastExit: 1}, jobAt("03", "02:00"), false},
		{"a new slot resets the attempt count", jobState{FirstSeen: first, LastSuccess: jobAt("01", "22:40"), Slot: jobAt("02", "22:30").Format(time.RFC3339), Attempts: 2, LastEnd: jobAt("02", "23:00"), LastExit: 1}, jobAt("03", "22:31"), true},
	}
	for _, c := range cases {
		if got, why := jobDue(j, c.st, c.now); got != c.want {
			t.Errorf("%s: due=%v (%s), want %v", c.name, got, why, c.want)
		}
	}
}

func TestLoadJobs(t *testing.T) {
	home := jobsHome(t)
	path := filepath.Join(home, ".cully", "jobs.json")

	if jobs, err := loadJobs(path); err != nil || jobs != nil {
		t.Fatalf("missing file: jobs=%v err=%v, want none", jobs, err)
	}

	writeJobsFile(t, home, `{"jobs":[{"name":"nightly-audit","command":["python3","~/nightly-audit/run.py"],"after":"22:30"}]}`, 0o600)
	jobs, err := loadJobs(path)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("valid file: %v %v", jobs, err)
	}
	j := jobs[0]
	if j.MinHours != 20 || j.TimeoutMinutes != 240 || j.MaxAttempts != 2 || j.RetryMinutes != 60 {
		t.Errorf("defaults not applied: %+v", j)
	}
	if want := filepath.Join(home, "nightly-audit", "run.py"); j.Command[1] != want {
		t.Errorf("~ not expanded: %q want %q", j.Command[1], want)
	}

	bad := map[string]string{
		"bad name":        `{"jobs":[{"name":"Bad Name","command":["/bin/true"],"after":"22:30"}]}`,
		"no command":      `{"jobs":[{"name":"a","command":[],"after":"22:30"}]}`,
		"empty program":   `{"jobs":[{"name":"a","command":[""],"after":"22:30"}]}`,
		"bad time":        `{"jobs":[{"name":"a","command":["/bin/true"],"after":"25:99"}]}`,
		"duplicate":       `{"jobs":[{"name":"a","command":["/bin/true"],"after":"22:30"},{"name":"a","command":["/bin/true"],"after":"22:30"}]}`,
		"unknown field":   `{"jobs":[{"name":"a","command":["/bin/true"],"after":"22:30","shell":"rm -rf /"}]}`,
		"negative hours":  `{"jobs":[{"name":"a","command":["/bin/true"],"after":"22:30","min_hours":-1}]}`,
		"not json":        `nope`,
		"timeout too big": `{"jobs":[{"name":"a","command":["/bin/true"],"after":"22:30","timeout_minutes":100000}]}`,
	}
	for name, body := range bad {
		writeJobsFile(t, home, body, 0o600)
		if _, err := loadJobs(path); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadJobsRefusesWritableByOthers(t *testing.T) {
	home := jobsHome(t)
	path := writeJobsFile(t, home, `{"jobs":[{"name":"a","command":["/bin/true"],"after":"22:30"}]}`, 0o666)
	_, err := loadJobs(path)
	if err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("err = %v, want a refusal about group/other-writable", err)
	}
}

func waitJob(t *testing.T, done <-chan jobResult) jobResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(20 * time.Second):
		t.Fatal("job did not finish")
		return jobResult{}
	}
}

func TestStartJobRecordsSuccessAndLogsOutput(t *testing.T) {
	jobsHome(t)
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "echo hello-from-$CULLY_JOB; exit 0"}
	done, err := startJob(j, time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := waitJob(t, done); r.Exit != 0 || r.TimedOut {
		t.Fatalf("result = %+v", r)
	}
	st := readJobStates()[j.Name]
	if st == nil || st.Running || st.LastExit != 0 || st.LastSuccess.IsZero() || st.LastEnd.IsZero() {
		t.Fatalf("state = %+v", st)
	}
	if st.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", st.Attempts)
	}
	log, _ := os.ReadFile(jobLogFile(j.Name))
	if !strings.Contains(string(log), "hello-from-nightly-audit") {
		t.Errorf("log = %q", log)
	}
}

func TestStartJobRecordsFailureWithoutSuccess(t *testing.T) {
	jobsHome(t)
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "exit 3"}
	done, err := startJob(j, time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := waitJob(t, done); r.Exit != 3 {
		t.Fatalf("exit = %d", r.Exit)
	}
	st := readJobStates()[j.Name]
	if st.LastExit != 3 || !st.LastSuccess.IsZero() {
		t.Fatalf("state = %+v", st)
	}
}

func TestStartJobRefusesOverlap(t *testing.T) {
	jobsHome(t)
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "sleep 3"}
	done, err := startJob(j, time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startJob(j, time.Now(), false, nil); !errors.Is(err, errJobRunning) {
		t.Fatalf("second start err = %v, want errJobRunning", err)
	}
	waitJob(t, done)
}

func TestStartJobKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	home := jobsHome(t)
	marker := filepath.Join(home, "child-survived")
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "(sleep 4; touch " + marker + ") & wait"}
	old := jobKillGrace
	jobKillGrace = 200 * time.Millisecond
	defer func() { jobKillGrace = old }()
	j.timeoutOverride = 500 * time.Millisecond
	done, err := startJob(j, time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := waitJob(t, done)
	if !r.TimedOut {
		t.Fatalf("result = %+v, want timed out", r)
	}
	time.Sleep(4500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a grandchild outlived the timeout; the process group was not killed")
	}
	if st := readJobStates()[j.Name]; st.Running || !st.LastSuccess.IsZero() || st.LastExit == 0 {
		t.Fatalf("state = %+v", st)
	}
}

func TestManualRunDoesNotConsumeSlotAttempts(t *testing.T) {
	jobsHome(t)
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "exit 0"}
	done, err := startJob(j, time.Now(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, done)
	if st := readJobStates()[j.Name]; st.Attempts != 0 || st.LastSuccess.IsZero() {
		t.Fatalf("state = %+v, want attempts 0 and a recorded success", st)
	}
}

func TestReconcileMarksALostJobFailedAndKeepsALiveOne(t *testing.T) {
	jobsHome(t)
	j := testJob()
	if err := updateJobState(j.Name, func(s *jobState) { s.Running, s.PID, s.LastStart = true, 999999, time.Now().Add(-time.Hour) }); err != nil {
		t.Fatal(err)
	}
	reconcileJobs([]scheduledJob{j}, time.Now())
	st := readJobStates()[j.Name]
	if st.Running || st.LastExit == 0 || !st.LastSuccess.IsZero() {
		t.Fatalf("lost job state = %+v", st)
	}

	if err := updateJobState(j.Name, func(s *jobState) { s.Running, s.PID, s.LastStart = true, os.Getpid(), time.Now() }); err != nil {
		t.Fatal(err)
	}
	reconcileJobs([]scheduledJob{j}, time.Now())
	if st := readJobStates()[j.Name]; !st.Running {
		t.Fatalf("a live job was marked finished: %+v", st)
	}
}

func TestRunJobsListAndRun(t *testing.T) {
	home := jobsHome(t)
	var out bytes.Buffer
	if err := RunJobs(&out, nil); err != nil || !strings.Contains(out.String(), "jobs.json") {
		t.Fatalf("empty list: %q %v", out.String(), err)
	}
	writeJobsFile(t, home, `{"jobs":[{"name":"nightly-audit","command":["/bin/sh","-c","echo ran-it"],"after":"22:30"}]}`, 0o600)
	out.Reset()
	if err := RunJobs(&out, []string{"list"}); err != nil || !strings.Contains(out.String(), "nightly-audit") || !strings.Contains(out.String(), "22:30") {
		t.Fatalf("list: %q %v", out.String(), err)
	}
	out.Reset()
	if err := RunJobs(&out, []string{"run", "nightly-audit"}); err != nil || !strings.Contains(out.String(), "ran-it") {
		t.Fatalf("run: %q %v", out.String(), err)
	}
	if err := RunJobs(&out, []string{"run", "nope"}); err == nil {
		t.Fatal("unknown job should fail")
	}
}

func waitState(t *testing.T, name string, ok func(*jobState) bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st := readJobStates()[name]; st != nil && ok(st) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("state never reached the expected condition: %+v", readJobStates()[name])
}

func setJobsClock(t *testing.T, at *time.Time) {
	t.Helper()
	old := jobsNow
	jobsNow = func() time.Time { return *at }
	t.Cleanup(func() { jobsNow = old })
}

func TestCheckScheduledJobsWaitsForTheFirstSlotThenRunsOnce(t *testing.T) {
	home := jobsHome(t)
	marker := filepath.Join(home, "ran")
	writeJobsFile(t, home, `{"jobs":[{"name":"nightly-audit","command":["/bin/sh","-c","echo x >> `+marker+`"],"after":"22:30"}]}`, 0o600)
	clock := jobAt("02", "09:00")
	setJobsClock(t, &clock)

	checkScheduledJobs(clock) // first sight: recorded only
	if st := readJobStates()["nightly-audit"]; st == nil || st.FirstSeen.IsZero() || !st.LastStart.IsZero() {
		t.Fatalf("first sight state = %+v", st)
	}
	clock = jobAt("02", "10:00")
	checkScheduledJobs(clock) // before the slot: nothing
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("job ran before its first slot")
	}
	clock = jobAt("02", "22:31")
	checkScheduledJobs(clock)
	waitState(t, "nightly-audit", func(s *jobState) bool { return !s.Running && !s.LastSuccess.IsZero() })
	clock = jobAt("02", "22:40")
	checkScheduledJobs(clock) // already succeeded this slot
	time.Sleep(300 * time.Millisecond)
	data, _ := os.ReadFile(marker)
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Fatalf("job ran %d times, want 1", got)
	}
}

func TestCheckScheduledJobsRunsNothingFromAnUnsafeConfig(t *testing.T) {
	home := jobsHome(t)
	marker := filepath.Join(home, "ran")
	writeJobsFile(t, home, `{"jobs":[{"name":"nightly-audit","command":["/bin/sh","-c","touch `+marker+`"],"after":"22:30"}]}`, 0o666)
	clock := jobAt("02", "23:00")
	setJobsClock(t, &clock)
	checkScheduledJobs(clock)
	checkScheduledJobs(clock)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("ran a job from a group/other-writable jobs.json")
	}
	if len(readJobStates()) != 0 {
		t.Fatalf("recorded state for an unsafe config: %v", readJobStates())
	}
}

func TestStartJobKillsDescendantsThatIgnoreSIGTERM(t *testing.T) {
	home := jobsHome(t)
	marker := filepath.Join(home, "stubborn-survived")
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "(trap '' TERM; sleep 3; touch " + marker + ") & wait"}
	old := jobKillGrace
	jobKillGrace = 300 * time.Millisecond
	defer func() { jobKillGrace = old }()
	j.timeoutOverride = 500 * time.Millisecond
	done, err := startJob(j, time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := waitJob(t, done); !r.TimedOut {
		t.Fatalf("result = %+v", r)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a descendant that ignores SIGTERM survived the timeout")
	}
}

func TestReconcileGivesAFreshClaimAGracePeriod(t *testing.T) {
	jobsHome(t)
	j := testJob()
	now := time.Now()
	if err := updateJobState(j.Name, func(s *jobState) { s.Running, s.PID, s.LastStart = true, 0, now }); err != nil {
		t.Fatal(err)
	}
	reconcileJobs([]scheduledJob{j}, now)
	if st := readJobStates()[j.Name]; !st.Running {
		t.Fatalf("a job being claimed right now was marked lost: %+v", st)
	}
	reconcileJobs([]scheduledJob{j}, now.Add(10*time.Minute))
	if st := readJobStates()[j.Name]; st.Running || st.LastExit == 0 {
		t.Fatalf("a stale claim was never released: %+v", st)
	}
}

func TestJobDueIgnoresTimestampsFromTheFuture(t *testing.T) {
	j := testJob()
	st := jobState{FirstSeen: jobAt("02", "09:00"), LastSuccess: jobAt("02", "09:00").AddDate(0, 1, 0)}
	if due, why := jobDue(j, st, jobAt("02", "22:31")); !due {
		t.Fatalf("a success stamped a month in the future blocked the job: %s", why)
	}
	st = jobState{FirstSeen: jobAt("02", "09:00").AddDate(1, 0, 0)}
	if due, _ := jobDue(j, st, jobAt("02", "22:31")); due {
		t.Fatal("a first-seen time in the future must still wait, not run")
	}
}

func TestCorruptStateIsMovedAsideNotOverwritten(t *testing.T) {
	jobsHome(t)
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobsStateFile(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateJobState("a", func(s *jobState) { s.LastExit = 7 }); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(jobsStateFile() + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt copies = %v, want the bad file kept", matches)
	}
	if got, _ := os.ReadFile(matches[0]); string(got) != "{not json" {
		t.Fatalf("corrupt copy = %q", got)
	}
	if st := readJobStates()["a"]; st == nil || st.LastExit != 7 {
		t.Fatalf("fresh state = %+v", st)
	}
}

func TestNullStateEntryDoesNotPanic(t *testing.T) {
	jobsHome(t)
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobsStateFile(), []byte(`{"x": null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reconcileJobs([]scheduledJob{testJob()}, time.Now())
	_ = readJobStates()
}

func TestUpdateJobStateWaitsForTheCrossProcessLock(t *testing.T) {
	jobsHome(t)
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	other, err := os.OpenFile(filepath.Join(jobsRunDir(), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX); err != nil { // as another process would
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		_ = updateJobState("a", func(s *jobState) { s.LastExit = 1 })
		close(finished)
	}()
	select {
	case <-finished:
		t.Fatal("updateJobState ignored a lock held by another process")
	case <-time.After(300 * time.Millisecond):
	}
	_ = syscall.Flock(int(other.Fd()), syscall.LOCK_UN)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("updateJobState never got the lock after release")
	}
}

func TestLoadJobsRejectsRelativeProgramPathsAndTrailingData(t *testing.T) {
	home := jobsHome(t)
	path := filepath.Join(home, ".cully", "jobs.json")
	for name, body := range map[string]string{
		"relative with slash": `{"jobs":[{"name":"a","command":["./run.sh"],"after":"22:30"}]}`,
		"parent traversal":    `{"jobs":[{"name":"a","command":["../run.sh"],"after":"22:30"}]}`,
		"trailing data":       `{"jobs":[]} {"jobs":[{"name":"a","command":["/bin/true"],"after":"22:30"}]}`,
	} {
		writeJobsFile(t, home, body, 0o600)
		if _, err := loadJobs(path); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	writeJobsFile(t, home, `{"jobs":[{"name":"a","command":["python3","./args-are-fine.py"],"after":"22:30"}]}`, 0o600)
	if _, err := loadJobs(path); err != nil {
		t.Errorf("relative arguments after the program must be allowed: %v", err)
	}
	if _, err := loadJobs("jobs.json"); err == nil {
		t.Error("a relative config path (unknown home) must be refused")
	}
}

func TestOversizedLogIsRotatedNotErased(t *testing.T) {
	jobsHome(t)
	j := testJob()
	j.Command = []string{"/bin/sh", "-c", "echo fresh"}
	if err := os.MkdirAll(jobsRunDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("old-failure-evidence\n", jobLogMax/20+10)
	if err := os.WriteFile(jobLogFile(j.Name), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	done, err := startJob(j, time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, done)
	if old, err := os.ReadFile(jobLogFile(j.Name) + ".1"); err != nil || !strings.Contains(string(old), "old-failure-evidence") {
		t.Fatalf("previous log not kept: %v", err)
	}
	if cur, _ := os.ReadFile(jobLogFile(j.Name)); strings.Contains(string(cur), "old-failure-evidence") || !strings.Contains(string(cur), "fresh") {
		t.Fatalf("current log = %q", cur)
	}
}

func TestCheckForgetsRemovedJobsSoAReAddedOneWaitsForItsFirstSlot(t *testing.T) {
	home := jobsHome(t)
	writeJobsFile(t, home, `{"jobs":[{"name":"nightly-audit","command":["/bin/true"],"after":"22:30"}]}`, 0o600)
	clock := jobAt("02", "09:00")
	setJobsClock(t, &clock)
	checkScheduledJobs(clock)
	writeJobsFile(t, home, `{"jobs":[]}`, 0o600)
	checkScheduledJobs(clock)
	if _, kept := readJobStates()["nightly-audit"]; kept {
		t.Fatal("state of a removed job was kept")
	}
	writeJobsFile(t, home, `{"jobs":[{"name":"nightly-audit","command":["/bin/true"],"after":"22:30"}]}`, 0o600)
	clock = jobAt("02", "22:31")
	checkScheduledJobs(clock) // re-added: first sight again, so recorded only
	if st := readJobStates()["nightly-audit"]; st == nil || !st.LastStart.IsZero() {
		t.Fatalf("a re-added job ran immediately: %+v", st)
	}
}

func TestMissingProgramIsRecordedNotSilent(t *testing.T) {
	jobsHome(t)
	j := testJob()
	j.Command = []string{"definitely-not-a-real-program-xyz"}
	if _, err := startJob(j, time.Now(), false, nil); err == nil {
		t.Fatal("expected an error for a missing program")
	}
	if st := readJobStates()[j.Name]; st == nil || st.Detail == "" || st.Running {
		t.Fatalf("state = %+v, want the failure recorded and not running", st)
	}
}
