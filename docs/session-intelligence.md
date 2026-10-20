---
title: Session intelligence
description: How Cully turns tool events into a private session journal, loop detection, session health, replay, timelines, handoffs and rescue.
---

# Session intelligence

Session intelligence is how Cully understands what a coding agent is doing. The [Cully terminal](/terminal) and agent hooks record small events. Everything on this page is derived from those events, plus live Git state read at the moment you ask.

```text
Terminal activity
       +
Agent hooks          ──►  Session journal  ──►  Loops · health · replay · handoff · rescue
       +
Live Git state
```

This works for an agent only while it runs inside the Cully terminal. Run `cully run claude`, `cully run codex`, `cully run cursor` or `cully run AGENT` first.

## The session journal

One journal file per session, in your Cully directory, readable only by you. Each line is one event:

| Field | Example |
| --- | --- |
| Time and agent | `10:34`, Claude Code |
| Kind | edit, check, search, other tool, Cully memory call, note, start, end |
| Result | passed, failed or unknown when the agent supplies no outcome |
| File path and operation | `internal/auth/resource.go`, edited (project-relative; read, write, edit, create, delete or move) |
| Command | `go test` (program and recognized subcommand only) |
| Command hash | a one-way hash, so the same failing command is recognized without keeping it |

It never holds prompts, file contents, command arguments or tool output. It is bounded in size, pruned after 30 days, and path and command recording can be turned off. See [privacy](/privacy) for the full guarantee.

When older events are discarded, the journal keeps a history-gap marker. Unreadable files, malformed records and scan limits are reported as unavailable or partial history. Status, timeline, replay, handoff and rescue label those limits; counts then describe only observed events. A missing session start is also reported. Receiving some tool events does not prove that every hook arrived.

## Loop detection

Cully reports when an agent appears to repeat itself. It states what repeated and never claims to know why.

| Rule | Fires when |
| --- | --- |
| Repeated failure | The same command failed 3 times within 20 events or 15 minutes. A passing run of that command resets it. |
| Edit and failed-check cycles | An edit was followed by a failed check 3 times with no passing check in between. Any passing check resets it. |

When a loop is detected the terminal shows `⚠ Loop 3x` in the health bar, adds a caution to the advisor, and writes one note to the journal for the timeline. The wording stays factual: "Cully noticed something: the same command failed 3 times after edits. Inspect the first failure before editing again."

Cursor's shell hook reports no exit code. Those check outcomes remain unknown: they do not count as passing tests, clear pending verification or resolve a detected loop. Explicit failure events still count as failures. The panel and status report this signal limitation.

## A workflow hint from your own history

If at least three earlier sessions in a project had edits, and a check ran after the last edit in at least 80% of them, Cully warns when the current session has edits with no check since. It reads at most 20 earlier journals and says nothing otherwise.

## `cully status`

```text
CULLY

  Agent          Codex
  Branch         main
  Session        27m (running)
  Context        ███████░░░ 71%
  Tests          34 ✓  2 ✕
  Verification   failed
  Loops          1 ⚠
  Uncommitted    4 files
  Memory         2 recalls · 1 save
  Risk           HIGH · the agent may be looping
```

Only measured values appear. Context shows when the agent reports a window size. Uncommitted comes from Git at run time. **Risk** is a plain rule, not a score, and it names its reason:

| Level | When |
| --- | --- |
| HIGH | A loop is still active, the last check failed, or context is 90% used or more |
| MEDIUM | Edits have not been checked, context is 75% used or more, or 15 or more files are uncommitted |
| UNKNOWN | Journal history or tool signals are incomplete, with no higher-priority observed warning |
| LOW | Nothing is flagged |

The Task row appears only for an explicitly accepted task. Cully does not infer task intent or progress from missing events. Before any tool event arrives, verification is unknown rather than clean.

## `cully timeline`

A grouped history of a session, not terminal output.

```text
00:09  Edited ×1
00:09  Check failed ×3 (same command)

Summary: 12m, 1 edit, checks 0 passed / 3 failed, 1 loop, 0 memory saves
```

`cully timeline` shows the newest session of the current project. `--all` lists recent sessions, `--session ID` picks one, and `--cwd DIR` chooses a project.

## `cully replay`

Replay is a step list of what the agent did, plus the files those steps touched and whether checks passed.

```text
10:31  Read     internal/auth/resource.go
10:34  Edited   internal/auth/resource.go
10:42  Ran      go test  ✕ failed
10:47  Ran      go test  ✓ passed
```

File paths come from structured file tools (read, write, edit, delete, apply_patch). A shell command shows up as its program and recognized subcommand, such as `go test`, and does not invent a file list from the command text.

In a terminal, replay is a step player beside the file list:

| Input | Action |
| --- | --- |
| `Space` | Pause or play |
| `←` `→` | Step one event |
| `↑` `↓` | Change speed |
| `f` | File activity only |
| `/` | Filter by file or command |
| `q` | Quit |

```sh
cully replay                     # newest session of this project
cully replay --session ID        # a specific session (a unique prefix works)
cully replay --files             # file activity only
cully replay --instant           # print the transcript, no animation
cully replay --json              # a versioned JSON document for other tools
cully replay --html [FILE]       # one self-contained HTML file
```

**File activity** lists created, edited, deleted, moved and read-only files, how many times each was touched, and hotspots edited three or more times. **Reconcile** compares the journal with live `git status`: files Git shows as changed that the journal never saw, and recorded deletions that still exist. It also says plainly when the data is partial, for example when hooks started late or path recording is off.

`--html` writes `cully-replay-<session>.html` with the same steps, a scrubber, a file heatmap and filters. It is one offline file: inline styles and script, no network requests, owner-only permissions. It contains file paths and command names but no file contents, so treat it like a build log before you share it.

With `CULLY_JOURNAL_PATHS=0` there are no file names or command names to show.

## `cully handoff`

A structured handoff so a different agent can continue without rebuilding context. It includes only sections that have data.

```sh
cully handoff --print            # print it
cully handoff codex              # start Codex in the Cully terminal with the handoff as its first prompt
```

It lists the branch, a short diff stat and the changed file names read live from Git, verification state, open problems such as detected loops, failing checks as counts only, and what the next agent should do: call `cully_context` to recover the task and earlier decisions, verify state with the project's checks, and save progress with `cully_log`. It does not guess the task, because Cully does not record prompts.

`cully handoff AGENT` needs an interactive terminal. Otherwise it prints.

## `cully rescue`

For a stuck session. It prints evidence from recorded data and rule-based recovery steps.

```text
Evidence
  - Newest session: claude, under 1m long
  - 1 edits; 0 checks passed, 3 failed
  - Verification: failed
  - The same check failed 3 times, with no edits between the failures
  - Git branch: main
  - 1 uncommitted file(s)

Recovery steps
  1. Stop editing. Run only the failing check and read the first failure before changing anything.
  2. Run the focused check for the code you just edited.
```

Without `--no-ai`, it also asks a headless advisor run (`--agent claude|codex|cursor`, default the first one installed) for a probable cause and next steps, using the evidence and file names but never file contents, with a 90 second limit. If the advisor is unavailable it says so and still prints the evidence. Cully never prints a confidence percentage.

## Why it is built this way

- **Measured, not guessed.** A signal Cully does not have is shown as missing.
- **Derived from one record.** Every command reads the same journal, so they agree with each other.
- **Private by design.** The journal is coarse on purpose. See [privacy](/privacy).
