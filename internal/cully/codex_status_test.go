package cully

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestCodexNativeFooter(t *testing.T) {
	emulator := vt.NewEmulator(180, 20)
	defer emulator.Close()
	_, _ = emulator.WriteString("\x1b[19;1HGPT-6.1-Sol medium · Context 73% left · 1.2K in · 240 out · 5h 82% left · Weekly 91% left · Fast off")
	var view sessionView
	rows, base := footerRows(emulator)
	upd, match := parseFooterRows(rows, base)
	view.mergeUpdate(upd)
	if !match.Found || match.Row != 18 {
		t.Fatalf("footer match = %+v", match)
	}
	if view.Model != "GPT-6.1-Sol medium" || !view.ContextKnown || view.ContextLeft != 73 || view.Input != "1.2K" || view.Output != "240" || view.FiveHour != "5h 82% left" || view.Weekly != "Weekly 91% left" || view.Fast != "Fast off" {
		t.Fatalf("incorrect native instruments: %+v", view)
	}
	content := sessionStatusRows(180, nil, toolStats{}, view)
	frame := ansi.Strip(renderTerminalPane(emulator, 180, 32, content, true, ""))
	if !strings.Contains(frame, "Context 73% left · 1.2K in") {
		t.Fatal("Cully removed the user's native footer")
	}
	if !strings.Contains(frame, "73% left") || !strings.Contains(frame, "GPT-6.1-Sol medium") {
		t.Fatal("native instruments missing from Cully")
	}
	// An ordinary message or stale metric in the body is not a footer.
	_, _ = emulator.WriteString("\x1b[19;1H\x1b[2KContext 8% left\x1b[2;1HGPT-6.1-Sol medium · Context 9% left · 1 in · 1 out")
	rows, base = footerRows(emulator)
	upd, match = parseFooterRows(rows, base)
	view.mergeUpdate(upd)
	if match.Found || match.Row != -1 || view.ContextLeft != 73 {
		t.Fatal("body text incorrectly used as footer telemetry")
	}
}

func TestCodexStatusReadableAndExpandable(t *testing.T) {
	stats := toolStats{Tools: 14, Searches: 3, Edits: 2, Checks: 1, Errors: 1, EditsSinceCheck: 2}
	view := sessionView{Project: "/work/cully", Branch: "main", Model: "GPT-6.1-Sol high", ContextKnown: true, ContextLeft: 8, Daemon: true}
	advice := []string{"ADV|Run a focused verification before finishing these changes; review failures and fix the first error before repeating a command."}
	wide := sessionStatusRows(150, advice, stats, view)
	narrow := sessionStatusRows(42, advice, stats, view)
	for _, row := range narrow {
		if ansi.StringWidth(row) > 41 {
			t.Fatalf("row overflows: %q", row)
		}
	}
	if len(narrow) <= len(wide) || paneTop(55, len(narrow)) >= paneTop(55, len(wide)) {
		t.Fatal("wrapped content should expand the panel upward")
	}
	plain := normalizedCodexPanel(strings.Join(wide, "\n"))
	for _, want := range []string{"Cully", "Tools 14", "Searches 3", "Edits 2", "Checks 1", "Errors 1", "92% used", "8% left", "/compact", "GPT-6.1-Sol high", "main", "daemon online", "Advisor", "Run a focused verification", "2 edits awaiting a successful check", gauge(92), "Tokens", "5h limit unavailable", "Weekly unavailable", "Fast unavailable", "/prompts:cully", "cully suggestions", "elapsed unavailable", "🧠", "📁", "🔧"} {
		if !strings.Contains(plain, normalizedCodexPanel(want)) {
			t.Fatalf("missing status field %q", want)
		}
	}
	for _, unwanted := range []string{"n/a", "Cost $0", "cache 0"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("status clutter remains: %q", unwanted)
		}
	}
	clipped := panelLines(42, 4, narrow, "")
	if len(clipped) != 4 || !strings.Contains(ansi.Strip(clipped[2]), "Advisor") || !strings.Contains(ansi.Strip(clipped[3]), "Run a focused") {
		t.Fatal("short terminals must preserve the advisor and recommendation")
	}
	if !strings.Contains(ansi.Strip(clipped[1]), "Context") {
		t.Fatal("short panels must prioritize context over decorative headings")
	}
}

