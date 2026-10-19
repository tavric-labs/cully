---
title: Connect an agent
description: Connect Claude Code, Codex or Cursor to Cully, then run any agent in the Cully terminal.
---

# Connect an agent

The [quickstart](/quickstart) starts Cully and connects your first agent. To connect another agent or use an existing Cully server, give that agent the MCP URL printed by setup.

<InstallCommand template="cully setup --agent {agent} --mcp-url http://127.0.0.1:8080/mcp" />

Pick your agent and use the URL printed by your server. Restart your agent after setup. In Codex, review and trust the Cully hooks in `/hooks` when prompted. For an OAuth-enabled HTTPS server, append `--oauth` and sign in using the steps below.

Setup connects **memory and hooks**. To get the **health bar, advisor panel, loop detection, replay and handoff**, start the agent through the [Cully terminal](/terminal):

```sh
cully run claude
cully run codex
cully run cursor     # starts cursor-agent
cully run my-agent   # any other agent on your PATH
```

| Agent | Run it in the Cully terminal | OAuth sign-in, when enabled |
| --- | --- | --- |
| Claude Code | `cully run claude` | Use `/mcp`. |
| Codex | `cully run codex` | Run `codex mcp login cully`. |
| Cursor | `cully run cursor` (the `cursor-agent` CLI) | Use Cursor's MCP settings. |

The Cursor editor itself is not a terminal program, so the Cully terminal wraps the `cursor-agent` CLI. The editor keeps Cully's hooks, command and MCP tools.

## What setup installs

| Agent | Installed by `cully setup --all` |
| --- | --- |
| Claude Code | Session and stop hooks, an asynchronous tool-event hook for the terminal, a status line that feeds the terminal panel (and shows itself when you run Claude outside the terminal), a `/cully` command and the Cully skill. |
| Codex | A session-start hook, an asynchronous tool-event hook, a `/prompts:cully` prompt, managed `AGENTS.md` guidance and the skill. Codex's native footer settings stay yours. |
| Cursor | Continuity hooks, asynchronous shell, file-edit and MCP hooks for the terminal, a project `/cully` command and the skill. |

The tool-event hooks send one small event per tool call to the local [session journal](/privacy), and only while the agent runs inside the Cully terminal. They store no prompts, commands or tool output. See [privacy](/privacy).

If the server uses OAuth, add `--oauth` to setup and sign in as shown in the table.

## Claude Code

Run `cully setup --agent claude --mcp-url URL`, using the URL printed by your private stack or supplied by your team. Restart Claude Code, then start it with `cully run claude`. Analysis jobs use the shared background worker through the Claude CLI. The worker consults configured Cully memory read-only and can research a concrete tool or documentation gap. The foreground session owns durable handoff writes.

Inside the terminal Claude Code's own status line stays silent so there is one display. The terminal panel reads Claude's context pressure from the same data.

## Codex

Run `cully setup --agent codex --mcp-url URL`, then restart Codex and start it with `cully run codex`. Review Cully's hooks in `/hooks` when Codex asks you to trust them.

The terminal reads Codex's native footer from the rendered screen in memory, never a transcript, and shows its model, reasoning effort, context, tokens and quota in the Cully panel. Your Codex configuration file stays unchanged. Cost and cache usage are unavailable from Codex and are shown as such. Exiting the wrapper stops its Codex child. `cully run codex resume` restores a thread's saved metrics.

The Codex wrapper also dispatches bounded session signals to the shared worker through an ephemeral Codex CLI process. Its findings are combined with immediate context and tool warnings. Worker availability never replaces those local checks.

## Cursor

Run `cully setup --agent cursor --mcp-url URL`, then restart Cursor. Start the CLI agent with `cully run cursor`. With an OAuth server, sign in from Cursor's MCP settings. User-level hooks on your laptop do not run in Cursor cloud agents. Connected cloud agents can still use the Cully MCP tools.

Cursor's shell hook reports no exit code, so a failed Cursor command is not marked failed, and loop detection is weaker for Cursor. The continuity stop hook can dispatch coarse session metadata to the shared worker through `cursor-agent` print and ask mode. It does not read transcripts. Cursor has no native model, context or rate-limit feed for the terminal panel, so those rows show unavailable instead of waiting for an agent signal that will not arrive. Use Cursor's own UI for model and context.

## Any other agent

`cully run NAME` starts any executable on your PATH in the same terminal. It gets the health bar, Git state and the advisor panel. It gets tool counts, loops, replay and handoff only if the agent can run Cully's hook, because those come from tool events.

## Running several agents in T3 Code

If you switch between agents, [T3 Code](/t3code) can start them from one window. Cully needs no extra setup there. See [Use Cully in T3 Code](/t3code).

## If you already installed the advisor

Use `cully mcp add --agent codex --url URL` to add only the MCP connection. Add `--oauth` when the server requires sign-in. If you installed Cully before the terminal hooks were available, rerun `cully setup --agent claude --all` (with your agent name; replace `--all` with `--mcp-url URL` if you use an existing server) to refresh the skill and hooks. Setup preserves unrelated agent configuration and does not replace a Cully connection that points at another URL. Run `cully status` to inspect the local integration.

Running `cully setup --all` starts the standard local memory stack and advisor, and installs integrations for detected agents. Use `--agent AGENT` to select one or `--agent all` for all supported agents. With `--mcp-url URL`, setup connects to that existing server and starts the local advisor without starting Docker services. Setup does not sign you into an OAuth server automatically. For server requirements, see [self-hosting](/hosting) and [team deployment](/team-deployment).

For detailed client configuration, see the [client reference](https://github.com/tavric-labs/cully/blob/main/clients/README.md).
