---
title: Use the local advisor
description: Check your session and review Cully suggestions inside your coding agent.
---

# Use the local advisor

Cully checks the session signals your coding agent makes available and suggests practical improvements. It can point out a missing project instruction, a useful skill or an MCP connection. Suggestions are advisory: you review a change before applying it.

## From a task to verified work

The installed Cully skill guides the foreground agent to understand your prompt, choose the relevant project workflow and show the intended changes and checks. It asks for approval when execution has not already been authorized, then implements the agreed work and consults `cully status` and `cully suggestions` at meaningful checkpoints. The foreground agent owns the work; the background advisor leaves project files unchanged and may save bounded private optimization evidence or proposals.

After agreeing on the task, use `cully task NAME` inside the terminal. Advisor requests include that short accepted title, bounded to 60 characters, so guidance and memory recall can relate to the task. Without an accepted title, task intent remains unknown. Raw prompts are not captured or forwarded by this path. Clear the local title with `cully task --clear` and unlink the durable session task when finished.

Completion depends on the project's acceptance checks and the final state, not on an advisor's silence. Missing signals and unavailable advice remain explicit. The skill guides supported agents; enforcing a task plan across arbitrary agent tools remains proposed in the [harness review](/harness-review).

See [task and token guardrails](/guardrails) for measured human check-ins, repeated-read warnings, scope guidance and the automatic advisor request budget. Automatic requests are limited to six per rolling hour per Cully session key, with a two-minute cooldown and duplicate-evidence suppression; local warnings continue when the model advisor is deferred.

`cully setup --all` installs the agent integrations and starts the advisor daemon along with the local memory services. After [setting up Cully](/quickstart), restart your agent and check suggestions:

| Agent | In the agent |
| --- | --- |
| Claude Code | Start `cully run claude` for the health bar and advisor panel, or run `/cully suggestions`. |
| Codex | Start `cully run codex` for the health bar and an expandable advisor panel, or run `/prompts:cully suggestions`. |
| Cursor | Start `cully run cursor` for the health bar and advisor panel, or run the user-level `/cully suggestions` command. |

You can always run these in a terminal:

```sh
cully status
cully suggestions
```

`cully status` shows whether the daemon is running. If startup failed, resolve the reported error and rerun `cully setup --all` with the same options. For an existing server, keep the `--mcp-url URL` option. Every agent can run in the [Cully terminal](/terminal), which gives all of them the same health bar and advisor panel. Codex's default native footer and Claude's usage callback are requested only for the wrapped child; saved settings and direct native launches stay unchanged. Explicit customizations can opt out of those feeds. The available advice may differ between agents.

| Agent | Installed local integration |
| --- | --- |
| Claude Code | Session and stop hooks, an asynchronous tool-event hook for the terminal, `/cully` command and skill. |
| Codex | `cully run codex` terminal, session-start and asynchronous tool-event hooks, `/prompts:cully`, and skill. Setup writes nothing to `AGENTS.md` and leaves Codex's saved footer settings to the user. |
| Cursor | Session and response hooks, asynchronous shell, file-edit and MCP hooks for the terminal, user-level `/cully` command and skill. |

The background advisor uses one shared analysis workflow with adapters for Claude Code, Codex and Cursor. The originating agent selects its own CLI adapter and configured MCP connection; it does not resume or take over the foreground conversation. Claude supplies its richer hook-derived signals, the Codex wrapper supplies bounded context/tool counters, and Cursor supplies coarse metadata through its continuity stop hook. Missing instruments remain unknown.

| Agent | Background worker | Live input |
| --- | --- | --- |
| Claude Code | `claude` print mode | Existing analysis-hook signals and session instruments. |
| Codex | An ephemeral `codex exec` process with a read-only workspace | Native context and bounded tool counters from `cully run codex`. |
| Cursor | `cursor-agent` print/ask mode | Coarse continuity-stop metadata, without transcript reads. |

The worker first analyses the completed prompt window and consults a few relevant owner-scoped Cully notes and optimization patterns. It uses text lookup first, then semantic recall through Mem0 when needed. Window counters and repeated program/subcommand labels distinguish new observations from cumulative activity; repeated necessary checks are not automatically waste. Saved observations and session links can reveal a repeated procedure across days. They are not a complete transcript or a record of every session.

Workers never edit the project or install anything. Claude and Codex allow bounded recall plus only `cully_optimization_save` for durable writes; their public scouts have no configured MCP access. Cursor uses ask mode and the same bounded instructions. The foreground agent retains `cully_log`, user decisions and implementation. Source states report the agent's tool use; they are not independent proof of service access. Local warnings continue if analysis, memory or research fails.

