---
name: cully
description: Use Cully to check session health, find and fix a stuck coding session, replay or hand off work, and save or recall project context through its MCP server. Works with Claude Code, Codex, Cursor and other coding agents.
metadata:
  mobile-profile: references/mobile-self.md
---

<!-- cully:skill:managed -->
# Cully

Mobile hosts use the [Self-mode profile](references/mobile-self.md) from this same skill. They expose their own allowed capabilities; the terminal commands below require a desktop host. The profile is an app integration hint, not a permission grant. Read the [mobile workflow guide](references/mobile-workflows.md) only when planning personal tasks or a brief.

Cully is the coding-agent harness runtime for Solo and private IC work. Solo runs existing coding agents in a shared terminal runtime, observes the session, keeps a private journal of what the agent did, detects problems such as loops, and keeps durable project memory through MCP. Each agent keeps its own reasoning and tools. Use the installed `cully` CLI for session guidance and the configured Cully MCP tools for durable records. The connected agent makes memory calls with its own MCP identity; the local advisor does not hold OAuth credentials. Local guidance works without a server. Do not assume a particular URL or login flow.

## Know where you are running

Cully has Solo and IC modes. Solo uses one local owner. IC is an individual contributor within a company or team using shared services, with an OAuth identity for each contributor. Both keep all memory private. Shared infrastructure does not share notes. Team project management and sharing are deferred; do not invoke workspace actions in Solo or IC.

Cully's session features need the agent to run inside the Cully terminal, started by the user with `cully run claude`, `cully run codex`, `cully run cursor` or `cully run AGENT`. There the user sees a health bar and an advisor panel, and Cully records the session journal. Outside it, memory and the CLI commands below still work, but there is no journal for the current session.

Never start `cully run` or `cully handoff AGENT` yourself. Both open an interactive terminal and will hang a tool call.

## Agree on the coding task

When a user gives a new substantive coding task inside the Cully terminal, first present a short, concrete plan: the intended result, the changes or areas you expect to touch, and the checks that will show the work is done. Use the host's planning or approval UI when available; otherwise ask in the conversation whether to approve or revise that plan. Read-only discovery can make the plan concrete before implementation.

Respect authorization already given. If the user supplied or approved that plan, or explicitly asked you to execute the agreed work, continue without asking again. After approval, carry routine steps within scope through verification. A material scope change needs an updated plan and the user's decision; publication and additional tool permissions still follow the host's own rules. A plan is not approval to merge, deploy or share private information.

Keep the outcome and next step in the existing Cully task/memory workflow; never create project `.cully` folders or rewrite instruction files to hold a plan. This skill guides the agent; it does not make the terminal wrapper an enforcement boundary for arbitrary tool calls.

## Carry the task through the harness

The foreground coding agent interprets the user's prompt and selects the relevant project workflow, skills and tools. After the plan is approved or execution is already authorized, set a short task title with `cully task NAME` inside the terminal and link it through `cully_session` as described below. The advisor receives this accepted title as task context; raw prompts stay out of the journal.

Implement the agreed work, consulting `cully status` and `cully suggestions` at meaningful checkpoints: before a substantial implementation, after repeated failures, when the approach changes, and before declaring completion. Treat advice as evidence to assess, not permission to change scope or apply configuration. Check whether it fits the accepted task and the active agent's capabilities. If the advisor is unavailable or signals are incomplete, say so and use the project's explicit checks; do not wait indefinitely or treat silence as success.

Fix and recheck within the authorized scope until the acceptance checks are satisfied or a concrete blocker needs the user. Save one concise outcome and next step through Cully memory, clear the local title with `cully task --clear` and the completed task link with `cully_session` (`clear_task`), and report the actual verification and any remaining limits. A check predating later edits is not evidence that the final state passes.

## Stay on track and within budget

- At a stalled approach or a material change of direction, give a short checkpoint: the agreed outcome, observed progress, blocker, and one proposed next step. Ask “Are we still on track, or should I change direction?” Wait before continuing that stalled approach unless the user already authorized it. Do not ask again for the same evidence after they answered, or interrupt ordinary progress with routine approval prompts.
- Keep work inside the agreed scope. Ask before expanding into unrelated refactors, adding dependencies or integrations, or launching extra agents when those actions were not already authorized. An advisor recommendation alone does not authorize them.
- Use a bounded read/search first. Reuse unchanged file context; reread only the necessary range when the earlier result was incomplete. Filter or limit noisy command output. Do not repeatedly search the same question or repeat a known failing command without a changed approach.
- State the checks that prove completion and run the relevant ones. Repeat checks after meaningful changes or new failures; a successful run does not justify endless broader testing or optional cleanup.
- Respect any user-supplied time, token, spend or iteration limit. If it is close, summarize what is complete and ask whether to stop, narrow the work or extend the budget. Do not invent token totals when the host cannot measure them. Save a concise handoff before context pressure makes continuation unreliable.
- Prefer the local Cully guards and cached guidance before asking a model to reanalyze unchanged evidence. Automatic advisor work has a request budget; a deferred advisor is not a reason to retry it in a loop. Local warnings and project verification remain available.

The local checkpoint rules flag observed repeated-failure loops, 20 read/search calls without an edit or check, or 12 edits without a successful check. They do not prove scope drift. Treat the observation as a reason to consult the user, and use their answer. Pre-tool nudges are advisory and host-dependent; the wrapper does not enforce a universal spending cap or tool permission gate.

## Drive Cully yourself

Run these on your own when the situation fits. They are read-only and local.

