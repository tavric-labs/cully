---
title: Mem0 and semantic recall
description: Understand how Cully indexes notes with self-hosted Mem0.
---

# Mem0 and semantic recall

Mem0 is part of Cully's standard memory stack. `cully setup --agent AGENT --all` starts Mem0 alongside Cully, PostgreSQL and the private data API. You do not need a Mem0 account or embedding API key.

PostgreSQL stores the source note. Cully sends an owner-scoped projection of its authored summary and populated approach, outcome, issue, learning and next-step fields to Mem0, which indexes them by meaning. When an agent uses `cully_recall`, Mem0 finds candidates and Cully checks the live source records before returning them. `cully_search` and `cully_recent` read PostgreSQL directly.

## What to expect

A new or changed note can take a little time to appear in semantic recall because indexing runs in the background. Text search remains available during a Mem0 outage, and the worker retries indexing. Deleted notes are filtered from recall immediately, even if projection cleanup is still pending.

The standard stack uses self-hosted Mem0 with a separate pgvector/PostgreSQL database and persistent history volume. Its FastEmbed model runs locally on the host CPU. Cully sends authored summaries with `infer=false`; it does not ask Mem0 to extract new facts from transcripts.

Each note gets a projection scoped to its owner. Cully derives the Mem0 user namespace from the Cully owner and uses the source note ID as the projection's run ID. A recall result is checked against the live PostgreSQL note, its owner and its update time before Cully returns it. An agent never receives the Mem0 service key or raw candidate results. The worker retries failed projections with backoff and reconciles duplicates; indexing is asynchronous, so a just-saved note may appear in text search before semantic recall.

## Manual deployment

The [Compose file](https://github.com/tavric-labs/cully/blob/main/deploy/self-hosted/compose.yaml) shows the Mem0 service, database, volumes and private network. If you deploy Cully's data API yourself, point `CULLY_MEM0_URL` at the Mem0 REST base URL and give the data API `CULLY_MEM0_API_KEY`. Mem0's `ADMIN_API_KEY` must match. Do not put these values in an agent's configuration or expose Mem0 REST publicly.

The supplied [image build](https://github.com/tavric-labs/cully/blob/main/Dockerfile.mem0) pins an upstream Mem0 REST commit and uses the local `BAAI/bge-small-en-v1.5` FastEmbed model with 384-dimensional vectors. The model is cached in the image, so the standard stack needs no external embedding provider. Cully's Go services do not need a Python runtime; Mem0 runs Python inside its own container. A separate Mem0 server must provide the REST endpoints Cully calls: `GET /memories`, `POST /memories`, `POST /search` and `DELETE /memories/{id}`, authenticated with `X-API-Key`. Use its base URL, without a trailing `/search` path.

If you add Mem0 to a database with existing notes, queue them for indexing with the data API's `reindex` command, for example:

```sh
docker compose run --rm data-api reindex
```

Use the Compose file for your deployment. The worker processes one source projection at a time and retries a failed request; it does not promise exactly-once delivery to Mem0. Text search and logging continue during an indexing outage. See the [configuration reference](/configuration) and [architecture](/architecture) for service details.
