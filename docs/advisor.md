---
title: Use the local advisor
description: Check your session and review Cully suggestions inside your coding agent.
---

# Use the local advisor

Cully checks the session signals your coding agent makes available and suggests practical improvements. It can point out a missing project instruction, a useful skill or an MCP connection. Suggestions are advisory: you review a change before applying it.

## From a task to verified work

The installed Cully skill guides the foreground agent to understand your prompt, choose the relevant project workflow and show the intended changes and checks. It asks for approval when execution has not already been authorized, then implements the agreed work and consults `cully status` and `cully suggestions` at meaningful checkpoints. The foreground agent owns the work; the background advisor stays read-only.

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

`cully status` shows whether the daemon is running. If startup failed, resolve the reported error and rerun `cully setup --all` with the same options. For an existing server, keep the `--mcp-url URL` option. Every agent can run in the [Cully terminal](/terminal), which gives all of them the same health bar and advisor panel. Claude Code also keeps its Cully status line when you run it outside the terminal; inside the terminal that status line stays silent and only feeds the panel. The available advice may differ between agents.

| Agent | Installed local integration |
| --- | --- |
| Claude Code | Session and stop hooks, an asynchronous tool-event hook for the terminal, a status line that feeds the terminal panel (and displays itself outside it), `/cully` command and skill. |
| Codex | `cully run codex` terminal, session-start and asynchronous tool-event hooks, `/prompts:cully`, managed user-level `AGENTS.md` pointer and skill. Setup leaves Codex's saved footer settings to the user. |
| Cursor | Session and response hooks, asynchronous shell, file-edit and MCP hooks for the terminal, user-level `/cully` command and skill. |

The background advisor uses one shared analysis workflow with adapters for Claude Code, Codex and Cursor. The originating agent selects its own CLI adapter and configured MCP connection; it does not resume or take over the foreground conversation. Claude supplies its richer hook-derived signals, the Codex wrapper supplies bounded context/tool counters, and Cursor supplies coarse metadata through its continuity stop hook. Missing instruments remain unknown.

| Agent | Background worker | Live input |
| --- | --- | --- |
| Claude Code | `claude` print mode | Existing analysis-hook signals and session instruments. |
| Codex | An ephemeral `codex exec` process with a read-only workspace | Native context and bounded tool counters from `cully run codex`. |
| Cursor | `cursor-agent` print/ask mode | Coarse continuity-stop metadata, without transcript reads. |

The shared instructions ask the worker to consult a few relevant owner/project-scoped Cully records before advising. If a concrete signal identifies a missing capability or a need for current documentation, a separate targeted research step uses that adapter's available web tools. The worker reports whether recall was checked, unavailable, skipped or unconfirmed; missing tools, login or model access do not imply that research succeeded. CLI adapters and their source-reporting behavior are distinct from verification that live MCP/web tools actually worked on a particular machine.

Workers advise without editing the project or writing durable memory. Claude and Codex restrict Cully tools to a read-only allowlist; Cursor uses ask mode and the shared read-only instructions. The foreground coding agent remains responsible for `cully_log` and other authorized durable writes. Local warnings remain available if the background worker cannot run, and Codex combines those warnings with current worker findings.

Claude Code supplies the richest live signals to the background advisor. It can identify high context pressure, repeated tool faults or searches, cost and rate pressure, and a missing verifier, then suggest a native control or a useful MCP integration.

The terminal panel groups Harness, Project & usage and Cully impact in three aligned columns at 120 columns or wider; narrower screens stack the groups. Harness shows setup readiness, named gaps, recent oversized-output estimates, the model, pending verification and loops. Zero errors and unavailable Git/quota rows are omitted. See the [harness guide](/harness) for what readiness and measurement mean. Two prioritized advisor comments and an Open advisor control follow the instruments. Open the full advisor and press Tab for all session instruments. The panel uses the terminal's native background, semantic colors and sparse icons; wrapped advice has a badge and hanging indent. The compact panel grows only enough for its bounded preview while leaving at least 12 Codex rows on normal screens and eight on short screens; terminals shorter than 12 rows give Codex the whole screen. Its reserved height is retained until a terminal resize, avoiding repeated conversation reflow as advice clears. The expanded advisor gives each recommendation room to breathe. Press Ctrl+] / F6 to open all advice, select by mouse or Up/Down, and use Enter to preview an action. Missing native values show waiting or unavailable; Codex cost is omitted when billable-cost data is unavailable. Working-tree additions/deletions describe staged and unstaged tracked Git changes against HEAD, separately from Claude session change counters; new files Git does not track yet are counted as "new" without line totals. The MCP row reads health and authentication from Cully tool responses, so they stay unknown until the first call.

