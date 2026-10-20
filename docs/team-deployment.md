---
title: Deploy Cully for a team
description: Run one Cully stack for multiple people with private, per-user memory.
---

# Deploy Cully for a team

This guide is for the company ops team running Cully for several people. For your own laptop, use the [quickstart](/quickstart). Everyone connects to the same public MCP endpoint and signs in through the company's identity provider. Memory remains private to each person: `personal` and `company` organize records and do not share them. Separate [workspace operations](/team-workflows) provide explicitly authorized project tasks, checkpoints and published lessons.

The memory services and the advisor are installed in different places:

| Location | Components |
| --- | --- |
| Each user's machine | Cully CLI, local advisor daemon, coding agent, Cully skill and session hooks. No local Mem0 or Docker stack is needed. |
| Remote deployment | Public HTTPS endpoint for Cully MCP; private Cully data API, Mem0, source-note PostgreSQL and Mem0's pgvector database. These can run on different hosts. |

The coding agent calls the remote MCP endpoint for shared memory. Its local hooks send session signals to the local advisor. Users' machines reach the public MCP and sign-in endpoints; the data API, Mem0 and databases communicate through private network connections.

```mermaid
flowchart LR
  subgraph Laptop["Each user's machine"]
    Agent[Coding agent] -->|Session signals| Advisor[Local Cully advisor]
    Skill[Cully skill and hooks] --> Agent
  end
  subgraph Remote[Remote deployment]
    MCP[Cully MCP] -->|Private service token| Data[Cully data API]
    Data --> PG[(PostgreSQL source notes)]
    Data --> Mem0[Self-hosted Mem0]
    Mem0 --> Vector[(pgvector database)]
  end
  Agent -->|OAuth sign-in| Auth[MCP Auth or compatible server]
  Auth -->|OIDC or OAuth 2.0| IdP[Company identity provider]
  Agent -->|MCP tools and bearer token| MCP[Cully MCP]
```

## Choose how to run the stack

