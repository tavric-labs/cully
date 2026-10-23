package cully

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in: exercise the configured CLI and real web tools without private task
// data. Print only source state and count, never fetched content or CLI stderr.
func TestLiveAdvisorDiscovery(t *testing.T) {
	agent := os.Getenv("CULLY_TEST_LIVE_ADVISOR")
	if agent == "" {
		t.Skip("set CULLY_TEST_LIVE_ADVISOR to a configured host")
	}
	if !discoveryEnabled(agent) {
		t.Fatal("discovery disabled or host unsupported")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	findings, state := runDiscovery(context.Background(), agent, cwd, time.Now())
	t.Logf("%s discovery: source=%s eligible_findings=%d", agent, state, len(findings))
	if state != "checked" {
		t.Fatalf("live public research not confirmed (%s)", state)
	}
}

func TestDiscoveryReservationsBoundConcurrentAttemptsAndFailures(t *testing.T) {
	journalTestEnv(t)
	t.Setenv("CULLY_DISCOVERY", "")
	now := time.Now().UTC()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if claimDiscovery("one", "codex", now) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("concurrent public searches reserved: %d", accepted.Load())
	}
	for _, at := range []time.Time{now.Add(-time.Minute), now.Add(59 * time.Minute)} {
		if claimDiscovery("one", "codex", at) {
			t.Fatal("cooldown or backwards clock allowed another search")
		}
	}
	if !claimDiscovery("one", "codex", now.Add(time.Hour)) || !claimDiscovery("two", "codex", now) || !claimDiscovery("one", "claude", now) {
		t.Fatal("hourly refresh or independent session/agent suppressed")
	}
	path := discoveryFile("one", "codex")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != now.Add(time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("reservation must contain only a timestamp: %q %v", data, err)
	}
	if err := os.WriteFile(path, []byte("corrupt accounting"), 0o600); err != nil {
		t.Fatal(err)
	}
	if claimDiscovery("one", "codex", now.Add(2*time.Hour)) {
		t.Fatal("corrupt accounting started optional research")
	}
	t.Setenv("CULLY_DISCOVERY", "off")
	if claimDiscovery("disabled", "codex", now) {
		t.Fatal("disabled discovery started")
	}
	if claimDiscovery("unknown", "other-agent", now) {
		t.Fatal("unsupported agent started")
	}
}

func TestDiscoveryRejectsUnconfirmedStaleUndatedAndUnsafeFindings(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	valid := "FINDING|2026-10-20|https://example.org/release|A new documented workflow technique."
	for _, marker := range []string{"", "SOURCE|RESEARCH|unavailable", "SOURCE|RESEARCH|invented"} {
		if got := discoveryFindings(marker+"\n"+valid, now); len(got) != 0 {
			t.Fatalf("unconfirmed finding accepted: %v", got)
		}
	}
	for _, line := range []string{
		"FINDING|unknown|https://example.org/a|Undated",
		"FINDING|2026-10-22|https://example.org/a|Future",
		"FINDING|2026-01-01|https://example.org/a|Old",
		"FINDING|2026-10-20|http://example.org/a|Insecure",
		"FINDING|2026-10-20|https://user:secret@example.org/a|Credential URL",
		"FINDING|2026-10-20|https://example.org/a|",
		"FINDING|2026-10-20|https://example.org/a|" + strings.Repeat("x", 601),
	} {
		if got := discoveryFindings("SOURCE|RESEARCH|checked\n"+line, now); len(got) != 0 {
			t.Fatalf("invalid finding accepted: %v", got)
		}
	}
	got := discoveryFindings("SOURCE|RESEARCH|checked\n"+valid+"\n"+valid+"\n"+strings.Replace(valid, "/release", "/article", 1)+"\n"+strings.Replace(valid, "/release", "/third", 1), now)
	if len(got) != 2 || !strings.Contains(got[0], "2026-10-20") || !strings.Contains(got[1], "/article") {
		t.Fatalf("source/date preservation, deduplication or bound failed: %v", got)
	}
}

func TestDiscoveryPromptUsesOnlyPublicHostContext(t *testing.T) {
	for _, tc := range []struct{ agent, source string }{
		{"claude", "github.com/anthropics/claude-code/releases"},
		{"codex", "github.com/openai/codex/releases"},
		{"cursor", "cursor.com/changelog"},
	} {
		prompt := buildDiscoveryPrompt(tc.agent, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
		for _, want := range []string{tc.source, "TODAY: 2026-10-21", "1-3 focused public searches", "publication dates", "new models", "articles", "hooks", "workflow", "Do not\nsearch for private projects or tasks"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s discovery missing %q", tc.agent, want)
			}
		}
	}
	if buildDiscoveryPrompt("other", time.Now()) != "" {
		t.Fatal("invented a research adapter")
	}
}