func TestCodexNativeFooterRetainsClippedInstruments(t *testing.T) {
	emulator := vt.NewEmulator(180, 20)
	defer emulator.Close()
	view := sessionView{Input: "1.2K", Output: "240", FiveHour: "5h 82% left", Weekly: "Weekly 91% left", Fast: "Fast off"}
	_, _ = emulator.WriteString("\x1b[19;1HGPT-6.1-Sol medium · Context 70% left · 1.4K in")
	rows, base := footerRows(emulator)
	upd, match := parseFooterRows(rows, base)
	view.mergeUpdate(upd)
	if !match.Found || match.Row != 18 || view.Input != "1.4K" || view.Output != "240" || view.FiveHour != "5h 82% left" || view.Weekly != "Weekly 91% left" || view.Fast != "Fast off" {
		t.Fatalf("clipped footer erased known instruments or failed to update: %+v", view)
	}
	_, _ = emulator.WriteString("\x1b[19;1H\x1b[2KGPT-6.1-Sol medium · Context 68% left · 300 out · 5h 80% left · Weekly 90% left · Fast on")
	rows, base = footerRows(emulator)
	upd, match = parseFooterRows(rows, base)
	view.mergeUpdate(upd)
	if !match.Found || match.Row != 18 || view.Input != "1.4K" || view.Output != "300" || view.FiveHour != "5h 80% left" || view.Weekly != "Weekly 90% left" || view.Fast != "Fast on" {
		t.Fatalf("new footer instruments were not applied: %+v", view)
	}
}

func TestCodexCompactGroupSpacingAdaptsWithoutLosingMetrics(t *testing.T) {
	isolateWorkflow(t)
	stats := toolStats{Tools: 60, Searches: 3, Edits: 2, Checks: 1}
	stats.Cully.Health, stats.Cully.Auth = "healthy", "authenticated"
	stats.Cully.CheckedAt = "2026-10-07T12:34:56Z"
	view := sessionView{Project: "/work/cully", Branch: "main", Model: "GPT-6.1-Sol high", ContextKnown: true, ContextLeft: 73, Input: "1.2K", Output: "240", FiveHour: "5h 82% left", Weekly: "Weekly 91% left", Fast: "Fast off"}
	advice := []string{"CAUT|Inspect repeated failures.", "ADV|Run focused verification."}
	wide := compactSessionStatusRows(149, 43, advice, stats, view)
	find := func(rows []string, text string) int {
		for i, row := range rows {
			if strings.Contains(ansi.Strip(row), text) {
				return i
			}
		}
		t.Fatalf("panel lost %q", text)
		return -1
	}
	for _, pair := range [][2]string{{"Model GPT", "Setup "}, {"Waste ", "Verification"}, {"Project cully", "Context "}, {"Tokens in", "5h 82%"}, {"Memory not used yet", "Net +0"}, {"Net +0", "MCP 0 calls"}} {
		if gap := find(wide, pair[1]) - find(wide, pair[0]); gap != 1 {
			t.Fatalf("metric spacing %q → %q = %d, want1", pair[0], pair[1], gap)
		}
	}
	heading := find(wide, "Advisor has")
	if heading < 1 || strings.TrimSpace(ansi.Strip(wide[heading-1])) != "" || strings.TrimSpace(ansi.Strip(wide[heading+1])) != "" {
		t.Fatal("Advisor title must have one separator before and after")
	}
	longAdvice := []string{"CAUT|" + strings.Repeat("Inspect the first failure before retrying. ", 4), "ADV|" + strings.Repeat("Run focused verification before finishing. ", 4)}
	longRows := compactSessionStatusRows(149, 43, longAdvice, stats, view)
	if gap := find(longRows, "Setup ") - find(longRows, "Model GPT"); gap != 1 {
		t.Fatal("wrapped previews removed metric spacing on a normal wide terminal", gap)
	}
	for _, size := range [][2]int{{149, 16}, {80, 43}} {
		rows := compactSessionStatusRows(size[0], size[1], advice, stats, view)
		for _, metric := range []string{"Setup ", "Verification", "Tokens in", "Auth authenticated", "Inspect repeated failures", "Run focused verification", "Open advisor"} {
			find(rows, metric)
		}
		if len(rows)+1 > min(size[1], 22) {
			t.Fatal("optional spacing exceeded compact panel budget")
		}
	}
}

