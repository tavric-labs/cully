# Changelog

## Unreleased

## 0.29.1

- Restore model, context, token and quota instruments inside `cully run codex` and `cully run claude` after the status-line removal. Request native feeds only for the wrapped child, preserve saved settings and explicit overrides, and keep Claude's existing user callback working.
- Show Cursor's selected model from native hooks and label context/token samples reported before compaction. Keep unavailable input/output totals unknown, clear stale samples, and isolate callbacks by live pane and native session. Add CLI pseudo-terminal and regression tests for all three agents.

## 0.29.0

- Add a VS Code extension beside Agent Chat with a live Cully dashboard, theme-aware section colors, harness readiness, session health and MCP details. Keep Advisor, Scout and extra details collapsed, and let users add advisor suggestions as editable chat prompts.
- Launch Claude Code, Codex and Cursor through `cully run`; show session risk in the status bar and provide read-only status, replay, handoff, harness, impact and improvement views without changing agent or terminal configuration.

## 0.28.0

- Support Warp for `cully run claude` and other agents. `WarpTerminal` now passes the terminal check; setup gives manual Nerd Font guidance (Settings → Appearance → Text) instead of rewriting Warp settings.

## 0.27.1

- Restore trackpad and wheel scrolling for alternate-screen agents that do not request mouse reports. Cully now turns each wheel notch into arrow keys for them instead of dropping it; agents that request the mouse still receive real wheel events.

## 0.27.0

- Make the session sidebar opt-in with `cully run --sidebar AGENT` (`CULLY_SIDEBAR=1` still opts in); the list costs 24 columns, so plain runs keep the full width.
- Support Ghostty, iTerm2, Kitty, WezTerm and Alacritty in the Cully terminal. They speak the mouse, keyboard and sync protocols Cully needs, so the brand allowlist is now capability-based; Warp stays unsupported with its own guidance. Ghostty's embedded Nerd symbols are accepted as-is, while the other added terminals get manual Nerd Font guidance instead of a rewrite of their settings.
- Reject session renames that duplicate another session's name, keeping the numbered sidebar unambiguous; session creation already guaranteed unique names.
- Add a left sidebar of named sessions to the Cully terminal. Start a new session, switch between them and rename them from the window; each keeps its own agent, input and scroll position, and they end with the window.
- Activate Cully only through `cully run`. Hooks (including plugin hooks and the PreToolUse guard) are gated on the run marker, the skill and workflow skill move out of the agents' skill folders, and setup no longer writes Codex `AGENTS.md` guidance, so direct `claude`, `codex` and `cursor-agent` launches stay native.
- Remove the Cully status line and its `_internal statusline` command; the Cully panel is the one display. Preserve native TUI behavior: unknown modified keys such as Shift+Right reach the agent whole, Cully shortcuts are opt-in with `CULLY_SHORTCUTS=1`, the Codex footer is no longer overridden, and native terminal modes, cursor and clipboard integrations pass through.
- Remove `cully setup --all --prepare`; setup starts the stack in one command, and OAuth deployments create their configuration directory before the first run. Rework the end-to-end tests for the new product: setup leaves direct agent launches native (checked by running every installed hook outside `cully run`), and a pseudo-terminal test drives `cully run` through Shift+Right passthrough and the session sidebar.
- Support macOS and Linux only; remove the Windows stubs and build tags.
- Keep headless/SSH service setup from installing desktop fonts or changing the account's login shell; direct users to local terminal setup. Verify the installer's quoted absolute setup commands.
- Focus the supported UI on macOS/Linux native terminals with required zsh + Oh My Zsh. Remove Warp's preference adapter, custom scrollbar and conditional mouse workarounds; identified third-party terminals receive native-terminal guidance.
- Apply the required shell and Nerd Font preset automatically during setup, with private backups and restore instructions. Select MesloLGS Nerd Font Mono at 14 pt on macOS or 12 pt on Linux to render icons such as the home glyph U+F015. Preserve unrelated preferences and reuse existing shell installations.
- Add terminal-only setup and check/configure/shell/test/restore commands. Verify Unicode/icons and colors through explicit visual confirmation; observe wheel, divider and advisor gestures and report incomplete checks honestly. Keep SGR/legacy wheel routing, modified gestures, ordered input and Page Up/Down history access. Preserve Esc immediately followed by mouse input so closing the advisor cannot swallow the next scroll gesture.

## 0.26.0

- Analyse completed user-prompt windows every 12 prompts by default, then scout current Claude Code, Codex or Cursor documentation, practical articles and primary research papers. Filter findings against the task and remembered workflows; keep immediate local warnings active between analyses.

