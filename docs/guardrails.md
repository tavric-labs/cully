---
title: Task and token guardrails
description: Human direction checks, bounded exploration, repeated-read warnings and automatic advisor request limits.
---

# Task and token guardrails

Cully combines cheap local rules with the installed agent skill. Local rules use recorded tool metadata and file timestamps; they make no model call. The skill guides the foreground agent to keep the user's outcome, scope and budget in view.

## Ask whether the approach is still on track

The local checkpoint rules flag:

| Observed evidence | Suggested action |
| --- | --- |
| An active repeated-failure loop | Explain the blocker and propose a different next step. |
| 20 read/search calls without an edit or check | Summarize what the exploration found before spending more context. |
| 12 edits without a successful check | Review the approach and verification plan before widening the change. |

The agent should summarize the agreed outcome, observed progress, blocker and proposed next step, then ask: **“Are we still on track, or should I change direction?”** It should wait before continuing the stalled approach unless the user already authorized proceeding. An answered checkpoint should not become a repeated approval prompt.

These counts describe activity, not semantic task progress. A long investigation can be necessary; a checkpoint asks the user to judge that. A successful check resets the unverified-edit count, and a newly accepted local task starts a fresh observation boundary. An unknown check outcome does not count as a success.

The checkpoint appears in session status and the terminal advisor. Supported Claude Code and Codex pre-tool hooks also add a deduplicated nudge before a subsequent watched tool call. The nudge is once per task, reason and successful-check epoch. It does not change tool permissions or block the hook. Cursor and arbitrary executables depend on available signals and skill guidance.

## Avoid repeated context and work

Existing pre-tool guards warn before large whole-file reads and a third attempt at a command that has failed twice. A further read guard notices a recorded read of a file of at least 4 KB whose current modification time is no newer than that read. It suggests reusing the context or reading a range, and deduplicates warnings using the current size and modification time. Limited reads are left alone; a newer modification time prevents a repeated-read warning. The rule uses metadata, not a content hash or transcript comparison, so it cannot prove that the contents are unchanged or the earlier result was complete.

The installed skill also asks the agent to filter noisy output, avoid repeating searches without a new hypothesis, verify changed work with relevant checks, and avoid optional cleanup or broader testing after completion unless a new concern justifies it. Unrelated refactors, unapproved dependencies/integrations and additional agents require authorization when outside the agreed scope.

## Bound automatic model-backed advice

Automatic advisor dispatches share a local budget per Cully session key:

- At most **six accepted automatic requests per rolling hour**.
- At least **two minutes between accepted requests**.
- Identical signal snapshots are suppressed within the hour. With discovery enabled, an active session can refresh public research after an hour even if the signals are unchanged.

The budget persists across daemon restarts and uses a file lock for concurrent hooks. It stores timestamps and a hash of the evidence, not the signal text. Unreadable accounting or a backwards clock defers optional model work. The panel and `cully status` explain the deferral; local warnings and the coding task continue. Skipped analyses appear in the impact ledger as estimated prompt overhead avoided.

This limits automatic dispatches, not total model tokens or billing. One job can include periodic public discovery, private analysis and evidence-triggered tool-gap research. Discovery allows one attempt per hour per session and agent, one to three searches, and a 45-second deadline; failed attempts also count toward its cooldown. Set `CULLY_DISCOVERY=off` to disable that optional discovery phase. Different session keys have separate budgets, and explicit rescue/live evaluation calls are outside this automatic budget. The existing worker deadline and bounded scheduler also apply.

Respect explicit user-supplied spend, time, token or iteration limits separately. An agent should ask whether to stop, narrow the work or extend that budget as it approaches the limit. Cully must not claim a measured token total for a host that does not expose one.

## Boundaries

`CULLY_GUARD=off` disables the pre-tool nudges. The runtime's automatic advisor request budget remains independent. Neither a skill nor a diagnostic warning is a universal execution gate; each host retains its tool permission system. A failed or missing signal must remain unknown, never proof that the task is safe, on track or complete.