func TestCodexStatusDistinguishesAvailableAndMissingInstruments(t *testing.T) {
	view := sessionView{Project: "/work/cully", Input: "1.2K", Output: "240", FiveHour: "5h 82% left", Weekly: "Weekly 91% left", Fast: "Fast off"}
	plain := normalizedCodexPanel(strings.Join(sessionStatusRows(180, nil, toolStats{}, view), "\n"))
	for _, want := range []string{"Input 1.2K", "Output 240", "5h limit 82% left", "Weekly 91% left", "Fast off", "daemon offline", "Watching Codex tool activity"} {
		if !strings.Contains(plain, normalizedCodexPanel(want)) {
			t.Fatalf("missing observed instrument %q", want)
		}
	}
	for _, want := range []string{"Context  waiting for Codex footer", "Model  waiting for Codex footer", "Working tree  unavailable", "Errors 0", "Verification  unknown: incomplete signals", "no tool events received"} {
		if !strings.Contains(plain, normalizedCodexPanel(want)) {
			t.Fatalf("missing instrument state %q", want)
		}
	}
}

func TestCodexStatusWorkingTreeAndPhase(t *testing.T) {
	view := sessionView{Project: "/work/cully", ChangesKnown: true, LinesAdded: 42, LinesRemoved: 7, ChangedFiles: 3}
	for _, tc := range []struct {
		stats toolStats
		left  int
		known bool
		phase string
	}{
		{phase: "preflight"},
		{stats: toolStats{Tools: 1}, phase: "cruise"},
		{stats: toolStats{Tools: 3, Errors: 3}, phase: "messy"},
		{left: 10, known: true, phase: "emergency"},
	} {
		view.ContextLeft, view.ContextKnown = tc.left, tc.known
		rows := sessionStatusRows(180, nil, tc.stats, view)
		if !strings.Contains(rows[0], formatPhaseBadge(tc.phase)) {
			t.Fatalf("wrong phase for %+v", tc)
		}
		plain := normalizedCodexPanel(strings.Join(rows, "\n"))
		if !strings.Contains(plain, "Working tree +42 / -7 · 3 tracked files") {
			t.Fatal("working tree must show actual Git totals")
		}
	}
	for _, cols := range []int{20, 42, 80, 150} {
		for _, row := range sessionStatusRows(cols, []string{"WARN|⚠️ Inspect this failure before retrying."}, toolStats{}, view) {
			if ansi.StringWidth(row) > cols-1 {
				t.Fatalf("icon/color row overflows %d columns: %q", cols, row)
			}
		}
	}
}

func TestCodexPanelShowsRichInstrumentsAndOverflow(t *testing.T) {
	view := sessionView{Project: "/work/cully", Branch: "main", Model: "GPT-6.1-Sol high", ContextKnown: true, ContextLeft: 73, Input: "1.2K", Output: "240", FiveHour: "5h 82% left", Weekly: "Weekly 91% left", Fast: "Fast off", ChangesKnown: true, LinesAdded: 42, LinesRemoved: 7, ChangedFiles: 3, Daemon: true}
	stats := toolStats{Tools: 14, Searches: 3, Edits: 2, Checks: 1}
	content := sessionStatusRows(149, []string{"ADV|Run a focused check before finishing."}, stats, view)
	height := 55 - paneTop(55, len(content))
	plain := normalizedCodexPanel(strings.Join(panelLines(149, height, content, ""), "\n"))
	for _, want := range []string{"GPT-6.1-Sol high", "27% used", "73% left", "Input 1.2K", "Output 240", "5h limit 82% left", "Weekly 91% left", "Fast off", "+42 / -7", "3 tracked files", "Errors 0", "Verification no pending edits", "daemon online", "Run a focused check", "/prompts:cully"} {
		if !strings.Contains(plain, normalizedCodexPanel(want)) {
			t.Fatalf("normal terminal lost instrument %q", want)
		}
	}
	clipped := normalizedCodexPanel(strings.Join(panelLines(149, 12, content, ""), "\n"))
	for _, want := range []string{"73% left", "Tools 14", "Advisor", "Run a focused check", "enlarge terminal for all fields"} {
		if !strings.Contains(clipped, want) {
			t.Fatalf("constrained panel lost priority instrument or overflow control %q", want)
		}
	}
}