## Discover current techniques

After the first successful private analysis and then at most once per hour per session/agent, a public scout runs one to three focused searches. It opens official Claude Code, Codex or Cursor documentation/release notes, practical articles, and primary research on harnesses, memory, tool use, evaluation and workflow automation. It verifies article claims against primary sources. At most two dated findings enter a second private analysis. Ordinary findings must be within 90 days; a directly relevant foundational research paper may be up to five years old and must state its experimental setting and an applicability limit.

The public scout receives only the agent name and today's date, without task titles, private memory, workflow profiles or transcripts. The private synthesis filters its findings against the accepted task, observed behavior and remembered user decisions. It can save one concrete plan for a skill, workflow, plugin, tool, process or harness improvement through `cully_optimization_save`. The plan includes the benefit, steps, source URLs and validation. A paper motivates an evaluation; it does not establish actual savings or compatibility with the installed agent.

Select a suggestion in the advisor and press Enter to add it to your coding agent's prompt. A workflow suggestion asks the agent to check compatibility, design a short task workflow with verification steps, and continue work already authorized. It does not require creating configuration files or installing a tool to make a plan. Skill, hook and integration setup suggestions use the reviewable setup request described below. You read and send the prompt yourself.

Discovery has a 90-second deadline inside the worker's overall five-minute limit and shares the automatic advisor request budget. Failed attempts enter the hourly cooldown. Scouting occurs after a completed prompt window, rather than because the terminal was left idle. The panel reports the last attempt and source state. Fetched papers/articles are not cached; the cooldown keeps a timestamp, while a useful selected proposal keeps its concise plan and source links in owner-scoped memory.

Set `CULLY_DISCOVERY=off` before starting Cully to disable periodic discovery while retaining local warnings, normal analysis and evidence-triggered tool-gap research. `CULLY_ANALYZE_DISABLE=1` disables automatic model-backed advice entirely.

Claude Code supplies the richest live signals to the background advisor. It can identify high context pressure, repeated tool faults or searches, cost and rate pressure, and a missing verifier, then suggest a native control or a useful MCP integration.

The terminal panel groups Harness and Session in two aligned columns at 120 columns or wider; narrower screens stack the groups. Session combines project, context, tokens, quota, Fast mode and Git changes with observed Cully activity. Harness shows setup readiness, named gaps, recent oversized-output estimates, the model, pending verification and loops. Zero errors and unavailable Git/quota rows are omitted. See the [harness guide](/harness) for what readiness and measurement mean. Two prioritized advisor comments and an Open advisor control follow the instruments. Open the full advisor and press Tab for all session instruments. The panel uses the terminal's native background, semantic colors and sparse icons; wrapped advice has a badge and hanging indent. The compact panel grows only enough for its bounded preview while leaving at least 12 Codex rows on normal screens and eight on short screens; terminals shorter than 12 rows give Codex the whole screen. Its reserved height is retained until a terminal resize, avoiding repeated conversation reflow as advice clears. The expanded advisor gives each recommendation room to breathe. Press Ctrl+] / F6 to open all advice, select by mouse or Up/Down, and use Enter to preview an action. Missing native values show waiting or unavailable; Codex cost is omitted when billable-cost data is unavailable. Working-tree additions/deletions describe staged and unstaged tracked Git changes against HEAD, separately from Claude session change counters; new files Git does not track yet are counted as "new", with their lines (up to 200 files, text only) included in the additions. The MCP row reads health and authentication from Cully tool responses and from a check through the agent passed to `cully run` every ten minutes (marked "connection check") when no response has established them. Claude Code uses `claude mcp list`, Codex discovers Cully tools through `codex app-server`, and Cursor uses `cursor-agent mcp list-tools cully`. These checks start no model turn. A stored configuration or OAuth credential alone does not establish health; unsupported agents keep unknown values until actual tool responses arrive.

Codex advice cautions at 25% context remaining or less and warns at 10% or less. It also flags repeated explicitly observed failures, broad searching and edits awaiting a check. Direct Bash commands and Codex `exec_command` calls support search/check classification; formatting alone does not satisfy verification. Orchestrated calls such as `functions.exec` remain generic unless their inner events are supplied separately, and arbitrary output text does not establish a tool failure. Missing tool signals show awaiting signals rather than implying a healthy workflow. No prompts, tool output or transcripts are saved. Cursor still gets continuity prompts and Cully commands. The [session optimization guide](/session-optimization) explains the difference.