| Situation | Run | Why |
| --- | --- | --- |
| The user asks how the session is going, or you are unsure of verification state | `cully status` | Measured health: duration, checks passed and failed, loops, uncommitted files, risk and why |
| The user says you or another agent is stuck, or the same command has failed repeatedly | `cully rescue --no-ai` | Evidence and recovery steps from recorded data. Without `--no-ai` it also starts a headless advisor run, so use that only when the user asks for a diagnosis. |
| The user asks what happened, what files changed, or what a previous agent did | `cully timeline`, or `cully replay --instant` for the step-by-step transcript with file activity | Never guess from memory when the journal can answer |
| You are about to finish substantive work, or the user wants another agent to continue | `cully handoff --print` | A structured summary of state, verification, open problems and next steps |
| The user wants suggested workflow improvements | `cully suggestions` | Preview a numbered suggestion with `cully apply <n> --dry-run` before changing files or agent settings. Apply only when the user requests or approves the concrete change. When the user wants a numbered suggestion carried out and the Cully terminal's advisor drawer is not available, as in T3 Code or any chat client, run `cully suggestions --request <n>` and follow the request it prints: inspect first, make the smallest repository change, ask before installing or using the network or credentials, then verify and say how to undo it. |

Do not run status checks routinely or interrupt unrelated work. When the journal is empty, say so; do not infer a session. A missing signal means unknown, not healthy.

## Read the advisor honestly

The advisor combines immediate local rules with an optional background worker that uses the originating agent's own CLI and Cully connection read-only. Explain only the suggestions that actually appear. An empty list can mean no issue was detected or that the agent supplied too few signals. A running daemon proves the background process is alive, not that a particular agent's signals arrive.

What the terminal can know differs by agent:

- Codex supplies model, context, tokens and quota through its native footer.
- Claude Code supplies context pressure through a hook-fed snapshot; its own status line stays silent inside the terminal.
- Cursor supplies shell, file-edit and MCP events. Its shell hook reports no exit code, so a failed Cursor command is not marked failed. Model, context, tokens and rate limits have no Cursor feed, so the panel shows them as unavailable rather than waiting.
- Other agents get the health bar and Git state, and tool events only if they can run Cully's hook.

In Warp, mouse tracking stays off until the advisor opens so Warp scroll mode keeps working. Open the advisor with Ctrl+] or F6; Esc closes it and releases the mouse again. Other terminals still support click-to-open on the compact panel.

Loop detection reports what repeated, for example that the same command failed three times after edits. It never states a cause. When it fires, read the first failure before editing again.

The journal records tool kind, success, project-relative file paths with the operation, and the program and subcommand of a command. It never records prompts, file contents, command arguments or output.

When asked whether Cully works, verify `cully version` and `cully status`, and for the terminal check that a real tool call changes the health bar. Describe any untested part plainly.

## Continue work across sessions

- At the start of a substantive task, automatically call `cully_context` with a focused task query and the supplied `session_ref` when known, using its default three-note limit. This returns concise previews; call `cully_get` only for a record whose full details matter. If the server is older and lacks `cully_context`, use one `cully_search` or `cully_recent` call with `limit: 3`.
- If text search misses, try `cully_context` with `mode: semantic` or use `cully_recall` with `limit: 3`. Mem0 recall may lag a recent write. Search again when the task or project changes; avoid repeated broad lookups. Do not ask the user to request memory manually.
- Before answering a question about earlier work, search the configured Cully MCP server with `cully_search`. Filter by `session_ref` or date when known; broaden a search if a narrow query misses relevant records.
- Ground claims about past work in the returned records. Include dates or record IDs when they help the user verify the answer, and say when no matching record was found.
- Use Git and live service checks for current branch, release, deployment, or process state; memory records are history, not a substitute for a fresh status check.

## Preserve what matters

- Before finishing substantive work, automatically use `cully_log` to save one concise record that answers: what task was this, what changed, how and why was it done, what worked, what failed or was missed, and what should happen next? Include only fields that add useful context. Record a reusable blocker or decision as it happens. Do not log every command, trivial turn, transient detail, or full transcript.
- If the installed Cully hook provides `session_ref` and `agent_session_id`, include both in `cully_log`. The reference groups notes; the native ID lets the user reopen the coding agent's session. Save the ID exactly as supplied, and do not invent either value when the hook omits it.
- When the user accepts a Cully task suggestion (`cully apply` prints `Task set: NAME`) or sets one with `cully task`, call `cully_session` once with the task name, your assistant name, the session's `session_ref` and the supplied native `agent_session_id`. This saves one task record linked to the session so later notes group under it. Calling it again with the same name keeps or relinks that record without adding a duplicate, and omitting the task keeps the current link. When the task is done, call `cully_session` with `clear_task` so later notes stop grouping under it; the task record itself is kept.
- Avoid duplicate records when work spans several turns or a stop hook asks you to check again. If an existing record needs correction, inspect it with `cully_get` and use `cully_update`.
- Set `assistant` to the agent in use. Save useful approach, outcome, issue, learning and next-step facts. Use session IDs exactly as supplied. Do not invent project, repository, section, category, theme or tag labels; those fields are not part of the private memory contract. Ownership comes from authentication and is never an agent-supplied field.
- Use `cully_delete` only when the user asks to remove that record.
- Never save credentials, private keys, raw transcripts, or unrelated personal details. Report a write only after the tool confirms it. If MCP is unavailable, explain that the shared record was not saved and continue the user's task.

Durable project handoffs and reusable lessons belong in authenticated Cully MCP records: PostgreSQL holds the source notes and Mem0 indexes them for semantic recall. Foreground agents save the durable summaries; background workers recall notes read-only. The local journal and counters support live warnings and review; they are not a second durable memory store. The local daemon does not receive the agent's OAuth credentials or call Mem0 as the agent. The private data API and Mem0 key are operator settings, never agent configuration.