func TestCodexPanelBudget(t *testing.T) {
	for rows := 8; rows <= 80; rows++ {
		top := paneTop(rows, 100)
		if rows < 12 {
			if top != rows {
				t.Fatal("tiny terminals should give Codex the full screen")
			}
			continue
		}
		if top < 8 || (rows >= 24 && top < 12) || paneTop(rows, 2) <= top {
			t.Fatalf("panel should fit its content within the screen budget: rows=%d top=%d", rows, top)
		}
	}
}

func normalizedCodexPanel(text string) string {
	return strings.Join(strings.Fields(ansi.Strip(text)), " ")
}

func TestCodexPanelUsesBothColumns(t *testing.T) {
	view := sessionView{Project: "/work/cully", Model: "GPT-6.1-Sol high", Input: "1.2K", Output: "240"}
	rows := sessionStatusRows(149, nil, toolStats{Tools: 14, Searches: 3, Edits: 2, Checks: 1}, view)
	positions := make(map[string]int)
	for _, row := range rows {
		plain := ansi.Strip(row)
		for _, label := range []string{"Tools", "Searches", "Edits", "Checks", "Errors", "Verification"} {
			if at := strings.Index(plain, label); at >= 0 {
				positions[label] = ansi.StringWidth(plain[:at])
			}
		}
	}
	for label, column := range positions {
		if column < 74 || column != positions["Tools"] {
			t.Fatalf("activity should be aligned in the right half: %s at %d", label, column)
		}
	}
	if len(positions) != 6 {
		t.Fatalf("missing right-column instruments: %v", positions)
	}
}

func TestCodexAdviceCompleteWhenRoomPermits(t *testing.T) {
	advice := []string{
		"CAUT|Several tool calls failed. Inspect the first failure before retrying.",
		"ADV|Many searches this session. Narrow the path or query before continuing.",
		"ADV|Files changed. Run a focused check before finishing.",
	}
	content := sessionStatusRows(60, advice, toolStats{}, sessionView{Project: "/work/cully"})
	top := paneTop(90, len(content))
	panel := ansi.Strip(strings.Join(panelLines(60, 90-top, content, ""), "\n"))
	for _, suggestion := range advice {
		_, text, _ := strings.Cut(suggestion, "|")
		// Wrapped text must still contain every word in its original order.
		if !strings.Contains(strings.Join(strings.Fields(panel), " "), text) {
			t.Fatalf("roomy panel lost advice: %q", text)
		}
	}
	if strings.Count(panel, "\n\n") < 3 {
		t.Fatal("status and separate recommendations need breathing room")
	}
}

