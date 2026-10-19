---
title: Use Cully in T3 Code
description: Run Claude Code, Codex and Cursor from one T3 Code window while they share Cully memory, tasks and handoffs.
---

# Use Cully in T3 Code

[T3 Code](https://github.com/pingdotgg/t3code) is an optional window for running several coding agents without a terminal per agent. Cully works with it, and works the same without it. Use T3 Code if you switch between agents. If you use one agent, you don't need it.

## Why it works

T3 Code starts the agent CLIs already installed on your machine. Those CLIs read the same configuration that `cully setup` writes, so the Cully MCP tools, skill and hooks load in every agent T3 Code starts. Cully does not depend on T3 Code and changes none of its settings.

## Set up

1. Install and sign in to each agent you want, as T3 Code asks.
2. Run Cully setup for each agent:

   <InstallCommand template="cully setup --agent {agent} --mcp-url http://127.0.0.1:8080/mcp" />

3. Restart T3 Code, or start a new session in it, so each agent loads the configuration.

When `t3` is on your PATH, `cully setup` prints a reminder of this page.

## What you get

- Memory, tasks and handoffs are shared by every agent that connects to the same Cully server. An agent can pick up what another one logged.
- Each session records its own agent session id, so you can tell which agent did what and resume it.
- In a [team workspace](/team-workflows), agents in T3 Code claim and hand off tasks like any other client.

## What you don't get

The health bar, advisor panel, loop detection and replay come from the [Cully terminal](/terminal), which wraps an agent in a terminal. T3 Code draws its own interface, so those stay available only through `cully run`. Hooks that send tool events do so only inside the Cully terminal, so nothing extra is recorded in T3 Code. See [privacy](/privacy).

## Troubleshooting

- **An agent can't see Cully tools.** Restart the agent session, then run `cully status`.
- **OAuth server.** Sign in from each agent as shown in [Connect an agent](/agents).