The [Docker Compose setup](/oauth#self-hosted-docker-with-mcp-auth) starts Cully, PostgreSQL, Mem0, MCP Auth and Caddy on one machine. It is the shortest deployment path.

Cully releases provide the CLI and installer for users, plus matching `ghcr.io/tavric-labs/cully-mcp`, `ghcr.io/tavric-labs/cully-data` and `ghcr.io/tavric-labs/cully-mem0` images for operators. Run the images with Compose, Kubernetes or another suitable platform, together or on separate hosts. The [Compose file](https://github.com/tavric-labs/cully/blob/main/deploy/self-hosted/compose.yaml) is a reference, not a required layout. Run the data image's `migrate` command before serving traffic. Give Mem0 its pgvector-enabled PostgreSQL database and persistent history volume. Keep both databases, Mem0 REST and the data API on private networks; expose only MCP through HTTPS.

### Connect the components

| Connection | Configure |
| --- | --- |
| Agent → Cully MCP | Give each user the public HTTPS MCP URL with `--mcp-url`. For a team endpoint, set `CULLY_MCP_AUTH_MODE=oauth` and configure the exact public URL as `CULLY_AUTH_RESOURCE`, plus `CULLY_AUTH_ISSUER` and `CULLY_JWKS_URL`. |
| Cully MCP → data API | Set `CULLY_DATA_API_URL` to the private data API base URL. Put the same `CULLY_DATA_API_TOKEN` on both services. |
| Data API → source PostgreSQL | Set `CULLY_DATABASE_URL` to the private database connection URL and run the data image's `migrate` command. |
| Data API → Mem0 | Set `CULLY_MEM0_URL` to the private Mem0 REST base URL. Set `CULLY_MEM0_API_KEY` to the same value as Mem0's `ADMIN_API_KEY`. |
| Mem0 → pgvector PostgreSQL | Set Mem0's `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER` and `POSTGRES_PASSWORD`. Give Mem0 a `JWT_SECRET`; persist its database and history volume. |

These connections are the deployment requirement; choose the hosts, platform and private network that fit your environment. The [configuration reference](/configuration) has the full variable list and credential boundaries. Agents receive only the public MCP URL and their own OAuth sign-in; service secrets stay with the deployment.

### Quick path with Docker Compose

After installing the Cully CLI, open a new terminal and run `cully setup --all --prepare` to create editable files. Set the public MCP and authorization hostnames and the upstream identity-provider client secret in `~/.cully/self-hosted/config/.env`. Add the provider's `connectors.json` and a persistent signing key as shown in the [OAuth setup](/oauth#self-hosted-docker-with-mcp-auth). Then start the stack:

```sh
cully setup --all --oauth
```

This starts PostgreSQL, Mem0, the data API, Cully MCP, MCP Auth and Caddy. Each person then connects their agent to the public MCP URL; the [agent guide](/agents) gives the client commands.

### Example: run Cully with MCP Runtime

The Cully maintainer runs a personal Cully MCP endpoint on [MCP Runtime](https://mcpruntime.org). Its [deployment workflow](https://github.com/tavric-labs/cully/blob/main/.github/workflows/deploy.yml) builds and publishes the MCP image, then deploys the [server manifest](https://github.com/tavric-labs/cully/blob/main/.mcp/servers.yaml). Cully's data API, PostgreSQL and Mem0 run separately. The [MCP Runtime publishing guide](https://docs.mcpruntime.org/publish-mcp-server/) shows the build, push and deploy flow.

A company can use this pattern to run Cully's MCP container as an `MCPServer` workload on its Kubernetes cluster. MCP Runtime manages the container rollout, service and workload status; Cully supplies the memory tools and owner-scoped storage. The [MCP Runtime API reference](https://github.com/tavric-labs/mcp-runtime/blob/main/docs/api.md#mcpserver-surface) lists the workload image, port and secret environment fields. The company ingress publishes Cully's OAuth and MCP routes.

1. Deploy Cully's PostgreSQL, Mem0 and private data API in the cluster. Keep those services on private networking and run `cully-data migrate` before starting the API. Use the [Compose file](https://github.com/tavric-labs/cully/blob/main/deploy/self-hosted/compose.yaml) to map the service dependencies and volumes.
2. Publish `ghcr.io/tavric-labs/cully-mcp:<matching-release-tag>` through MCP Runtime as a workload listening on Cully's configured port. Set `CULLY_DATA_API_URL` to the private data API and set `CULLY_MCP_AUTH_MODE=oauth`, `CULLY_AUTH_ISSUER`, `CULLY_AUTH_RESOURCE` and `CULLY_JWKS_URL` as described below. Give `CULLY_DATA_API_TOKEN` through a Kubernetes Secret with the same value used by the private data API. The maintainer's manifest expects `cully-data-api-token` in `mcp-servers` with key `CULLY_DATA_API_TOKEN`; provision it before rollout and keep the value out of Git. Do not set a fixed `CULLY_MCP_OWNER` for a multi-person server.
3. Route a dedicated HTTPS host, for example `https://cully.example.com/mcp`, to the Cully service. Forward its OAuth discovery and MCP paths as well as the `Authorization` header to Cully. Connect the company's identity provider through MCP Auth or a compatible authorization server, then sign in from each person's agent.

Keep MCP Runtime's OAuth gateway **off for this Cully route** in the current integration. Its gateway [strips the client bearer token before forwarding](https://github.com/tavric-labs/mcp-runtime/blob/main/docs/api.md#security-and-auth), while Cully currently validates that token to identify each memory owner. Use Cully's OAuth mode at the service boundary until an explicit, verified identity handoff is implemented. This example uses MCP Runtime's workload management; it does not claim its grant and session policy for Cully.

After deployment, check that the Cully Deployment's updated and ready replicas match its desired replicas and that its pod runs the new image tag. The CLI can report Ready while an older pod serves traffic during a failed rollout ([Runtime issue #636](https://github.com/tavric-labs/mcp-runtime/issues/636)).

<div class="related-product">
  <p class="related-product__eyebrow">Another product from the Cully maintainer</p>
  <h3>MCP Runtime</h3>
  <p>An open source Kubernetes platform for deploying and operating MCP servers. The Cully maintainer uses it to publish Cully's MCP workload and route, while Cully keeps its owner-scoped memory and token validation.</p>
  <div class="related-product__links">
    <a href="https://mcpruntime.org">Explore MCP Runtime ↗</a>
    <a href="https://docs.mcpruntime.org/publish-mcp-server/">Read the publishing guide ↗</a>
  </div>
  <p class="related-product__contact">If your team needs a platform for MCP servers, <a href="mailto:princekrroshan01@gmail.com?subject=MCP%20Runtime%20for%20our%20team">contact the maintainer</a>.</p>
</div>

## Connect sign-in

1. Choose the public MCP URL, such as `https://mcp.example.com/mcp`, and an authorization-server URL. The exact MCP URL must be the token's resource audience.
2. Configure [MCP Auth](https://github.com/tavric-labs/mcp-auth/blob/main/docs/auth-server.md#connect-an-organizations-identity-provider) or a compatible authorization server with the company's identity provider. Grant `tools:read` and `tools:write` for the MCP resource.
3. Configure Cully MCP with `CULLY_MCP_AUTH_MODE=oauth`, the issuer, exact resource URL and JWKS URL. The [configuration reference](/configuration#manual-mcp-service-settings) lists the service variables.
4. Give each person the public MCP URL. They pick their agent and install and connect with:

   <InstallCommand template="curl -fsSL https://cully.net/install.sh | sh -s -- --agent {agent} --mcp-url https://mcp.example.com/mcp --oauth" />

   Then they sign in; for Codex, run `codex mcp login cully`. See [connect an agent](/agents) for its sign-in step.

MCP uses a private service token to call the data API. The data API holds database and Mem0 credentials. Keep those credentials in the deployment's secret store; agents need only the public MCP URL and their own OAuth sign-in. The [architecture](/architecture) shows the service flow.

## Enable the team workspace

Apply migration 006 before starting the updated data service. Gate `cully_workspace_read` with `tools:read` and `cully_workspace_write` with `tools:write`; both require OAuth even though they are discoverable in no-OAuth mode. Existing memory owners and notes are unchanged.

Each person calls `whoami` to obtain their workspace principal, bound to the verified issuer and subject. The team creator becomes its first admin. Admins explicitly add team members and projects; project maintainers or team admins grant project roles. Team membership alone does not grant project reads. See [team workflows](/team-workflows).

Changes and audit events commit together in PostgreSQL. Limits per team are 100 members, 50 projects, 500 tasks, 500 lessons, 200 playbooks and a 1 MiB serialized aggregate. Per-team writes serialize under a row lock. Scale and normalize storage before raising these limits. Audit events are operator-accessible database records; there is no user-facing audit export or web board yet.
