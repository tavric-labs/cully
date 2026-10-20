package cully

// journalCoverage describes evidence limits, without retaining bad records or
// OS error text. Empty means no known gap, not proof that every hook arrived.
func journalCoverage(events []journalEvent) string {
	started, trimmed, invalid := false, false, false
	for _, e := range events {
		started = started || e.Class == journalStart
		if e.Class != journalNote {
			continue
		}
		switch e.Note {
		case noteHistoryUnreadable:
			return "unavailable: journal could not be read"
		case noteHistoryTrimmed:
			trimmed = true
		case noteHistoryInvalid:
			invalid = true
		}
	}
	if invalid {
		return "partial: journal records were unreadable or invalid"
	}
	if trimmed {
		return "partial: older journal history was discarded"
	}
	if len(events) > 0 && !started {
		return "partial: session start was not recorded"
	}
	return ""
}

func signalCoverage(agent string, tools int) string {
	if tools == 0 {
		return "unavailable: no tool events received"
	}
	switch normalizeAgentID(agent) {
	case "cursor":
		return "partial: shell exit status unavailable"
	case "claude", "codex":
		return "tool events observed; completeness unconfirmed"
	default:
		return "partial: agent tool coverage is unknown"
	}
}
