package cully

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The impact ledger is an append-only log of things Cully did that show its
// value or its cost: memory that returned prior work, lookups that came back
// empty, advisor analyses it skipped, loops it caught and token waste it
// flagged. Events carry counts and token estimates only, never content.
const (
	impactMemoryHit       = "memory_hit"
	impactMemoryEmpty     = "memory_empty"
	impactAnalysisSkipped = "analysis_skipped"
	impactLoopCaught      = "loop_caught"
	impactWasteFlagged    = "waste_flagged"

	// typicalSignalTokens is the usual size of the SIGNALS block sent with the
	// advisor instructions. A skipped analysis avoids both.
	typicalSignalTokens = 450
	impactLedgerMax     = 4 << 20
)

type impactEvent struct {
	At      time.Time `json:"at"`
	Session string    `json:"session,omitempty"`
	Kind    string    `json:"kind"`
	Tokens  int64     `json:"tokens,omitempty"`
	Tool    string    `json:"tool,omitempty"`
}

func impactLedgerFile() string { return filepath.Join(cullyDir(), "impact.jsonl") }

// recordImpact appends one event. It never fails the caller: a hook must not
// break a coding session because the ledger could not be written.
func recordImpact(session, kind, tool string, tokens int64) {
	line, err := json.Marshal(impactEvent{At: time.Now().UTC(), Session: safeSession(session), Kind: kind, Tool: tool, Tokens: tokens})
	if err != nil {
		return
	}
	path := impactLedgerFile()
	if info, err := os.Stat(path); err == nil && info.Size() > impactLedgerMax {
		trimImpactLedger(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// trimImpactLedger keeps the newest half of the file.
func trimImpactLedger(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	data = data[len(data)/2:]
	if i := strings.IndexByte(string(data), '\n'); i >= 0 {
		data = data[i+1:]
	}
	_ = os.WriteFile(path, data, 0o600)
}

func readImpactEvents(path string, since time.Time) []impactEvent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var events []impactEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e impactEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Kind != "" && !e.At.Before(since) {
			events = append(events, e)
		}
	}
	return events
}

// cullyResponseHit reports whether a Cully lookup returned prior work. Only a
// lookup tool can hit; a write or a get is not a retrieval win.
func cullyResponseHit(tool string, raw json.RawMessage) (isLookup, hit bool, tokens int64) {
	switch tool {
	case "cully_context", "cully_search", "cully_recall", "cully_recent":
	default:
		return false, false, 0
	}
	text := string(raw)
	var blocks struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) == nil && len(blocks.Content) > 0 {
		var b strings.Builder
		for _, c := range blocks.Content {
			b.WriteString(c.Text)
		}
		text = b.String()
	}
	tokens = approxTokens(len(text))
	return true, !emptyCullyResult(text) && strings.Contains(text, `"summary"`), tokens
}

// noteCullyLookup records whether a lookup helped. It is called from the hooks
// that already see every Cully tool response.
func noteCullyLookup(session, tool string, response json.RawMessage) {
	isLookup, hit, tokens := cullyResponseHit(tool, response)
	if !isLookup {
		return
	}
	kind := impactMemoryEmpty
	if hit {
		kind = impactMemoryHit
	}
	recordImpact(session, kind, tool, tokens)
}

// Impact is the headline view of what Cully did over a period.
type Impact struct {
	Days                       int
	MemoryHits, MemoryEmpty    int
	HitTokens, EmptyTokens     int64 // what the lookups cost
	AnalysesSkipped            int
	AnalysisTokensAvoided      int64 // estimated: instructions and signals not sent
	LoopsCaught                int
	WasteSessions              int
	WasteTokensFlagged         int64 // measured: oversized output the advisor pointed out
	PerHitSavedTokens          int64 // observational, from `cully savings`; 0 when unknown
	MemoryTokensSaved          int64
	TotalSaved, TotalCost, Net int64
	SavedBasis                 string
	Sessions                   int
	LastEvents                 []impactEvent
	byTool                     map[string]int
	wasteBySession             map[string]int64
}