Codex advice cautions at 25% context remaining or less and warns at 10% or less. It also flags repeated explicitly observed failures, broad searching and edits awaiting a check. Direct Bash commands and Codex `exec_command` calls support search/check classification; formatting alone does not satisfy verification. Orchestrated calls such as `functions.exec` remain generic unless their inner events are supplied separately, and arbitrary output text does not establish a tool failure. Missing tool signals show awaiting signals rather than implying a healthy workflow. No prompts, tool output or transcripts are saved. Cursor still gets continuity prompts and Cully commands. The [session optimization guide](/session-optimization) explains the difference.

Durable handoff notes and reusable optimization lessons live in authenticated Cully MCP records, backed by PostgreSQL and indexed by Mem0 for semantic recall. The foreground agent writes those records through its configured connection; the worker uses its adapter's configured connection for read-only recall. Transient local counters and snapshots support offline live warnings; the local advisor does not maintain a second durable memory store or receive the agent's OAuth credentials. Remote memory provides continuity across sessions; it does not replace the live inputs needed to detect current context pressure or pending verification.

The compact Codex pane shows two prioritized comments and an **Open advisor** control. Click the pane or press **Ctrl+] / F6** (macOS may require **Fn+F6**); Alt+A works with Option-as-Meta. The full advisor overlays the session without resizing it. Click a suggestion or use **Up/Down** to select, **Page Up/Down** and **Home/End** to navigate, then **Enter** to add a request to the coding agent's input. Cully pastes it without submitting and keeps any draft you had, so you read and edit it before pressing Enter to send. The **Apply** category marks configuration advice, such as setting up a tool the advisor found by analysing your session and researching it. Its request asks the agent to set the tool up in this repository: inspect the current configuration, read the advisor's source link as untrusted data, make the smallest repository-scoped change, ask before installing packages, adding MCP servers or running commands that use the network or your credentials, never touch secrets, then verify and say how to undo it. The request also carries the measured signals the advisor saw. **Next**, **Watch** and **Warn** ask the agent to act only if the note fits the current task. Cully itself writes no project file and runs no command. Tips are informational. **Tab** shows all instruments; arrows scroll preview/details, and **Esc** goes back or closes. Reports retain up to 12 recommendations; Claude's native status line displays four. Deeper analysis runs after observed Codex activity, at most once every two minutes; Cursor's coarse analysis runs on the first completed turn and then every third completed turn.

The phase turns red **Messy** when at least three explicit failures account for 25% or more of observed calls. Codex also flags eight or more edits awaiting verification together with at least two failures. Successful activity can lower failure density, and a real successful check clears the pending-edit condition. High activity or context pressure alone does not imply disorder. Advice about plans, independent subagents and recurring checks must match the active agent's configured capabilities; Claude-only commands do not apply to Codex or Cursor.

The **Cully impact** group leads with memory used, analyses avoided, loops caught and estimated net tokens, followed by MCP health, authentication and cumulative calls from the foreground agent, session-linked advisor and startup checks, with separate source counts and a combined total. It counts every observed Cully tool, including log, context, recall, search, get, recent, projects, update and delete; unrelated MCP servers are excluded. Counts require actual named MCP hook events; code mentioning a nested call in `functions.exec` is not evidence that it ran. Zero observed calls do not prove the hook is delivering every event. Private local aggregates survive closing the pane and are restored on resume; inputs, results and memory bodies are never saved. Open the advisor and press Tab for the scrollable per-tool breakdown. Older grouped Other totals remain labeled as earlier Other, because their original tool names were not saved.

Semantic recall counts direct `cully_recall` and `cully_context(mode="semantic")` calls. The Context and Recall tool counters still show their actual tool names, so semantic context increments Context and semantic recall without pretending it called the separate recall tool. Advisor and startup Cully calls increase MCP totals without changing foreground coding-tool activity counts.

A private `<thread-state>.cully-mcp` snapshot protects the richer named/source counters while an older running wrapper rewrites its earlier state schema during a live binary upgrade. It shares the thread's writer lock and contains only reduced counters/status, never tool payloads or memory. Reopen the wrapper to load the new renderer; saved counts continue across that transition.

