---
title: Privacy
description: Exactly what Cully records about your coding sessions, where it is stored and how to turn parts of it off.
---

# Privacy

Cully watches your coding sessions, so it is specific about what it keeps. Your conversations are not Cully's database.

```text
Agent conversation
       │
       │  hooks send one small event per tool call
       ▼
┌──────────────────────────────┐
│ Session journal (local)      │   when · agent · kind of tool
│                              │   passed or failed
│                              │   file path + operation
│                              │   program + subcommand
└──────────────────────────────┘

Never recorded:  prompts · file contents · diffs · command arguments
                 tool output · environment values · URLs · credentials
```

## The session journal

| Recorded | Example | Why |
| --- | --- | --- |
| Time and agent | `10:34`, Claude Code | Timeline and replay |
| Kind of tool | edit, check, search, memory call | Loop detection, status |
| Passed or failed | check failed | Verification and risk |
| Project-relative file path and operation | `internal/auth/resource.go`, edited | Replay and file activity. Taken from file tools only, never from shell text |
| Program and recognized CLI subcommand of a command | `go test`, `git commit` | Replay |
| A one-way hash of a normalized command | `3f9a1c07` | Spotting the same failing command repeated, without keeping the command |
| Fixed notes | a loop was detected | Timeline |

Paths outside the project are dropped, as are URLs and other schemes. Paths longer than 200 characters are dropped. Shell commands contribute a program and, when it matches a fixed list of CLI verbs, a subcommand. Free-form targets and script names are omitted.

**Not recorded:** prompts, file contents, diffs, command arguments, tool output, environment values, URLs, credentials and transcripts.

The journal is stored in your Cully directory on your machine, readable only by you, bounded in size per session, and pruned after 30 days or beyond the newest 40 sessions. It is never uploaded by Cully.

## Turn path and command recording off

```sh
export CULLY_JOURNAL_PATHS=0
```

With this set, the journal still records the kind of tool and whether it passed. Replay then shows activity without file names or commands, and says so.

## Project memory is separate

The journal is local and automatic. Project memory is different: your connected agent chooses what is worth keeping and saves a short summary through its own MCP connection. Hooks do not upload transcripts. See [memory](/memory) for what a saved note contains.

The local advisor does not hold your agent's OAuth credentials. Its worker may save a concise workflow observation or researched plan through the agent's authenticated `cully_optimization_save` tool. Stored evidence contains a summary, opaque session/window links and timestamps, not a transcript or tool payload. User decisions remain foreground-only. Deleting a pattern removes its observation records and queues removal of its semantic projection.

## Explicit team sharing

Workspace tasks, criteria, selected checkpoints, branch/artifact references and reported checks are visible to authorized project members. Separate workspace actions author this content; the journal and private memory are never imported automatically. Do not include transcripts or credentials. The same credential-pattern validation used for memory applies; it cannot guarantee arbitrary prose is safe to share.

Draft lessons are visible only to their author while that author retains project access. `learning_publish` shares the lesson with its project; `learning_edit` makes it private again, and `learning_unshare` or `learning_delete` removes it from shared lookup immediately. Dependent playbooks stop appearing. There are no shared semantic projections or managed lesson caches yet. Membership removal denies subsequent reads and writes; Cully cannot erase material already read or exported.

Audit events contain actor, action, target IDs and timestamp, without checkpoint or lesson text. Operators access them in the private database; they are not a coworker activity feed. Agent/session metadata is declared, with no inferred productivity scores or automatic journal uploads.

## Your workflow profile

The optional `dev-workflow-rules` skill is a private file under `~/.cully/skills/dev-workflow-rules/` that you write and edit yourself; Cully installs only a starter with hint comments. Cully sends at most 12 KiB of it, with likely credentials redacted, to your configured advisor model as background for local analysis. It is excluded from web-research prompts, is never saved to memory automatically, and grants no tool or infrastructure access. Hint comments are not sent, and likely credentials are redacted. Delete the file to remove it.

## Public advisor discovery

The periodic public scout receives only the active agent's name and the current date. Cully does not include the project, accepted task title, private memory, workflow profile or transcript in that prompt. The scout is instructed to use public web tools only. After private analysis, dated documentation, article and research-paper findings enter a private synthesis, which determines relevance to the current task; web content remains untrusted evidence. The local cooldown stores only a timestamp. The session report can retain relevant recommendations with their public source links, not fetched articles. Set `CULLY_DISCOVERY=off` to disable periodic discovery. See [the advisor](/advisor#discover-current-techniques) for cadence and limitations.

## Harness measurements

`cully harness` checks instruction and skill file names and selected agent configuration keys without printing their contents. It also streams recent local transcripts of the running agent (Claude Code or Codex) to count oversized tool output and actual subagent calls. The terminal’s Harness column does this automatically for up to 20 sessions from the exact project’s folder, over the last 30 days, refreshed every five minutes. The CLI defaults to 40 sessions per project; `--all` explicitly measures all project folders, optionally filtered with `--only`.

These measurements retain aggregate counts and token estimates, not transcript text, prompts, credentials or output. The background panel cache is in memory only. Nothing from these measurements is uploaded or saved to project memory automatically. Cursor transcripts are not measured; unsupported history formats are reported explicitly. See [the harness guide](/harness) for limitations.

The PreToolUse guard reads the size of a file the agent is about to read, never its contents, and compares a command's one-way signature with the session journal. It stores only empty marker files, so it does not repeat a note. It sends nothing off the machine.

## Cost reports, evaluation and mobile

`cully savings` reads local Claude Code or Codex transcripts for the selected agent and reports aggregate injection and exploration estimates. `cully impact` reads a private local event ledger and may use a transcript-derived comparison for an estimated memory benefit. `cully eval advisor` replays that same agent’s supported local history; its explicit `--live` option sends derived signal summaries through the configured agent/model and may use recall or targeted public research. The replay evaluator creates private temporary checkpoint files and removes them after evaluation. These tools do not retain or upload raw transcript text. See [token impact](/token-impact) for the separate cost and sample boundaries.

The [mobile Self preview](/mobile) retrieves bounded private personal notes through your signed-in MCP connection. Local tasks stay on the phone, downloaded models run locally, microphone capture is explicit and inference stops on backgrounding. It does not expose memory writes or Team actions. Device-specific credential handling and unverified preview behavior are documented in the mobile guide.

## Where file names can appear

- In `cully replay`, `cully timeline`, `cully handoff` and `cully rescue` output on your machine.
- In the advisor's prompt only as part of an evidence block you can read in the command's output. File contents are never sent.
- In a replay HTML file you generate. It contains paths and command names, no file contents. Treat it like a build log before you share it.
