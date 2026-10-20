package cully

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdvisorBudgetLimitsDuplicateRapidAndHourlyRequests(t *testing.T) {
	journalTestEnv(t)
	now := time.Now().UTC()
	if got := admitAdvisorRequest("one", "first", now); got != "" {
		t.Fatal(got)
	}
	if got := admitAdvisorRequest("one", "first", now.Add(3*time.Minute)); !strings.Contains(got, "unchanged") {
		t.Fatal(got)
	}
	if got := admitAdvisorRequest("one", "new", now.Add(time.Minute)); !strings.Contains(got, "cooldown") {
		t.Fatal(got)
	}
	for i := 1; i < advisorRequestLimit; i++ {
		if got := admitAdvisorRequest("one", strings.Repeat("x", i), now.Add(time.Duration(i)*3*time.Minute)); got != "" {
			t.Fatal(got)
		}
	}
	if got := admitAdvisorRequest("one", "over budget", now.Add(20*time.Minute)); !strings.Contains(got, "limit reached") {
		t.Fatal(got)
	}
	if got := admitAdvisorRequest("two", "independent", now.Add(20*time.Minute)); got != "" {
		t.Fatal(got)
	}
	if got := admitAdvisorRequest("one", "later", now.Add(time.Hour)); got != "" {
		t.Fatal(got)
	}
	data, err := os.ReadFile(advisorBudgetFile("one"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "later") || strings.Contains(string(data), "first") {
		t.Fatal("raw signals saved in budget")
	}
}

func TestAdvisorBudgetConcurrentAdmissionAndBrokenAccounting(t *testing.T) {
	journalTestEnv(t)
	now := time.Now().UTC()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if admitAdvisorRequest("shared", "same", now) == "" {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("concurrent requests admitted: %d", accepted.Load())
	}
	if err := os.WriteFile(advisorBudgetFile("broken"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := admitAdvisorRequest("broken", "signals", now); !strings.Contains(got, "unavailable") {
		t.Fatal(got)
	}
	if got := admitAdvisorRequest("shared", "changed", now.Add(-time.Minute)); !strings.Contains(got, "clock") {
		t.Fatal(got)
	}
}

func TestAdvisorDispatchReportsDeferralAndKeepsLocalWarnings(t *testing.T) {
	cwd := journalTestEnv(t)
	t.Setenv("CULLY_ANALYZE_DISABLE", "")
	if err := os.MkdirAll(cullyDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemonPIDFile(), []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	sig := "agent=claude\ntools=1\n"
	dispatchAdvisor(sig, "budget-session", cwd)
	dispatchAdvisor(sig, "budget-session", cwd)
	jobs, _ := filepath.Glob(filepath.Join(jobDir(), "*.job"))
	if len(jobs) != 1 {
		t.Fatalf("duplicate request queued: %v", jobs)
	}
	snap := readSnapshot("budget-session")
	if snap.AdvisorDeferred != "unchanged advisor evidence" {
		t.Fatalf("missing deferral: %+v", snap)
	}
	lines := combinedAdvice("budget-session", toolStats{Tools: 3, EditsSinceCheck: 2}, sessionView{Agent: "claude", Project: cwd})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Automatic advisor deferred") || !strings.Contains(joined, "focused check") {
		t.Fatalf("lost local warnings: %s", joined)
	}
}
