# Connect a coding agent

Install Cully for the agent you use:

```sh
curl -fsSL https://cully.net/install.sh | sh -s -- --agent codex
```

Use `claude` or `cursor` instead of `codex` as needed. This installs the local advisor and Cully skill. It does not require an MCP server.

To run your own private MCP service and connect the agent, install Docker Compose, then run:

```sh
cully setup --agent codex --all
```

This starts PostgreSQL, Mem0, the data API and Cully MCP. OAuth is off by default. The MCP endpoint binds to loopback. See [self-hosting](../docs/hosting.md).

To connect an agent to a server that is already running, use its MCP URL:

```sh
cully setup --agent codex --mcp-url https://mcp.example.com/mcp
```

Add `--oauth` if the server requires sign-in. A team can connect Cully to its identity provider through [MCP Auth](../docs/team-deployment.md). The agent receives only the MCP URL; private database and service credentials stay with the operator.

For client-specific sign-in instructions, see the [agent guide](../docs/agents.md). For the MCP resource, scopes and authorization flow, see [MCP OAuth](../docs/oauth.md).

Native Cully phone clients live under [`apps/`](../apps/README.md). They use the same canonical skill through its mobile profile and consume mcp-mobile; they do not run a coding terminal on the phone.

The VS Code extension lives in [`vscode/`](vscode/README.md); it wraps the CLI for launching agents and viewing session status, replay and handoff.
