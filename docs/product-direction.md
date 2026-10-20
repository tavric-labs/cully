---
title: Product vision
description: Cully is the coding-agent harness runtime. Solo provides session intelligence and private memory. Team mode connects people and agents through tasks, handoffs, review and learning.
---

# Product vision

**The harness runtime for coding agents and software projects.**

Cully has two product modes around the coding agents people already use. They share the same CLI, terminal and services; these are workflows, not new `--solo` or `--team` setup flags.

## Solo mode

The local runtime around your coding agent. Understand session health, notice loops and unchecked edits, recover private context, replay what happened and continue in another agent. The journal stays local; saved memory stays owner-scoped. Immediate advisor warnings work offline. Start with [Solo mode](/solo).

## Team mode

A project management harness runtime for people and AI agents building software together. Keep your team's agent work moving, whoever picks it up next. A task outlives any session, person or agent: its outcome, accountable person, attempts, decisions, checkpoint, review evidence and useful lessons remain connected. Members claim work and hand it off; a noncontributing maintainer accepts delivery and adopts explicitly published guidance. Private solo notes and journals remain separate.

The first [team workflow](/team-workflows) uses CLI/MCP and manual agent launch. It implements issuer-scoped OAuth identity, explicit project roles, atomic versioned claims, dependencies, board/inbox, checkpoints, reported review evidence and project lessons/playbooks. Managed execution and automatic learning remain planned.

The adoption hypothesis is that connected tasks reduce repeated investigation and explicit evidence improves review confidence. Validate those outcomes with real teams before claiming savings or fixing pricing. Measure handoff time, review waiting time, reopened tasks and lesson usefulness, with counts and limitations.

This page separates **shipped** work from **proposals**. Proposals are not promises and carry no dates. For the exact list of what exists today, see [current capabilities](/capabilities).

```text
Terminal and hooks ──► Session journal ──► Session intelligence ──► Advisor and review
 (observe)              (understand)         (decide)                 (advise)
                                                  │
                                                  └──► Project memory (remember) ──► Improve
```

## Shipped

