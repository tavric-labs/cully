<p align="center">
  <img src="assets/brand/cully-logo.png" alt="Cully" width="420">
</p>

# Cully

**Your agent's copilot. Your team's workspace.**

[Website](https://cully.net) · [Documentation](https://docs.cully.net)

Cully has two modes around Claude Code, Codex, Cursor and the agents you already use. **Solo** is a copilot for your coding agent and work environment: session health, advice, replay, handoff and private memory. **Team** connects project tasks, ownership, portable checkpoints, review evidence and explicitly shared lessons across people and agents.

Start with [Solo mode](https://docs.cully.net/solo) or [Team mode](https://docs.cully.net/team-workflows). Both use the same installed CLI; team operations require OAuth and explicit project access. No new mode flag is needed, and solo notes stay private.

```text
Prepare  →  Work  →  Hand off  →  Review  →  Reuse
```

Works across agents. Learns across sessions.

## Why Cully

A coding agent is good at the task in front of it. It does not see the whole session: that the same test has failed three times, that the edits were never checked, that you solved this last week, or what the previous agent left half done. Cully keeps track of that wider picture.

## What Cully does

```sh
cully run claude      # or codex, cursor, or any agent on your PATH
```

| Capability | What you get |
| --- | --- |
| **One terminal for every agent** | Claude Code, Codex, Cursor CLI and any executable run in the same wrapper with a live health bar and advisor panel. |
| **Loop detection** | "Cully noticed something: the same command failed 3 times after edits." |
| **Advisor** | A second pair of eyes: context pressure, missing verification, broad searching, workflow gaps. Click it to open. |
| **Replay and timeline** | See what the agent did and how: which files it read, edited, created and deleted, which checks failed, where it looped. |
| **Handoff** | A structured summary so a different agent can continue without rebuilding context. |
| **Rescue** | Evidence and recovery steps when a session is stuck. |
| **Project memory** | Decisions, attempts, checks and next steps saved through MCP, found again by meaning with Mem0. |
| **Team workspace** | Project access, a task board and inbox, atomic claims and portable checkpoints through CLI or MCP. |
| **Review and shared learning** | Acceptance evidence tied to a revision, independent human approval, private lesson drafts and explicit project publication. |

```text
── Codex │ oauth-service:main │ 27m │ Context 63% │ ⚠ Loop 3x │ ● 2 unchecked ── Ctrl+] advisor ──
```

Every value Cully shows is measured. A value with no data is left out, never guessed. See [current capabilities](https://docs.cully.net/capabilities) for exactly what ships today and what is only planned.

## Team mode

Team workflows require an OAuth-enabled server and explicit project membership. Start agents manually, checkpoint selected context, submit reported evidence and ask a project maintainer who did not contribute to approve it. Lessons stay private until their author publishes them; playbooks require a maintainer and a completed source task. See [team workflows](https://docs.cully.net/team-workflows) for actions and examples. Managed execution and automatic learning remain planned.

## Supported agents

| Agent | In the Cully terminal | Notes |
| --- | --- | --- |
| Claude Code | `cully run claude` | Context pressure through a hook-fed snapshot |
| Codex | `cully run codex` | Native footer instruments: model, context, tokens, quota |
| Cursor | `cully run cursor` | Wraps the `cursor-agent` CLI. The editor keeps hooks and MCP tools. |
| Any other agent | `cully run NAME` | Health bar and Git state; tool events if the agent can run Cully's hook |

macOS and Linux.

## Get started

```sh
curl -fsSL https://cully.net/install.sh | sh -s -- --agent codex
```

Open a new terminal, then run `cully setup --all` to start the local memory stack and advisor and connect detected coding agents. Use `--agent codex` to select an agent. Setup logs each stage and reports completion only after all components are ready. Then:

```sh
cully run codex        # work as usual, with the health bar and advisor
cully status           # measured session health and risk
cully replay           # what the agent did, and how
cully handoff          # hand the session to another agent
cully rescue           # when a session is stuck
cully harness          # audit instructions, guardrails, memory, skills and hooks
cully update --check   # see whether a newer CLI release exists
cully workflow         # optional private dev-workflow-rules profile for the advisor
```

See the [quickstart](https://docs.cully.net/quickstart) for prerequisites and other agents.

## How it works

```text
Developer ─► Cully terminal ─► your agent
                  │
         tool events from hooks
                  ▼
          Session journal ─► Session intelligence ─► Advisor · Replay · Handoff · Rescue
                                      │
                                      ▼
                           Project memory (MCP · PostgreSQL · Mem0)
```

Your agent talks to its model and tools directly. Cully draws around it and records small events.

| Component | What it does |
| --- | --- |
| `cully` | Terminal, advisor, agent setup, session status, replay, handoff and rescue |
| `cully-mcp` | Shared memory tools; private single-owner mode by default, optional OAuth |
| `cully-data` | Private PostgreSQL API and Mem0 projection worker |

See the [architecture](https://docs.cully.net/architecture) and [how Cully works](https://docs.cully.net/how-cully-works).

## Privacy

Your conversations are not Cully's database. The local session journal keeps the time, agent, kind of tool, pass or fail, project-relative file paths with the operation, and the program and recognized subcommand of a command. It never keeps prompts, file contents, command arguments or tool output. Set `CULLY_JOURNAL_PATHS=0` to stop recording paths and commands. See [privacy](https://docs.cully.net/privacy).

## Documentation

[Quickstart](https://docs.cully.net/quickstart) · [The Cully terminal](https://docs.cully.net/terminal) · [Session intelligence](https://docs.cully.net/session-intelligence) · [Advisor](https://docs.cully.net/advisor) · [Memory](https://docs.cully.net/memory) · [Self-hosting](https://docs.cully.net/hosting) · [Team deployment](https://docs.cully.net/team-deployment)

## Roadmap

Shipped work and proposals are kept apart in [product direction](https://docs.cully.net/product-direction): guardrails, a published benchmark, a lighter local install, and learned team workflow.

## Contributing

See [development](https://docs.cully.net/development). Companies can also [deploy Cully with MCP Runtime](https://docs.cully.net/team-deployment#example-run-cully-with-mcp-runtime).

Licensed under [Apache 2.0](LICENSE).
