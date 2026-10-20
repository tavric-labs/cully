---
title: Team mode
description: Project tasks, portable checkpoints, review evidence and explicit learning through CLI or MCP.
---

# Team mode: work across people and agents

**Keep your team's agent work moving, whoever picks it up next.**

Cully Team is a project management harness runtime for people and AI agents building software together. It connects project tasks, ownership, a board/inbox, portable checkpoints, reported review evidence and explicitly shared lessons. Start agents manually; task state outlives the session. [Solo mode](/solo) remains your local coding-agent harness runtime, with private journals and owner-scoped memory.

## What works today

| Capability | Current behavior |
| --- | --- |
| Run an agent | `cully run AGENT` provides the terminal and supported session signals. |
| Track a session/task link | `cully_session` stores an owner-scoped session and optional task; `cully_session_get` retrieves it. |
| Save useful work | `cully_log` records decisions, attempts, outcomes and next steps. |
| Continue with another agent | Your agents can retrieve your authorized notes through `cully_context` and `cully_get`. |
| Inspect an interrupted session | `cully replay`, `cully handoff` and `cully rescue` use the local session journal. |
| Host for multiple users | OAuth identifies each user's private records. The `company` label does not share records with coworkers. |

These solo session/task links stay separate from shared workspace tasks. Local handoff never automatically publishes journal data to teammates. See [memory](/memory), [session intelligence](/session-intelligence) and [team deployment](/team-deployment).

## The team workflow

::: info Manual launch, explicit sharing
CLI/MCP operations implement this workflow. Managed execution, automatic journal checkpoints, a browser board and tracker integration remain planned. See [product vision](/product-direction) and the [roadmap](/roadmap).
:::

1. A lead creates a project task with a clear outcome and acceptance criteria.
2. A member claims it, prepares authorized project context and starts their preferred agent.
3. The member explicitly checkpoints selected context when work stops or needs a decision.
4. Another authorized member resumes with that checkpoint in a different agent. The original human owner remains accountable until ownership is explicitly changed.
5. A reviewer sees the result, artifact revision, check evidence and missing criteria before accepting delivery.
6. The author chooses whether to share a reusable lesson. A maintainer can adopt it into a project playbook for later tasks.

For example, one teammate investigates an authentication failure, another implements the fix, and a maintainer accepts the reported evidence at its exact revision. A later task retrieves an explicitly published lesson without exposing private memory or transcripts.

## Connect and create a project

Apply migration 006 and configure OAuth as described in [team deployment](/team-deployment). Agents use their configured sign-in with `tools:read`/`tools:write`; no-OAuth mode cannot identify workspace members.

1. Each person calls `cully_workspace_read` with `{"action":"whoami"}`. Share the returned issuer/subject-scoped principal with the admin, never an access token.
2. The lead calls `cully_workspace_write` with `{"action":"team_create","name":"Engineering"}`. Keep the returned `team_id`; the creator becomes its first admin.
3. Admins add team members with `team_member`, `team_id`, `principal` and `role: "member"` or `"admin"`.
4. Admins create projects with `project_create`, `team_id`, `name` and HTTPS `repository`. Its UUID is the identity; its creator becomes a maintainer.
5. A team admin or project maintainer grants project access with `project_member`, `team_id`, `project_id`, `principal` and `role`: `maintainer`, `member`, `viewer` or `remove`.

Viewers read; members create/work on tasks; maintainers approve and adopt playbooks. Team admins manage membership, but need an explicit project role for content reads. `team_member` with `role: "remove"` removes project grants too, and is rejected when that person is the sole maintainer of any project. Subsequent server access is denied; previously exported information cannot be recalled.

## Workspace actions

Every project action supplies `team_id` and `project_id`. Task mutations supply `task_id` and the current task `version`. Refresh with `task_get` after a conflict; do not blindly retry stale writes.