| Capability | What it gives you |
| --- | --- |
| [One terminal for every agent](/terminal) | Claude Code, Codex, Cursor CLI and any executable share one wrapper |
| Agent health bar | Agent, project, time, context, loops and unchecked edits at a glance |
| [Session journal](/session-intelligence#the-session-journal) | A private, coarse record every other feature reads |
| [Loop detection](/session-intelligence#loop-detection) | "The same command failed 3 times after edits" |
| `cully replay` and `cully timeline` | What the agent did, step by step, and which files it touched |
| `cully handoff` | A structured handoff for the next agent |
| `cully rescue` | Evidence and recovery steps when a session is stuck |
| `cully status` | Measured session health and a rule-based risk label |
| [Session forensics](/use-cases) | Flight-recorder postmortems, audit-grade review and replayable playbooks from the same record |
| Project workflow hint | "You usually run checks after editing here" |
| Team workspace through CLI/MCP | Authorized project roles, stable tasks, board/inbox, claims and selected checkpoints |
| Task acceptance | Reported evidence for each criterion and revision/version-pinned maintainer approval |
| Explicit project learning | Private drafts, publication/withdrawal and maintained source-linked playbooks |

## Next

Work that builds on the journal and needs moderate additions.

### Approved task plans

The installed Cully skill guides agents to show the intended outcome, work items and verification checks, then seek approval when the user has not already authorized the work. A structured gate bound to a task, project and plan revision remains proposed. Keep routine work within the approved scope moving without repeated questions. Team should build on its existing task criteria, versioned claims and review. Native host capabilities determine whether a gate can be enforced or only suggested. See the [harness review](/harness-review) for sources and implementation boundaries.

### Guardrails

| | |
| --- | --- |
| User problem | An agent can run a destructive command or touch a protected path before you notice. |
| Experience | A short rule file in the project. When a tool call matches, Cully warns in the health bar, and for agents with pre-tool hooks it can ask before the call runs. |
| Reuses | The hook path, the journal, the advisor panel. |
| Missing | Pre-tool hooks for each agent, a rule format, an approval prompt in the terminal. |
| Complexity | Medium. |
| Differentiation | High. Observation becomes control. |

### Cully Bench

| | |
| --- | --- |
| User problem | Cully cannot yet say whether it saves time or tokens. |
| Experience | A published benchmark of multi-session tasks with and without Cully. |
| Reuses | The journal for time to first useful action, repeated searches and repeated failures. |
| Missing | A task suite, a harness, a way to score continuation correctness. |
| Complexity | Medium to high. |
| Differentiation | Credibility. Replaces "ship faster" with numbers. |

### A lighter local install

| | |
| --- | --- |
| User problem | Trying Cully starts PostgreSQL, Mem0, a data API and MCP in Docker. |
| Experience | A single-binary mode that needs no Docker for the terminal, journal, replay, handoff and rescue, with memory added when you want it. |
| Reuses | Everything in the terminal and journal already runs without the memory stack. |
| Missing | A lightweight memory store, or a clear "terminal only" install path. |
| Complexity | Medium. |
| Differentiation | Lowers the first-run cost. |

### Session report

| | |
| --- | --- |
| User problem | After a long session you want a summary for a pull request. |
| Experience | `cully report` renders the journal as a short review note: what changed, what was verified, what is open. |
| Reuses | Replay, handoff, health. |
| Missing | A format and a place to attach it. |
| Complexity | Low to medium. |
| Differentiation | Medium. |

## Team workspace target

The [manual-launch workflow](/team-workflows) is the shipped Team slice. Existing private sessions and task links remain owner-scoped. Team work uses separate records and explicit authorization; Cully does not intercept model traffic.

### Demo and commercial validation

Demonstrate one task: one developer starts it, another continues in a different agent, the reviewer sees missing evidence, the checks are supplied, and an accepted lesson improves a later task. Include a private note that never appears to the teammate and a membership removal that stops new reads.

Measure time to first useful action after handoff, repeated investigation, time waiting for review, reopened tasks and playbook usefulness. Compare like-for-like work with and without Cully. Show counts and denominators; no arbitrary productivity score. Validate willingness to adopt before fixing packaging or pricing.

### Planned team stages

| Stage | Deliverable | Gate |
| --- | --- | --- |
| Managed execution | Capability-aware adapters, isolated worktrees, bounded runs, cancellation and recovery | Duplicate events do not duplicate runs; unsupported controls are explicit |
| Shared learning extensions | Team-wide visibility, semantic shared retrieval, ranking and optional background draft suggestions | Private notes never enter shared retrieval; edits/withdrawals invalidate derived tips |
| Browser and trackers | Web board/inbox and GitHub/Linear-style artifact sync | Signed events, delivery deduplication and external permission checks before bidirectional writes |
| Measured improvement | Team workflow suggestions and agent comparisons from verified outcomes | Show cohort size and confounders before any automatic routing |

## Later

Ideas that need new data, new instrumentation or a lot of history first.

### Learned team workflow

| | |
| --- | --- |
| User problem | "We always run a security review after authorization changes" lives only in people's heads. |
| Experience | When a session type usually ends with certain steps and some are missing, Cully says so. |
| Reuses | The journal, project memory, the existing workflow hint. |
| Missing | Classifying work without reading prompts and collecting enough verified outcomes to support useful patterns. |
| Complexity | High. |
| Differentiation | High, if it is accurate. |

### Shared learning workers

| | |
| --- | --- |
| User problem | Useful lessons stay private drafts or drown in a long project history. |
| Experience | Opt-in preparation suggests draft lessons and ranks currently authorized tips; publishing and playbook adoption stay explicit. |
| Reuses | Project lessons, playbooks and live authorization checks from Team mode. |
| Missing | Bounded job IDs, cancellation, membership-aware caches and Mem0 hydration that never exposes private notes. |
| Complexity | High. |
| Differentiation | High if withdrawal and access removal stay correct. |

### Agent performance intelligence

| | |
| --- | --- |
| User problem | Which agent should take this task? |
| Experience | A comparison across work types from your own history. |
| Reuses | The journal records which agent did what. |
| Missing | Measured outcomes: task completion, rework, human intervention and test results, over hundreds of sessions. |
| Complexity | High. |
| Differentiation | High. |
| Principle | No arbitrary scores. Any comparison must come from signals Cully measures, with the sample size shown. |

### An agent cockpit

| | |
| --- | --- |
| User problem | Several agents run at once and you lose track. |
| Experience | One view of every running session, its health and its open risks. |
| Reuses | The terminal, the health bar, the journal. |
| Missing | A multi-session view, in the terminal or the browser. |
| Complexity | High. |
| Differentiation | Others are building cockpits. Cully's edge would be understanding the work, not the panes. |

## Principles

- **Do not make a commodity feature the identity.** Memory alone, a terminal alone, an MCP server alone or an advisor alone is not Cully. The combination is.
- **Measure before you claim.** Anything shown as a number comes from a signal Cully has.
- **Stay around the agent.** Cully is not another coding agent and does not intercept your agent's model traffic.
- **Keep it private by default.** New data must justify itself in [privacy](/privacy).


## Cully Self on mobile

The native phone app is a personal companion to Cully: a dated daily brief from saved context, a small task list with one focus, Advisor handoffs, and an Ask Cully composer with microphone input. Mobile calls this personal experience Self; it maps to the existing owner-scoped Solo/private service behavior. Workspace mode is a later surface and is not enabled by the mobile preview.

Local language inference chooses from a discovered, host-restricted MCP tool catalog, guided by the canonical Cully skill's mobile profile. Embeddings may later help retrieval; they are not the voice recognizer or tool-calling agent. Engines ship with the app and users explicitly download weights through Setup. Private Cully context still requires the user's authenticated server connection. Local tasks, saved evidence and measured live coding-session health must remain distinguishable.

Android and iOS app source belongs in `apps/`; native connection libraries are versioned and released independently from `tavric-labs/mcp-mobile`. The mobile preview is a consumer of these SDKs, not an alternate protocol implementation. It does not replace the phone OS or expose dialer, camera, settings, agent terminals, Team writes or background automation.