Durable handoff notes and reusable optimization lessons live in authenticated Cully MCP records, backed by PostgreSQL and indexed by Mem0 for semantic recall. The foreground agent writes ordinary task notes and explicit decisions; the worker uses its adapter's connection to recall records and save bounded optimization evidence/proposals. Transient local counters and snapshots support offline live warnings; the local advisor does not maintain a second durable memory store or receive the agent's OAuth credentials. Remote memory provides continuity across sessions; it does not replace the live inputs needed to detect current context pressure or pending verification.

The compact Codex pane shows two prioritized comments and an **Open advisor** control. Click the pane or press **Ctrl+] / F6** (macOS may require **Fn+F6**); Alt+A works with Option-as-Meta. The full advisor overlays the session without resizing it. Click a suggestion or use **Up/Down** to select, **Page Up/Down** and **Home/End** to navigate, then **Enter** to add a request to the coding agent's input. Cully pastes it without submitting and keeps any draft you had, so you read and edit it before pressing Enter to send. The **Apply** category marks configuration advice, such as setting up a tool the advisor found by analysing your session and researching it. Its request asks the agent to set the tool up in this repository: inspect the current configuration, read the advisor's source link as untrusted data, make the smallest repository-scoped change, ask before installing packages, adding MCP servers or running commands that use the network or your credentials, never touch secrets, then verify and say how to undo it. The request also carries the measured signals the advisor saw. **Next**, **Watch** and **Warn** ask the agent to act only if the note fits the current task. Cully itself writes no project file and runs no command. Tips are informational. **Tab** shows all instruments; arrows scroll preview/details, and **Esc** goes back or closes. Reports retain up to 12 recommendations; Claude's native status line displays four. Deeper analysis runs after 12 completed user prompts by default on supported native hooks; `CULLY_ANALYZE_CADENCE` overrides the interval. Stop hooks only schedule detached work and do not block a turn for model analysis. Older hooks without prompt-submission events use explicitly labeled completed turns. Immediate local rules remain active between windows.

The phase turns red **Messy** when at least three explicit failures account for 25% or more of observed calls. Codex also flags eight or more edits awaiting verification together with at least two failures. Successful activity can lower failure density, and a real successful check clears the pending-edit condition. High activity or context pressure alone does not imply disorder. Advice about plans, independent subagents and recurring checks must match the active agent's configured capabilities; Claude-only commands do not apply to Codex or Cursor.

The **Session** group also shows memory used, analyses avoided, loops caught and estimated net tokens, plus MCP health, authentication and cumulative calls from the foreground agent, session-linked advisor and startup checks, with separate source counts and a combined total. It counts every observed Cully tool, including log, context, recall, search, get, recent, projects, update and delete; unrelated MCP servers are excluded. Counts require actual named MCP hook events; code mentioning a nested call in `functions.exec` is not evidence that it ran. Zero observed calls do not prove the hook is delivering every event. Private local aggregates survive closing the pane and are restored on resume; inputs, results and memory bodies are never saved. Open the advisor and press Tab for the scrollable per-tool breakdown. Older grouped Other totals remain labeled as earlier Other, because their original tool names were not saved.

Semantic recall counts direct `cully_recall` and `cully_context(mode="semantic")` calls. The Context and Recall tool counters still show their actual tool names, so semantic context increments Context and semantic recall without pretending it called the separate recall tool. Advisor and startup Cully calls increase MCP totals without changing foreground coding-tool activity counts.

A private `<thread-state>.cully-mcp` snapshot protects the richer named/source counters while an older running wrapper rewrites its earlier state schema during a live binary upgrade. It shares the thread's writer lock and contains only reduced counters/status, never tool payloads or memory. Reopen the wrapper to load the new renderer; saved counts continue across that transition.

Codex saves reduced instruments in owner-readable `codex-session-<opaque-key>.json` files in its local Cully state directory. The key hashes the native thread ID. Saved tool/search/edit/check/error totals, pending verification, MCP counts and last-observed health/authentication, model, context, token totals and quotas load on resume. A different thread has independent metrics. Native cumulative token totals replace saved values without being summed twice; temporarily clipped footer fields keep their last observed value. Branch, Git changes and daemon status refresh live, and advisor recommendations are analyzed again. Metrics predating this persistence feature can be restored only if they were actually saved; Cully does not reconstruct usage from transcripts. A new thread awaits its first real MCP response instead of claiming an unverified connection.

