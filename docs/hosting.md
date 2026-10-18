---
title: Host Cully on your laptop
description: Start Cully's private memory service with Docker Compose and connect your coding agent.
---

# Host Cully on your laptop

Cully's memory service runs on your laptop for the default single-user setup. Your connected agents use it to save and find notes across sessions. You do not need OAuth, a domain name or an identity provider for this setup.

## One-command full stack

1. Install and start Docker with the Compose plugin. Install the [Cully CLI](/installation) if you have not already.
2. Run setup for your agent:

   <InstallCommand kind="setup" />

   Run `cully setup --all` without `--agent` to detect and connect installed coding agents, or use `--agent all` to configure all three supported agents.
3. Wait for `Setup complete` to appear, then restart your agent. Setup registers the MCP connection, configures the Cully skill, hooks and agent controls, and starts the advisor daemon if it is not already running. If any component fails, setup exits with an error and does not report completion. Resolve the reported error and rerun the same command; existing credentials and running services are reused.
4. Work on a substantive task. Your connected agent is prompted to find relevant notes and save a concise work summary for later sessions. The [memory guide](/memory) explains what gets saved.

The command downloads the matching Cully release's Docker files, prepares the local configuration, starts PostgreSQL, Mem0, the private data API and the MCP server, and creates the database schema. No repository checkout is needed. It generates service credentials and a stable single-user owner in `~/.cully/config.json`; keep that file private and back it up with your Docker volumes. Editable stack settings live in `~/.cully/self-hosted/config/.env`. You do not need to edit either file for the default laptop setup. `cully setup --all --prepare` only downloads the stack and creates editable configuration without starting services; use it when following the [team deployment guide](/team-deployment) to set up OAuth and public hostnames before the first start.

### What starts on your laptop

| Service | Job | Reachability |
| --- | --- | --- |
| Advisor daemon | Keeps session signals current and processes advice. | Local background process. |
| Cully MCP | Gives connected agents the memory tools. | Published on loopback by default. |
| Cully data API | Validates and stores memory requests from MCP. | Private Compose network. |
| PostgreSQL | Holds authoritative notes and indexing jobs. | Private Compose network. |
| Mem0 and its pgvector database | Indexes notes for semantic recall. | Private Compose network. |

The downloaded [Compose file](https://github.com/tavric-labs/cully/blob/main/deploy/self-hosted/compose.yaml) also defines optional Caddy and MCP Auth services for the [team sign-in path](/oauth). Setup runs the data service's schema migration before the API serves memory requests.

### Keep the generated state together

On first setup, Cully generates PostgreSQL passwords, the MCP-to-data token, the data-to-Mem0 key, a Mem0 signing secret and the stable single-user owner. Later runs reuse them rather than changing a password behind an existing database volume. Back up `~/.cully/config.json` with the Docker volumes; changing one without the other can break access to existing notes. Setup passes credentials to Compose without printing them. The database and Mem0 credentials stay with the private services, not in agent configuration.

The default MCP URL is printed by setup and binds to `127.0.0.1`. Only programs on that machine can reach it through that address. Anyone who can reach a no-OAuth endpoint can use its memory tools, so keep this mode on loopback or a trusted private network.

## Uninstall the laptop stack

Run `cully uninstall` from the project where you connected your agent. It stops
the local Docker Compose services, stops the advisor, and removes Cully-managed
agent hooks, skills and matching local MCP connections. It removes the CLI
binary when run from a standard installer location. Your PostgreSQL and Mem0
volumes, private credentials and downloaded stack remain so you can set Cully
up again without losing notes.

Run `cully uninstall --purge-data` only when you also want to delete the local
memory volumes and self-hosted configuration. This permanently removes locally
stored notes. Both commands leave unrelated agent settings and other Docker
projects alone. If you installed a custom CLI binary or added a PATH line
manually, remove those yourself. `cully uninstall codex` removes only Codex's
managed integration and leaves the local services running; use `claude`,
`cursor` or `all` for other integration-only removals.

## Connect another agent on the same machine

Use the MCP URL printed by setup:

```sh
cully setup --agent claude --mcp-url http://127.0.0.1:8080/mcp
```

Replace the example URL with the one printed on your machine. You can use `codex` or `cursor` instead of `claude`. Restart that agent after setup. See [connect an agent](/agents) for each client's commands.

## When you need sign-in

OAuth is optional for a company team deployment. If your ops team exposes Cully over HTTPS to multiple people, each person can sign in and access their own records. That setup needs an authorization server, identity provider, public hostnames and TLS. Follow the [team deployment guide](/team-deployment) and [OAuth setup](/oauth). A `personal` or `company` section in memory does not share records between people; ownership still follows the signed-in user.

## Run the services another way

The [Compose file](https://github.com/tavric-labs/cully/blob/main/deploy/self-hosted/compose.yaml) is the reference for the containers and private networks. PostgreSQL holds source records. Mem0 uses a separate pgvector database for semantic recall; its local embedding model does not need an embedding API key. The data API, Mem0 and both databases stay private. The [team deployment guide](/team-deployment) covers other container platforms, and the [configuration reference](/configuration) lists settings for a manual deployment.

Setup requires an explicit mode: `--all` starts the stack on this machine; `--mcp-url URL` connects to a deployed stack. With neither option, setup prints guidance and makes no setup changes. The full local setup reports each stage and streams Docker build progress. First builds can take several minutes. Setup checks MCP initialization and Cully tool discovery; OAuth endpoints report authenticated verification as pending until you sign in through your agent.
