package cully

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJobScheduleForms(t *testing.T) {
	good := map[string]string{
		"": jobDaily, "daily": jobDaily, "Weekdays": jobWeekdays, "hourly": jobInterval, "every 15m": jobInterval,
		"every 168h": jobInterval, " every 2h ": jobInterval, "in 30m": jobOnce, "in 168h": jobOnce, "in 1m": jobOnce,
	}
	for s, want := range good {
		if k, _, err := jobSchedule(s); err != nil || k != want {
			t.Errorf("%q: kind=%q err=%v, want %q", s, k, err, want)
		}
	}
	for _, s := range []string{"every 5m", "every 169h", "every 0m", "in 0m", "in 169h", "in 30", "0 2 * * *", "every day", "weekly", "in -5m", "every 15m; rm"} {
		if _, _, err := jobSchedule(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
	base := scheduledJob{Name: "a", Command: []string{"/bin/true"}}
	if _, err := base.normalized(); err == nil {
		t.Error("a daily job without a time was accepted")
	}
	h := base
	h.Schedule = "every 2h"
	if _, err := h.normalized(); err != nil {
		t.Errorf("an interval job needs no time of day: %v", err)
	}
	o := base
	o.Schedule = "in 30m"
	if _, err := o.normalized(); err == nil {
		t.Error("a one-shot job without run_at was accepted")
	}
	o.RunAt = time.Now()
	if _, err := o.normalized(); err != nil {
		t.Error(err)
	}
	h.MaxRuns = jobsMaxRuns + 1
	if _, err := h.normalized(); err == nil {
		t.Error("an absurd run limit was accepted")
	}
}

func intervalJob(every string) scheduledJob {
	j := scheduledJob{Name: "tick", Command: []string{"/bin/true"}, Schedule: every}
	n, err := j.normalized()
	if err != nil {
		panic(err)
	}
	return n
}

func TestJobDueIntervalOnceLimitPauseAndCatchUp(t *testing.T) {
	now := jobAt("10", "12:00")
	seen := now.Add(-time.Hour)
	j := intervalJob("every 2h")
	due := func(j scheduledJob, st jobState, at time.Time) bool { d, _ := jobDue(j, st, at); return d }

	if due(j, jobState{FirstSeen: seen}, now) {
		t.Error("an interval job ran before its first interval")
	}
	if !due(j, jobState{FirstSeen: seen}, seen.Add(2*time.Hour)) {
		t.Error("an interval job did not run after its interval")
	}
	// Down for ten intervals: one catch-up run, and none right after it starts.
	st := jobState{FirstSeen: seen, LastStart: now.Add(-20 * time.Hour), LastSuccess: now.Add(-20 * time.Hour)}
	if !due(j, st, now) {
		t.Error("no catch-up run after downtime")
	}
	st.LastStart, st.Running = now, true
	if d, why := jobDue(j, st, now.Add(5*time.Minute)); d || why != "running" {
		t.Errorf("a running job was due: %v %s", d, why)
	}
	st.Running = false
	if due(j, st, now.Add(5*time.Minute)) || due(j, st, now.Add(119*time.Minute)) {
		t.Error("more than one catch-up run")
	}
	// A failed run is retried after the retry delay, up to max attempts, then waits for the interval.
	failed := jobState{FirstSeen: seen, LastStart: now, LastEnd: now.Add(time.Minute), LastExit: 1, Slot: "i:x", Attempts: 1}
	if due(j, failed, now.Add(30*time.Minute)) || !due(j, failed, now.Add(62*time.Minute)) {
		t.Error("retry delay not honoured")
	}
	failed.Attempts = 2
	if due(j, failed, now.Add(90*time.Minute)) || !due(j, failed, now.Add(2*time.Hour)) {
		t.Error("attempt limit not honoured")
	}
	// Pause.
	if d, why := jobDue(j, jobState{FirstSeen: seen, Paused: true}, now.Add(48*time.Hour)); d || why != "paused" {
		t.Errorf("a paused job was due: %v %s", d, why)
	}
	// Run limit.
	lim := j
	lim.MaxRuns = 3
	if d, why := jobDue(lim, jobState{FirstSeen: seen, Runs: 3}, now.Add(48*time.Hour)); d || why != "run limit reached" {
		t.Errorf("run limit: %v %s", d, why)
	}
	if !due(lim, jobState{FirstSeen: seen, Runs: 2}, now.Add(48*time.Hour)) {
		t.Error("run limit stopped a job early")
	}
	// One shot.
	once := scheduledJob{Name: "once", Command: []string{"/bin/true"}, Schedule: "in 30m", RunAt: now.Add(30 * time.Minute)}
	once, _ = once.normalized()
	if due(once, jobState{FirstSeen: now}, now.Add(10*time.Minute)) || !due(once, jobState{FirstSeen: now}, now.Add(5*time.Hour)) {
		t.Error("one-shot timing")
	}
	if due(once, jobState{FirstSeen: now, LastSuccess: now.Add(time.Hour)}, now.Add(5*time.Hour)) {
		t.Error("a finished one-shot job ran again")
	}
	if !jobNext(once, jobState{FirstSeen: now}, now).Equal(once.RunAt) {
		t.Error("next run of a one-shot job")
	}
}

func TestJobWeekdaysSkipTheWeekend(t *testing.T) {
	// 2026-11-27 is a Friday; the 28th and 29th are the weekend.
	j := scheduledJob{Name: "wk", Command: []string{"/bin/true"}, After: "09:00", Schedule: "weekdays"}
	j, _ = j.normalized()
	first := jobAt("26", "08:00")
	fri := jobAt("27", "10:00")
	if d, _ := jobDue(j, jobState{FirstSeen: first}, fri); !d {
		t.Error("not due on Friday")
	}
	done := jobState{FirstSeen: first, LastSuccess: fri}
	if d, _ := jobDue(j, done, jobAt("29", "12:00")); d {
		t.Error("ran on Sunday")
	}
	if d, _ := jobDue(j, done, jobAt("30", "09:30")); !d {
		t.Error("not due on Monday")
	}
	if n := jobNext(j, done, jobAt("28", "12:00")); n.Weekday() != time.Monday {
		t.Errorf("next = %v", n)
	}
}

func TestJobRunHistoryStreakAndRedaction(t *testing.T) {
	jobsHome(t)
	j := intervalJob("hourly")
	run := func(script string) {
		t.Helper()
		j.Command = []string{"/bin/sh", "-c", script}
		done, err := startJob(j, time.Now(), false, nil)
		if err != nil {
			t.Fatal(err)
		}
		waitJob(t, done)
	}
	run("echo fine; exit 0")
	run("echo 'password=hunter2'; echo broke; exit 4")
	run("exit 5")
	h := jobHistory(j.Name)
	if len(h) != 3 || h[0].Status != "failed" || h[2].Status != "ok" || h[0].Exit != 5 {
		t.Fatalf("history = %+v", h)
	}
	if jobFailureStreak(h) != 2 {
		t.Errorf("streak = %d, want 2", jobFailureStreak(h))
	}
	if strings.Contains(h[1].Tail, "hunter2") || !strings.Contains(h[1].Tail, "broke") || strings.Contains(h[1].Tail, "--- ") {
		t.Errorf("tail = %q", h[1].Tail)
	}
	run("exit 0")
	if jobFailureStreak(jobHistory(j.Name)) != 0 {
		t.Error("a success did not end the streak")
	}
	for i := 0; i < 30; i++ {
		recordJobRun(j.Name, jobRunRec{Start: time.Now(), End: time.Now(), Status: "ok"})
	}
	if n := len(jobHistory(j.Name)); n != jobHistoryKeep {
		t.Errorf("kept %d runs, want %d", n, jobHistoryKeep)
	}
}

func TestSkipTickWhileRunningAndRunLimitCounts(t *testing.T) {
	home := jobsHome(t)
	writeJobsFile(t, home, `{"jobs":[{"name":"slow","command":["/bin/sh","-c","sleep 2"],"schedule":"every 15m","max_runs":1}]}`, 0o600)
	now := time.Now()
	checkScheduledJobs(now) // first sight: recorded only
	if st := readJobStates()["slow"]; st == nil || st.FirstSeen.IsZero() {
		t.Fatal("job not recorded")
	}
	checkScheduledJobs(now.Add(16 * time.Minute)) // starts
	waitFor(t, func() bool { st := readJobStates()["slow"]; return st != nil && st.Running })
	checkScheduledJobs(now.Add(32 * time.Minute)) // still running: skipped, not started twice
	if st := readJobStates()["slow"]; st.Runs != 1 || st.Attempts != 1 {
		t.Fatalf("state after skipped tick = %+v", st)
	}
	waitFor(t, func() bool { st := readJobStates()["slow"]; return st != nil && !st.Running })
	if len(jobHistory("slow")) != 1 {
		t.Fatalf("history = %+v", jobHistory("slow"))
	}
	checkScheduledJobs(now.Add(5 * time.Hour)) // the limit of one run is reached
	time.Sleep(200 * time.Millisecond)
	if st := readJobStates()["slow"]; st.Runs != 1 || st.Running {
		t.Fatalf("a job past its run limit started again: %+v", st)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("condition not met in time")
}

func TestPauseAndResumeKeepTheJob(t *testing.T) {
	home := jobsHome(t)
	writeJobsFile(t, home, `{"jobs":[{"name":"p","command":["/bin/true"],"schedule":"hourly"}]}`, 0o600)
	now := time.Now()
	checkScheduledJobs(now)
	if err := setJobPaused("p", true); err != nil {
		t.Fatal(err)
	}
	checkScheduledJobs(now.Add(3 * time.Hour))
	time.Sleep(100 * time.Millisecond)
	if len(jobHistory("p")) != 0 {
		t.Fatal("a paused job ran")
	}
	if v := uiJobs(); len(v) != 1 || !v[0].Paused || v[0].State != "paused" {
		t.Fatalf("view = %+v", v)
	}
	if err := setJobPaused("p", false); err != nil {
		t.Fatal(err)
	}
	checkScheduledJobs(now.Add(3 * time.Hour)) // one catch-up after the pause
	waitFor(t, func() bool { return len(jobHistory("p")) == 1 })
	if jobs, _ := loadJobs(jobsConfigFile()); len(jobs) != 1 {
		t.Fatal("pausing removed the job")
	}
}

func TestParseJobDraft(t *testing.T) {
	folders := map[string]string{"app": "/tmp"}
	line := func(f ...string) string { return "JOB|" + strings.Join(f, "|") }
	ok := line("test-app", "app", `["make","-C","{folder}","test"]`, "every 6h", "none", "5", "Run the tests every six hours.")
	d, err := uiParseJobDraft(ok, folders, nil)
	if err != nil || d.Name != "test-app" || d.Schedule != "every 6h" || d.After != "" || d.MaxRuns != 5 || d.Command[2] != "/tmp" || d.Folder != "/tmp" {
		t.Fatalf("draft = %+v, %v", d, err)
	}
	d, err = uiParseJobDraft(line("nightly", "app", `["go","-C","{folder}","vet","./..."]`, "weekdays", "bad", "none", "Vet before standup."), folders, nil)
	if err != nil || d.Schedule != "weekdays" || d.After != "02:00" {
		t.Fatalf("weekday draft = %+v, %v", d, err)
	}
	d, err = uiParseJobDraft(line("later", "app", `["git","-C","{folder}","fetch","--all"]`, "in 30m", "none", "9", "Fetch soon."), folders, nil)
	if err != nil || d.Schedule != "in 30m" || d.MaxRuns != 0 {
		t.Fatalf("one-shot draft = %+v, %v", d, err)
	}
	bad := []string{
		line("a", "app", `["bash","-c","id"]`, "hourly", "none", "none", "x"),
		line("a", "app", `["make","-C","/","test"]`, "hourly", "none", "none", "x"),
		line("a", "other", `["make","-C","{folder}","test"]`, "hourly", "none", "none", "x"),
		line("a", "app", `["make","-C","{folder}","test"]`, "0 2 * * *", "none", "none", "x"),
		line("A_b", "app", `["make","-C","{folder}","test"]`, "hourly", "none", "none", "x"),
		line("a", "app", `["make","-C","{folder}","test"]`, "hourly", "none", "none", ""),
		"JOB|a|app|short",
		"NONE",
	}
	for _, b := range bad {
		if _, err := uiParseJobDraft(b, folders, nil); err == nil {
			t.Errorf("accepted %q", b)
		}
	}
	if _, err := uiParseJobDraft(ok, folders, []scheduledJob{{Name: "test-app"}}); err == nil {
		t.Error("a draft reused the name of an existing job")
	}
}

func TestJobsPageCreateEditPauseRemove(t *testing.T) {
	srv, m, repo := uiTestServer(t, false)
	t.Setenv("HOME", t.TempDir())
	m.cfg.Advisor = func(agent, cwd string, research bool, prompt string) (string, error) {
		return "JOB|fetch-repo|repo|[\"git\",\"-C\",\"{folder}\",\"fetch\",\"--all\"]|every 2h|none|3|Keep remotes fresh.", nil
	}
	var d uiJobDraft
	res := uiDo(t, "POST", srv.URL+"/api/jobs/draft", map[string]string{"text": "fetch my remotes every two hours"}, uiAuthed)
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&d) != nil || d.Folder != repo || d.Schedule != "every 2h" {
		t.Fatalf("draft: %d %+v", res.StatusCode, d)
	}
	if _, err := os.Stat(jobsConfigFile()); err == nil {
		t.Fatal("a draft wrote jobs.json")
	}
	evil := d
	evil.Command = []string{"git", "-C", repo, "fetch", "--upload-pack=touch x"}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs", evil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("an unsafe command was saved: %d", r.StatusCode)
	}
	other := d
	other.Folder, other.Command = t.TempDir(), []string{"git", "-C", "/", "fetch"}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs", other, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("an unknown folder was accepted: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs", d, uiAuthed); r.StatusCode != 201 {
		t.Fatalf("create: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs", d, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("duplicate create: %d", r.StatusCode)
	}
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil || len(jobs) != 1 || jobs[0].Schedule != "every 2h" || jobs[0].MaxRuns != 3 {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	if r := uiDo(t, "PUT", srv.URL+"/api/jobs/fetch-repo", map[string]any{"schedule": "daily", "after": "03:30"}, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("edit: %d", r.StatusCode)
	}
	if r := uiDo(t, "PUT", srv.URL+"/api/jobs/fetch-repo", map[string]any{"schedule": "cron 5"}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bad edit: %d", r.StatusCode)
	}
	jobs, _ = loadJobs(jobsConfigFile())
	if jobs[0].After != "03:30" || jobs[0].Schedule != "" || jobs[0].MaxRuns != 0 || jobs[0].Command[3] != "fetch" {
		t.Fatalf("edited = %+v", jobs[0])
	}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs/fetch-repo/pause", nil, uiAuthed); r.StatusCode != 204 || !uiJobs()[0].Paused {
		t.Fatalf("pause: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs/fetch-repo/resume", nil, uiAuthed); r.StatusCode != 204 || uiJobs()[0].Paused {
		t.Fatalf("resume: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/jobs/nope/pause", nil, uiAuthed); r.StatusCode != 404 {
		t.Fatalf("pause unknown: %d", r.StatusCode)
	}
	recordJobRun("fetch-repo", jobRunRec{Start: time.Now(), End: time.Now(), Status: "ok"})
	if r := uiDo(t, "DELETE", srv.URL+"/api/jobs/fetch-repo", nil, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("remove: %d", r.StatusCode)
	}
	if jobs, _ := loadJobs(filepath.Join(filepath.Dir(jobsConfigFile()), "jobs.json")); len(jobs) != 0 || len(jobHistory("fetch-repo")) != 0 {
		t.Fatalf("after remove: %+v", jobs)
	}
}

func TestSaveOneShotJobSetsRunAt(t *testing.T) {
	jobsHome(t)
	if err := uiSaveJob(scheduledJob{Name: "soon", Command: []string{"git", "status"}, Schedule: "in 30m"}, false); err != nil {
		t.Fatal(err)
	}
	jobs, err := loadJobs(jobsConfigFile())
	if err != nil || len(jobs) != 1 {
		t.Fatalf("%v %v", jobs, err)
	}
	if d := time.Until(jobs[0].RunAt); d < 29*time.Minute || d > 31*time.Minute {
		t.Fatalf("run_at is %v from now", d)
	}
	raw, _ := os.ReadFile(jobsConfigFile())
	if strings.Contains(string(raw), `"after"`) {
		t.Fatalf("a one-shot job saved an empty time: %s", raw)
	}
}