func TestCodexAdvisorViewportScrollsOnlyAdvice(t *testing.T) {
	view := sessionView{Project: "/work/cully", Model: "GPT-6.1-Sol high", ContextKnown: true, ContextLeft: 73}
	var advice []string
	for i := 0; i < 30; i++ {
		advice = append(advice, "ADV|Recommendation "+fmt.Sprint(i)+". Run a focused check.")
	}
	const cols, height = 149, 31
	first := sessionStatusRowsForHeight(cols, height, advice, toolStats{Tools: 14}, view)
	page := advisorPageSize(cols, height, advice, toolStats{Tools: 14}, view)
	maximum := advisorScrollMax(cols, height, advice, toolStats{Tools: 14}, view)
	if maximum <= 0 || page < 2 {
		t.Fatalf("long advice needs a usable scroll range: page=%d max=%d", page, maximum)
	}
	view.AdvisorScroll = maximum
	view.AdvisorFocused = true
	last := sessionStatusRowsForHeight(cols, height, advice, toolStats{Tools: 14}, view)
	firstText := normalizedCodexPanel(strings.Join(first, "\n"))
	lastText := normalizedCodexPanel(strings.Join(last, "\n"))
	if !strings.Contains(firstText, "Recommendation 0.") || strings.Contains(firstText, "Recommendation 29.") || !strings.Contains(lastText, "Recommendation 29.") || strings.Contains(lastText, "Recommendation 0.") {
		t.Fatal("scrolling did not reach the last advice while leaving the first viewport")
	}
	if len(first) != len(last) || len(first) > height-1 {
		t.Fatal("scrolling must not resize the panel or exceed its height")
	}
	for _, want := range []string{"73% left", "GPT-6.1-Sol high", "Tools 14", "Verification no pending edits"} {
		if !strings.Contains(firstText, want) || !strings.Contains(lastText, want) {
			t.Fatalf("scrolling lost instrument %q", want)
		}
	}
	if !strings.Contains(firstText, "Ctrl+] / F6 focus advisor") || !strings.Contains(lastText, "Esc return to Codex") || !strings.Contains(lastText, "active") || !strings.Contains(firstText, "↓") || !strings.Contains(lastText, "↑") {
		t.Fatal("viewport must expose focus controls and current scroll position")
	}
	view.AdvisorScroll = maximum + 1000
	if got := sessionStatusRowsForHeight(cols, height, advice, toolStats{Tools: 14}, view); strings.Join(got, "\n") != strings.Join(last, "\n") {
		t.Fatal("scroll past the bottom must clamp to its last page")
	}
}

