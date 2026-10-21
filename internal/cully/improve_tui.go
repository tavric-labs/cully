package cully

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// The improve window lists Cully's proposals so you can read one, apply it or
// drop it. A proposal is never dropped by looking at it, by cancelling an
// apply, or by quitting: only Drop, confirmed, removes it. Only runImproveTUI
// touches the terminal, so the model is testable with plain values.
//
// Keys: up/down or j/k select; Enter details; a apply; d drop; q or Esc back.

const (
	improveList   = "list"
	improveDetail = "detail"
	improveReview = "review"
	improveDrop   = "drop"

	improveListTop = 4 // first screen row of the list, 1-based
)

type improveActions struct {
	Plan  func(improvement) (applyPlan, []string, error)
	Apply func(improvement, applyPlan) error
	Drop  func(improvement) error
	Home  string
}

type improveButton struct {
	Label  string
	Key    rune
	X0, X1 int // 1-based inclusive columns
}

type improveModel struct {
	Items         []improvement
	Cursor        int
	Mode          string
	Plan          applyPlan
	Dropped       []string
	Message       string
	Busy          string // "plan" while the model plans
	Width, Height int
	Color         bool
	Quit          bool
	Act           improveActions
	buttons       []improveButton
}

func newImproveModel(items []improvement, act improveActions, width, height int, color bool) *improveModel {
	return &improveModel{Items: items, Mode: improveList, Width: width, Height: height, Color: color, Act: act}
}

func (m *improveModel) selected() (improvement, bool) {
	if m.Cursor < 0 || m.Cursor >= len(m.Items) {
		return improvement{}, false
	}
	return m.Items[m.Cursor], true
}

func (m *improveModel) move(delta int) {
	if len(m.Items) == 0 {
		return
	}
	m.Cursor = min(max(m.Cursor+delta, 0), len(m.Items)-1)
}

func (m *improveModel) remove() {
	m.Items = append(m.Items[:m.Cursor], m.Items[m.Cursor+1:]...)
	m.Cursor = min(m.Cursor, max(len(m.Items)-1, 0))
}

// Update handles one key or click. It never writes or drops anything except
// through a confirmed Apply or Drop.
func (m *improveModel) Update(k replayKey) {
	if m.Busy != "" {
		return
	}
	switch k.Kind {
	case "ctrlc":
		m.Quit = true
	case "up":
		if m.Mode == improveList || m.Mode == improveDetail {
			m.move(-1)
		}
	case "down":
		if m.Mode == improveList || m.Mode == improveDetail {
			m.move(1)
		}
	case "enter":
		switch m.Mode {
		case improveList:
			if _, ok := m.selected(); ok {
				m.Mode = improveDetail
			}
		case improveDetail:
			m.Mode = improveList
		case improveReview:
			m.apply()
		}
	case "esc":
		m.press('q')
	case "rune":
		m.press(k.R)
	case "click":
		m.click(k.X, k.Y)
	}
}

func (m *improveModel) click(x, y int) {
	if y == m.Height {
		for _, b := range m.buttons {
			if x >= b.X0 && x <= b.X1 {
				if b.Key == '\n' {
					m.Update(replayKey{Kind: "enter"})
				} else {
					m.press(b.Key)
				}
				return
			}
		}
		return
	}
	if m.Mode == improveList || m.Mode == improveDetail {
		if i := y - improveListTop; m.Mode == improveList && i >= 0 && i < len(m.Items) {
			m.Cursor, m.Mode = i, improveDetail
		}
	}
}

func (m *improveModel) press(r rune) {
	switch m.Mode {
	case improveList, improveDetail:
		switch r {
		case 'q':
			if m.Mode == improveDetail {
				m.Mode = improveList
			} else {
				m.Quit = true
			}
		case 'b':
			m.Mode = improveList
		case 'j':
			m.move(1)
		case 'k':
			m.move(-1)
		case 'a':
			if _, ok := m.selected(); ok {
				m.Busy, m.Message = "plan", "Asking your model for a plan…"
			}
		case 'd':
			if _, ok := m.selected(); ok {
				m.Mode, m.Message = improveDrop, ""
			}
		}
	case improveReview:
		switch r {
		case 'y':
			m.apply()
		case 'n', 'q':
			m.Mode, m.Message = improveDetail, "Not applied. The suggestion is still pending."
		}
	case improveDrop:
		switch r {
		case 'y':
			m.drop()
		case 'n', 'q':
			m.Mode, m.Message = improveDetail, "Kept. The suggestion is still pending."
		}
	}
}

