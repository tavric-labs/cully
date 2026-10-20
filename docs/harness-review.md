---
title: Harness review and next steps
description: Implementation findings from MCP Events, Ralph and other coding harnesses, and how they apply to Cully Solo and Team.
---

# Harness review and next steps

Reviewed on 2026-10-21. The starting point was [MCP Events SEP-3415](https://github.com/modelcontextprotocol/modelcontextprotocol/pull/3415), followed by Ralph and other coding harnesses. The SEP is still a draft. The linked repositories include SDKs, event infrastructure and agent runtimes; their scope and maturity differ.

The implementation review used temporary shallow clones, including the open Mastra, TanStack and conformance PR branches where the relevant code was absent from their default branches. It inspected source and test fixtures; it did not run or certify the external projects. The recommendations below are Cully design judgments, rather than claims that another project's approach will improve task completion here.

## Product boundary

**Solo is the coding-agent harness runtime. Team is the project management harness runtime for people and AI agents building software together.**

Solo owns the terminal session, observation, guidance, recovery and private memory around an existing coding agent. The underlying agent retains its reasoning, tools and permissions. Team owns durable project coordination: outcomes, criteria, ownership, dependencies, claims, checkpoints, review and explicitly published lessons. Today people start agents manually. Team is not yet an autonomous scheduler that executes a software project end to end.

## Findings implemented in this change

| Subsystem | Source of inspiration | Concrete Cully improvement |
| --- | --- | --- |
| Evidence and replay | Herdr distinguishes lost history from an empty result; event clients expose delivery gaps. | Preserve a marker when the bounded journal rotates. Report invalid or unreadable records, scanner limits and missing session starts. Surface limits across status, timeline, replay, handoff, rescue and the terminal. |
| Agent signals | Capability negotiation and explicit unavailable states in the reviewed clients. | Show unknown verification before any tool event arrives, report partial legacy signal windows, and keep Cursor shell outcomes unknown. An unknown check cannot become a passing test or resolve a loop. |
| Advisor scheduling | Herdr MCP separates admission capacity; subscriptions in Mastra own their retry and cancellation state. | Allow four sessions to receive advice concurrently, serialize work within each session, coalesce queued refreshes, publish queue records and signals atomically, and cancel workers on daemon shutdown. |
| Host integration | Pi's explicit host extension boundaries. | Send the actual terminal agent identity to the advisor. Previously that path labeled every host `codex`, even for Claude or Cursor. Include signal and journal limits in the advisor's input. |
| Evaluators | The proposed MCP conformance suites pair a conformant fixture with one broken behavior. | Challenge setup, harness audit, waste detection, loop detection and retrieval scoring with deliberately broken implementations. An absent audit result must fail rather than disappear from evaluation. |
| Retrieval scoring | Failure-oriented evaluator review. | Count each relevant result once, reject duplicates, retain unseeded results as irrelevant, detect cross-project results and fail an explicitly requested live evaluation that cannot run. |
| Task planning | Aider's architect approval and Ralph's explicit work items. | The installed Cully skill guides task interpretation, workflow selection, planning, approval, implementation, advisor consultation and verification. Advisor requests receive the bounded accepted task title, never a raw prompt. This is agent guidance, not a runtime-enforced tool gate. |

The advisor pool is bounded; four stalled requests can occupy all four slots until their deadlines. Each worker has a three-minute total deadline and existing per-process limits. Immediate local warnings remain independent of model-backed advice. A known gap means counts describe observed events; receiving events never establishes that every asynchronous hook arrived.

## What the coding harnesses contribute

| Project | Source inspected | Useful idea for Cully |
| --- | --- | --- |
| [snarktank/ralph](https://github.com/snarktank/ralph) | `ralph.sh`, `prompt.md`, PRD example | Small stories with explicit acceptance criteria and bounded iterations; retain progress across fresh agent contexts. Cully already has durable memory and Team task records, so use those boundaries for continuity. |
| [sagar-aps/ralph-harness](https://github.com/sagar-aps/ralph-harness) | README and operating model | Separate building, checking, reviewing and human integration. A candidate being ready for review is different from permission to merge it. |
| [ralphctl](https://github.com/lukas-grigis/ralphctl) | Plan critic and generator/evaluator organization | Check plans structurally before execution: dependencies, repositories, acceptance criteria and executable checks. Separate refinement, planning and implementation. |
| [Aider](https://github.com/Aider-AI/aider/blob/main/aider/coders/architect_coder.py) | Architect-to-editor transition | Present an approach, then ask whether to edit before transferring it to the editor. This is a close match for the requested task-start experience. |
| [Goose](https://github.com/block/goose/blob/main/crates/goose/src/agents/state_machine/ops_tool_approval.rs) | Tool approval operation | Bind an approval to a concrete pending request and arguments. An approved task plan and an individual tool permission are separate decisions. |
| [Pi](https://github.com/earendil-works/pi) | Extension events, compaction reference, durable harness | Use explicit lifecycle events and compact continuation state. Integrate through supported host seams where available. |
| [Herdr](https://github.com/herdrdev/herdr/blob/main/src/api/event_hub.rs) | Checked event history and native session resume plans | Distinguish missing history from no activity. Preserve native agent session identity when restoring work. A restarted session is different from a process that survived disconnection. |
| [Hermes Agent](https://github.com/NousResearch/hermes-agent) | Checkpoint and resume source organization | Checkpoint lifecycle and recovery deserve a dedicated surface. Cully should link a selected checkpoint to the native session rather than duplicate the agent's transcript store. |
| [Gleipnir](https://github.com/Felag-Engineering/gleipnir) | Tool dispatch and trigger concurrency pipeline | Enforce capability boundaries in dispatch and model run states explicitly. Its full automation platform has a different scope from Cully's current companion runtime. |

These are behavioral inspirations. The changes here are Go implementations in Cully; no external implementation was copied. Gleipnir's checked license is Business Source License 1.1, so it should not be treated as permissively reusable source.

## What the Events implementations contribute

| Project | Finding |
| --- | --- |
| [Hookdeck bridge](https://github.com/hookdeck/mcp-events-bridge/blob/main/src/host/watch.ts) | Poll outside the model loop, retain a cursor, deduplicate, back off and let a supported host wake on an event. Its current watch code can reset an invalid cursor to now; Cully should expose the resulting evidence gap instead of silently treating coverage as complete. |
| [mcpkit](https://github.com/panyam/mcpkit/blob/main/experimental/ext/events/clients/go/stream.go) | Detect a silently dead stream, reconnect from the last cursor, and distinguish transient failures from termination. A Go reference for transport behavior, without requiring a switch away from Cully's official Go SDK. |
| [Mastra Events PR](https://github.com/mastra-ai/mastra/pull/27331) | A subscription separates the latest received position from the position actually delivered to its consumer. It bounds buffers, reports overflow and owns retry/cancellation. This was inspected on the unmerged PR branch. |
| [TanStack Events PR](https://github.com/TanStack/ai/pull/1816) | Separate subscription storage from the webhook receiver. Verify the raw signed body, route by subscription and bound request sizes. This was inspected on the unmerged PR branch; it is a webhook-focused phase, not complete poll/push support. |
| [Event Intelligence](https://github.com/sarooo17/event-intelligence/blob/main/test/mcp-events-gap-negative-finality-repro.test.mjs) | A delivery gap must block an absence claim such as “no failure occurred.” Keep wake delivery state durable and scope gaps to the affected source rather than suspending unrelated work. |
| [Pi MCP extensions](https://github.com/richardanaya/pi-mcp-extensions) | Keep long-running MCP tasks distinct from synchronous results; return their terminal outcome to the host. The Tasks bridge reuses the host connection, while the Events package can open its own authenticated connections. Do not assume one universal connection pattern. |
| [Herdr MCP](https://github.com/Orange-County-AI/herdr-mcp/blob/main/internal/herdr/queue.go) | Separate long waits from short calls; bound queue admission. Retry an unavailable connection only when delivery is known not to have occurred. A connection loss after a mutation leaves an uncertain result. |
| [mcp.d](https://github.com/Poita/mcp.d/blob/main/source/mcp/client/event_subscription.d) | Use one managed subscription handle for cursor, liveness and cancellation across delivery modes; bound deduplication memory. |
| [mcpx](https://github.com/yatainc/mcpx) | Keep connection lifecycle and protocol mode explicit. Discovery and a live connection are different from application readiness. |
| [Conformance server PR](https://github.com/modelcontextprotocol/conformance/pull/521) and [client PR](https://github.com/modelcontextprotocol/conformance/pull/541) | Test the test: pair healthy and deliberately broken implementations and require the verdict to flip. Missing prerequisites and unsupported behavior need explicit outcomes. These suites were inspected on their open PR branches. |
| [Triggers & Events WG](https://github.com/modelcontextprotocol/experimental-ext-triggers-events) | Track the evolving protocol separately from Cully's internal event model. Subscription semantics are still being discussed. |

## Next: an approved task plan

The installed skill now guides the task-start conversation. The structured, runtime-enforced workflow below remains proposed. It addresses the requested experience when a user gives an agent inside Cully a task:

```text
Task → inspect and draft → show plan → user approves or edits
     → execute agreed scope → verify → review result
```

The plan shown to the user should include the intended result, concrete work items, affected areas, acceptance checks and operations that need separate authorization. For example:

> I will fix the resource validator, add a regression case, run the package tests and vet, and show you the diff. Approve this plan or tell me what to change.

Keep approval bound to a task ID, plan revision and project. An agent can propose a plan; a model tool call must not manufacture a human approval. After approval, routine steps inside that scope should continue without repeated questions. A material scope change should show the changed plan; external publication or additional permissions remain separate from the plan decision. Existing authorization should carry forward when the user already approved the same work.

For Team, build this on the existing task criteria, version checks, membership, claims and independent review. For Solo, keep explicitly selected task state in the user's Cully configuration and durable authored summaries in authenticated memory. Ordinary setup must continue to avoid creating project `.cully` folders or instruction files.

Start with host-supported planning and approval UI where available. Cully's current PTY wrapper cannot reliably intercept every tool call or authenticate a human decision across arbitrary executables. A skill reminder can guide behavior, but it is not an enforcement boundary. Capability reporting must distinguish guidance-only hosts from hosts where the gate is actually enforced.

## Further priorities

1. **Verification tied to a revision.** Checks from before a subsequent edit should not verify the current result. Team already stores revision-pinned reported evidence; extend equivalent provenance to local verification without retaining commands, output or diffs in the journal.
2. **Targeted recovery.** Preserve the accepted outcome, last verified revision, first unresolved failure and next step in a concise checkpoint. Offer supported native resume routes. Detached processes and automatic restarts require a separate runtime design.
3. **Event waits outside model calls.** Start with a narrow CI/task notification path, using a durable cursor and pending notification. Fetch authoritative state after gaps. Notify or offer continuation first; launching an agent requires its own authorized workflow.
4. **Advisor outcome evaluation.** Keep deterministic checks, model grounding, advice quality and actual task outcomes separate. Compare useful interventions and regressions on held-out tasks; a token metric alone should not decide whether a planning, recovery or harness change is good.
5. **Dynamic capability checks.** Detect host version and supported lifecycle features rather than treating installed configuration as a working integration. Continue to expose unknown sources instead of borrowing another agent's signals.

These changes should be evaluated independently. A better plan critic needs plan-quality cases; a scheduler needs isolation and cancellation tests; memory needs retrieval cases; an advisor needs evidence and task-outcome evaluation.
