package cully

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func goldenCompare(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs (UPDATE_GOLDEN=1 to regenerate)\n--- got ---\n%s", name, got)
	}
}

func TestStepTranscriptFits(t *testing.T) {
	doc := fixtureDoc(t)
	for _, width := range []int{80, 120, 200} {
		out := renderReplayText(doc, replayOptions{}, width, false)
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > width && width >= 40 && strings.Contains(line, "┌") {
				t.Errorf("width %d line %q", width, line)
			}
		}
		if !strings.Contains(out, "go test") || strings.Contains(out, "┌") {
			t.Errorf("width %d transcript:\n%s", width, out)
		}
	}
}

func newTestModel(t *testing.T, w, h int) *replayModel {
	t.Helper()
	return newReplayModel(fixtureDoc(t), replayOptions{Speed: 1}, w, h, false)
}

func TestModelQuit(t *testing.T) {
	m := newTestModel(t, 100, 40)
	if m.View != viewSteps || !m.Paused {
		t.Fatalf("opens on the step player, paused: %s %v", m.View, m.Paused)
	}
	m.Update(replayKey{Kind: "rune", R: 'q'})
	if !m.Quit {
		t.Error("q quits")
	}
}

func TestModelFilterAndViews(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.Update(replayKey{Kind: "rune", R: '/'})
	for _, r := range "helper" {
		m.Update(replayKey{Kind: "rune", R: r})
	}
	if !m.Filtering {
		t.Fatal("not filtering")
	}
	m.Update(replayKey{Kind: "enter"})
	matched := 0
	for _, s := range m.Doc.Steps {
		if stepMatches(s, m.Filter) {
			matched++
		}
	}
	if m.Filter != "helper" || matched == 0 {
		t.Fatalf("filter %q matched %d", m.Filter, matched)
	}
	m.Pos = len(m.Doc.Steps)
	if !strings.Contains(strings.Join(renderStepFeed(m.Doc.Steps, m.Pos, m.Filter, 80, 20, false), "\n"), "helper.go") {
		t.Error("filtered feed lacks helper.go")
	}
	m.Update(replayKey{Kind: "esc"})
	if m.Filter != "" {
		t.Error("esc clears filter")
	}
	m.Update(replayKey{Kind: "rune", R: 'f'})
	if m.View != viewFiles {
		t.Fatal("f opens files")
	}
	if !strings.Contains(strings.Join(m.Render(), "\n"), "resource.go") {
		t.Error("files view lacks files")
	}
	m.Update(replayKey{Kind: "rune", R: 'f'})
	if m.View != viewSteps {
		t.Fatalf("f toggles back, view %s", m.View)
	}
	m.Update(replayKey{Kind: "rune", R: 's'})
	if m.View != viewSteps || m.Pos != 0 || m.Paused {
		t.Fatalf("s restarts the player: %s pos %d paused %v", m.View, m.Pos, m.Paused)
	}
}

func TestPlayerControls(t *testing.T) {
	m := newTestModel(t, 120, 30)
	m.Update(replayKey{Kind: "rune", R: 'p'})
	n := len(m.Doc.Steps)
	if !m.Tick() || m.Pos != 1 {
		t.Fatalf("tick pos %d", m.Pos)
	}
	// Real gaps are compressed: 10:31:00 -> 10:31:05 is 5s, capped at 1.5s.
	if d := m.NextDelay(); d != replayDelay(m.Doc.Steps[1].Time.Sub(m.Doc.Steps[0].Time), 1) || d > replayMaxGap {
		t.Errorf("delay %v", d)
	}
	m.Update(replayKey{Kind: "space", R: ' '})
	if !m.Paused || m.Tick() {
		t.Error("space pauses playback")
	}
	m.Update(replayKey{Kind: "right"})
	m.Update(replayKey{Kind: "right"})
	if m.Pos != 3 {
		t.Errorf("right steps: %d", m.Pos)
	}
	m.Update(replayKey{Kind: "left"})
	if m.Pos != 2 {
		t.Errorf("left steps: %d", m.Pos)
	}
	m.Update(replayKey{Kind: "up"})
	m.Update(replayKey{Kind: "up"})
	if m.Speed != 4 {
		t.Errorf("speed %v", m.Speed)
	}
	for i := 0; i < 10; i++ {
		m.Update(replayKey{Kind: "down"})
	}
	if m.Speed != replayMinSpeed {
		t.Errorf("speed floor %v", m.Speed)
	}
	for i := 0; i < 100; i++ {
		m.Update(replayKey{Kind: "right"})
	}
	if m.Pos != n {
		t.Errorf("pos clamp %d", m.Pos)
	}
	// Future steps are hidden: at pos 2 only two steps are in the feed.
	m.Pos = 2
	feed := strings.Join(renderStepFeed(m.Doc.Steps, m.Pos, "", 100, 20, false), "\n")
	if !strings.Contains(feed, "▸") || strings.Contains(feed, "Created") || !strings.Contains(feed, "Read") {
		t.Errorf("feed = %s", feed)
	}
}

