---
title: Configuration reference
description: Find settings for the local advisor, Docker setup or a manual Cully deployment.
---

# Configuration reference

Most people do not need to edit configuration files. The [quickstart](/quickstart) installs Cully and starts its memory stack with generated service credentials. Use this page when you need to change a setting or run the services yourself.

## Local advisor

Run `cully status` to check the integration. The [advisor controls](/advisor#controls) cover display and analysis options such as `CULLY_DISPLAY` and `CULLY_ANALYZE_DISABLE`. Cully honors `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `CURSOR_CONFIG_DIR` when your agent uses a custom configuration directory. Agent setup preserves unrelated user-owned settings.

## Docker setup files

| File | When you need it |
| --- | --- |
| `~/.cully/self-hosted/config/.env` | Editable deployment options. The default laptop setup needs no edits. Add public hostnames and a client secret here for [OAuth setup](/oauth). |
| `~/.cully/config.json` | Generated database passwords, service tokens and single-user owner. Keep it private and back it up with Docker volumes. |
| `~/.cully/self-hosted/config/connectors.json` | Identity-provider connector settings for MCP Auth. |
| `~/.cully/self-hosted/config/.secrets/signing-key.pem` | MCP Auth signing key for OAuth. |

Run `cully setup --all --prepare` to create editable stack files before starting containers. Run `cully setup --agent AGENT --all` for the default private stack, or follow the [OAuth guide](/oauth) before adding `--oauth`.

## Manual MCP service settings

These variables are for deploying `cully-mcp` and `cully-data` yourself. Docker setup supplies them automatically.

| Variable | Service | Purpose |
| --- | --- | --- |
| `CULLY_DATA_API_URL` | MCP | Private data API base URL; required. |
| `CULLY_DATA_API_TOKEN` | MCP and data API | Same private bearer token on both services; required for serving. |
| `CULLY_DATABASE_URL` | Data API | PostgreSQL connection URL; required. |
| `CULLY_MEM0_URL` | Data API | Self-hosted Mem0 base URL for semantic recall. The standard stack sets this. |
| `CULLY_MEM0_API_KEY` | Data API | Mem0 API key. Set it with `CULLY_MEM0_URL` for semantic recall. |
| `CULLY_DB_MAX_CONNS` | Data API | Database pool limit; default 8, allowed 2–100. |
| `CULLY_HOST`, `CULLY_PORT` | Both | Bind address and port. MCP defaults to `127.0.0.1:8080` without OAuth and `0.0.0.0:8080` with OAuth; data API defaults to `0.0.0.0:8083` on its private network. |
| `CULLY_MCP_AUTH_MODE` | MCP | `none` by default; set `oauth` for per-user sign-in. |
| `CULLY_MCP_OWNER` | MCP | Required stable owner in no-OAuth mode; unset it for OAuth. |
| `CULLY_AUTH_ISSUER` | MCP with OAuth | Exact token issuer. |
| `CULLY_AUTH_RESOURCE` | MCP with OAuth | Exact public MCP URL and token audience. |
| `CULLY_JWKS_URL` | MCP with OAuth | Signing-key endpoint; configure explicitly because issuer paths vary. |

Keep the data API, Mem0 and databases on private networks. The MCP service receives the data API token; it does not need database credentials or the Mem0 key.

### Credential boundaries

| Connection | Credential | Keep it in |
| --- | --- | --- |
| Agent to Cully MCP | OAuth bearer token for team sign-in; no token on the default private endpoint. | Agent and MCP service. |
| Cully MCP to data API | `CULLY_DATA_API_TOKEN`, independent of OAuth. | MCP and data service secrets. |
| Data API to Mem0 | `CULLY_MEM0_API_KEY`. | Data service and Mem0 secrets. |
| Data API to PostgreSQL | Password in `CULLY_DATABASE_URL`. | Data service and PostgreSQL secrets. |

The MCP service derives the record owner from `CULLY_MCP_OWNER` or the verified OAuth subject. The data API accepts that owner only from a request authenticated with the private service token. Protect the token as access to all owners' records. When MCP and the data API run on different hosts, provision the same token on both services; do not pass PostgreSQL or Mem0 credentials to the MCP workload.

### MCP access mode

For one person on a private endpoint, set a stable `CULLY_MCP_OWNER` (1–512 bytes, no surrounding whitespace or control characters). Every request uses that owner. `CULLY_MCP_AUTH_MODE=none` is the default, and the MCP listener defaults to `127.0.0.1` in this mode. Keep it on loopback or a trusted private network.

For per-user sign-in, set `CULLY_MCP_AUTH_MODE=oauth` or start `cully-mcp --oauth`. Unset `CULLY_MCP_OWNER`, and set `CULLY_AUTH_ISSUER`, `CULLY_AUTH_RESOURCE` (the exact public MCP URL) and `CULLY_JWKS_URL`. MCP defaults to binding `0.0.0.0` in this mode; put HTTPS in front of it. The provided [OAuth setup](/oauth) derives these values from hostnames.

On MCP Runtime, `MCP_AUTH_ISSUER` and `MCP_AUTH_RESOURCE` take precedence over the matching `CULLY_AUTH_*` values. `MCP_PATH` changes the internal MCP transport path; it defaults to `/mcp`. These values do not enable OAuth by themselves.

The Docker setup derives the public resource URL from `CULLY_MCP_HOST`; with MCP Auth it derives the issuer and JWKS URL from `CULLY_AUTH_HOST`. Set `CULLY_JWKS_URL` explicitly when an external authorization server uses a different endpoint. The public resource can have a route prefix even when the internal `MCP_PATH` is `/mcp`. See [OAuth setup](/oauth) and the [MCP Runtime example](/team-deployment#example-run-cully-with-mcp-runtime).

## Agent installation scope

Agent integrations are user-level: `~/.claude`, `~/.codex` and `~/.cursor`, honoring their configuration directory overrides. Setup works from any directory and does not create a repository `.cully` folder. See [installation paths and legacy migration](/installation#one-setup-across-projects). Cully deployment configuration under `~/.cully` belongs to your user account; project identity for sessions and memory comes from the active repository.