Codex saves reduced instruments in owner-readable `codex-session-<opaque-key>.json` files in its local Cully state directory. The key hashes the native thread ID. Saved tool/search/edit/check/error totals, pending verification, MCP counts and last-observed health/authentication, model, context, token totals and quotas load on resume. A different thread has independent metrics. Native cumulative token totals replace saved values without being summed twice; temporarily clipped footer fields keep their last observed value. Branch, Git changes and daemon status refresh live, and advisor recommendations are analyzed again. Metrics predating this persistence feature can be restored only if they were actually saved; Cully does not reconstruct usage from transcripts. A new thread awaits its first real MCP response instead of claiming an unverified connection.

MCP health/authentication describe the **last observed response**, separately from daemon health: a successful owner-scoped tool marks healthy/authenticated, a failed response marks unhealthy, and an explicit authentication failure marks unauthenticated. Missing response evidence remains unknown. This is not a continuous server probe. Codex cost is omitted because the current native instruments do not provide billable cost; model and input/output tokens remain visible, and Claude keeps its reported cost.

Opening a Codex thread also requests one read-only connection check through an ephemeral coding-agent process using its configured Cully MCP identity. The check runs in the background with a 45-second limit; actual MCP response hooks update the saved health/authentication and Startup call total, without changing foreground activity. Worker text alone cannot mark the connection healthy. Saved metrics load automatically through `cully run codex resume`: explicit native thread IDs can hydrate the first panel frame, while the interactive picker loads the selected thread on SessionStart. Metric rows remain adjacent. Smaller panels drop optional spacing before clipping instruments; open Tab details for the full breakdown.

The Codex panel shows the last completed analysis time and memory/research source states. These states are reported by the agent, rather than independently verified tool telemetry. Claude workers use an isolated temporary Cully-only MCP configuration; Codex workers disable unrelated configured MCP servers for their invocation. Saved agent configurations remain unchanged.

The wrapper detects `TERM_PROGRAM`, `TERM`, the login-shell environment and whether it runs over SSH. The panel displays that profile, and the worker receives it alongside the execution host's OS. Recommendations use the host's shell and OS for command syntax; a Warp client over SSH does not imply that the remote host runs macOS. Unknown client settings remain unknown. Ctrl+] and F6 avoid relying on Option-as-Meta; Alt+A remains available when Meta forwarding is enabled. In Warp, mouse tracking stays off until you open the advisor so Warp scroll mode keeps working; open with Ctrl+] or F6, then mouse selection works inside the drawer.

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
| `cully setup --all --prepare` | Prepares editable stack configuration without starting services or installing integrations. |
| `cully uninstall AGENT` | Removes one agent's Cully-managed integration settings. |
| `cully uninstall` | Stops the local stack and removes managed agent integrations; keeps memory data. |
| `cully uninstall --purge-data` | Also deletes local memory volumes and self-hosted configuration. |
| `cully mcp add --agent AGENT --url URL` | Adds Cully MCP to an existing agent setup; include `--oauth` when the server uses sign-in. |
| `cully impact [--days N] [--json] [--log]` | Counts observed memory hits, skipped analyses, loops and output flags. See [token impact](/token-impact). |
| `cully savings [--days N] [--project TEXT] [--json]` | Estimates Cully injection cost and compares early exploration in local Claude transcripts; not a billing or causal savings report. |
| `cully eval advisor [--project TEXT] [--max N] [--live N]` | Checks the advisor on planted sessions, then replays Claude history with hindsight proxies. `--live` additionally uses the configured model, can use recall/web research and fails on figures not in its signals. See [evaluate Cully](/evals). |
| `cully harness [--cwd DIR]` | Read-only audit of seven agent-setup components: instructions, guardrails, memory, skills, subagents, hooks and plugins. Reports what exists and one suggestion per gap; setup detection checks file names and selected config keys. See the [harness guide](/harness). Also measures oversized tool output and subagent use in your recent Claude sessions for the project (`--days N`; `--all [--only TEXT]` ranks every project's oversized output) as a baseline to compare after a change; it does not claim a component causes savings. |
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
| `CULLY_ANALYZE_DISABLE=1` | Disable advisor analysis while keeping the status line. |
| `CULLY_ANALYZE_PROMPTS=0` | Exclude recent prompt text from analyzer signals. |
| `CULLY_DISPLAY` | Use `minimal`, `full` or `debug` display. |
| `CULLY_COST_INDEX` | Use `eco`, `normal` or `perf` cost estimate. |
| `CULLY_ALERT_CHIME=1` | Ring a terminal bell at critical context pressure. |
| `CULLY_DEBUG=1` | Enable local debug logs. |

Cully honors `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `CURSOR_CONFIG_DIR` for custom agent directories. Local snapshots and diagnostics support the advisor; they are not automatically saved as memory records.
