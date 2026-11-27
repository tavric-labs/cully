package cully

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A harness whose tokens are used up is not offered for new work. Claude Code reports its usage windows,
// so it is exhausted while a known window is at 100 percent and has not reset. Codex and Cursor have no
// reliable limits feed, so Cully learns it from a failure: a task that ends with a usage, rate, quota or
// credits message marks the harness exhausted until the time the message names (30 minutes when it names
// none), and a later task that works clears it.

const (
	uiExhaustedNS      = "exhausted"
	uiExhaustedDefault = 30 * time.Minute
	uiExhaustedMax     = 24 * time.Hour
	uiExhaustedDateMax = 35 * 24 * time.Hour // a stated reset date may be days away
)

// uiExhaustedDoc is what is kept for one harness.
type uiExhaustedDoc struct {
	Until  time.Time `json:"until"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason,omitempty"`
}

var uiLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)usage[ _-]?limit`),
	regexp.MustCompile(`(?i)rate[ _-]?limit`),
	regexp.MustCompile(`(?i)\bquota\b`),
	regexp.MustCompile(`(?i)out of (credits|tokens|usage)`),
	regexp.MustCompile(`(?i)(insufficient|no) (credits|quota)`),
	regexp.MustCompile(`(?i)credits? (have been |are |is )?(used up|exhausted|depleted)`),
	regexp.MustCompile(`(?i)limit (has been )?reached`),
	regexp.MustCompile(`(?i)reached (your|the|its) [a-z0-9 -]{0,20}limit`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`(?i)(http|status|error|code)\W{0,3}429\b|\b429\W{1,3}(too many|rate|quota)`),
	regexp.MustCompile(`(?i)\bhit (your|its|the) [a-z0-9 -]{0,20}limit`),
	regexp.MustCompile(`(?i)resource[_ ]exhausted`),
}

var (
	uiResetDate  = regexp.MustCompile(`(?i)resets?\s+(?:on\s+)?(\d{1,2})/(\d{1,2})/(\d{4})`)
	uiResetClock = regexp.MustCompile(`(?i)(?:resets?|try again|retry|back|available|until)\s+(?:again\s+)?(?:at|after|about|around)?\s*(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\b`)
	uiResetIn    = regexp.MustCompile(`(?i)(?:resets?|try again|retry|back|available|wait)\s+(?:again\s+)?in\s+(?:about\s+)?(?:(\d+)\s*(?:hours?|hrs?|h)\s*)?(?:(\d+)\s*(?:minutes?|mins?|m)\b)?(?:\s*(\d+)\s*(?:seconds?|secs?|s)\b)?`)
)

// uiClassifyLimit says whether an error message is a usage limit, and until when. The patterns are
// deliberately specific: an ordinary failure must not hide a harness.
func uiClassifyLimit(msg string, now time.Time) (bool, time.Time) {
	hit := false
	for _, re := range uiLimitPatterns {
		if re.MatchString(msg) {
			hit = true
			break
		}
	}
	if !hit {
		return false, time.Time{}
	}
	d := uiExhaustedDefault
	if m := uiResetDate.FindStringSubmatch(msg); m != nil { // month/day/year, as Cursor prints it
		mo, _ := strconv.Atoi(m[1])
		dy, _ := strconv.Atoi(m[2])
		yr, _ := strconv.Atoi(m[3])
		if mo >= 1 && mo <= 12 && dy >= 1 && dy <= 31 {
			if t := time.Date(yr, time.Month(mo), dy, 0, 0, 0, 0, now.Location()); t.After(now) {
				if t.Sub(now) > uiExhaustedDateMax {
					t = now.Add(uiExhaustedDateMax)
				}
				return true, t
			}
		}
	}
	if m := uiResetIn.FindStringSubmatch(msg); m != nil && (m[1] != "" || m[2] != "" || m[3] != "") {
		n := func(s string) int { v, _ := strconv.Atoi(s); return v }
		if t := time.Duration(n(m[1]))*time.Hour + time.Duration(n(m[2]))*time.Minute + time.Duration(n(m[3]))*time.Second; t > 0 {
			d = t
		}
	} else if m := uiResetClock.FindStringSubmatch(msg); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h >= 1 && h <= 12 && mi < 60 {
			if strings.EqualFold(m[3], "p") && h != 12 {
				h += 12
			} else if strings.EqualFold(m[3], "a") && h == 12 {
				h = 0
			}
			t := time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, now.Location())
			if !t.After(now) {
				t = t.Add(24 * time.Hour)
			}
			d = t.Sub(now)
		}
	}
	if d < time.Minute {
		d = time.Minute
	}
	if d > uiExhaustedMax {
		d = uiExhaustedMax
	}
	return true, now.Add(d)
}

// uiLimitsExhausted works out, from the limits each Claude account last reported, whether every account
// is used up, and when the first of them frees up. An account is used up while a known window is at
// 100 percent and has not reset yet. With no report, nothing is exhausted.
func uiLimitsExhausted(accounts []uiLimits, now time.Time) (time.Time, bool) {
	if len(accounts) == 0 {
		return time.Time{}, false
	}
	var first time.Time
	for _, l := range accounts {
		var back time.Time
		for _, w := range []uiWindow{l.FiveHour, l.SevenDay} {
			if w.Known && w.Used >= 100 && w.ResetsAt > now.Unix() {
				if t := time.Unix(w.ResetsAt, 0); t.After(back) {
					back = t
				}
			}
		}
		if back.IsZero() {
			return time.Time{}, false // this account still has room
		}
		if first.IsZero() || back.Before(first) {
			first = back
		}
	}
	return first, true
}

// uiExhaustedAt is the stored exhaustion of a harness, if it has not expired.
func uiExhaustedAt(harness string, now time.Time) (time.Time, bool) {
	var d uiExhaustedDoc
	if !uiDB().get(uiExhaustedNS, harness, &d) {
		return time.Time{}, false
	}
	if !d.Until.After(now) {
		uiDB().del(uiExhaustedNS, harness)
		return time.Time{}, false
	}
	return d.Until, true
}

func uiMarkExhausted(harness string, until time.Time, reason string) {
	uiDB().put(uiExhaustedNS, harness, uiExhaustedDoc{Until: until.UTC(), At: time.Now().UTC(), Reason: uiClip(reason, 160)})
}

func uiClearExhausted(harness string) { uiDB().del(uiExhaustedNS, harness) }

// harnessExhaustion says whether a harness is out of tokens now, and until when.
func (m *uiManager) harnessExhaustion(id string, now time.Time) (time.Time, bool) {
	id = uiHarnessID(id)
	if id == "claude" {
		m.limitMu.Lock()
		accts := make([]uiLimits, 0, len(m.accountLimits))
		for _, l := range m.accountLimits {
			accts = append(accts, l)
		}
		m.limitMu.Unlock()
		return uiLimitsExhausted(accts, now)
	}
	return uiExhaustedAt(id, now)
}

// noteHarnessOutcome learns from how a task ended its turn. Only Codex and Cursor are learned this way.
// An error in the latest turn that reads as a usage limit marks the harness; a turn that finished with
// no such error clears it.
func (m *uiManager) noteHarnessOutcome(a *uiAgent, state string) {
	h := uiHarnessID(a.req.Harness)
	if h == "claude" || (state != uiFailed && state != uiDone && state != uiWaiting) {
		return
	}
	evs := a.allEvents()
	start := 0
	for i, e := range evs {
		if e.Kind == "user" {
			start = i
		}
	}
	finished := false
	now := time.Now()
	for _, e := range evs[start:] {
		switch e.Kind {
		case "error":
			if hit, until := uiClassifyLimit(e.Text, now); hit {
				uiMarkExhausted(h, until, e.Text)
				return
			}
		case "result":
			if !e.Failed {
				finished = true
			}
		}
	}
	if finished && state != uiFailed {
		uiClearExhausted(h)
	}
}

// markExhausted adds what is known about used-up harnesses to the picker's list.
func (m *uiManager) markExhausted(list []uiHarnessChoice) {
	now := time.Now()
	for i := range list {
		if until, ok := m.harnessExhaustion(list[i].ID, now); ok {
			list[i].Exhausted, list[i].Until = true, until.Unix()
		}
	}
}
