package cully

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startTestAdvisorQueue(t *testing.T, run func(context.Context, string, string, string)) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); runAdvisorQueue(ctx, time.Millisecond, run) }()
	t.Cleanup(func() { cancel(); awaitQueueStop(t, done) })
	return cancel, done
}

func awaitQueueStop(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("advisor queue did not stop")
	}
}

func awaitAdvisorStart(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case s := <-started:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("advisor job did not start")
		return ""
	}
}

func TestAdvisorQueueSlowSessionDoesNotBlockAnother(t *testing.T) {
	root := journalTestEnv(t)
	for _, session := range []string{"slow", "fast"} {
		if err := enqueueAdvisorJob(session+".signals", session, root); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan string, 2)
	startTestAdvisorQueue(t, func(ctx context.Context, _, session, cwd string) {
		if cwd != root {
			t.Errorf("lost project: %q", cwd)
		}
		started <- session
		if session == "slow" {
			<-ctx.Done()
		}
	})
	seen := map[string]bool{awaitAdvisorStart(t, started): true}
	seen[awaitAdvisorStart(t, started)] = true
	if !seen["slow"] || !seen["fast"] {
		t.Fatal(seen)
	}
}

func TestAdvisorQueueBoundsConcurrencyAndRetainsInterruptedJobs(t *testing.T) {
	root := journalTestEnv(t)
	for i := 0; i < advisorConcurrency+2; i++ {
		if err := enqueueAdvisorJob("signals", fmt.Sprint(i), root); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan string, advisorConcurrency+2)
	cancel, done := startTestAdvisorQueue(t, func(ctx context.Context, _, session, _ string) { started <- session; <-ctx.Done() })
	for i := 0; i < advisorConcurrency; i++ {
		awaitAdvisorStart(t, started)
	}
	select {
	case s := <-started:
		t.Fatalf("exceeded concurrency limit: %s", s)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	awaitQueueStop(t, done)
	jobs, _ := filepath.Glob(filepath.Join(jobDir(), "*.job"))
	if len(jobs) != advisorConcurrency+2 {
		t.Fatalf("lost interrupted jobs: %v", jobs)
	}
}

func TestAdvisorQueueSerializesOneSession(t *testing.T) {
	root := journalTestEnv(t)
	if err := enqueueAdvisorJob("first", "same", root); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 3)
	release := make(chan struct{})
	startTestAdvisorQueue(t, func(ctx context.Context, path, _, _ string) {
		started <- path
		if path == "first" {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	if got := awaitAdvisorStart(t, started); got != "first" {
		t.Fatal(got)
	}
	if err := enqueueAdvisorJob("second", "same", root); err != nil {
		t.Fatal(err)
	}
	if err := enqueueAdvisorJob("other", "another", root); err != nil {
		t.Fatal(err)
	}
	if got := awaitAdvisorStart(t, started); got != "other" {
		t.Fatalf("same-session jobs overlapped: %s", got)
	}
	close(release)
	if got := awaitAdvisorStart(t, started); got != "second" {
		t.Fatal(got)
	}
}

func TestAdvisorQueueCoalescesRefreshesAndSkipsMalformedJobs(t *testing.T) {
	root := journalTestEnv(t)
	for _, path := range []string{"old", "latest"} {
		if err := enqueueAdvisorJob(path, "session", root); err != nil {
			t.Fatal(err)
		}
	}
	bad := filepath.Join(jobDir(), "bad.job")
	if err := os.WriteFile(bad, []byte(`{"session":`), 0o600); err != nil {
		t.Fatal(err)
	}
	job, path, ok := nextAdvisorJob(nil)
	if !ok || job.SignalsPath != "latest" {
		t.Fatalf("not latest: %+v", job)
	}
	jobs, _ := filepath.Glob(filepath.Join(jobDir(), "*.job"))
	if len(jobs) != 1 || jobs[0] != path {
		t.Fatalf("obsolete jobs survived: %v", jobs)
	}
	if _, _, ok := nextAdvisorJob(map[string]bool{"session": true}); ok {
		t.Fatal("selected an active session")
	}
}

func TestCanceledWorkerDoesNotWriteResults(t *testing.T) {
	root := journalTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runWorkerContext(ctx, filepath.Join(root, "missing"), "canceled", root)
	if _, err := os.Stat(sessionSnapshotFile("canceled")); !os.IsNotExist(err) {
		t.Fatalf("canceled worker published results: %v", err)
	}
}

func TestAdvisorSignalRefreshIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signals")
	old, next := "agent=claude\n"+strings.Repeat("a", 10000), "agent=codex\n"+strings.Repeat("b", 10000)
	if err := writeAdvisorFile(path, []byte(old)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 30; i++ {
			data := old
			if i%2 == 0 {
				data = next
			}
			if err := writeAdvisorFile(path, []byte(data)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != old && string(data) != next {
			t.Fatal("worker observed a partial signal refresh")
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("signal permissions: %v", info.Mode().Perm())
			}
			return
		default:
		}
	}
}