func TestParseReplayKeys(t *testing.T) {
	keys := parseReplayKeys([]byte("q \x1b[A\x1b[B\x1b[C\x1b[D\x1b[5~\x1b[6~\r\t\x7f\x1b\x03/"))
	var kinds []string
	for _, k := range keys {
		kinds = append(kinds, k.Kind)
	}
	if got := strings.Join(kinds, ","); got != "rune,space,up,down,right,left,pgup,pgdn,enter,tab,backspace,esc,ctrlc,rune" {
		t.Errorf("kinds = %s", got)
	}
	mouse := parseReplayKeys([]byte("\x1b[<0;12;7M\x1b[<0;12;7m\x1b[<64;1;1M\x1b[<65;1;1M\x1b[<2;5;5M"))
	if len(mouse) != 3 || mouse[0].Kind != "click" || mouse[0].X != 12 || mouse[0].Y != 7 || mouse[1].Kind != "wheelup" || mouse[2].Kind != "wheeldown" {
		t.Errorf("mouse = %+v", mouse)
	}
	if got := parseReplayKeys([]byte("\x1b[<0;1")); len(got) != 0 {
		t.Errorf("partial mouse = %+v", got)
	}
}

func TestRenderFramesFitAt80_120_200(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {200, 50}} {
		w, h := size[0], size[1]
		for _, view := range []string{viewSteps, viewFiles} {
			m := newTestModel(t, w, h)
			m.View, m.Pos, m.Selected = view, len(m.Doc.Steps), 1
			if view == viewSteps {
				m.Pos = 8
			}
			lines := m.Render()
			if len(lines) != h {
				t.Fatalf("%dx%d %s: %d lines", w, h, view, len(lines))
			}
			for i, l := range lines {
				if cw := ansi.StringWidth(l); cw > w {
					t.Errorf("%dx%d %s line %d is %d wide", w, h, view, i, cw)
				}
			}
			frame := strings.Join(lines, "\n")
			switch view {
			case viewSteps:
				if !strings.Contains(frame, "FILE ACTIVITY") || !strings.Contains(frame, "space pause") {
					t.Errorf("%dx%d steps frame:\n%s", w, h, frame)
				}
			}
		}
	}
	// Golden for the 80x24 player layout (stacked) and 120x30 (split).
	m := newTestModel(t, 80, 24)
	m.View, m.Pos = viewSteps, 10
	goldenCompare(t, "replay_frame_80_steps.golden", strings.Join(m.Render(), "\n")+"\n")
	m = newTestModel(t, 120, 30)
	m.View, m.Pos = viewSteps, 13
	goldenCompare(t, "replay_frame_120_steps.golden", strings.Join(m.Render(), "\n")+"\n")
}

func TestFilePanelHeatAndHotspots(t *testing.T) {
	steps := buildSteps(replayFixture())
	panel := strings.Join(renderFilePanel(steps, len(steps), "", 60, 20, false), "\n")
	for _, want := range []string{"FILE ACTIVITY (3)", "internal/auth/", "~ resource.go ████ ×4 ▲", "+ resource_test.go", "− helper.go"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel missing %q\n%s", want, panel)
		}
	}
	early := strings.Join(renderFilePanel(steps, 2, "", 60, 20, false), "\n")
	if !strings.Contains(early, "· resource.go") || strings.Contains(early, "helper.go") {
		t.Errorf("early panel = %s", early)
	}
	hot := strings.Join(renderFilePanel(steps, len(steps), "", 60, 20, true), "\n")
	if !strings.Contains(hot, bold+yellow) {
		t.Error("hotspot is not highlighted")
	}
}

func TestScrubberMarksLoopsAndFailures(t *testing.T) {
	doc := fixtureDoc(t)
	lines := renderScrubber(doc, at(10*time.Minute), at(3*time.Minute), at(4*time.Minute), 100, false)
	if len(lines) != 2 || !strings.Contains(lines[0], "✕") || !strings.Contains(lines[0], "⚠") {
		t.Errorf("markers = %q", lines[0])
	}
	if !strings.Contains(lines[1], "●") || !strings.Contains(lines[1], "━") || !strings.Contains(lines[1], "▬") || !strings.HasPrefix(lines[1], "10:31 ") || !strings.HasSuffix(lines[1], " 10:50") {
		t.Errorf("track = %q", lines[1])
	}
	if ansi.StringWidth(lines[1]) > 100 {
		t.Errorf("track too wide")
	}
}

// ---------------------------------------------------------------------------
// HTML