- Remember repeated procedures across sessions with owner-scoped optimization evidence and saved skill, workflow, plugin, tool, process or harness plans. Add `cully_optimization_save`, `cully_optimizations` and versioned `cully_optimization_decide`; deduplicate observations by session/window, preserve user decisions and project source notes through Mem0. Schema version 10 adds pattern/evidence storage; migrate before starting matching data/MCP services.

- Restore Warp conversation scrolling before and after opening the advisor, including over the compact panel. Scope Codex workers to real Cully MCP registrations instead of creating an invalid synthetic server, isolate public scouts from configured MCP tools, and report pattern recall alongside memory and research.

- Stop creating duplicate plugin marketplace submission issues on every release; keep one ongoing checklist in #72 for the latest release.

## 0.25.1

- Refresh the README and docs for 0.25.0: list technique discovery in the README capability table, add `CULLY_DISCOVERY=off` to the advisor controls table and mention discovery in the docs index and on the website advisor card.

## 0.25.0

- Add periodic advisor discovery of recent coding-agent releases, model updates, articles, workflows, skills and hooks through the active agent's web tools. Filter dated public findings against the accepted task, bound searches to one attempt per hour, and turn selected workflow ideas into prompts that continue the agreed work. Keep private task context out of the public scout and allow opting out with `CULLY_DISCOVERY=off`.

- Simplify the terminal panel to two sections, Harness and Session. Combine project, context, tokens, quota, Fast mode, Git changes and observed Cully activity in the right Session column; keep Harness unchanged and rename the expanded Cully impact heading to Cully activity.

## 0.24.0

- Replace the docs homepage's narrow handoff-workflow tagline with session intelligence, private memory, recovery, evaluation and harness improvement; describe Solo and IC as private usage patterns and distinguish existing Team workflows from future enhancements.

- Color the terminal health bar and remove lower-panel duplicates only when those metrics are visible in the bar. Retain clipped metrics on narrow terminals and all instruments in the expanded view. Recognize Cursor's native MCP sign-in request and explicitly label agents without connection-check support.

- Name missing setup components beside the readiness count, for example `Setup 6/7 ready · missing: Skills`, and color incomplete setup yellow. Keep configuration gaps distinct from live MCP service health.

- Keep Solo and IC memory private on local or shared infrastructure. Setup discovers an existing deployment's OAuth challenge without a client mode flag; verified sign-in assigns contributor ownership. Solo and IC are high-level descriptions, with no mode flag. Source notes, sessions and Mem0 projections stay owner-scoped, and existing Team workflows retain their separate project authorization and explicit sharing controls.

- Simplify private memory in schema version 8: remove project/repository identity, section, category, theme and tags; retain authenticated ownership, authoring agent, session IDs, task links, timestamps and useful task notes. Add native agent session IDs to session records, scope retrieval evaluations by session, refresh mobile clients and bundled skills, and queue owner-scoped Mem0 metadata rebuilds. Upgrade matching MCP/data services and agent integrations together after a database backup; an image-only rollback cannot restore the removed columns.

- Turn advisor workflow warnings into concrete prompts for the active agent: narrow searches using known paths and symbols, or inspect changes and run a focused check. The user reviews and sends the prompt. Suppress current token-waste advice after newer output moves on, without letting duplicate transcript entries refresh it.

- Clarify Google-only sign-in and how to add LDAP only when another login option is wanted.

- Refocus the homepage on catching stalled work, evaluating and improving the harness, and carrying private context across agents. Replace repeated replay and Team marketing sections with concrete workflows, and update history measurement coverage for Codex.

- Check all Codex Cully registrations before reporting MCP failure, including across status pages, so an expired registration cannot hide another working connection. Distinguish a failed check from authentication that needs sign-in, and remove the first-call hint after a connection check has already supplied status.

- Let `cully setup --all --oauth` accept provider config files and a private environment file, automatically selecting the latest stable mcp-auth release. Mount the config directory read-only, remember input paths for reruns, and keep image overrides limited to the current testing invocation. Document the setup with links to mcp-auth's LDAP, Google and OIDC guides.

## 0.23.0

- Run the test suite in a clean Cully environment. Two statusline tests failed for anyone running them from inside a Cully terminal, because `CULLY_PANE_SESSION` and related variables made the statusline correctly print nothing; the suite now clears them.

- Make savings, advisor replay and the impact transcript comparison follow the agent selected by `cully run`, with Codex history support and explicit unavailable results for unsupported agents. Check MCP connections through the selected Claude Code, Codex or Cursor CLI without a model turn, and keep replay tool/result counts consistent across checkpoints.

- Prepare a separate Cully deployment example for native LDAP, Google and other mcp-auth login methods. The example requires an explicitly selected mcp-auth release containing pluggable login support; the existing Keycloak setup is unchanged.

## 0.22.0