func TestCodexAdvisorViewportResponsiveAndComplete(t *testing.T) {
	view := sessionView{Project: "/work/cully", ContextKnown: true, ContextLeft: 73}
	advice := []string{"WARN|Inspect the first failure before retrying.", "ADV|Run a focused verification check before finishing the current change."}
	for _, cols := range []int{20, 42, 80, 149} {
		for _, height := range []int{0, 1, 3, 4, 8, 43, 70} {
			rows := sessionStatusRowsForHeight(cols, height, advice, toolStats{}, view)
			if len(rows) > max(0, height-1) {
				t.Fatalf("viewport overflow: cols=%d height=%d rows=%d", cols, height, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > cols-1 {
					t.Fatalf("viewport width overflow: cols=%d row=%q", cols, row)
				}
			}
		}
	}
	rows := sessionStatusRowsForHeight(149, 43, advice, toolStats{}, view)
	text := normalizedCodexPanel(strings.Join(rows, "\n"))
	if !strings.Contains(text, "Inspect the first failure before retrying.") || !strings.Contains(text, "Run a focused verification check before finishing the current change.") || advisorScrollMax(149, 43, advice, toolStats{}, view) != 0 {
		t.Fatal("roomy viewport should show complete advice without a hidden scroll tail")
	}
	compact := sessionStatusRowsForHeight(42, 4, advice, toolStats{}, view)
	if !strings.Contains(ansi.Strip(compact[0]), "Context") || !strings.Contains(ansi.Strip(compact[2]), "Inspect") {
		t.Fatal("small panels must prioritize context and the first advice row")
	}
}

func TestCodexCompactPanelKeepsMetricsAndTwoPriorityComments(t *testing.T) {
	view := sessionView{Project: "/work/cully", Branch: "main", Model: "GPT-6.1-Sol high", ContextKnown: true, ContextLeft: 73, Input: "1.2K", Output: "240", FiveHour: "5h 82% left", Weekly: "Weekly 91% left", Fast: "Fast off", ChangesKnown: true, LinesAdded: 42, LinesRemoved: 7, ChangedFiles: 3, Daemon: true, Started: time.Now()}
	stats := toolStats{Tools: 14, Searches: 3, Edits: 2, Checks: 1, Errors: 0, EditsSinceCheck: 2}
	advice := []string{"MEMO|Nominal marker should not crowd out a warning.", "ADV|A useful next step.", "CAUT|A caution needs attention.", "WARN|An urgent warning needs attention."}
	for _, cols := range []int{80, 149} {
		rows := compactSessionStatusRows(cols, 43, advice, stats, view)
		limit := 22
		if cols == 80 {
			limit = 22
		}
		if len(rows)+1 > limit {
			t.Fatalf("compact panel too tall at %d columns: %d rows", cols, len(rows)+1)
		}
		text := normalizedCodexPanel(strings.Join(rows, "\n"))
		for _, want := range []string{"Cully", "main", "GPT-6.1-Sol high", "27% used", "73% left", "in 1.2K / out 240", "Setup ", "Waste ", "5h 82% left", "Week 91% left", "Fast off", "+42/-7", "3 tracked", "Verification 2 edits need check", "daemon online", "elapsed", "An urgent warning", "A caution needs attention", "+2 more", "Ctrl+] / F6 Open advisor"} {
			if !strings.Contains(text, want) {
				t.Fatalf("compact panel at %d lost %q", cols, want)
			}
		}
		if strings.Contains(text, "Nominal marker") || strings.Contains(text, "A useful next step") {
			t.Fatal("compact preview should show only the two highest-priority comments")
		}
	}
}

func TestCodexCompactPanelBoundsCommentWrapping(t *testing.T) {
	advice := []string{"WARN|First " + strings.Repeat("inspect this failure ", 40), "ADV|Second " + strings.Repeat("run a focused check ", 40), "MEMO|Third should remain in the expanded view."}
	rows := compactSessionStatusRows(149, 43, advice, toolStats{}, sessionView{Project: "/work/cully", Started: time.Now()})
	count := 0
	advisor := false
	for _, row := range rows {
		if strings.Contains(ansi.Strip(row), "Advisor has") {
			advisor = true
		}
		if advisor && strings.HasPrefix(row, "    ") {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("two comments should occupy at most two wrapped rows each: %d", count)
	}
	text := normalizedCodexPanel(strings.Join(rows, "\n"))
	if !strings.Contains(text, "First") || !strings.Contains(text, "Second") || strings.Contains(text, "Third") || !strings.Contains(text, "+1 more") || !strings.Contains(text, "…") {
		t.Fatal("bounded comment preview needs truncation and expansion controls")
	}
}

func TestCodexCompactPanelResponsiveBounds(t *testing.T) {
	view := sessionView{Project: "/work/cully", ContextKnown: true, ContextLeft: 9}
	advice := []string{"WARN|Inspect a repeated failure before retrying.", "ADV|Run a focused verification before finishing."}
	for _, cols := range []int{20, 42, 80, 149} {
		for _, height := range []int{0, 1, 2, 3, 4, 8, 14, 43} {
			rows := compactSessionStatusRows(cols, height, advice, toolStats{}, view)
			if len(rows) > max(0, height-1) {
				t.Fatalf("height overflow cols=%d height=%d", cols, height)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > cols-1 {
					t.Fatalf("width overflow cols=%d row=%q", cols, row)
				}
			}
		}
	}
	rows := compactSessionStatusRows(149, 4, advice, toolStats{}, view)
	text := normalizedCodexPanel(strings.Join(rows, "\n"))
	for _, want := range []string{"Context", "9% left", "Inspect a repeated failure", "Open advisor"} {
		if !strings.Contains(text, want) {
			t.Fatalf("small compact panel lost %q", want)
		}
	}
}

func TestCodexMessyPhaseUsesObservedFailuresAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stats toolStats
		phase string
	}{
		{"dense failures", toolStats{Tools: 12, Errors: 3}, "messy"},
		{"failure ratio recovers", toolStats{Tools: 13, Errors: 3}, "cruise"},
		{"unverified work with failures", toolStats{Tools: 100, Errors: 2, EditsSinceCheck: 8}, "messy"},
		{"successful check clears pending branch", toolStats{Tools: 101, Errors: 2, Checks: 1}, "cruise"},
		{"busy is not messy", toolStats{Tools: 500, Searches: 100, Edits: 80, EditsSinceCheck: 20}, "cruise"},
		{"no tools observed", toolStats{}, "preflight"},
	} {
		if got := sessionPhase(tc.stats, sessionView{}); got != tc.phase {
			t.Fatalf("%s phase=%s want=%s", tc.name, got, tc.phase)
		}
	}
	if got := sessionPhase(toolStats{Tools: 100}, sessionView{ContextKnown: true, ContextLeft: 8}); got != "emergency" {
		t.Fatal("context alone signals pressure, not disorganization")
	}
	badge := formatPhaseBadge("messy")
	if !strings.Contains(badge, red) || !strings.Contains(badge, "Messy") {
		t.Fatal("shared Messy badge must be red")
	}
	if got := detectPhase(Signals{Turns: 20, ToolHistogram: map[string]int{"Bash": 12}, ToolErrors: 3}, ""); got != PhaseMessy {
		t.Fatalf("Claude failure evidence did not reach shared phase: %s", got)
	}
	if got := detectPhase(Signals{Turns: 20, ToolHistogram: map[string]int{"Bash": 13}, ToolErrors: 3}, ""); got == PhaseMessy {
		t.Fatal("Claude phase did not recover as healthy observations accumulated")
	}
}