func TestReplayHTMLStructureAndOffline(t *testing.T) {
	page, err := renderReplayHTML(fixtureDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://", "https://", "//cdn", "src=", "<link", "@import", "url(", "fetch(", "XMLHttpRequest", "innerHTML", "eval("} {
		if strings.Contains(page, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
	for _, want := range []string{"<!doctype html>", "Content-Security-Policy", "prefers-color-scheme:dark", `id="feed"`, `id="files"`, `id="reconcile"`, `id="summary"`, `type="range"`,
		"Generated locally by Cully. Contains file paths and command names, no file contents.", "default-src 'none'", "<noscript>"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Count(page, "<script") != 2 || strings.Count(page, "<style") != 1 {
		t.Error("expected exactly one style and two script blocks")
	}
	// CSP hashes match the inline blocks.
	hashRe := regexp.MustCompile(`'sha256-([A-Za-z0-9+/=]+)'`)
	hashes := hashRe.FindAllStringSubmatch(page, -1)
	if len(hashes) != 2 {
		t.Fatalf("hashes = %v", hashes)
	}
	for _, block := range []string{replayHTMLStyle, replayHTMLScript} {
		if want := strings.Trim(cspHash(block), "'"); !strings.Contains(page, want) {
			t.Errorf("missing CSP hash %s", want)
		}
		if !strings.Contains(page, block) {
			t.Error("inline block missing")
		}
	}
	if _, err := base64.StdEncoding.DecodeString(hashes[0][1]); err != nil {
		t.Error(err)
	}
	// The embedded data parses and is the shared export.
	m := regexp.MustCompile(`(?s)<script type="application/json" id="data">(.*?)</script>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no data block")
	}
	var data replayJSON
	if err := json.Unmarshal([]byte(m[1]), &data); err != nil {
		t.Fatal(err)
	}
	if data.SchemaVersion != 2 || len(data.Steps) == 0 || data.Session != "sess-1" {
		t.Errorf("data = %+v", data.Meta)
	}
	if strings.Contains(m[1], `"stages"`) {
		t.Error("export still carries a stage graph")
	}
}

func TestReplayHTMLEscapesHostileText(t *testing.T) {
	hostile := `<img src=x onerror=alert(1)>/</script><script>alert(2)</script>&"'.go`
	events := []journalEvent{
		{Time: at(0), Agent: "claude", Class: journalStart},
		{Time: at(time.Minute), Agent: "claude", Class: journalEdit, Op: opEdit, Path: hostile},
		{Time: at(2 * time.Minute), Agent: "claude", Class: journalCheck, Cmd: "go test"},
	}
	git := func(string) ([]gitEntry, bool) { return []gitEntry{{Path: hostile + "-untracked"}}, true }
	doc := buildReplay(events, replayMeta{Session: "s", Project: `<b onmouseover=1>proj`, Branch: `"><svg onload=1>`}, git, "/x", nil)
	page, err := renderReplayHTML(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"<img", "</script><script>", "<svg onload", "<b onmouseover", `onerror=alert(1)>`} {
		if strings.Contains(page, raw) {
			t.Errorf("unescaped hostile text %q in page", raw)
		}
	}
	if strings.Count(page, "</script>") != 2 {
		t.Errorf("script blocks were broken out of: %d closers", strings.Count(page, "</script>"))
	}
	m := regexp.MustCompile(`(?s)<script type="application/json" id="data">(.*?)</script>`).FindStringSubmatch(page)
	var data replayJSON
	if err := json.Unmarshal([]byte(m[1]), &data); err != nil {
		t.Fatal(err)
	}
	if data.Events[1].Path != hostile {
		t.Errorf("data round trip = %q", data.Events[1].Path)
	}
	if !strings.Contains(page, "&lt;img src=x") {
		t.Error("noscript transcript is not HTML-escaped")
	}
}

func TestWriteReplayHTMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out", "r.html")
	if err := writeReplayHTML(path, fixtureDoc(t)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v %v", info, err)
	}
	// Re-writing keeps owner-only permissions even over a looser file.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeReplayHTML(path, fixtureDoc(t)); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode after rewrite = %v", info.Mode().Perm())
	}
}

func TestRunReplayHTMLDefaultsAndPrintsPath(t *testing.T) {
	cwd := journalTestEnv(t)
	recordJournalTool("claude", "sess/1", makeToolEvent(cwd, "Edit", map[string]string{"file_path": "a.go"}), 'E', false)
	work := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old) //nolint:errcheck
	var out strings.Builder
	if err := RunReplay(&out, []string{"--cwd", cwd, "--html"}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	real, _ := filepath.EvalSymlinks(work)
	if filepath.Base(got) != "cully-replay-sess_1.html" || (filepath.Dir(got) != work && filepath.Dir(got) != real) {
		t.Errorf("printed path = %q", got)
	}
	if info, err := os.Stat(got); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file = %v %v", info, err)
	}
}
