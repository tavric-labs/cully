package cully

// The steering columns of the capability matrix: what a harness lets Cully do while a task runs.
// Values describe what Cully can do through that harness today, read from how Cully drives it and
// from `codex --help` and `cursor-agent --help`, not what the harness could do in its own terminal.
func uiSteeringCaps(yn func(bool) string, have func(string) bool) []uiCapability {
	return []uiCapability{
		{ID: "steering", Label: "Send a message mid-turn", Why: "Talk to a task while it is working.",
			Claude: yn(have("--input-format")), ClaudeWhy: "Messages go in over stream-json input while the turn runs.",
			Codex: "no", CodexWhy: "Cully runs Codex one-shot. Codex has a queue command Cully does not use yet.",
			Cursor: "no", CursorWhy: "Cully runs Cursor one-shot with --print."},
		{ID: "interrupt", Label: "Interrupt a running task", Why: "Stop the turn without losing the session.",
			Claude: "yes", ClaudeWhy: "Cully stops the task process and keeps its branch.",
			Codex: "no", CodexWhy: "Cully does not run Codex tasks, so it has nothing to stop.",
			Cursor: "no", CursorWhy: "Cully does not run Cursor tasks, so it has nothing to stop."},
		{ID: "resume", Label: "Resume a session", Why: "Carry on the same conversation.",
			Claude: yn(have("--resume")), ClaudeWhy: "--resume continues the same session.",
			Codex: "partial", CodexWhy: "Codex has a resume command; Cully continues from a handoff note.",
			Cursor: "partial", CursorWhy: "Cursor has --resume; Cully continues from a handoff note."},
		{ID: "fork", Label: "Fork a session", Why: "Branch from an earlier point.",
			Claude: yn(have("--fork-session")), ClaudeWhy: "--fork-session starts a new session from an old one.",
			Codex: "partial", CodexWhy: "Codex has a fork command; Cully starts a new task from a handoff instead.",
			Cursor: "no", CursorWhy: "Cursor lists no fork option."},
		{ID: "relay", Label: "Relay permission questions", Why: "Ask you in the page before a risky call.",
			Claude: yn(have("--permission-prompt-tool") && have("--settings")), ClaudeWhy: "A permission tool turns a blocked call into a card in the task window.",
			Codex: "no", CodexWhy: "Codex decides approvals in its own policy.",
			Cursor: "no", CursorWhy: "Cursor decides approvals in its own app."},
		{ID: "queue", Label: "Live queue", Why: "Tasks wait for a free slot and start by themselves.",
			Claude: "yes", ClaudeWhy: "Cully queues Claude tasks and starts them in order.",
			Codex: "no", CodexWhy: "Only Claude tasks are queued.",
			Cursor: "no", CursorWhy: "Only Claude tasks are queued."},
		{ID: "cost", Label: "Cost reporting", Why: "Tokens and spend for a task.",
			Claude: "yes", ClaudeWhy: "Each turn reports tokens and cost.",
			Codex: "partial", CodexWhy: "Token counts are in transcripts, read afterwards.",
			Cursor: "no", CursorWhy: "Cursor does not record tokens or cost."},
		{ID: "structured", Label: "Structured events", Why: "Tool calls as data, not text.",
			Claude: yn(have("--output-format")), ClaudeWhy: "--output-format stream-json gives one event per step.",
			Codex: "partial", CodexWhy: "Events are in session files, read afterwards.",
			Cursor: "partial", CursorWhy: "--output-format stream-json exists; Cully reads transcripts instead."},
	}
}
