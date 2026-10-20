package cully

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The advisor evaluation replays past transcripts through the same signal
// pipeline the live advisor uses and judges each checkpoint in hindsight:
// would the advisor have spoken, and did the session go on to waste tokens
// that a timely suggestion could have saved? An optional live pass runs the
// real two-phase advisor (memory, then web research) on a few checkpoints and
// scores what it says. Nothing here stores transcript text.

type evalVerdict string

const (
	verdictUseful       evalVerdict = "useful"        // advised, and heavy waste followed
	verdictLowValue     evalVerdict = "low-value"     // advised, little waste followed
	verdictMissed       evalVerdict = "missed"        // quiet, then heavy waste followed
	verdictCorrectQuiet evalVerdict = "correct-quiet" // quiet, and nothing heavy followed
)

// futureWasteTokens is how much big output must follow a checkpoint for the
// waste to count as something a suggestion could have saved.
const futureWasteTokens = 5000

type evalCheckpoint struct {
	Session     string
	Percent     int
	Advised     bool
	SeenWaste   int // big-result tokens up to the checkpoint
	FutureWaste int // big-result tokens after it
	Verdict     evalVerdict
	Signals     string `json:"-"`
	Project     string `json:"-"`
}

// AdvisorEval is the offline result across sessions.
type AdvisorEval struct {
	Sessions, Checkpoints                    int
	Useful, LowValue, Missed, Quiet          int
	Precision, Recall                        float64
	Points                                   []evalCheckpoint
	Live                                     []liveResult
	LiveOK, LiveWithEvidence, LiveResearched int
}

func scanWaste(lines [][]byte) *TokenWaste {
	w := newTokenWaste()
	for _, line := range lines {
		var e tEntry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		var items []contentItem
		if json.Unmarshal(e.Message.Content, &items) != nil {
			continue
		}
		for _, it := range items {
			switch it.Type {
			case "tool_use":
				w.useCall(it.ID, it.Name, it.Input.Offset == 0 && it.Input.Limit == 0)
			case "tool_result":
				w.result(it.ToolUseID, toolResultText(it.Content))
			}
		}
	}
	return w
}

func readTranscriptLines(path string) [][]byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) > 0 {
			lines = append(lines, append([]byte(nil), sc.Bytes()...))
		}
	}
	return lines
}

func classifyCheckpoint(advised bool, future int) evalVerdict {
	switch {
	case advised && future >= futureWasteTokens:
		return verdictUseful
	case advised:
		return verdictLowValue
	case future >= futureWasteTokens:
		return verdictMissed
	}
	return verdictCorrectQuiet
}

// evalSession replays one transcript at 30, 60 and 90 percent of its length.
func evalSession(path, cwd string, scratch string) []evalCheckpoint {
	lines := readTranscriptLines(path)
	if len(lines) < 40 {
		return nil
	}
	session := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	var out []evalCheckpoint
	for _, percent := range []int{30, 60, 90} {
		cut := len(lines) * percent / 100
		prefix := filepath.Join(scratch, fmt.Sprintf("%s-%d.jsonl", session, percent))
		if os.WriteFile(prefix, append(bytes.Join(lines[:cut], []byte("\n")), '\n'), 0o600) != nil {
			continue
		}
		turns := 0
		for _, line := range lines[:cut] {
			if bytes.Contains(line, []byte(`"role":"assistant"`)) {
				turns++
			}
		}
		t := os.Getenv("CULLY_ANALYZE_PROMPTS")
		os.Setenv("CULLY_ANALYZE_PROMPTS", "0") // replay needs no prompt text
		sig := collectSignals(stopInput{TranscriptPath: prefix, Cwd: cwd, SessionID: "eval-" + session}, turns)
		os.Setenv("CULLY_ANALYZE_PROMPTS", t)
		_ = os.Remove(prefix)
		seen := scanWaste(lines[:cut])
		future := scanWaste(lines[cut:])
		advised := sig.WasteTokens > 0
		out = append(out, evalCheckpoint{
			Session: session, Percent: percent, Advised: advised,
			SeenWaste: seen.BigTokens, FutureWaste: future.BigTokens,
			Verdict: classifyCheckpoint(advised, future.BigTokens),
			Signals: formatSignals(sig), Project: cwd,
		})
	}
	return out
}

func summarizeEval(points []evalCheckpoint, sessions int) AdvisorEval {
	ev := AdvisorEval{Sessions: sessions, Checkpoints: len(points), Points: points}
	for _, p := range points {
		switch p.Verdict {
		case verdictUseful:
			ev.Useful++
		case verdictLowValue:
			ev.LowValue++
		case verdictMissed:
			ev.Missed++
		default:
			ev.Quiet++
		}
	}
	if advised := ev.Useful + ev.LowValue; advised > 0 {
		ev.Precision = float64(ev.Useful) / float64(advised)
	}
	if heavy := ev.Useful + ev.Missed; heavy > 0 {
		ev.Recall = float64(ev.Useful) / float64(heavy)
	}
	return ev
}

type liveResult struct {
	Session, Verdict string
	Lines            []string
	ToolGap          bool
	Researched       bool
	URL              bool
	HasEvidence      bool
	Err              string
}

var evidenceRe = regexp.MustCompile(`(?i)(full_file_reads|big_bash|big_results|top_output_tool|cully_result_tokens|\b\d[\d,.]*\s?(k|tok|tokens)\b)`)
var urlRe = regexp.MustCompile(`https?://\S+`)

