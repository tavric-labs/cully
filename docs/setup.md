---
title: Set up Cully
description: Install Cully, start cully ui and finish setup from the browser or with cully setup.
---

# Set up Cully

::: warning New in the next release
This page describes the target behaviour of the guided Setup page and the interactive `cully setup` wizard. They are being built; the flags and screens below may change before release.
:::

Setup connects your agents, picks a mode and writes one shared dev-workflow rules skill. You can finish it in the browser or in the terminal. Both do the same thing.

## 1. Install and start

Install the CLI as described in the [quickstart](/quickstart), then start the browser UI:

```sh
cully ui
```

If setup is not done yet, the UI sends you to its Setup page. Prefer the terminal? Run `cully setup` instead.

## 2. The Setup page

1. **Welcome and check.** Cully looks for the agents you have installed and for an existing configuration.
2. **Mode.** Choose local mode (one machine) or MCP mode (many machines). See [One machine or many?](/choose-a-mode).
3. **Rules interview.** Cully asks a few questions about how you work. Answer or skip each one. Answers are prefilled from an analysis of your sessions.
4. **Review and approve.** Cully shows the rules it will write as a diff. Nothing is written until you approve.
5. **Suggestions.** Cully shows your session count, time and token use, and suggests agents and background jobs you could add.
6. **Done.**

## 3. The terminal route

```sh
cully setup
```

On a terminal this runs an interactive wizard with the same steps. The existing flags still work: `--all` (local stack), `--mcp-url URL` (connect to a deployment), `--local` (local mode, no stack) and `--yes` (apply without a prompt). Two flags are new: `--no-interview` skips the rules questions, and `--answers FILE` takes the answers from a file. See [installer options](/installation) for the rest.

## Where things live

The rules skill has a single source, `~/.cully/skills/dev-workflow-rules/SKILL.md`. Setup links it into the skills folder of each harness you use: `~/.claude/skills`, `~/.codex/skills` and Cursor's skills folder. Edit the one file and every harness sees the change.

## Approval and rollback

Nothing is written until you approve a diff. Every change is recorded in the [history of changes](/ui#history-of-changes), where you can roll it back.

## What the session analysis uses

The analysis uses counts, hours and tokens, and session titles. No transcripts leave your machine. See [privacy](/privacy).

## Suggestions need history

Suggestions of agents and background jobs need at least 20 analysed sessions. With fewer, Setup skips them and says so. See [agents](/agents) and [scheduled jobs](/scheduled-jobs).