// Finish runs the work that Update deferred, so the screen shows the message
// first. It is a no-op when nothing is pending.
func (m *improveModel) Finish() {
	if m.Busy != "plan" {
		return
	}
	m.Busy = ""
	p, ok := m.selected()
	if !ok {
		return
	}
	plan, dropped, err := m.Act.Plan(p)
	m.Mode = improveDetail
	switch {
	case err != nil:
		m.Message = "Could not plan a change: " + terminalSafe(err.Error()) + ". The suggestion is still pending."
	case !planWritesAnything(plan):
		m.Plan, m.Dropped = plan, dropped
		m.Message = "Nothing to write: this needs a change to Cully's code or a person. The suggestion is still pending."
		if plan.Notes != "" {
			m.Message += " Note: " + plan.Notes
		}
	default:
		m.Plan, m.Dropped, m.Mode, m.Message = plan, dropped, improveReview, ""
	}
}

func (m *improveModel) apply() {
	p, ok := m.selected()
	if !ok {
		return
	}
	if err := m.Act.Apply(p, m.Plan); err != nil {
		m.Mode, m.Message = improveDetail, "Apply failed: "+err.Error()+". The suggestion is still pending."
		return
	}
	summary := m.Plan.Summary
	m.remove()
	m.Mode, m.Message = improveList, "Applied: "+summary+". Run cully improve --run to check the effect."
}

func (m *improveModel) drop() {
	p, ok := m.selected()
	if !ok {
		return
	}
	if err := m.Act.Drop(p); err != nil {
		m.Mode, m.Message = improveDetail, "Drop failed: "+err.Error()
		return
	}
	m.remove()
	m.Mode, m.Message = improveList, "Dropped "+improvementTitle(p)+"."
}

// ---------------------------------------------------------------------------
// Pure layout

func (m *improveModel) style(code, s string) string {
	if !m.Color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (m *improveModel) wrap(prefix, text string) []string {
	text = terminalSafe(text)
	width := max(10, m.Width-2-ansi.StringWidth(prefix))
	var out []string
	for i, line := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
		if i == 0 {
			out = append(out, prefix+line)
		} else {
			out = append(out, strings.Repeat(" ", ansi.StringWidth(prefix))+line)
		}
	}
	return out
}

func (m *improveModel) footerButtons() []improveButton {
	switch m.Mode {
	case improveReview:
		return []improveButton{{Label: "Yes, apply", Key: 'y'}, {Label: "No, keep it", Key: 'n'}}
	case improveDrop:
		return []improveButton{{Label: "Yes, drop it", Key: 'y'}, {Label: "No, keep it", Key: 'n'}}
	case improveDetail:
		return []improveButton{{Label: "Apply", Key: 'a'}, {Label: "Drop", Key: 'd'}, {Label: "Back", Key: 'b'}}
	}
	return []improveButton{{Label: "Details", Key: '\n'}, {Label: "Apply", Key: 'a'}, {Label: "Drop", Key: 'd'}, {Label: "Quit", Key: 'q'}}
}