func TestCodexAdviceCategoriesIncludeApply(t *testing.T) {
	for _, tc := range []struct{ advice, color, badge string }{
		{"ADV|Install the Playwright MCP integration.", magenta, "Apply"},
		{"ADV|Update AGENTS.md with a project rule.", magenta, "Apply"},
		{"ADV|Run a focused check before finishing.", cyan, "Next"},
		{"CAUT|Repeated searches need a narrower query.", yellow, "Watch"},
		{"WARN|Context is nearly full. Use /compact.", red, "Warn"},
		{"MEMO|No new action from available signals.", dim, "Tip"},
	} {
		color, badge := adviceBadge(tc.advice)
		if color != tc.color || badge != tc.badge {
			t.Fatalf("%q category=%s color=%q", tc.advice, badge, color)
		}
	}
	advice := []string{"ADV|Install a useful MCP integration.", "ADV|Run a focused verification."}
	for _, rows := range [][]string{sessionStatusRows(149, advice, toolStats{}, sessionView{}), compactSessionStatusRows(149, 43, advice, toolStats{}, sessionView{})} {
		text := normalizedCodexPanel(strings.Join(rows, "\n"))
		if !strings.Contains(text, "Apply") || !strings.Contains(text, "Next") {
			t.Fatal("full and compact views should expose the same action categories")
		}
	}
}