MCP health/authentication describe the **last observed response**, separately from daemon health: a successful owner-scoped tool marks healthy/authenticated, a failed response marks unhealthy, and an explicit authentication failure marks unauthenticated. Missing response evidence remains unknown. This is not a continuous server probe. Codex cost is omitted because the current native instruments do not provide billable cost; model and input/output tokens remain visible, and Claude keeps its reported cost.

Opening a Codex thread also requests one read-only connection check through an ephemeral coding-agent process using its configured Cully MCP identity. The check runs in the background with a 45-second limit; actual MCP response hooks update the saved health/authentication and Startup call total, without changing foreground activity. Worker text alone cannot mark the connection healthy. Saved metrics load automatically through `cully run codex resume`: explicit native thread IDs can hydrate the first panel frame, while the interactive picker loads the selected thread on SessionStart. Metric rows remain adjacent. Smaller panels drop optional spacing before clipping instruments; open Tab details for the full breakdown.

The Codex panel shows the last completed analysis time and memory/research source states. These states are reported by the agent, rather than independently verified tool telemetry. Claude workers use an isolated temporary Cully-only MCP configuration; Codex workers disable unrelated configured MCP servers for their invocation. Saved agent configurations remain unchanged.

The wrapper detects terminal client metadata, the login shell and SSH, keeping the terminal client separate from the execution host. Supported terminal UI targets macOS Terminal, Ghostty, iTerm2, Kitty, WezTerm, Alacritty, Warp or a native Linux terminal, using zsh + Oh My Zsh. Mouse, wheel and pointer reports remain enabled. Scroll over the agent or compact panel to read normal-screen history; fullscreen agents receive wheel events inside their viewport. Drag the divider to resize the panel; release or Esc ends a drag. See [terminal setup](/terminal#configure-and-verify-your-terminal) for the automatic Nerd Font preset, backups and visual verification.

If the agent process cannot start, memory is marked **not run**, with a bounded failure category such as invalid MCP configuration or authentication required. This differs from a completed analysis whose actual recall failed. Raw CLI stderr is not exposed because it can include private tool data or credentials.

## The PreToolUse guard

Before a tool call, a small hook can nudge the agent away from a known waste. It only adds a line of context next to the tool result. It never blocks a call or changes a permission, and it fails open on any error. Two rules, both deterministic and with no model call:

- **Whole-file read.** A `Read` of a file over 32 KB with no offset or limit gets a note with the file's size and a pointer to `offset` and `limit`. Once per file per session. Claude Code only, since Codex reads files through the shell.
- **Repeated failure.** A shell command that has already failed twice in the Cully terminal's session journal, since it last passed, gets a note to read the first failure before retrying. It speaks again only when the count grows. It needs the journal, so it works only inside `cully run`.

Set `CULLY_GUARD=off` to disable it. Claude Code and Codex install it with `cully setup` (Codex asks you to trust it in `/hooks`). Cursor has no equivalent hook here yet. The design follows LivePlan's finding that a rule-based monitor steers an agent cheaply when it speaks only on a flag; see [evaluate Cully](/evals).

## Preview a suggestion

If Cully lists a numbered improvement, preview it with the number shown:

```sh
cully apply 1 --dry-run
```

Review the preview. To apply it, run `cully apply 1` and confirm the proposed change. Cully preserves unrelated agent settings and does not silently replace user-owned configuration.

## Commands

| Command | What it does |
| --- | --- |
| `cully status [directory]` | Shows session warnings and integration state for the current or specified directory. |
| `cully suggestions` | Lists improvements and informational notes. |
| `cully suggestions --request <n>` | Prints the guarded setup request for a numbered improvement, the same one Enter adds to your prompt in the advisor drawer. Use it in a client without the Cully terminal. |
| `cully apply <n> --dry-run` | Previews a numbered improvement. |
| `cully apply <n>` | Applies one after confirmation. |
| `cully setup --all [--agent AGENT]` | Starts the local stack, installs agent integrations and starts the advisor; detects agents by default. |
| `cully setup --mcp-url URL [--agent AGENT]` | Connects an existing server, installs agent integrations and starts the advisor. |
| `cully uninstall AGENT` | Removes one agent's Cully-managed integration settings. |
| `cully uninstall` | Stops the local stack and removes managed agent integrations; keeps memory data. |
| `cully uninstall --purge-data` | Also deletes local memory volumes and self-hosted configuration. |
| `cully mcp add --agent AGENT --url URL` | Adds Cully MCP to an existing agent setup; include `--oauth` when the server uses sign-in. |
| `cully improve [--run] [--apply N] [--dismiss N]` | Shows how Cully scored itself in its scheduled eval and applies an approved improvement to Cully's own guidance. See [self-improvement](/self-improvement). |
| `cully impact [--days N] [--json] [--log] [--explain]` | Counts observed memory hits, skipped analyses, loops and output flags; `--explain` shows how the Net token figure is built and what it leaves out. See [token impact](/token-impact). |
| `cully savings [--days N] [--project TEXT] [--json]` | Estimates Cully injection cost and compares early exploration in local Claude Code or Codex transcripts, following `cully run` or `--agent`; not a billing or causal savings report. |
| `cully eval advisor [--project TEXT] [--max N] [--live N]` | Checks the advisor on planted sessions, then replays the running agent’s history (Claude Code or Codex) with hindsight proxies. `--live` additionally uses the configured model, can use recall/web research and fails on figures not in its signals. See [evaluate Cully](/evals). |
| `cully harness [--cwd DIR]` | Read-only audit of seven agent-setup components: instructions, guardrails, memory, skills, subagents, hooks and plugins. Reports what exists and one suggestion per gap; setup detection checks file names and selected config keys. See the [harness guide](/harness). Also measures oversized tool output and subagent use in your recent Claude Code or Codex sessions for the project (`--days N`; `--all [--only TEXT]` ranks every project's oversized output) as a baseline to compare after a change; it does not claim a component causes savings. |
| `cully update [--check] [--version TAG]` | Updates this machine's CLI after verifying the release checksum and binary version, and restarts a running local advisor. `--check` changes nothing; `--version` selects a release or rollback. Services are upgraded with your deployment tooling. |
| `cully workflow [--skip]` | Installs a starter private `dev-workflow-rules` skill for you to fill in, which the advisor reads as background, or records that you skipped it. Existing skills are preserved. |
| `cully version` | Prints the CLI version. |

Cully's [memory tools](/memory) keep useful context across sessions. The installed Cully hooks ask the connected agent to retrieve and save concise work notes. The local advisor can still show session guidance if the memory server is temporarily unavailable.

Applying a suggestion can update a project instruction or skill, register an MCP integration, or run an accepted install command. Review the dry run first; Cully keeps unrelated MCP connections and user-owned settings. Hook, worker and daemon entry points are internal commands installed by Cully, not steps you need to run yourself.

The [session optimization guide](/session-optimization) shows how bounded memory previews, native agent controls and advisor suggestions work together without loading an old transcript into a new task.

## Controls

Set these environment variables only when you want to change the defaults:

| Variable | Effect |
| --- | --- |
| `CULLY_ANALYZE_DISABLE=1` | Disable advisor analysis. |
| `CULLY_DISCOVERY=off` | Disable periodic public discovery of current coding-agent techniques; local warnings and tool-gap research remain. |
| `CULLY_ANALYZE_PROMPTS=0` | Exclude recent prompt text from analyzer signals. |
| `CULLY_DISPLAY` | Use `minimal`, `full` or `debug` display. |
| `CULLY_COST_INDEX` | Use `eco`, `normal` or `perf` cost estimate. |
| `CULLY_ALERT_CHIME=1` | Ring a terminal bell at critical context pressure. |
| `CULLY_DEBUG=1` | Enable local debug logs. |

Cully honors `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `CURSOR_CONFIG_DIR` for custom agent directories. Local snapshots and diagnostics support the advisor; they are not automatically saved as memory records.

## Act on a workflow warning

Open the advisor with Ctrl+] or F6, select a warning and press Enter. Cully puts a proposed next step in the active agent’s input for you to review and send. A many-searches warning asks the agent to narrow the unanswered question to known paths or symbols; a focused-check warning asks it to inspect the diff and choose an appropriate check. The prompt explains the scope and preserves existing permissions. Cully never submits it automatically or treats an advisor suggestion as authorization.

## Reuse a saved optimization

`cully_optimizations` retrieves three active patterns by default; use a focused text query, semantic mode for Mem0 recall, or recent mode. `status: all` also includes accepted, implemented and dismissed plans so the advisor can respect previous decisions. `cully_optimization_save` deduplicates a stable pattern/session/window combination and computes distinct observation/session counts in PostgreSQL. A retry or research synthesis does not increase the count. Each result carries up to three recent evidence summaries and a version.

When a researched proposal fits, the advisor links `[cully-plan:ID]`. Selecting it prepares a prompt for the foreground agent to retrieve the full plan with `cully_get`, check applicability and implement within the user's authorization. `cully_optimization_decide` records accepted, dismissed, implemented or explicitly reopened status using the current version. It does not execute the plan. Workers cannot overwrite these decisions. A new researched idea may have one observation; it must be distinguished from repeated behavior across sessions.