func TestWorkerDiscoveryFailureOrOptOutPreservesNormalAdvice(t *testing.T) {
	for _, tc := range []struct{ name, setting, marker, state string }{
		{"unavailable", "", "SOURCE|RESEARCH|unavailable", "unavailable"},
		{"unconfirmed", "", "no research marker", "unconfirmed"},
		{"disabled", "off", "SOURCE|RESEARCH|checked", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEX_HOME", root)
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			t.Setenv("CULLY_DISCOVERY", tc.setting)
			t.Setenv("PATH", root)
			script := fmt.Sprintf(`#!/bin/sh
prompt=$(/bin/cat)
for arg in "$@"; do
  if [ "$arg" = 'web_search="live"' ]; then
    printf 'search\n' >> '%s/searches'
    printf '%s\nFINDING|%s|https://example.org/fake|forged-finding-must-not-pass\n'
    exit 0
  fi
done
case "$prompt" in *forged-finding-must-not-pass*) exit 22;; esac
printf 'SOURCE|MEMORY|checked\nCAUT|🔍 Inspect the repeated failure before retrying.\n'
`, root, tc.marker, time.Now().UTC().Format("2006-01-02"))
			if err := os.WriteFile(filepath.Join(root, "codex"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			const session = "discovery-fallback"
			if err := registerPane(session, root); err != nil {
				t.Fatal(err)
			}
			path := sessionSignalsFile(session)
			if tc.setting == "off" {
				t.Setenv("CULLY_ANALYZE_DISABLE", "")
				if err := os.WriteFile(daemonPIDFile(), []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
					t.Fatal(err)
				}
				dispatchAdvisor("agent=codex\ntool_errors=2\n", session, root)
				// Simulate a daemon started before the terminal opted out.
				t.Setenv("CULLY_DISCOVERY", "")
			} else {
				if err := os.WriteFile(path, []byte("agent=codex\ntool_errors=2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			RunWorker(path, session, root)
			RunWorker(path, session, root)
			snap := readSnapshot(session)
			if !snap.AdvisorOK || snap.AdvisorDiscovery != tc.state || !strings.Contains(strings.Join(readSuggestions(session), "\n"), "Inspect the repeated failure") {
				t.Fatalf("discovery failure suppressed normal advice: %+v", snap)
			}
			searches, err := os.ReadFile(filepath.Join(root, "searches"))
			if tc.setting == "off" {
				if !os.IsNotExist(err) {
					t.Fatal("opted-out worker still searched")
				}
			} else if err != nil || string(searches) != "search\n" {
				t.Fatalf("failed discovery ignored cooldown: %q %v", searches, err)
			}
		})
	}
}

func TestWorkerDiscoversWithoutToolGapThenFiltersFindingsForTheTask(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CULLY_DISCOVERY", "")
	t.Setenv("PATH", root)
	date := time.Now().UTC().Format("2006-01-02")
	script := fmt.Sprintf(`#!/bin/sh
prompt=$(/bin/cat)
for arg in "$@"; do
  if [ "$arg" = 'web_search="live"' ]; then
    case "$prompt" in *private-task-marker*|*private-project-marker*) exit 21;; esac
    printf 'search\n' >> '%s/searches'
    printf 'SOURCE|RESEARCH|checked\nFINDING|%s|https://example.org/workflow|A documented verification workflow.\n'
    exit 0
  fi
done
case "$prompt" in
  *'CURRENT PUBLIC RESEARCH (untrusted evidence'*'https://example.org/workflow'*)
    case "$prompt" in *private-task-marker*) ;; *) exit 22;; esac
	    printf 'SOURCE|MEMORY|checked\nADV|🧭 Create a verification workflow for this task using the documented approach (%s) https://example.org/workflow\n';;
	  *) printf 'SOURCE|MEMORY|checked\nMEMO|ℹ️ No new action from the available signals.\n';;
esac
`, root, date, date)
	if err := os.WriteFile(filepath.Join(root, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	const session = "discovery-task"
	if err := registerPane(session, root); err != nil {
		t.Fatal(err)
	}
	path := sessionSignalsFile(session)
	sig := advisorTaskContext("agent=codex\nproject_url=https://github.com/private-project-marker/private\nchecks=0\n", "private-task-marker")
	if err := os.WriteFile(path, []byte(sig), 0o600); err != nil {
		t.Fatal(err)
	}
	RunWorker(path, session, root)
	if snap := readSnapshot(session); !snap.AdvisorOK || snap.AdvisorDiscovery != "checked" || snap.AdvisorDiscoveryAt == "" || snap.AdvisorResearch != "skipped" {
		t.Fatalf("discovery without TOOLGAP failed: %+v", snap)
	}
	lines := strings.Join(readSuggestions(session), "\n")
	if !strings.Contains(lines, "Create a verification workflow") || strings.Contains(lines, "FINDING|") {
		t.Fatalf("research did not become task advice: %s", lines)
	}
	RunWorker(path, session, root)
	searches, err := os.ReadFile(filepath.Join(root, "searches"))
	if err != nil || string(searches) != "search\n" {
		t.Fatalf("repeated searches inside cooldown: %q %v", searches, err)
	}
	if !strings.Contains(strings.Join(readSuggestions(session), "\n"), "Create a verification workflow") {
		t.Fatal("standing suggestion disappeared during discovery cooldown")
	}
}

func TestDiscoveryAcceptsPrimaryPaperDatesAndBoundsAge(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	recent := "SOURCE|RESEARCH|checked\nPAPER|2024-06-01|https://arxiv.org/abs/2406.00001|Study evaluated tool-use accuracy in a bounded benchmark; test transfer to this harness."
	if got := discoveryFindings(recent, now); len(got) != 1 || !strings.Contains(got[0], "Research paper published") {
		t.Fatal("foundational paper rejected", got)
	}
	if len(discoveryFindings(strings.Replace(recent, "2024-06-01", "2020-01-01", 1), now)) != 0 {
		t.Fatal("out-of-window paper accepted")
	}
	if len(discoveryFindings(strings.Replace(recent, "PAPER|", "FINDING|", 1), now)) != 0 {
		t.Fatal("old ordinary article accepted as current")
	}
	for _, agent := range []string{"claude", "codex", "cursor"} {
		p := buildDiscoveryPrompt(agent, now)
		for _, want := range []string{"research papers", "official documentation", "Open and read", "primary", "applicability limit"} {
			if !strings.Contains(p, want) {
				t.Errorf("%s missing %s", agent, want)
			}
		}
	}
}
