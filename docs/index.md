---
title: Cully documentation
description: Solo is a copilot for your agent work environment. Team connects people and agents with project tasks, handoffs, review evidence and shared learning.
---

# Cully docs

**Your agent's copilot. Your team's workspace.**

Cully has two modes. [Solo](/solo) is a copilot for your coding agent and work environment, with local session health, advice and private memory. [Team](/team-workflows) connects people and agents through project tasks, portable checkpoints, review evidence and explicitly shared lessons. They use the same CLI and services; project access is explicit and solo records stay private.

```text
Prepare  →  Work  →  Hand off  →  Review  →  Reuse
```

Works across agents. Learns across sessions.

## Start here

| What you want to do | Guide |
| --- | --- |
| Install Cully and run your first agent | [Quickstart](/quickstart) |
| Audit instructions, skills, hooks and memory setup | [Audit your harness](/harness) |
| Inspect lookup overhead and recent output | [Measure token impact](/token-impact) |
| Read your own work on a phone | [Mobile developer preview](/mobile) |
| Update an existing CLI or add workflow context | [Installation and updates](/installation) |
| See how the parts fit together | [How Cully works](/how-cully-works) |
| Run any agent with a live health bar and advisor | [The Cully terminal](/terminal) |
| Replay a session, hand it off or get unstuck | [Session intelligence](/session-intelligence) |
| Put sessions on the record: forensics, review, playbooks | [Use cases](/use-cases) |
| Know what is built and what is planned | [Current capabilities](/capabilities) |
| Coordinate tasks, handoffs and review with teammates | [Team workflows](/team-workflows) |

## Core concepts

| Concept | In one line |
| --- | --- |
| [Terminal](/terminal) | One wrapper for every agent, with a health bar and an advisor panel |
| [Session intelligence](/session-intelligence) | A private journal of what the agent did, turned into loops, health, replay and handoff |
| [Harness](/harness) | Read-only setup detection and historical output estimates |
| [Token impact](/token-impact) | Observed events, estimated overhead and diagnostic comparisons |
| [Evaluate Cully](/evals) | Score setup, harness, advisor and memory against planted ground truth |
| [Advisor](/advisor) | A second pair of eyes that warns and suggests while you work |
| [Project memory](/memory) | Decisions and results that outlive the session |
| [Agent integration](/agents) | How Claude Code, Codex, Cursor and others connect |
| [Team workspace](/team-workflows) | Project access, human-owned tasks, portable checkpoints, review evidence and published lessons |

## Run Cully

Solo's default stack runs on your laptop with one stable owner and no sign-in. For Team, deploy the services with [OAuth](/oauth), identify each person and grant explicit project roles. Each person's private memory stays separate from shared workspace records. See [self-hosting](/hosting) and [team deployment](/team-deployment).

## Trust and direction

| Topic | Guide |
| --- | --- |
| What Cully records and what it never does | [Privacy](/privacy) |
| Where Cully is going | [Product direction](/product-direction) |
| Manual service settings and system details | [Configuration](/configuration) and [architecture](/architecture) |

For project work, see [development](/development), the [roadmap](/roadmap) and the [changelog](https://github.com/tavric-labs/cully/blob/main/CHANGELOG.md).

## Teams and project work

A [team deployment](/team-deployment) provides private user memory alongside explicitly authorized project work. Read [work across people and agents](/team-workflows) to create tasks, continue from checkpoints, review evidence and publish lessons. [Product vision](/product-direction) separates this manual-launch implementation from further proposals.