- Describe Cully as a self-improving harness on the website, README and docs: a new Self-improving section, the improve window, task and impact evaluation, and which agents each measurement reads. Capabilities lists the scheduled self-evaluation and task efficiency evaluation as partial where they are not yet observed over a long run or not controlled.
- Make measurements follow the agent you use. The panel's Waste row read only Claude transcripts, so Codex showed "no recent Claude sessions". The Harness column and `cully harness` now read the running agent's own sessions (Claude Code, or Codex from `~/.codex/sessions`), name it, and say "not measured for Cursor yet" where Cully cannot read the history. `cully harness --agent` and automatic detection choose the agent. `cully improve` plans with the running agent, or any installed one, instead of only Claude.

- Count each tool call and result once when measuring transcripts. Some Claude transcripts repeat entries, which made the harness waste, whole-file read and Bash estimates, delegation counts and `cully savings` cost figures up to about twice too high. On one project the oversized-output estimate fell from 149,504 to 127,151 tokens and Cully's measured injected cost from 82,640 to 63,087.
- `cully impact --explain` now shows what Cully's own calls injected in your Claude transcripts, which the net figure does not subtract, so it no longer reads as more favorable than `cully savings`.

- Scroll back through the agent's output with the mouse wheel. Cully draws the agent from its own emulator, so the host terminal had no scrollback of it and the wheel did nothing. The emulator's 10,000 lines are now readable: the wheel moves three lines a notch, the panel says how far back you are, new output does not move what you are reading, and typing returns to the live screen. An agent on the alternate screen still receives the wheel.

- Color the panel: the 5h and weekly limits turn green, yellow or red by what remains; the Harness, Project & usage and Cully impact columns get their own header colors, with colored token counts, waste and MCP call counts.
- Strip terminal escape sequences and control characters from model-written plan text and evaluation details before the improve window or `cully improve` prints them, and from rules and skills before they are written.

- Open `cully improve` as a window: select a suggestion, read its failing checks, then Apply (the plan is shown first and written only on `y`) or Drop (confirmed, and the only way a suggestion is dropped). Cancelling, a failed plan and quitting keep it pending. `--list` prints instead.
- Count lines in new, untracked files in the panel's Git row, so `+N` reflects uncommitted work and not only tracked changes.
- Show the Cully MCP connection and authentication in the panel from `claude mcp list`, checked every ten minutes without a model call, whenever tool responses have not established them. The row notes "(connection check)". Hook-observed values still win.

- Add a self-improvement loop. The advisor daemon runs `cully eval` every five hours (`CULLY_EVAL_INTERVAL` changes it, `off` disables it), keeps the history, and turns failing checks into proposals. `cully improve` shows the trend and proposals; `cully improve --apply N` writes an approved change, limited to rules and one skill under `~/.cully`, and never adds MCP servers or runs commands. Skill names that escape their folder are now rejected by `cully apply` too.
- Add a `tasks` eval suite that scores sessions as tasks: verified or not, time, steps, orientation before the first edit, rework and loops, with rules checked against hand-worked sessions and your own history reported as measurements. Cully now records when `cully apply` and `cully improve --apply` change something, and `tasks` compares median time and steps for tasks before and after each change in the same project, without a verdict when the sample is thin.
- Add an `improve` eval suite that scores the loop against planted failures, and `cully eval --repeat N`, which runs the deterministic suites N times and fails when their results differ.

## 0.21.0

- Replace the workflow questionnaire with a starter `dev-workflow-rules` skill. The installer and first launch install `SKILL.md` with hint comments plus example `references/` files under `~/.cully/skills/dev-workflow-rules/`; Cully asks nothing, never replaces an existing skill, and strips hint comments before the advisor reads it.
- Recognize instruction files used by other coding agents in the harness audit: `GEMINI.md`, Aider `CONVENTIONS.md`, `.windsurfrules`, `.github/copilot-instructions.md` and `.openhands/microagents`, so a project that serves several agents no longer looks uninstructed. Print a feature table after installation.
- Show new files in the panel's Git row (for example `3 tracked, 5 new`), since a diff against HEAD cannot see files Git does not track yet. Explain that MCP health and authentication stay unknown until the first Cully tool call. Let divider drag follow motion that a terminal reports without a button code, which stopped the drag in Warp even though the divider highlighted.
- Add `cully impact --explain`, which itemizes how the Net token figure is built (skipped analyses × estimated tokens each, memory hits, lookup cost), marks each figure as estimated, observed or measured, and lists what is not counted.

## 0.20.0

- Add evidence-based “are we still on track?” checkpoints for failure loops, prolonged exploration and unchecked edits, with deduplicated pre-tool nudges and task boundaries. Warn before redundant full reads of unchanged files. Limit automatic advisor requests to six per rolling hour per Cully session, with a two-minute cooldown and unchanged-evidence suppression; explain deferrals while keeping local warnings available. Extend the canonical skill with scope, verification and user-budget guardrails.

