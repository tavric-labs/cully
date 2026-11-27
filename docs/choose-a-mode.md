---
title: One machine or many?
description: Choose local mode for a single machine or MCP mode for many machines and VMs.
---

# One machine or many?

Cully keeps your notes and recall in one of two ways. Pick by how many machines you work on.

::: tip New in the next release
The browser Setup page and `cully setup` wizard described in [Set up Cully](/setup) ask which mode you want. The two modes themselves are described in the [memory modes design](https://github.com/tavric-labs/cully/blob/main/docs/superpowers/specs/2026-11-26-memory-modes-design.md); see the status notes below for what is built.
:::

## Local mode and MCP mode side by side

| | Local mode | MCP mode |
| --- | --- | --- |
| Where memory lives | On this machine, under `~/.cully/` (the browser UI keeps its data in one SQLite file, `cully.db`) | In PostgreSQL behind the Cully MCP, shared by every machine you sign in from |
| Recall quality | Recency and keyword (SQLite FTS5, no model call). Agents may read more context and use more tokens | Semantic recall through Mem0, checked against your live records |
| What you deploy | Nothing. No Docker, Postgres, Mem0 or MCP server | The MCP service, the Cully service, PostgreSQL and Mem0 |
| Who it is for | One person on a single machine | One person on many VMs, shared machines and a laptop, or a team where each person keeps private notes |
| Effort | Lowest: install and set up | Higher: deploy and operate the services once |
| Works offline | Yes | Needs a reachable, signed-in MCP |

## Which should I use?

**You work on one machine.** Local mode is enough. Your sessions, notes and handoffs stay on that machine, and nothing needs deploying.

**You work on many machines.** If you move between several VMs, shared machines and a local laptop, use MCP mode. It is the preferred way if you can deploy it, because it is more efficient:

- One shared memory and recall across every machine. A decision saved on a VM is found from your laptop.
- Semantic recall through Mem0, so an agent retrieves only what is relevant instead of re-reading recent notes.
- One place for sessions and handoffs, so you can continue work started elsewhere.

## What MCP mode needs

Deploy four things:

1. the Cully MCP service, which gives agents the memory tools;
2. the Cully service (the data API) that validates and stores requests;
3. PostgreSQL, which holds the authoritative notes;
4. Mem0, which indexes notes for semantic recall.

[Self-hosting](/hosting) starts all four with Docker Compose on one machine, and the [contributor deployment guide](/team-deployment) shows how to run them for shared use with [OAuth sign-in](/oauth). [Mem0 recall](/mem0) explains what the index does. Then run `cully setup --mcp-url URL` on every other machine to connect it to the deployment.

## Switching later

- **Local to MCP.** `cully mcp add --url URL --oauth` registers an MCP, and after you sign in MCP mode is available.
- **Keeping your local notes.** The design is that a reconciliation job pushes local notes to the MCP with an idempotency key, so a retry or a second machine never duplicates a note, and asks before deleting any local copy. This is designed but not built yet: the design document states that nothing in it is built. Do not rely on automatic note migration until it ships.
- **MCP to local.** Syncing in that direction is a stated non-goal.

## What stays the same

Both modes use the same CLI, the same agents (Claude Code, Codex and Cursor), the same skill and hooks, the same advisor and the same [browser UI](/ui). Neither mode writes into your project folders. Memory stays private to you; shared infrastructure does not share notes. See [privacy](/privacy).

Next: [Set up Cully](/setup).