| Write action | Additional input | Result |
| --- | --- | --- |
| `task_create` | `name`, nonempty `criteria`, optional existing-project task `dependencies` | Ready task at version 1 |
| `task_claim` | Current `version`, `agent`, optional opaque `session_ref` | Human ownership, attempt and 30-minute lease |
| `task_checkpoint` | Current `version`, `checkpoint`, `next_step`, optional `branch`, `session_ref` | Selected portable context and refreshed lease |
| `task_state` | Current `version`, `state` (`active`, `blocked`, `cancelled`); `next_step` for blockers | Explicit state change; ready tasks may be cancelled by any project writer; the owner may cancel their own open task even after lease expiry |
| `task_release` | Current `version`; checkpoint/next step already present for owner release | Owner returns work to ready under a live lease; a maintainer may force-release someone else's review, or their active/blocked claim after lease expiry |
| `task_submit` | Current `version`, `revision`, HTTPS `artifact`, available `evidence` | Proposed result in review, including gaps or failures |
| `task_approve` | Current `version`, exact submitted `revision` | Noncontributing maintainer accepts delivery |

Read actions are `projects` (team only), `board`, `inbox`, `task_get` and `lessons`. The inbox lists blockers, reviews and stale active work. `task_get` is the handoff/review packet: selected checkpoint, next action, branch, artifact and evidence. It uploads no conversation, source files or uncommitted work. Reconstruct from repository/branch references.

Concurrent claims have one winner. Dependencies refer to existing non-cancelled tasks and cannot be edited, preventing cycles; claiming requires all predecessors done. Cancelling a task that still has open dependents is rejected. Ready means unclaimed, so check dependency readiness. Lease expiry marks active, blocked and review work stale, without declaring failure or stopping an agent. Reclaim explicitly after checking the previous person is no longer working; their old attempt loses mutation access. An expired review task can only be reclaimed by its owner, or force-released by a maintainer. Removing a person from the team or project, or demoting them to viewer, returns their owned open tasks to ready (checkpoint kept, evidence cleared) so the board does not strand handoffs. Deleting a lesson removes dependent playbooks. Done/cancelled tasks are immutable in this version.

## Review evidence

Each evidence item has a zero-based `criterion`, named `check`, `status` (`pass`/`fail`), exact `revision` and past RFC3339 `observed_at`. Submission can contain missing or failed checks so the reviewer can inspect the gaps; approval requires every criterion to have passing evidence at the submitted revision. All evidence is labeled `reported`; clients cannot self-attest independent verification. No CI attestation integration exists yet.

A maintainer who contributed to any attempt cannot approve the task. Approval checks task version, artifact revision and project guidance version. Guidance changes require resubmission. A checkpoint from review returns work to active and clears evidence; state changes also clear it. Current policy is noncontributing-maintainer acceptance, with configurable merge/tracker policies planned.

## Draft, publish and reuse

`learning_draft` takes a completed project `task_id`, `lesson`, `applies_when` and `limitations`. The draft belongs to its author and records source task version/artifact revision. `learning_publish` explicitly shares it with the project using `learning_id` and current learning `version`.

`lessons` returns up to three lessons and three active playbooks. Published lessons fill the lesson slots first; authors may see their own drafts in any remaining slots. Optional `query` matches text case-insensitively; shared lessons do not enter Mem0. Source-task changes hide stale published tips from other members.

Authors use `learning_edit` with replacement lesson/applicability/limitations, `learning_unshare` or `learning_delete`, with the current learning version. Editing refreshes the task reference and makes the lesson private again. Dependent playbooks stop appearing. Maintainers use `playbook_adopt` with published learning ID/version and nonempty `steps`; the source task must be completed and current. Adoption creates versioned guidance without editing instruction files.

Team-wide visibility, automated drafting/ranking, playbook editing/archive controls and semantic shared retrieval remain planned. See [product vision](/product-direction#later).

## Use the CLI

Normally use your agent's configured MCP tools. For a CLI session, place one action in a local JSON file and supply a short-lived user OAuth token securely through `CULLY_WORKSPACE_TOKEN`:

```sh
cully workspace --mcp-url https://mcp.example.com/mcp --input action.json
```

The CLI selects the read/write tool and prints JSON. It never persists tokens or reads agent credential stores. `--input -` reads stdin; `CULLY_MCP_URL` can supply the endpoint. HTTPS is required except on loopback. Keep tokens out of action files, command lines and Git. See [deployment limits](/team-deployment#enable-the-team-workspace).

## Where to start

Use [quickstart](/quickstart) for a private installation, or [team deployment](/team-deployment) for shared hosting with separate user identities. The [roadmap](/roadmap) and [product vision](/product-direction) separate current work from proposed stages.
