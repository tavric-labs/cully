package cully

import "fmt"

// bigResultTokens is where one tool result starts to matter: it stays in the
// context for the rest of the session and is re-read on every later turn.
const bigResultTokens = 2000

// TokenWaste summarises where a session's tool output went. It holds counts
// and token estimates only, never tool output.
type TokenWaste struct {
	ResultTokens               int64
	BigResults, BigTokens      int
	FullReads, FullReadTokens  int // Read without offset or limit
	BigBash, BigBashTokens     int
	TopTool                    string
	TopToolTokens, CullyTokens int64
	ctxTokens                  int64
	lastBigAt                  int64
	resultN, lastBigN          int
	byTool                     map[string]int64
	pendingRead                map[string]bool
	toolOf                     map[string]string
	usedIDs, resultIDs         map[string]bool // a transcript can repeat an entry; count each tool id once
}

func newTokenWaste() *TokenWaste {
	return &TokenWaste{byTool: map[string]int64{}, pendingRead: map[string]bool{}, toolOf: map[string]string{}, usedIDs: map[string]bool{}, resultIDs: map[string]bool{}}
}

// firstUse reports whether this is the first time a tool call id is seen.
// Claude Code transcripts can contain the same tool call more than once.
func (w *TokenWaste) firstUse(id string) bool {
	if id == "" {
		return true // nothing to dedupe on
	}
	if w.usedIDs[id] {
		return false
	}
	w.usedIDs[id] = true
	return true
}

// useCall records a tool call so its result can be attributed. wholeFile marks
// a Read with neither offset nor limit.
func (w *TokenWaste) useCall(id, name string, wholeFile bool) {
	if id == "" {
		return
	}
	w.toolOf[id] = name
	if name == "Read" {
		w.pendingRead[id] = wholeFile
	}
}

// result records the size of a tool result.
func (w *TokenWaste) result(id, text string) {
	name := w.toolOf[id]
	if name == "" {
		return
	}
	if w.resultIDs[id] {
		return // the same result repeated in the transcript
	}
	w.resultIDs[id] = true
	w.resultN++
	tokens := approxTokens(len(text))
	w.ResultTokens += tokens
	w.byTool[name] += tokens
	if cullyTool(name) != "" {
		w.CullyTokens += tokens
	}
	if tokens < bigResultTokens {
		return
	}
	w.BigResults++
	w.lastBigAt = w.ResultTokens
	w.lastBigN = w.resultN
	w.BigTokens += int(tokens)
	switch {
	case name == "Read" && w.pendingRead[id]:
		w.FullReads++
		w.FullReadTokens += int(tokens)
	case name == "Bash":
		w.BigBash++
		w.BigBashTokens += int(tokens)
	}
}

func (w *TokenWaste) finish(ctxTokens int64) {
	w.ctxTokens = ctxTokens
	for name, tokens := range w.byTool {
		if tokens > w.TopToolTokens || tokens == w.TopToolTokens && name < w.TopTool {
			w.TopTool, w.TopToolTokens = name, tokens
		}
	}
}

// Significant is true only when the waste is large enough to be worth the
// user's attention: several big results, and a real share of the context or a
// large absolute amount. It also requires oversized output in the last 15
// results and the newer half of output tokens; old waste is not current advice.
func (w *TokenWaste) Significant() bool {
	if w.BigResults < 3 || w.resultN-w.lastBigN >= 15 || w.lastBigAt*2 < w.ResultTokens {
		return false
	}
	return w.BigTokens >= 10000 || (w.ctxTokens > 0 && int64(w.BigTokens)*100 >= w.ctxTokens*15)
}

func (w *TokenWaste) String() string {
	top := "none"
	if w.TopTool != "" {
		top = fmt.Sprintf("%s(%d tok)", w.TopTool, w.TopToolTokens)
	}
	significant := "no"
	if w.Significant() {
		significant = "yes"
	}
	return fmt.Sprintf("token_waste_significant=%s  tool_result_tokens=%d  big_results=%d(%d tok)  full_file_reads=%d(%d tok)  big_bash=%d(%d tok)  top_output_tool=%s  cully_result_tokens=%d",
		significant, w.ResultTokens, w.BigResults, w.BigTokens, w.FullReads, w.FullReadTokens, w.BigBash, w.BigBashTokens, top, w.CullyTokens)
}
