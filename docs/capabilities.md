---
title: Current capabilities
description: What Cully does today, what works only partly, and what is planned, with the agent each capability applies to.
---

# Current capabilities

This page separates what ships today from what is planned. Cully's other pages describe only the first two groups.

**Shipped** means it works as described and has tests. **Partial** means it works with limits listed here. **Planned** means it does not exist yet; see [product direction](/product-direction).

## Observe

| Capability | Status | Notes |
| --- | --- | --- |
| [Cully terminal](/terminal) for Claude Code, Codex, Cursor CLI and any executable | Shipped | macOS Terminal, Ghostty, iTerm2, Kitty, WezTerm, Alacritty or a native Linux terminal, with zsh + Oh My Zsh and the Nerd Font preset. Warp is outside support. |
| [Harness column and CLI audit](/harness) | Shipped | Setup detection across supported configurations; historical estimates read the running agent's local transcripts (Claude Code or Codex; Cursor is not read yet). Presence does not prove a working connection. |
| Resizable panel divider | Shipped | Drag to resize; `Ctrl+\` enables explicit resize mode. Hover requests a resize cursor where supported. |
| Agent health bar | Shipped | Agent, project and branch, time, context, loops, unchecked edits. Segments with no data are omitted. |
| Tool events from hooks | Shipped | Claude Code and Codex hooks, and Cursor shell, file-edit and MCP hooks. Hooks do nothing outside the Cully terminal. |
| Cursor shell exit codes | Partial | Cursor's shell hook reports no exit code, so a failed Cursor command is not marked failed. Loop detection is weaker for Cursor. |
| Native footer (model, tokens, quota) | Partial | Codex only. |
| Context pressure | Partial | Codex and Claude Code. Other agents show none. |
| Cost and cache values | Planned | Shown only if an agent reports them. |

## Understand

| Capability | Status | Notes |
| --- | --- | --- |
| [Session journal](/session-intelligence#the-session-journal) | Shipped | Local, bounded, no contents. See [privacy](/privacy). |
| [Loop detection](/session-intelligence#loop-detection) | Shipped | Rule based. Reports what repeated, never a cause. |
| Verification state | Shipped | Whether a check passed since the last edit. |
| [Session health](/session-intelligence#cully-status) and a rule-based risk label | Shipped | The label names its reason. It is not a score. |
| Session task and progress | Partial | The task comes from `cully task` and the linked session record; a progress percentage is still planned because the journal has no measured source for it. |

## Remember

| Capability | Status | Notes |
| --- | --- | --- |
| [Project memory](/memory) through MCP | Shipped | PostgreSQL source records, Mem0 semantic recall, owner-scoped. |
| `cully_context` bounded recall | Shipped | Up to five short previews. |
| Continuity hooks | Shipped | Claude Code and Codex start hooks; Claude Code and Cursor turn-end reminders. |
| Shared project learning | Partial | Private drafts, explicit publish/edit/unshare/delete, current-source lookup and maintainer adoption; no team-wide visibility, shared semantic indexing or learning worker. |

## Advise

| Capability | Status | Notes |
| --- | --- | --- |
| [Advisor](/advisor) with context, search, failure and verification warnings | Shipped | Rule warnings work offline. |
| Model-backed advisor analysis | Partial | Uses the agent's own CLI in a headless run. Needs that CLI and its Cully MCP connection. |
| Clickable advisor in the terminal | Shipped | Click the panel, or `Ctrl+]` / `F6`. |
| Project workflow hint | Shipped | After three earlier sessions with edits where checks usually followed, it warns when the current session has unchecked edits. |
| Private workflow profile | Shipped | Optional `dev-workflow-rules` skill. The installer adds a starter with hint comments and example reference files; bounded redacted context (hint comments removed) goes to the configured advisor model. |
| Token-spend evidence rule | Shipped | Advisor prompt requires measured figures; generated advice still needs review. |
| Guardrails that block or approve actions | Planned | |

## Continue and review

| Capability | Status | Notes |
| --- | --- | --- |
| `cully timeline` | Shipped | Grouped history of a session. |
| `cully replay` | Shipped | Step player, file activity, git reconcile, JSON and offline HTML export. File paths come from structured file tools. The interactive view and the HTML page have been exercised in a pseudo-terminal and a stubbed DOM, not yet in a range of terminals and browsers. |
| `cully handoff` | Shipped | A structured handoff you can print or start another agent with. |
| `cully rescue` | Shipped | Evidence and recovery steps; an optional headless advisor adds a probable cause. |
| `cully status` session health | Shipped | |
| [Session forensics](/use-cases) | Shipped | Recorded-session postmortems, verification-trail review and replayable examples, composed of timeline, replay, reconcile and handoff. |

## Coordinate project work

| Capability | Status | Notes |
| --- | --- | --- |
| Team/project access and stable tasks | Shipped | OAuth principals; explicit project roles; additive migration 006. Private memory stays separate. |
| Board and decision inbox | Partial | CLI/MCP project views for tasks, blockers, reviews and stale work. No web view. |
| Atomic claims and attempt leases | Shipped | Current-version writes; one claim winner; 30-minute lease and explicit reclaim. Manual agent launch. |
| Portable team checkpoint | Shipped | Selected context, next step and branch/artifact references; no automatic journal uploads or process transfer. |
| Evidence-backed task acceptance | Partial | Reported evidence for each criterion, exact revision/version and noncontributing maintainer approval. No independent CI attestation or configurable completion policy. |
| Maintained project playbooks | Partial | Explicit adoption from completed tasks; withdrawal invalidates retrieval. Editing/archive controls remain planned. |
| Audit events | Partial | Transactional database events for operators; no user-facing audit query/export. |
| Managed agent execution | Planned | No autonomous dispatch, enforced tool budgets or automatic cancellation. |

See [team workflows](/team-workflows) for inputs, limits and roles.

## Improve

| Capability | Status | Notes |
| --- | --- | --- |
| Workflow hint from past sessions | Shipped | Narrow on purpose. |
| [Impact ledger and savings report](/token-impact) | Shipped | Observed events and estimated costs; benefit comparisons are observational and Claude transcript based. |
| Advisor replay evaluation | Shipped | Hindsight precision/recall proxies on selected Claude Code or Codex sessions, following the running agent; optional live model/research pass consumes tokens. |
| Verified CLI updater | Shipped | `cully update`; preserves agent links and restarts an active local advisor. Services upgrade separately. |
| [Scheduled self-evaluation and suggestions](/self-improvement) | Partial | The daemon runs `cully eval` every five hours and turns failing checks into suggestions. `cully improve` opens a window to read, apply or drop each one; an approved change writes only rules and one skill under `~/.cully`. Not yet observed over a long-lived daemon, and no suggestion writes hooks. |
| [Task efficiency evaluation](/evals#task-rules-did-the-work-finish-and-what-did-it-cost) | Partial | Scores sessions as tasks (verified, time, steps, orientation, rework, loops) against hand-worked sessions, and compares tasks before and after an applied change in the same project. The comparison is uncontrolled; matching similar tasks by semantic similarity is not built. |
| `cully impact --explain` | Shipped | Itemizes how the net token figure is built and labels each part estimated, observed or measured. |
| Learned team workflow | Planned | |
| Agent performance comparison | Planned | Needs measured outcomes first. See [product direction](/product-direction#later). |

## Mobile Self preview

| Capability | Status | Notes |
| --- | --- | --- |
| [Android and iOS clients](/mobile) | Partial | Source-built developer preview; store distribution is not configured. |
| Today brief and local-model reflection | Partial | Dated, bounded personal-note samples; updated date-filter-capable services required. |
| Local tasks and read-only Ask Cully | Partial | Explicit task changes stay on device; no memory writes, Team actions or remote agent execution. |
| Physical device coverage | Partial | Selected Android checks and Mac/unsigned iOS compilation; full iPhone sign-in/voice/chat remains unverified. |

## What Cully does not claim

- Token costs and observed differences are estimates, not causal proof of savings or a productivity benchmark. A controlled benchmark remains on the [roadmap](/product-direction#next).
- The harness audit, `cully savings` and advisor replay read Claude Code or Codex transcripts, following the running agent. Cursor and other agents without a validated history reader report measurement as unavailable. Named Codex MCP calls are measured when present; orchestrated calls may hide them. Codex file-level context metrics are unavailable. Raw transcript text is not uploaded by these measurements; an explicitly requested live advisor evaluation sends signal summaries to the configured adapter.
- It does not see inside an agent's reasoning. It sees tool calls and results.
- A missing signal is shown as missing, never guessed.
