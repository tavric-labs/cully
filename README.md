<p align="center">
  <img src="assets/brand/cully-logo.png" alt="Cully" width="420">
</p>

# Cully

**A self-improving harness for coding agents and software projects.**

[Website](https://cully.net) · [Documentation](https://docs.cully.net)

Cully runs Claude Code, Codex, Cursor and other coding agents inside one terminal runtime, adding session health, advisor guidance, private project memory and shared team workflows. Each agent keeps its own reasoning and tools; Cully supplies the session around it. It also scores itself every five hours and proposes fixes, which it writes only after you approve them.

## Why Cully

Useful context should survive a session. Cully surfaces repeated failures and unchecked edits, and brings back saved decisions and next steps when you switch sessions or agents. See what happened, then continue with the context that matters.

## Get started

Install on macOS or Linux:

```sh
curl -fsSL https://cully.net/install.sh | sh -s -- --agent codex
```

Open a new terminal, then:

```sh
cully setup --all      # start local memory services and connect your agents
cully run codex        # or claude, cursor, another executable
```

Setup is user-wide: run it once, then use Cully in any project. No project `.cully` folder is required. The local memory stack requires Docker. For an existing server, use `cully setup --mcp-url URL`. See the [quickstart](https://docs.cully.net/quickstart) for prerequisites and other agents.

## What you get

| Capability | What it helps with |
| --- | --- |
| [Live guidance](https://docs.cully.net/advisor) | Notice context pressure, repeated failures and edits awaiting checks. |
| [Terminal compatibility](https://docs.cully.net/terminal#configure-and-verify-your-terminal) | Configure required mouse/scroll settings, verify live wheel/divider/advisor behavior, and drag a visible Warp scrollbar through normal-screen history. |
| [Technique discovery](https://docs.cully.net/advisor#discover-current-techniques) | Hourly, bounded public research on current agent releases, workflows, skills and hooks, filtered against your task. Opt out with `CULLY_DISCOVERY=off`. |
| [Harness audit](https://docs.cully.net/harness) | Find setup gaps in instructions, permissions, memory, skills and hooks. |
| [Token impact](https://docs.cully.net/token-impact) | Inspect lookup overhead, oversized output and observational savings estimates. |
| [Replay and handoff](https://docs.cully.net/session-intelligence) | Review recorded steps, recover a stuck session and prepare the next agent. |
| [Project memory](https://docs.cully.net/memory) | Recall saved decisions, attempts, results and next steps through MCP and Mem0. |
| [Self-improvement](https://docs.cully.net/self-improvement) | Cully evaluates itself on a schedule, proposes fixes from failing checks, and applies one only when you approve it in `cully improve`. |
| [Evals](https://docs.cully.net/evals) | Score setup, harness, the improvement loop, task efficiency (verified, time, steps), advisor and memory against planted ground truth. |
| [Workflow profile](https://docs.cully.net/installation#optional-workflow-skill) | A starter skill with hint comments for your build, test and release habits; fill it in when you know more. |

```sh
cully status           # session health and pending checks
cully harness          # read-only setup audit and recent output baseline
cully replay           # recorded session activity
cully handoff --print  # a continuation summary
cully improve         # review what Cully suggests for itself
cully eval tasks       # task efficiency from your own sessions
cully impact --explain # how the net token figure is built
cully update --check   # check for a newer CLI
```

Signals depend on the agent's integrations. Token figures are estimates, not billing totals or proof of savings. See [current capabilities](https://docs.cully.net/capabilities).

## Solo and Team

- **[Solo](https://docs.cully.net/solo):** a coding-agent harness runtime with session guidance, replay and private owner-scoped memory.
- **[Team](https://docs.cully.net/team-workflows):** a project management harness runtime for people and AI agents, with shared tasks, checkpoints, independent review and explicitly published lessons. Requires OAuth and project membership; agents launch manually.

Both use the same CLI. Private notes and local journals stay separate from shared project records.

## Privacy

The local journal records session metadata, not prompts, file contents or tool output. Historical measurement tools read local Claude transcripts without saving raw transcript text. Set `CULLY_JOURNAL_PATHS=0` to omit paths and command names from the journal. Read the [privacy guide](https://docs.cully.net/privacy) for storage and model boundaries.

## Mobile preview

[Cully Self](https://docs.cully.net/mobile) brings dated personal work briefs, local tasks and read-only questions to Android and iOS. Source builds are available; store distribution and device verification remain incomplete.

## Learn more

[Architecture](https://docs.cully.net/architecture) · [Self-hosting](https://docs.cully.net/hosting) · [Development](https://docs.cully.net/development) · [Product direction](https://docs.cully.net/product-direction)

Licensed under [Apache 2.0](LICENSE).