- Report incomplete journal history and missing agent signals across session reports and the terminal. Keep Cursor shell outcomes unknown instead of treating them as successful checks. Run bounded advisor jobs across sessions with per-session ordering, coalesced refreshes and cancellation; route advice through the active agent.
- Test evaluators against deliberately broken installers, audits, monitors and retrieval stores. Reject duplicate retrieval results, include unseeded results in scoring, and fail explicit live evaluations that cannot run.
- Position Solo as the coding-agent harness runtime and Team as the project management harness runtime across the website, CLI, docs and bundled agent skills. Guide agents through task planning, approval, implementation, advisor consultation and verification; pass only accepted task titles to the advisor. Document the harness review and proposed enforcement workflow.

- Fix the release workflow so each release's notes include the tag-pinned plugin install commands. The goreleaser footer that was meant to add them is ignored when a notes file is supplied, so v0.19.0 shipped without them until its notes were edited by hand; the workflow now composes the notes itself.

## 0.19.0

- Publish the plugins through each release: the release notes carry tag-pinned install commands for the Claude Code and Codex marketplaces, one tracked issue per release lists the directory submissions that need a person (Anthropic's directory and the Cursor Marketplace), and a CI job validates the Claude plugin with the real Claude Code CLI. Each plugin gains a README, and a test checks the manifests against both directories' checklists. The manifests still omit a version, which Anthropic's strict validation asks for; that decision is left open.

- Add a PreToolUse guard for Claude Code and Codex: a hook that adds one line of context before a whole-file read of a file over 32 KB, or a shell command that already failed twice in the Cully terminal's journal. It never blocks or changes a permission, fails open, nudges once per file or failure count, and has `CULLY_GUARD=off`. Setup installs and uninstalls it, the plugins carry it, `cully eval advisor` checks it against planted file sizes and command histories, and the release now attaches each plugin as a zip and runs the plugin drift tests first.

## 0.18.0

- Ship a Cully plugin for Claude Code, Cursor and Codex with marketplace manifests for each, carrying the same hooks and skill as `cully setup` (plus the `/cully` command for Claude Code and Cursor). Files are generated from the setup definitions; tests fail if a plugin's hooks or skill drift from setup, and Claude Code's own `plugin validate` checks the Claude plugin and marketplace. The Claude plugin is validated; Cursor and Codex follow their documented formats and are not yet installed in a live agent.

- Add `cully suggestions --request N`, which prints the guarded setup request for a numbered advisor suggestion, so agents in T3 Code or any client without the Cully terminal can offer the same one-step setup. The Cully skill tells agents to use it when asked to carry a suggestion out.

- Add `cully eval`, which scores Cully as one agent against ground truth instead of a model judge: `setup` installs every agent into a scratch home and checks the skills, hooks, harness-audit view, untouched project, repeatability and uninstall; `harness` checks planted setups; `advisor` checks planted waste and loop sessions, replays your transcripts, and with `--live` fails on figures the advisor was never given; `memory` scores text search and semantic recall on recall, precision, F1, ranking, abstention and cross-project leakage. Metrics follow ContextBench, LongMemEval and LivePlan; `docs/evals.md` states the method and its limits. This replaces `cully advisor-eval`, and the end-to-end setup check now runs it.

- Make Enter in the advisor drawer add a ready-made request to the coding agent's input, without submitting it or replacing your draft. For configuration advice, such as a tool the advisor found by analysing the session and researching the web, the request carries the measured signals and source link and asks the agent to set it up in the repository: inspect first, smallest repository-scoped change, ask before installing or using the network or credentials, never touch secrets, verify and explain how to undo. Remove the background model preview and Cully-written file changes from the drawer; `cully apply` is unchanged.

- Make agent setup and uninstall user-wide across projects: install Codex instructions and Cursor commands in user configuration, detect agents independently of repository files. Remove the tracked generated project skill, ignore local `.cully` state, and update setup documentation and website guidance.

- Shorten the README around Cully’s value, first-run setup and core capabilities, with detailed behavior linked to the guides.

- Fix the published-CLI end-to-end check to use the required `cully setup --all` mode, including its preparation check.

## 0.17.0

- Add `cully harness`, a read-only setup audit with named gaps and a historical oversized-output baseline from recent local Claude sessions. Detect project/shared skills and supported agent config, and recognize actual subagent delegations.
- Replace the panel’s leftmost activity counters with Harness readiness, gaps, historical estimates, model and verification state. Keep MCP health/authentication visible in compact layouts, omit unavailable quota/Git fields and label the Claude measurement source.
- Stream harness transcript measurements for the exact project, exclude nested subagent histories, reject nonpositive limits and prevent stale background results from replacing the current project’s data.
- Require measured signal figures in advisor token-spend recommendations and token tool-gap evidence; remain quiet when those figures are unavailable.
- Expand docs and the website for harness auditing, impact/savings reports, advisor evaluation, workflow profiles, CLI updates, divider controls and the mobile Self developer preview. Correct stale measurement, privacy and verification claims.

## 0.16.0

- Add `cully update` with release checks, version selection, checksum verification and atomic CLI replacement; keep existing agent links, restart a running local advisor and direct service upgrades to deployment docs.
- Offer a skippable first-run `dev-workflow-rules` skill with a reviewed private profile of codebases, workflow, infrastructure aliases and access boundaries; use bounded, redacted context in advisor analysis and support later creation with `cully workflow`.
- Connect terminal mouse hover and drag handling to the divider highlight and resize-cursor request; restore the default cursor when leaving the bar or opening the advisor.
- Add `deploy/migrate-registry-namespace.sh` to move an installed VM's website, docs and personal-data controllers to the `ghcr.io/tavric-labs` image namespace: dry-run report, snapshot, local image retag and controller install, with sandbox tests.

## 0.15.0

- Add optional T3 Code support as a multi-agent surface, with a setup hint and documentation in `docs/t3code.md`.
- Add `cully advisor-eval` to test the advisor on past sessions: it replays each transcript at 30, 60 and 90 percent through the live signal pipeline and judges token-waste advice in hindsight (useful, low-value, missed, correct-quiet, with precision and recall proxies). `--live N` also runs the real two-phase advisor, memory recall and web research on a tool gap, on N checkpoints and scores whether it cites numbers and finds a tool. It reads only personal-project transcripts by default and keeps no transcript text.
- Replace the panel's raw Cully MCP call counters and "last observed" timestamp with a Cully impact column: memory used, analyses avoided, loops caught and net tokens for this terminal session, plus one compact MCP health and authentication line.
- Add an impact ledger and `cully impact [--days N] [--json] [--log]`: memory lookups that returned prior work or came back empty, advisor analyses skipped (with the tokens not sent), loops caught and oversized output flagged, against the measured token cost of Cully's own lookups. A memory saving is claimed only when `cully savings` has enough sessions to compare; otherwise the report says it is not claimed.
- Teach the advisor to find token waste: it now sees how much tool output the session spent on oversized results, whole-file reads, noisy Bash output and Cully's own calls. When the waste is significant it suggests the single most important fix, with the numbers, and may ask the research step to look for a published tool; it stays quiet otherwise.
- Keep the native agent session ID in the compact `cully_log`/`cully_update` acknowledgement so a saved note can still be used to resume its conversation.
- Add `cully savings` to measure whether Cully costs more tokens than it saves: measured tokens injected by Cully calls and schemas, plus an observational, confidence-labelled estimate of exploration avoided when a lookup returned prior work. It reads local Claude Code transcripts and keeps counts only.
- Return a compact acknowledgement from MCP `cully_log` and `cully_update` instead of echoing the saved record back into the agent's context. Add the token-efficiency plan in `docs/token-efficiency-plan.md`.
- Make the Cully panel divider an obvious drag handle: a heavy tinted bar with a grip chip at rest, a brighter bar under the pointer, and a solid band while dragging. The pointer becomes a vertical-resize cursor (OSC 22) over the bar where the terminal supports it; button-less motion is tracked only to highlight the bar and is not forwarded to the agent.
- Give mobile Self a dated Today briefing: last 24 hours, a sampled seven-day review, projects, recorded learnings and explicit continuation drafts. Add optional read-only time-window filters with timestamp/project evidence, clear coverage and older-server states, and local AI reflection in Today and Advisor. Promote last-planned continuations, expand the saved approach/outcome in Recall how/why, and draft learning practice questions from recorded lessons; keep personal task changes explicit.
- Update repository, installer, mobile SDK and container publishing/deployment references for the Tavric Labs organization rename. MCP Runtime remains the product name and existing Go module imports remain stable.

## 0.14.0

- Add native Android and iOS Cully Self clients under `apps/`: Today briefs, personal task/focus management, Advisor handoffs, and a shared Ask Cully composer with microphone input. Reuse the Cully icon and the released mcp-mobile native SDKs; browser sign-in keeps service credentials on the server.
- Add explicit local-model Setup downloads with pinned checksums, on-device Qwen inference, Android Whisper speech and iOS on-device speech capture. The model follows the canonical Cully skill's short mobile profile and chooses bounded read-only context lookups; task changes remain explicit UI actions. Add mobile build CI and document preview/device-verification limits. Build the iOS app against the unsigned device SDK because the pinned upstream inference framework has no simulator slice.

## 0.13.0

- Save the coding agent's native session ID with Cully work notes, alongside the opaque reference, so a later lookup can reopen the original Codex, Claude Code or Cursor conversation without storing its transcript.
- Add website handler allocation benchmarks using verified site fixtures and isolated successful submission and profile-import requests.

## 0.12.0

- Forward agent utility commands and redirected streams without requiring a panel or changing arguments, diagnostics or exit status; keep the panel for interactive coding sessions.
- Resize the Cully panel by dragging its divider, keeping the chosen height across refreshes and terminal resizes. Warp offers an explicit resize mode with `Ctrl+\` so normal host scrolling remains available.

- Centralize website limits and validation errors while preserving optional-photo submissions and rate limiting. Build LinkedIn profile imports from a fixed HTTPS origin and reject unsafe profile paths and lookalike hosts.

## 0.11.3

- Refuse to start `cully-data serve` until schema version 6 is applied (sessions/tasks and Team workspaces), instead of accepting a database that only reached version 3. Migration tests assert the required version stays current.

## 0.11.2

- Let a task owner cancel their own active/blocked work after lease expiry without reclaiming first. Mark expired review packets stale. Allow project maintainers to force-release someone else's review (or their expired active/blocked claim) back to ready. Delete dependent playbooks when a lesson is deleted. Treat unknown workspace actions as non-writes for CLI/MCP tool selection.

## 0.11.1

- When a Team member is removed from the team or project, or demoted to viewer, their owned open tasks return to ready so incomplete reviews and live claims are not stranded. Keep checkpoint/next step for handoff and clear evidence. Align `task_get` stale with inbox/reclaim at the exact lease expiry instant.

## 0.11.0

- Harden Team workspace invariants: keep a project maintainer when removing a team member, reject cancelled dependencies and canceling tasks that still have open dependents, allow project writers to cancel ready tasks, require completed tasks before lesson drafts, prefer published lessons over drafts in `lessons`, and keep team-scoped audit events free of client `project_id`. Restore installer setup without a default `--agent codex`. Fold implemented design pages into team workflows and product vision; remove the standalone team-workspace and shared-learning design docs.
- Add optional T3 Code support: a guide for running several agents from one T3 Code window with shared Cully memory and handoffs, and a `cully setup` hint when `t3` is installed. Cully stays independent of T3 Code and works the same in a plain terminal.
- Split E2E coverage into Solo setup/memory/session checks and an authenticated Team handoff, review, sharing and revocation workflow using production binaries. Refactor workspace routing into focused access, task, review and learning handlers with one shared action contract; fix the Go formatting failure.
- Introduce the manual-launch Team workspace alongside Solo's agent copilot: OAuth-scoped project roles, stable tasks/dependencies, atomic claims, board/inbox, portable checkpoints, revision-pinned reported evidence and noncontributing-maintainer acceptance. Add transactional audit events, private lesson drafts, explicit project publication/edit/withdrawal and source-validated playbook adoption through MCP and `cully workspace`; private memory and journals stay separate. Update the website tagline, product vision, mode guides, architecture, privacy, deployment and capability docs. Migration 006 is required; managed execution and automatic learning remain planned.
- Fix two macOS test harness issues: resolve temporary-directory symlinks when checking child working directories, and drain terminal output during the hangup test.
- Install one Cully binary at `~/.local/bin/cully` and link agent command paths to it, migrating older Cully copies. Installer logs show versions, paths, migrations and terminal refresh instructions; full uninstall removes managed agent links.
- Require `cully setup --all` for a full local setup or `--mcp-url URL` for a deployed server, reject positional agent names, and verify MCP initialization and Cully tool discovery. Local setup streams build progress and identifies failed stages; OAuth verification remains pending until agent sign-in.

## 0.10.2

- In Warp, the Cully terminal no longer enables mouse tracking at startup. That mode was consuming Warp scroll mode and the host mouse wheel. Mouse tracking turns on only while the advisor drawer is open (Ctrl+] / F6) and turns off again when it closes. Other terminals keep click-to-open on the compact panel.

## 0.10.1

- In the Cursor (and other no-usage-feed) terminal panel, show Model and Context as unavailable instead of "waiting for agent". Cursor has no Codex footer or Claude statusline feed for those instruments; Tokens and rate limits already said unavailable. Claude and Codex still wait for their real feeds.

## 0.10.0

- Define explicit session task-link semantics: omitting the task keeps the current link, a new name links it, and `clear_task` in `cully_session` unlinks it while keeping the task record. Repeating a task name after a clear relinks the kept record instead of adding a duplicate. `cully_update` can no longer move a linked task entry to another section, and deleting a task entry clears only the link. The deployment manifests now gate all eleven registered tools, and a contract test keeps registration, manifests and the skill in agreement.
- Refactor the terminal core without changing behavior: one agent catalog replaces the scattered per-agent switches, shared Codex-named panel types use neutral names, and footer/snapshot feeds parse through explicit updates with a documented merge contract. On-disk paths, hook commands, panel output and goldens are unchanged.
- Show researched tool suggestions only when the advisor's web scout reports a successful search.
- Fix the compact panel in Claude and other non-Codex agents: it no longer shows a Fast row, and a Cully MCP result sent as a string of JSON text now counts as healthy and authenticated.
- Add Subagents, Web and Commits counters to the panel's activity rows. Subagent (`Task`/`Agent`) calls, web fetches and searches, and `git commit` commands are counted from tool events without recording their arguments.
- Fix the panel's Cully MCP health and Auth rows in Claude Code: a successful tool result sent as an array of content blocks now counts as healthy and authenticated instead of staying "unknown".
- Add sessions and tasks to the memory schema (migration 004): a `cully_sessions` table with project, branch, assistant and a linked task, a `task` entry type, and the MCP tools `cully_session` (start or update a session, optionally with a task) and `cully_session_get`. The migration is additive; existing records, tools and the `entry_type` values `work`, `issue`, `learning` and `decision` are unchanged.
- Make the terminal panel agent-neutral: in Claude it now shows Claude's model, tokens, limits and line changes from its statusline feed, says "waiting for Claude" instead of "waiting for Codex" when data is missing, and hides the Codex-only Fast row.
- Fix review findings on sessions and tasks: the capabilities page now lists the session task as partial instead of planned, task names are capped at 60 characters in both the CLI and `cully_session`, `cully_log` rejects `entry_type: task` in favour of `cully_session`, the task file uses owner-only permissions, task suggestions wait for three edits, concurrent `cully_session` calls serialize per session with a PostgreSQL-safe lock key, and constraint validation runs after the exclusive migration lock is released. Session updates reject section changes, and advisor refreshes retain Claude's panel metrics.
- Add a Task row to the health panel. When a session has edits and no task, Cully suggests one from the Git branch ("Looks like you're working on ... Add it as the task to Cully"); accept it with `cully apply <n>`, or set it with `cully task NAME`. The Cully skill then records the accepted task through the new `cully_session` tool and links it to the session. Progress is not shown yet because the journal has no measured source for it.
- Keep free-form command arguments out of session journal labels, count `create_file` and `notebookedit` as edits, and reconcile individual untracked files and rename destinations in replay.
- Reposition Cully as the intelligent workspace around coding agents ("Your coding agents need a copilot too"): rewrite the README, homepage, docs introduction, agents guide and Cully skill, and add how-it-works, terminal, session intelligence, privacy, capabilities and product-direction pages. Remove the "better work and everyday life" tagline from the CLI help, MCP instructions and skill metadata.
- Add `cully run AGENT [ARGS...]`, which starts Claude Code, Codex, `cursor-agent` or any executable in the same Cully terminal that `cully codex` used. `cully claude`, `cully codex` and `cully cursor` are shortcuts. Cully still needs macOS or Linux for the terminal.
- Add an Agent Health bar to the terminal: agent, project and branch, session time, context, loops and unchecked edits. Segments with no data are omitted.
- Add a local session journal fed by asynchronous tool-event hooks for Claude Code, Codex and Cursor. It records time, agent, kind of tool, pass or fail and a one-way command hash, and never prompts, file contents, command arguments or output. Run `cully setup` again to install the new Claude Code and Cursor hooks.
- Add loop detection (the same command failing three times, or three edit and failed-check cycles) and a project workflow hint when edits have not been checked.
- Add `cully replay`: a step player, file activity with hotspots, a reconcile check against `git status`, `--json` output and a self-contained offline `--html` export. File paths come from structured file tools. A shell command records its program and recognized subcommand only. Set `CULLY_JOURNAL_PATHS=0` to stop recording paths and command labels.
- Keep session risk on a loop that is still happening. A resolved loop stays in the count and no longer marks the session HIGH. Journal pruning keeps the newest sessions per project, and journal writes lock the file.
- Add `cully timeline`, `cully handoff` and `cully rescue`, and a measured session health block with a rule-based risk label in `cully status`.
- Add a [use cases](docs/use-cases.md) page positioning the shipped primitives as a flight recorder for agent sessions, audit-grade session review and replayable playbooks, with a matching section on the homepage.
- Claude Code's status line stays silent inside the Cully terminal and feeds the panel instead. It behaves as before outside the terminal.

## 0.9.1

- Run a single end-to-end CI check that follows the documented path: install, `cully setup`, MCP use and uninstall, replacing the separate compose-only job.
- Describe Cully in the docs introduction as a companion for work and everyday life, not only session resume and guidance.

## 0.9.0

- `cully setup` now runs the local stack without OAuth on port 3393 for Cully MCP and fixed uncommon loopback ports for the data API, Mem0 and both databases, picks the next free port when one is busy, saves the choice in `.env`, and keeps ports of an already running stack.

## 0.8.3

- Preserve richer Cully MCP counts when an older running wrapper rewrites its earlier state schema during an upgrade, using a private thread-scoped counter snapshot.
- Add an opt-in live check confirming an advisor's actual semantic Cully call reaches its linked session counter without increasing foreground activity.

## 0.8.2

- Count every observed Cully MCP tool by name across foreground, advisor and startup calls, with saved combined and per-source totals restored on resume.
- Show all Cully tool counts in expanded session instruments and distinguish semantic context recall from direct recall calls.
- Preserve earlier category totals during telemetry upgrades, without inventing names for previously grouped Other calls.

## 0.8.1

- Reduce panel row spacing: adjacent metric rows, one blank row after section titles and between major sections; keep the three-column gap.
- Restore saved same-thread instruments before the first panel frame when resuming an explicit Codex thread ID, while the resume picker loads them on SessionStart.
- Check Cully MCP through the coding agent's configured identity after opening a thread; only actual response hooks update health/authentication, without adding probe calls to foreground counters.

## 0.8.0

- Organize the Codex pane into Model & activity, Project & usage, and Cully MCP groups, with aligned columns on wide screens and consistent spacing when stacked.
- Add a small uniform gap between wide-panel metric rows and a larger break before the advisor, shedding optional spacing first on short screens.
- Count directly observed Cully MCP calls per session, including log/context/recall/search/get, and show last-observed health/authentication with explicit unknown and failure states.
- Show verified MCP health as a green Healthy or red Unhealthy heading badge, with a clear awaiting-response startup state and no duplicate Health row.
- Save private thread-scoped MCP and tool totals, verification state and native model/context/token/quota instruments locally; restore them when resuming the same Codex thread and keep different threads isolated.
- Keep the last observed native token totals when a narrow footer omits them, and replace cumulative totals without double counting.
- Omit Codex cost/cache placeholders when reliable billable cost is unavailable; retain model, context and token usage.

## 0.7.0

- Make the Codex pane's row spacing consistent within each section, with a single separator row between identity, instruments, advice and controls.
- Require a changelog update in every PR through repository instructions, a PR checklist and a dedicated CI check.
- Add a compact full-width Codex pane with two prioritized comments, suggestion/tip counts and an interactive advisor opened by mouse or Ctrl+] / F6.
- Preview and accept advice with mouse/arrow selection and Enter; apply supported shared instruction/skill changes locally or hand task actions to the current coding input without discarding its draft.
- Share owner-scoped memory recall and targeted research across Claude, Codex and Cursor advisor workers, with isolated native CLI adapters and clear startup/source status.
- Add an evidence-based red Messy phase, terminal-aware command guidance, native-background styling and adaptive group spacing while retaining full session instruments.
- Release Codex sessions and process descendants when the wrapper exits or loses its terminal.

## 0.6.1

- Bind Codex pane signals to their SessionStart session when hooks run through a persistent app server, without saving prompts or tool output.
- Document the distinct Claude, Codex and Cursor surfaces, live signal checks, and local CLI setup with a remote MCP/data/Mem0 stack.

## 0.6.0

- Replace `cully agent setup` with one `cully setup` command for local services, detected agent integrations and the advisor; use `--mcp-url` for existing servers.
- Log startup stages and return errors when the advisor or another component fails; keep `--prepare` configuration-only.
- Install project hooks in the caller's directory, and have the installer direct local users to the complete setup command.

## Earlier changes

- Accept GitHub source archives with global PAX headers so the published CLI can download and start its matching self-hosted stack.
- Clarify that `cully setup` prepares configuration and starts the full local stack; `--prepare` stops before starting services for team configuration.
- Add `cully uninstall` for local Docker and agent cleanup, with explicit `--purge-data` for memory deletion.
- Combine local agent guidance and shared memory as Cully.
- Rename the local command, agent integrations, MCP tools and configuration to Cully.
- Port the MCP and private memory data services from Python to Go.
- Add self-hosted Mem0 semantic recall with transactional indexing retries, source hydration and deletion propagation.
- Store authoritative records in plain PostgreSQL with owner isolation, full-text search and IST timestamps; use Mem0 for semantic recall.
- Add an explicit fresh-database schema, VM cutover documentation, separate CI checks, database E2E and server container builds.
- Add separate website/docs delivery and a gated personal-service release workflow.