func (m *improveModel) body() []string {
	p, ok := m.selected()
	var lines []string
	title := fmt.Sprintf("Cully self-improvement · %s", plural(len(m.Items), "suggestion", "suggestions"))
	switch m.Mode {
	case improveList:
		lines = append(lines, m.style("1", title), m.style("2", "↑/↓ select · Enter details · a apply · d drop · q quit"), "")
		if len(m.Items) == 0 {
			lines = append(lines, " Nothing is waiting. Cully re-evaluates itself every 5 hours.")
		}
		for i, it := range m.Items {
			row := fmt.Sprintf(" %s %s", map[bool]string{true: "▸", false: " "}[i == m.Cursor], fitTo(improvementTitle(it), max(10, m.Width-24)))
			row += fmt.Sprintf("  %s · %d failing", terminalSafe(it.Suite), len(it.Evidence))
			if i == m.Cursor {
				row = m.style("7", fitTo(row, m.Width))
			}
			lines = append(lines, row)
		}
	case improveDetail:
		if !ok {
			break
		}
		lines = append(lines, m.style("1", " "+improvementTitle(p)), m.style("2", fmt.Sprintf(" %s · first seen %s · last seen %s", terminalSafe(p.Status), p.Created.Local().Format("Jan 02 15:04"), p.Updated.Local().Format("Jan 02 15:04"))), "", m.style("1", " Failing checks"))
		for _, f := range p.Evidence {
			text := f.Check
			if f.Detail != "" {
				text += " — " + f.Detail
			}
			lines = append(lines, m.wrap("   • ", text)...)
		}
		lines = append(lines, "", m.style("1", " What Apply does"))
		lines = append(lines, m.wrap("   ", "Asks your model for a plan from these checks and shows it to you. Nothing is written until you confirm. A plan can add rules and one skill under ~/.cully, never MCP servers or commands. You can drop this suggestion at any time; viewing or cancelling never does.")...)
	case improveReview:
		if !ok {
			break
		}
		lines = append(lines, m.style("1", " Plan for "+improvementTitle(p)), "")
		lines = append(lines, m.wrap(" ", m.Plan.Summary)...)
		lines = append(lines, "")
		for _, b := range planBullets(m.Act.Home, m.Plan, m.Dropped) {
			lines = append(lines, m.wrap("   • ", b)...)
		}
		lines = append(lines, "", m.style("1", " Apply this change? y writes it · n keeps the suggestion pending"))
	case improveDrop:
		if !ok {
			break
		}
		lines = append(lines, m.style("1", " Drop "+improvementTitle(p)+"?"), "")
		lines = append(lines, m.wrap(" ", "A dropped suggestion stays dropped. Nothing else changes. y drops it · n keeps it.")...)
	}
	return lines
}

// Render returns exactly Height lines, each at most Width cells wide.
func (m *improveModel) Render() []string {
	lines := m.body()
	if m.Message != "" {
		lines = append(lines, "", m.style("33", " "+terminalSafe(m.Message)))
	}
	out := make([]string, m.Height)
	for i := range out {
		if i < len(lines) {
			out[i] = ansi.Truncate(lines[i], m.Width, "…")
		}
	}
	// Buttons sit on the last row; remember their columns for clicks.
	m.buttons = nil
	var b strings.Builder
	col := 2
	b.WriteString(" ")
	for _, button := range m.footerButtons() {
		label := "[ " + button.Label + " ]"
		button.X0, button.X1 = col, col+ansi.StringWidth(label)-1
		m.buttons = append(m.buttons, button)
		b.WriteString(" " + m.style("7", label))
		col += ansi.StringWidth(label) + 1
	}
	if m.Height > 0 {
		out[m.Height-1] = ansi.Truncate(b.String(), m.Width, "")
	}
	return out
}

// ---------------------------------------------------------------------------
// Terminal loop

// runImproveTUI shows the window on a real terminal and returns when you quit.
func runImproveTUI(out, in *os.File, items []improvement, act improveActions) error {
	cols, rows, err := term.GetSize(int(out.Fd()))
	if err != nil || cols < 40 || rows < 10 {
		return fmt.Errorf("terminal too small for the window; use cully improve --list")
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(in.Fd()), state) //nolint:errcheck
	if _, err := io.WriteString(out, "\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h"); err != nil {
		return err
	}
	defer io.WriteString(out, "\x1b[?1000l\x1b[?1006l\x1b[0m\x1b[?25h\x1b[?1049l") //nolint:errcheck

	m := newImproveModel(items, act, cols, rows, os.Getenv("NO_COLOR") == "")
	input := make(chan []replayKey, 8)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				input <- parseReplayKeys(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				close(input)
				return
			}
		}
	}()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	draw := func() {
		var b strings.Builder
		b.WriteString("\x1b[H")
		for i, l := range m.Render() {
			if i > 0 {
				b.WriteString("\r\n")
			}
			b.WriteString(l + "\x1b[0m\x1b[K")
		}
		_, _ = io.WriteString(out, b.String())
	}
	for !m.Quit {
		draw()
		if m.Busy != "" {
			m.Finish()
			continue
		}
		select {
		case keys, ok := <-input:
			if !ok {
				return nil
			}
			for _, k := range keys {
				m.Update(k)
			}
		case <-winch:
			if c, r, err := term.GetSize(int(out.Fd())); err == nil {
				m.Width, m.Height = c, r
				_, _ = io.WriteString(out, "\x1b[2J")
			}
		}
	}
	return nil
}
