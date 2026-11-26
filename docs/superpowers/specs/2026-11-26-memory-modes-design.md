# Cully memory modes: MCP, local, and reconciliation

Status: draft for review. Nothing in this document is built yet.

## Intent

Cully keeps what an agent did and learned so the next session can continue. Today that needs a signed-in MCP server (Postgres plus Mem0 behind it). Two problems follow: a machine with no working or signed-in MCP records nothing durable, and every save and recall costs a tool call. This design adds a second, local way to keep notes and a job that moves them to the MCP when it becomes available.

## The two modes

| | MCP mode | Local mode |
|---|---|---|
| Storage | Postgres and Mem0 behind the MCP | Plain markdown files under `~/.cully/` in the user's home, never in a project directory |
| Recall | Semantic: the agent retrieves only what is relevant | Recency and keyword: the last few notes for the project |
| Cost to save | An MCP tool call | A `cully note` command, no network and no sign-in |
| Reach | Across devices and a team | This machine |
| Needs | A reachable, signed-in MCP | Nothing |

MCP mode is the better recall at scale. Local mode is the safety net and the offline option. It is not a degraded copy: it is the same save and recall operations with a different backend.

## Decisions (from the discussion)

1. **One interface, switchable backend.** Save and recall go through one repository interface. Postgres and HTTP adapters already implement it; local files become a third adapter. Agents, hooks and the skill do not know which backend answers.
2. **`cully setup --local`.** An explicit third setup path. It skips the Docker stack (Postgres, Mem0, MCP server) and does not register the Cully MCP in the agent's configuration. It still installs the skill, the hooks (writing through `cully note`), the terminal setup and the advisor. Without `--all`, `--mcp-url` or `--local`, setup keeps refusing, so nobody ends up in local mode by accident.
3. **Switching later.** `cully mcp add --url URL --oauth` already registers an MCP. After the user signs in, MCP mode becomes available and the reconciliation job runs.
4. **Fallback, not only a mode.** When the MCP is configured but unauthenticated or unreachable, saves go to the local outbox instead of being lost.

## Reconciliation job

When a probe reports the Cully MCP connected (the same `agentMCPProbe` the harness and the browser UI use), a job pushes the local outbox to it:

1. List local notes not yet marked synced.
2. Push each note through the MCP's normal save tool using a stable idempotency key (a hash of project key, note id and content), so a retry or a second machine never creates a duplicate.
3. On success, mark the note synced locally. Failures stay queued with their reason and are retried next run.
4. When everything pushed in a run succeeded, **ask the user** before removing the local copies: show how many notes, where they are, and offer Delete or Keep. Nothing is deleted without that answer, and a declined delete is remembered so the question is not repeated each run.

Triggers: a session start with MCP connected and a non-empty outbox, the moment the browser UI sees the status change to connected, and the existing scheduled-jobs daemon as a low-frequency catch-up. The job never needs the user to be present to push; it only needs them to approve deletion.

## Privacy and safety

- Cully's journal deliberately stores no prompts or summaries. Local notes would, so local mode is opt-in, user-level only, size-capped per note and per project, and runs a secret-redaction pass (tokens, keys, URLs with credentials) before writing.
- Files are mode 0600 in a 0700 directory. No project-level `.cully` folder is created, matching existing rules.
- The reconciliation job never prints note content in logs and never deletes without confirmation.
- Local mode never touches agent MCP configuration, and `--local` does nothing to an existing configuration it did not create.

## Surfaces

- **`cully status`:** shows the active mode, outbox size and last reconcile result.
- **Browser UI:** the Cully MCP chip gains a count of unsynced notes and a **Sync now** action, then a **Delete local copies?** prompt after a clean sync. The sign-in dialog already exists.
- **Terminal harness:** the same prompt appears in the panel at the next session start.

## Non-goals

- Semantic recall in local mode.
- Syncing in the other direction (MCP to local).
- Merging notes from two machines beyond idempotent push.

## Open questions

- Retention: how long may synced-but-kept local notes live before being offered for deletion again?
- Note format: one file per task, or an append-only log per project? Per-task files make idempotent push and per-note deletion simpler.
- Should the advisor's recall step read local notes when the MCP is down, or stay silent?