// liveAdvice runs the real advisor on one checkpoint's signals: phase 1 for
// advice and memory, then phase 2 web research when it names a tool gap. It
// returns what the advisor said without touching any session state.
func liveAdvice(ctx context.Context, agent, cwd, signals string) liveResult {
	var res liveResult
	prompt := sharedAdvisorInstructions + "\nACTIVE AGENT: " + agent + "\n\nSIGNALS:\n" + signals
	out1, err := runAdvisorAgentContext(ctx, agent, cwd, false, prompt)
	if err != nil {
		res.Err = advisorFailureReason(err)
		return res
	}
	res.Lines = advisorLines(out1, 3)
	if gap, ok := extractToolGap(out1); ok && gap.Evidence != "" {
		res.ToolGap = true
		out2, err := runAdvisorAgentContext(ctx, agent, cwd, true, buildScoutPrompt(gap, signals))
		if err == nil && advisorSourceState(out2, "RESEARCH", false) == "checked" {
			res.Researched = true
			if tool := emojiLines(out2, 1); len(tool) > 0 {
				res.Lines = append(res.Lines, "ADV|"+tool[0])
				res.URL = urlRe.MatchString(tool[0])
			}
		}
	}
	for _, line := range res.Lines {
		if evidenceRe.MatchString(line) {
			res.HasEvidence = true
		}
	}
	return res
}

// Write prints the report.
func (ev AdvisorEval) Write(w io.Writer) {
	fmt.Fprintf(w, "ADVISOR REPLAY  (%d sessions, %d checkpoints at 30/60/90%% of each session)\n\n", ev.Sessions, ev.Checkpoints)
	fmt.Fprintf(w, "  Token-waste advice, judged in hindsight (heavy waste = %d+ tokens of big output after the checkpoint)\n", futureWasteTokens)
	fmt.Fprintf(w, "    useful         %4d  advised, and heavy waste followed\n", ev.Useful)
	fmt.Fprintf(w, "    low-value      %4d  advised, little waste followed\n", ev.LowValue)
	fmt.Fprintf(w, "    missed         %4d  quiet, then heavy waste followed\n", ev.Missed)
	fmt.Fprintf(w, "    correct-quiet  %4d  quiet, nothing heavy followed\n\n", ev.Quiet)
	fmt.Fprintf(w, "    precision %.0f%%  recall %.0f%%  (proxies: they say the advice was timely, not that it was followed)\n", ev.Precision*100, ev.Recall*100)
	if len(ev.Live) > 0 {
		fmt.Fprintf(w, "\n  Live advisor on %d checkpoints (real model, memory recall, web research on a tool gap)\n", len(ev.Live))
		fmt.Fprintf(w, "    answered %d · cited numbers %d · ran research %d\n", ev.LiveOK, ev.LiveWithEvidence, ev.LiveResearched)
		for _, r := range ev.Live {
			fmt.Fprintf(w, "    %s [%s]", short(r.Session), r.Verdict)
			if r.Err != "" {
				fmt.Fprintf(w, " failed: %s\n", r.Err)
				continue
			}
			fmt.Fprintf(w, " gap=%v researched=%v url=%v\n", r.ToolGap, r.Researched, r.URL)
			for _, line := range r.Lines {
				fmt.Fprintf(w, "        %s\n", truncateRunes(redactSecrets(line), 220))
			}
		}
	}
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// RunAdvisorEval implements `cully advisor-eval`.
func RunAdvisorEval(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("advisor-eval", flag.ContinueOnError)
	days := fs.Int("days", 30, "look back this many days")
	project := fs.String("project", "personal", "only transcript folders containing this text")
	max := fs.Int("max", 40, "most sessions to replay")
	live := fs.Int("live", 0, "also run the real advisor on this many checkpoints (uses model tokens and web search)")
	agent := fs.String("agent", "claude", "advisor agent for the live pass")
	asJSON := fs.Bool("json", false, "print JSON")
	root := fs.String("dir", "", "Claude projects directory (default ~/.claude/projects)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*root = filepath.Join(home, ".claude", "projects")
	}
	scratch, err := os.MkdirTemp("", "cully-eval-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	since := time.Now().AddDate(0, 0, -*days)
	type found struct {
		path string
		mod  time.Time
	}
	var paths []found
	_ = filepath.WalkDir(*root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if !strings.Contains(filepath.Base(filepath.Dir(path)), *project) {
			return nil
		}
		if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
			paths = append(paths, found{path, info.ModTime()})
		}
		return nil
	})
	sort.Slice(paths, func(i, j int) bool { return paths[i].mod.After(paths[j].mod) })
	if len(paths) > *max {
		paths = paths[:*max]
	}
	var points []evalCheckpoint
	sessions := 0
	cwd, _ := os.Getwd()
	for _, p := range paths {
		got := evalSession(p.path, cwd, scratch)
		if len(got) > 0 {
			sessions++
			points = append(points, got...)
		}
	}
	ev := summarizeEval(points, sessions)
	if *live > 0 {
		// Prefer checkpoints that carry the most signal: advised ones first.
		sort.SliceStable(points, func(i, j int) bool { return points[i].Advised && !points[j].Advised })
		for i := 0; i < len(points) && i < *live; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			r := liveAdvice(ctx, *agent, cwd, points[i].Signals)
			cancel()
			r.Session, r.Verdict = points[i].Session, string(points[i].Verdict)
			ev.Live = append(ev.Live, r)
			if r.Err == "" {
				ev.LiveOK++
			}
			if r.HasEvidence {
				ev.LiveWithEvidence++
			}
			if r.Researched {
				ev.LiveResearched++
			}
		}
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(ev)
	}
	ev.Write(w)
	return nil
}
