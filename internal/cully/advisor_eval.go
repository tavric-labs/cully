package cully

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	LiveCited, LiveUngrounded                int
	Context                                  ContextSummary
}

// ContextSummary averages SessionContext over replayed sessions: redundancy
// over sessions with at least two reads, the gold-based metrics over sessions
// that edited a file.
type ContextSummary struct {
	Sessions, WithEdits       int
	Redundancy                float64
	Recall, Precision, AUCCov float64
}

func summarizeContext(all []SessionContext) ContextSummary {
	var c ContextSummary
	redundant := 0
	for _, s := range all {
		if s.Steps > 1 {
			redundant++
			c.Redundancy += s.Redundant
		}
		if s.Steps > 0 && s.Edited > 0 {
			c.WithEdits++
			c.Recall += s.Recall
			c.Precision += s.Precision
			c.AUCCov += s.AUCCov
		}
	}
	c.Sessions = len(all)
	if redundant > 0 {
		c.Redundancy /= float64(redundant)
	}
	if c.WithEdits > 0 {
		n := float64(c.WithEdits)
		c.Recall, c.Precision, c.AUCCov = c.Recall/n, c.Precision/n, c.AUCCov/n
	}
	return c
}

func scanWaste(lines [][]byte) *TokenWaste {
	w := newTokenWaste()
	for _, line := range lines {
		scanWasteLine(w, line)
	}
	return w
}

// scanWasteLine updates counts without retaining the source line. Delegations
// count actual tool calls rather than strings quoted inside tool output.
func scanWasteLine(w *TokenWaste, line []byte) int {
	var e tEntry
	if json.Unmarshal(line, &e) != nil {
		return 0
	}
	var items []contentItem
	if json.Unmarshal(e.Message.Content, &items) != nil {
		return 0
	}
	delegations := 0
	for _, it := range items {
		switch it.Type {
		case "tool_use":
			w.useCall(it.ID, it.Name, it.Input.Offset == 0 && it.Input.Limit == 0)
			if (it.Name == "Task" || it.Name == "Agent") && w.firstUse(it.ID) {
				delegations++
			}
		case "tool_result":
			w.result(it.ToolUseID, toolResultText(it.Content))
		}
	}
	return delegations
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
	Quiet            bool     // the checkpoint carried no token-waste signal
	Cited            int      // figures of 10 or more in the advice
	Ungrounded       []string // cited figures that appear nowhere in the signals
	Err              string
}

var evidenceRe = regexp.MustCompile(`(?i)(full_file_reads|big_bash|big_results|top_output_tool|cully_result_tokens|\b\d[\d,.]*\s?(k|tok|tokens)\b)`)
var urlRe = regexp.MustCompile(`https?://\S+`)

// figureRe finds numbers such as 36000, 36,000 and 12k. A trailing percent sign
// is excluded by the caller, since shares are derived rather than measured.
var figureRe = regexp.MustCompile(`(\d[\d,]*(?:\.\d+)?)(\s?[kK]\b)?(%?)`)

func figures(text string, skipPercent bool) []float64 {
	var out []float64
	for _, m := range figureRe.FindAllStringSubmatch(urlRe.ReplaceAllString(text, ""), -1) {
		if skipPercent && m[3] != "" {
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			continue
		}
		if m[2] != "" {
			v *= 1000
		}
		out = append(out, v)
	}
	return out
}

// groundFigures checks that every figure of 10 or more in the advice matches a
// number the advisor was actually given, within 10% so that "about 12k" can
// stand for 12,340. A figure with no counterpart in the signals was invented.
// Percentages are skipped because they are derived, not measured.
func groundFigures(advice []string, signals string) (cited int, ungrounded []string) {
	known := figures(signals, false)
	for _, line := range advice {
		for _, v := range figures(line, true) {
			if v < 10 {
				continue
			}
			cited++
			ok := false
			for _, k := range known {
				if math.Abs(v-k) <= math.Max(1, 0.1*k) {
					ok = true
					break
				}
			}
			if !ok {
				ungrounded = append(ungrounded, strconv.FormatFloat(v, 'f', -1, 64))
			}
		}
	}
	return cited, ungrounded
}

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
		fmt.Fprintf(w, "    answered %d · cited numbers %d · ran research %d · figures cited %d, not in the signals %d\n", ev.LiveOK, ev.LiveWithEvidence, ev.LiveResearched, ev.LiveCited, ev.LiveUngrounded)
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

// replayTranscripts replays the most recent Claude transcripts whose project
// folder contains project, newest first.
func replayTranscripts(root, project string, days, max int) (points []evalCheckpoint, sessions []SessionContext, err error) {
	scratch, err := os.MkdirTemp("", "cully-eval-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(scratch)
	since := time.Now().AddDate(0, 0, -days)
	type found struct {
		path string
		mod  time.Time
	}
	var paths []found
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if !strings.Contains(filepath.Base(filepath.Dir(path)), project) {
			return nil
		}
		if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
			paths = append(paths, found{path, info.ModTime()})
		}
		return nil
	})
	sort.Slice(paths, func(i, j int) bool { return paths[i].mod.After(paths[j].mod) })
	if len(paths) > max {
		paths = paths[:max]
	}
	cwd, _ := os.Getwd()
	for _, p := range paths {
		got := evalSession(p.path, cwd, scratch)
		if len(got) > 0 {
			points = append(points, got...)
			sessions = append(sessions, measureSessionContext(readTranscriptLines(p.path)))
		}
	}
	return points, sessions, nil
}

// runLive asks the real advisor about up to n checkpoints, alternating between
// ones with a waste signal and quiet ones so both speaking and silence are
// tested, then checks that every figure it cites was in its signals.
func (ev *AdvisorEval) runLive(agent string, points []evalCheckpoint, n int) {
	var advised, quiet []evalCheckpoint
	for _, p := range points {
		if p.Advised {
			advised = append(advised, p)
		} else {
			quiet = append(quiet, p)
		}
	}
	var picked []evalCheckpoint
	for len(picked) < n && (len(advised) > 0 || len(quiet) > 0) {
		if len(advised) > 0 {
			picked, advised = append(picked, advised[0]), advised[1:]
		}
		if len(picked) < n && len(quiet) > 0 {
			picked, quiet = append(picked, quiet[0]), quiet[1:]
		}
	}
	cwd, _ := os.Getwd()
	for _, p := range picked {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		r := liveAdvice(ctx, agent, cwd, p.Signals)
		cancel()
		r.Session, r.Verdict, r.Quiet = p.Session, string(p.Verdict), !p.Advised
		r.Cited, r.Ungrounded = groundFigures(r.Lines, p.Signals)
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
		ev.LiveCited += r.Cited
		ev.LiveUngrounded += len(r.Ungrounded)
	}
}
