---
title: Use Cully memory
description: Private work notes and session links for Solo and individual contributors.
---

# Use Cully memory

Solo keeps records for one local owner. IC (individual contributor) lets people within a company or team use the same Cully, PostgreSQL, Mem0 and embedding services, with private records for each signed-in contributor. Shared infrastructure does not share notes. Team project management and sharing are deferred.

Start with `cully setup --all` for a local owner, or `cully setup --mcp-url URL` for an existing deployment. Setup detects an OAuth challenge and directs you to sign in; contributors do not need to select IC. The server infers IC from OAuth and assigns each request's owner from verified sign-in. Solo and IC describe how the deployment is used; there is no mode flag. To enable sign-in on a new shared deployment, use `cully setup --all --oauth`; see [authentication](/oauth) for provider configuration.

## Save useful facts

`cully_log` requires only `summary` and `assistant`. Optional content includes `approach`, `outcome`, `issue`, `learning` and `next_steps`. Save the task, what was tried, the result, any blocker and a useful next action. Text fields allow up to 8,000 characters. Keep credentials and transcripts out of notes.

Use the exact `session_ref` and `agent_session_id` supplied by the installed hook when available. The opaque reference groups related notes; the native ID lets you reopen the agent conversation, for example with `codex resume ID`. Both remain private. `cully_session` accepts these IDs, an optional branch and a short task name. Repeating the task name keeps or relinks one task record; omitting it preserves the current link and `clear_task` unlinks it without deleting the record.

There are no project, repository URL, section, category, theme or tag fields in private memory. Cully does not guess them from `.git`, a submodule, a folder or a company name. Search by the task's words, session reference or time instead. Measured tool activity and counters belong to the local harness journal; a saved summary is not a measurement.

Ownership is assigned by the server: a fixed configured owner in Solo, or the verified OAuth subject in IC. Public tool inputs cannot supply an owner. Every source read/write, session and projection job is owner-scoped. Mem0 uses a hashed owner namespace, and candidate results are checked against live records belonging to that owner before being returned.

## Find and change notes

| Tool | Purpose |
| --- | --- |
| `cully_log` | Save a structured private note. |
| `cully_context` | Get three concise previews by default, at most five. |
| `cully_search` | Search PostgreSQL source text. |
| `cully_recall` | Recall semantic candidates through Mem0, verified against source records. |
| `cully_recent` | List recent owned records. |
| `cully_get` | Read an owned note by ID. |
| `cully_update` | Change supplied content fields; empty optional text clears a field. |
| `cully_delete` | Delete an owned note and queue removal of its semantic projection. |
| `cully_session` | Link a session to a task or explicitly clear that link. |
| `cully_session_get` | Read an owned session and its current task. |

Start substantive work with `cully_context` and a focused query. Use semantic mode if text lookup misses, then fetch only needed details with `cully_get`. Save one concise note before finishing. If MCP is unavailable, continue the task and report that the note was not saved.

Search and recall require query text and accept an optional `session_ref`, entry type and `since`. Recent records also accept `until`; time windows are inclusive at `since` and exclusive at `until`. Limits are capped at 50. `cully_context` accepts text, semantic or recent mode, with time windows in recent mode. Times use RFC3339; omitted `occurred_at` uses the current time. Stored instants use PostgreSQL `timestamptz`, returned in Asia/Kolkata (`+05:30`).

Entry types are `work`, `issue`, `learning` and `decision`; `work` is the default. `task` records are created only by `cully_session`. Missing or differently owned IDs return no entry, and deletion reports whether an owned record was removed. Read tools require `tools:read`; mutations require `tools:write` in IC.

## Upgrade the private schema

Schema version 8 removes the obsolete classification columns and `cully_projects` tool, while preserving note IDs, authenticated owners, content, timestamps and session/task links. It adds a native agent session ID to session records. Back up the database, stop old data-service writers, run the new `cully-data migrate`, then start matching data/MCP versions and refresh agent integrations with `cully setup` using your existing options. Old clients that send removed fields must be refreshed. Migration is explicit; normal startup refuses an outdated database.

The migration queues a rebuild of each owner's Mem0 metadata. PostgreSQL text lookup remains available while the worker catches up; semantic recall may lag. Mem0 owns embeddings, and no extraction model is used (`infer=false`). The advisor only recalls notes; the foreground agent owns durable writes. See [Mem0](/mem0) and [privacy](/privacy).