func TestCodexCompactTwoGroupsAndObservedCullyStates(t *testing.T) {
	isolateWorkflow(t)
	stats := toolStats{Tools: 60, Searches: 3, Edits: 2, Checks: 1}
	stats.Cully.Calls = 27
	stats.Cully.Log = 2
	stats.Cully.Context = 3
	stats.Cully.Recall = 4
	stats.Cully.Search = 5
	stats.Cully.Get = 6
	stats.Cully.Other = 7
	stats.Cully.Health = "healthy"
	stats.Cully.Auth = "authenticated"
	stats.Cully.CheckedAt = "2026-10-07T12:34:56Z"
	view := sessionView{Project: "/work/cully", Branch: "main", Model: "GPT-6.1-Sol high", ContextKnown: true, ContextLeft: 73, Input: "0", Output: "0", FiveHour: "5h 79% left", Weekly: "Weekly 91% left", Fast: "Fast off", ChangesKnown: true, Daemon: true, Started: time.Now()}
	advice := []string{"CAUT|Inspect repeated failures before retrying.", "ADV|Run a focused verification before finishing."}
	wide := compactSessionStatusRows(149, 43, advice, stats, view)
	text := normalizedCodexPanel(strings.Join(wide, "\n"))
	for _, want := range []string{"Harness", "Session", "MCP 27 calls", "● Healthy", "Auth authenticated", "daemon online", "2 suggestions", "Open advisor"} {
		if !strings.Contains(text, want) {
			t.Fatalf("two-group panel lost %q", want)
		}
	}
	for _, old := range []string{"Project & usage", "Cully impact", "Cully activity"} {
		if strings.Contains(text, old) {
			t.Fatalf("compact panel still has a third section: %q", old)
		}
	}
	for _, metric := range []string{"Project cully", "Context ", "Tokens in 0 / out 0", "5h 79% left", "Fast off", "Git +0/-0 (0 tracked)", "MCP 27 calls"} {
		found := false
		for _, row := range wide {
			plain := ansi.Strip(row)
			if at := strings.Index(plain, metric); at >= 0 {
				if column := ansi.StringWidth(plain[:at]); column < 77 {
					t.Fatalf("%q must be in the right Session column, got column %d", metric, column)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("Session column lost %q", metric)
		}
	}
	if len(wide)+1 > 22 {
		t.Fatalf("wide panel should preserve at least35of55coding rows: %d", len(wide)+1)
	}
	headerFound := false
	for _, row := range wide {
		plain := ansi.Strip(row)
		model, session := strings.Index(plain, "Harness"), strings.Index(plain, "Session")
		if model < 0 || session < 0 {
			continue
		}
		left := ansi.StringWidth(plain[:model])
		right := ansi.StringWidth(plain[:session])
		if left != 2 || right != 77 {
			t.Fatalf("groups must form two aligned columns: %d/%d", left, right)
		}
		headerFound = true
	}
	if !headerFound {
		t.Fatal("wide panel did not align both group headings on one row")
	}
	narrow := compactSessionStatusRows(80, 43, advice, stats, view)
	narrowText := normalizedCodexPanel(strings.Join(narrow, "\n"))
	for _, want := range []string{"Harness", "Session", "● Healthy", "Auth authenticated", "MCP 27 calls"} {
		if !strings.Contains(narrowText, want) {
			t.Fatalf("stacked panel lost %q", want)
		}
	}
	if len(narrow)+1 > 22 {
		t.Fatalf("stacked default panel should remain bounded: %d", len(narrow)+1)
	}
}

func TestCodexCullyHealthAndAuthAreIndependentAndUnknownByDefault(t *testing.T) {
	view := sessionView{Daemon: true}
	stats := toolStats{}
	for _, tc := range []struct{ health, auth string }{
		{"", ""}, {"unknown", "unknown"}, {"unhealthy", "unauthenticated"}, {"healthy", "unknown"},
	} {
		stats.Cully.Health, stats.Cully.Auth = tc.health, tc.auth
		rows := compactSessionStatusRows(149, 43, nil, stats, view)
		text := normalizedCodexPanel(strings.Join(rows, "\n"))
		health, auth := tc.health, tc.auth
		switch health {
		case "healthy":
			health = "● Healthy"
		case "unhealthy":
			health = "● Unhealthy"
		default:
			health = "Awaiting first MCP response"
		}
		if auth == "" || auth == "unknown" {
			auth = "not verified"
		}
		if !strings.Contains(text, health) || !strings.Contains(text, "Auth "+auth) || !strings.Contains(text, "MCP 0 calls") {
			t.Fatalf("observed MCP state lost or invented: %s", text)
		}
		if strings.Contains(text, "Health ") || strings.Contains(text, "last response") {
			t.Fatal("redundant health row/label remains")
		}
		if strings.Contains(text, "Cost") || strings.Contains(text, "cache") {
			t.Fatal("unsupported Codex cost/cache rows must be omitted")
		}
	}
	full := normalizedCodexPanel(strings.Join(sessionStatusRows(149, nil, stats, view), "\n"))
	if !strings.Contains(full, "Cully activity") || strings.Contains(full, "Cost / cache") {
		t.Fatal("expanded instruments should retain MCP metrics and omit unsupported cost")
	}
}

func TestQuotaColorFollowsWhatRemains(t *testing.T) {
	for _, tc := range []struct {
		value string
		color string
	}{
		{"82% left", green}, {"50% left", green}, {"49% left", yellow}, {"20% left", yellow},
		{"19% left", red}, {"0% left", red}, {"30% used", green}, {"60% used", yellow}, {"85% used", red},
	} {
		if got := quotaColor(tc.value); got != tc.color+tc.value+rst {
			t.Errorf("%q = %q, want color %q", tc.value, got, tc.color)
		}
	}
	if got := quotaColor("resets soon"); got != "resets soon" {
		t.Errorf("a value without a percentage was changed: %q", got)
	}
	if got := quotaColor(""); !strings.Contains(got, "unavailable") {
		t.Errorf("empty quota: %q", got)
	}
}