// SummarizeImpact folds events into an Impact. perHit is the observed tokens
// saved per memory hit, or 0 when there is no trustworthy figure.
func SummarizeImpact(events []impactEvent, days int, perHit int64) Impact {
	im := Impact{Days: days, PerHitSavedTokens: perHit, byTool: map[string]int{}, wasteBySession: map[string]int64{}}
	sessions := map[string]bool{}
	for _, e := range events {
		if e.Session != "" {
			sessions[e.Session] = true
		}
		switch e.Kind {
		case impactMemoryHit:
			im.MemoryHits++
			im.HitTokens += e.Tokens
			im.byTool[e.Tool]++
		case impactMemoryEmpty:
			im.MemoryEmpty++
			im.EmptyTokens += e.Tokens
		case impactAnalysisSkipped:
			im.AnalysesSkipped++
			im.AnalysisTokensAvoided += e.Tokens
		case impactLoopCaught:
			im.LoopsCaught++
		case impactWasteFlagged:
			// A session reports its running total; keep the largest.
			im.wasteBySession[e.Session] = max(im.wasteBySession[e.Session], e.Tokens)
		}
	}
	for _, tokens := range im.wasteBySession {
		im.WasteSessions++
		im.WasteTokensFlagged += tokens
	}
	im.Sessions = len(sessions)
	if perHit > 0 {
		im.MemoryTokensSaved = perHit * int64(im.MemoryHits)
		im.SavedBasis = "memory saving uses the observed gap in exploration tokens between sessions with and without a hit"
	} else {
		im.SavedBasis = "memory saving is not claimed: no trustworthy with/without comparison yet"
	}
	im.TotalSaved = im.MemoryTokensSaved + im.AnalysisTokensAvoided
	im.TotalCost = im.HitTokens + im.EmptyTokens
	im.Net = im.TotalSaved - im.TotalCost
	events = append([]impactEvent(nil), events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
	im.LastEvents = events[:min(len(events), 8)]
	return im
}

// empty is true when no event has been recorded in the period.
func (im Impact) empty() bool {
	return im.MemoryHits+im.MemoryEmpty+im.AnalysesSkipped+im.LoopsCaught+im.WasteSessions == 0
}

// Lines renders the compact view used in the panel: the wins first, never the
// last tool call.
func (im Impact) Lines() []string {
	var out []string
	switch {
	case im.MemoryHits > 0:
		saved := ""
		if im.MemoryTokensSaved > 0 {
			saved = fmt.Sprintf(" · ≈%s tokens saved", compactCount(im.MemoryTokensSaved))
		}
		out = append(out, fmt.Sprintf("Memory used %d× (prior work found)%s", im.MemoryHits, saved))
	case im.MemoryEmpty > 0:
		out = append(out, fmt.Sprintf("Memory checked %d× · nothing relevant yet", im.MemoryEmpty))
	default:
		out = append(out, "Memory not used yet")
	}
	if im.AnalysesSkipped > 0 {
		out = append(out, fmt.Sprintf("Analysis avoided %d× · ≈%s tokens not spent", im.AnalysesSkipped, compactCount(im.AnalysisTokensAvoided)))
	}
	if im.LoopsCaught > 0 {
		out = append(out, fmt.Sprintf("Loops caught %d", im.LoopsCaught))
	}
	if im.WasteTokensFlagged > 0 {
		out = append(out, fmt.Sprintf("Oversized output flagged · %s tokens", compactCount(im.WasteTokensFlagged)))
	}
	out = append(out, fmt.Sprintf("Net %s tokens (≈saved %s, cost %s)", signedCount(im.Net), compactCount(im.TotalSaved), compactCount(im.TotalCost)))
	return out
}

func compactCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

func signedCount(n int64) string {
	if n < 0 {
		return "-" + compactCount(-n)
	}
	return "+" + compactCount(n)
}

// Write prints the full report for `cully impact`.
func (im Impact) Write(w io.Writer, showLog bool) {
	fmt.Fprintf(w, "CULLY IMPACT  (last %d days, %d sessions)\n\n", im.Days, im.Sessions)
	fmt.Fprintf(w, "  Wins\n")
	fmt.Fprintf(w, "    Memory used            %5d lookups returned prior work\n", im.MemoryHits)
	fmt.Fprintf(w, "    Analyses avoided       %5d  ≈%s tokens not sent (estimated)\n", im.AnalysesSkipped, compactCount(im.AnalysisTokensAvoided))
	fmt.Fprintf(w, "    Loops caught           %5d\n", im.LoopsCaught)
	fmt.Fprintf(w, "    Oversized output flagged %3d sessions · %s tokens (measured)\n\n", im.WasteSessions, compactCount(im.WasteTokensFlagged))
	fmt.Fprintf(w, "  Tokens\n")
	fmt.Fprintf(w, "    Saved (estimated)      %8s   memory %s + analyses %s\n", compactCount(im.TotalSaved), compactCount(im.MemoryTokensSaved), compactCount(im.AnalysisTokensAvoided))
	fmt.Fprintf(w, "    Cost (measured)        %8s   %d hits %s + %d empty %s\n", compactCount(im.TotalCost), im.MemoryHits, compactCount(im.HitTokens), im.MemoryEmpty, compactCount(im.EmptyTokens))
	verdict := "Cully is saving tokens"
	switch {
	case im.empty():
		verdict = "nothing recorded yet; this fills in as lookups and the advisor run"
	case im.Net < 0:
		verdict = "Cully is costing more than it saves so far"
	}
	fmt.Fprintf(w, "    Net                    %8s   %s\n\n", signedCount(im.Net), verdict)
	fmt.Fprintf(w, "  Basis: %s.\n", im.SavedBasis)
	if im.MemoryEmpty > 0 && im.MemoryHits == 0 {
		fmt.Fprintf(w, "  Hint: every memory lookup came back empty. Save better notes with cully_log so the next session can reuse them.\n")
	}
	if showLog {
		fmt.Fprintf(w, "\n  Recent events\n")
		for _, e := range im.LastEvents {
			fmt.Fprintf(w, "    %s  %-17s %-14s %s\n", e.At.Local().Format("Jan 02 15:04"), e.Kind, e.Tool, tokenText(e.Tokens))
		}
		if len(im.LastEvents) == 0 {
			fmt.Fprintf(w, "    none yet\n")
		}
	}
}

// WriteExplain justifies the Net figure: where each saved and spent token
// comes from, how it was obtained, and what is deliberately not counted.
func (im Impact) WriteExplain(w io.Writer) {
	fmt.Fprintf(w, "\nHOW THE NET FIGURE IS BUILT  (last %d days)\n\n", im.Days)
	fmt.Fprintf(w, "  Net %s = saved %s - cost %s\n\n", signedCount(im.Net), compactCount(im.TotalSaved), compactCount(im.TotalCost))
	fmt.Fprintf(w, "  Saved\n")
	if im.AnalysesSkipped > 0 {
		fmt.Fprintf(w, "    Advisor analyses skipped   %d × ≈%s tokens = ≈%s   (estimated)\n", im.AnalysesSkipped, compactCount(im.AnalysisTokensAvoided/int64(im.AnalysesSkipped)), compactCount(im.AnalysisTokensAvoided))
		fmt.Fprintf(w, "      Each skip avoided re-sending the advisor instructions and the session signals to the model.\n")
		fmt.Fprintf(w, "      Size is the length of that prompt at about 4 characters per token, not billed usage.\n")
	} else {
		fmt.Fprintf(w, "    Advisor analyses skipped   none recorded\n")
	}
	if im.MemoryTokensSaved > 0 {
		fmt.Fprintf(w, "    Memory hits                %d × ≈%s tokens = ≈%s   (observed)\n", im.MemoryHits, compactCount(im.PerHitSavedTokens), compactCount(im.MemoryTokensSaved))
		fmt.Fprintf(w, "      Per-hit saving is the observed gap in exploration tokens between sessions with and without a hit.\n")
	} else {
		fmt.Fprintf(w, "    Memory hits                %d, no saving claimed: %s\n", im.MemoryHits, im.SavedBasis)
	}
	fmt.Fprintf(w, "\n  Cost\n")
	fmt.Fprintf(w, "    Memory lookups             %d hits %s + %d empty %s   (measured from response size)\n", im.MemoryHits, compactCount(im.HitTokens), im.MemoryEmpty, compactCount(im.EmptyTokens))
	if im.MemoryHits+im.MemoryEmpty == 0 {
		fmt.Fprintf(w, "      Cost is 0 because no Cully memory lookup was observed. Lookups are seen only through Cully hooks, so\n")
		fmt.Fprintf(w, "      sessions outside the Cully terminal or without MCP calls record none.\n")
	}
	fmt.Fprintf(w, "\n  Shown but not counted in Net\n")
	fmt.Fprintf(w, "    Oversized output flagged   %d sessions · %s tokens   (measured; advice, not a saving)\n", im.WasteSessions, compactCount(im.WasteTokensFlagged))
	fmt.Fprintf(w, "    Loops caught               %d   (no token value is assigned)\n", im.LoopsCaught)
	fmt.Fprintf(w, "\n  Limits: estimated figures are not billing data, and the tokens the advisor spends on the analyses it\n")
	fmt.Fprintf(w, "  does run are not subtracted. Use cully savings to compare token use from transcripts.\n")
}

// writeInjectedCost shows what Cully's own calls added to the conversations in
// Claude transcripts, which the Net above does not subtract. Without it the
// ledger's estimate and `cully savings` can disagree in sign.
func writeInjectedCost(w io.Writer, s Savings) {
	if s.Calls == 0 {
		return
	}
	cost := s.ResultTokens + s.ArgTokens + s.SchemaTokens
	fmt.Fprintf(w, "\n  Seen in your Claude transcripts (cully savings)\n")
	fmt.Fprintf(w, "    Cully's calls injected ≈%s tokens across %d calls: results %s, arguments %s, schemas %s.\n",
		compactCount(cost), s.Calls, compactCount(s.ResultTokens), compactCount(s.ArgTokens), compactCount(s.SchemaTokens))
	fmt.Fprintf(w, "    The Net above does not subtract this. cully savings weighs it against exploration avoided and reports Net %s. Confidence: %s.\n", signedCount(s.NetTokens), s.Confidence)
}

func tokenText(n int64) string {
	if n == 0 {
		return ""
	}
	return compactCount(n) + " tok"
}

// observedPerHitSaving reuses the `cully savings` comparison, but only when
// both cohorts are large enough to mean something.
func observedPerHitSaving(root string, since time.Time) int64 {
	s, err := ComputeSavings(root, "", since)
	if err != nil || s.WithHit < 5 || s.WithoutCully+s.WithMiss < 5 || s.WithHit == 0 {
		return 0
	}
	return s.EstimatedSaved / int64(s.WithHit)
}

// RunImpact implements `cully impact [--days N] [--json] [--log]`.
func RunImpact(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("impact", flag.ContinueOnError)
	days := fs.Int("days", 14, "look back this many days")
	asJSON := fs.Bool("json", false, "print JSON")
	showLog := fs.Bool("log", false, "also list recent events")
	explain := fs.Bool("explain", false, "show how the Net token figure is built and what it does not count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	since := time.Now().AddDate(0, 0, -*days)
	var perHit int64
	if home, err := os.UserHomeDir(); err == nil {
		perHit = observedPerHitSaving(filepath.Join(home, ".claude", "projects"), since)
	}
	im := SummarizeImpact(readImpactEvents(impactLedgerFile(), since), *days, perHit)
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(im)
	}
	im.Write(w, *showLog)
	if *explain {
		im.WriteExplain(w)
		if home, err := os.UserHomeDir(); err == nil {
			if s, err := ComputeSavings(filepath.Join(home, ".claude", "projects"), "", since); err == nil {
				writeInjectedCost(w, s)
			}
		}
	}
	return nil
}

// impactCache avoids re-reading the ledger on every panel refresh.
var impactCache struct {
	sync.Mutex
	size  int64
	mtime time.Time
	since time.Time
	value Impact
	valid bool
}

// ImpactSince is the impact of everything recorded since the given time, for
// the panel. It re-reads the ledger only when the file changed.
func ImpactSince(since time.Time) Impact {
	path := impactLedgerFile()
	info, err := os.Stat(path)
	if err != nil {
		return SummarizeImpact(nil, 0, 0)
	}
	impactCache.Lock()
	defer impactCache.Unlock()
	if impactCache.valid && impactCache.size == info.Size() && impactCache.mtime.Equal(info.ModTime()) && impactCache.since.Equal(since) {
		return impactCache.value
	}
	value := SummarizeImpact(readImpactEvents(path, since), 0, 0)
	impactCache.size, impactCache.mtime, impactCache.since, impactCache.value, impactCache.valid = info.Size(), info.ModTime(), since, value, true
	return value
}
