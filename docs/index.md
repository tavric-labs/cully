---
title: Cully documentation
description: Session intelligence, private memory, recovery and an evidence-based improvement loop around your coding agents.
---

# Cully docs

**Session intelligence. Private memory. A better harness.**

Cully gives your coding agents a wider view of the work: live session health, loop detection, private recall, recovery and an evaluation loop that proposes improvements you approve. Run Claude Code, Codex, Cursor or another agent in the same terminal harness. Each agent keeps its own reasoning, model and tools.

Cully observes the session, keeps useful context between agents, shows what was checked, and helps you recover when work stalls. It evaluates its setup and guidance every five hours, turns failing checks into proposals, and lets you inspect the results after an approved change.

Use it on your own machine or as an individual contributor on shared infrastructure. Saved memory stays private to your identity. Solo and IC describe those uses; there is no mode flag. Existing [Team workflows](/team-workflows) separately coordinate project tasks, checkpoints, review and explicitly shared lessons.

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
| Review and approve what Cully proposes for itself | [Let Cully improve itself](/self-improvement) |
| Know what is built and what is planned | [Current capabilities](/capabilities) |
| Share infrastructure while keeping each contributor's memory private | [Contributor deployment](/team-deployment) |
| Coordinate project tasks, handoffs and review | [Team workflows](/team-workflows) |

## Core concepts

| Concept | In one line |
| --- | --- |
| [Terminal](/terminal) | One wrapper for every agent, with a health bar and an advisor panel |
| [Session intelligence](/session-intelligence) | A private journal of what the agent did, turned into loops, health, replay and handoff |
| [Harness](/harness) | Read-only setup detection and historical output estimates |
| [Token impact](/token-impact) | Observed events, estimated overhead and diagnostic comparisons |
| [Evaluate Cully](/evals) | Score setup, harness, the improvement loop, task efficiency, advisor and memory against planted ground truth |
| [Self-improvement](/self-improvement) | A scheduled eval, suggestions you read, apply or drop, and a before/after check |
| [Advisor](/advisor) | A second pair of eyes that warns and suggests while you work |
| [Private memory](/memory) | Decisions, attempts, results and session links that outlive the conversation |
| [Agent integration](/agents) | How Claude Code, Codex, Cursor and others connect |
| [Install as a plugin](/plugins) | Add Cully's hooks and skill from an agent's plugin marketplace |
| [Team workspace](/team-workflows) | Project roles, task ownership, portable checkpoints, review and explicitly published lessons |

## Run Cully

Solo's default stack runs on your laptop with one stable owner and no sign-in. Shared deployments use [OAuth](/oauth) to identify each contributor and keep records private. Clients discover sign-in from the server; they cannot choose a different owner. See [self-hosting](/hosting) and [contributor deployment](/team-deployment).

## Trust and direction

| Topic | Guide |
| --- | --- |
| What Cully records and what it never does | [Privacy](/privacy) |
| Where Cully is going | [Product direction](/product-direction) |
| Manual service settings and system details | [Configuration](/configuration) and [architecture](/architecture) |

For project work, see [development](/development), the [roadmap](/roadmap) and the [changelog](https://github.com/tavric-labs/cully/blob/main/CHANGELOG.md).

## Work across people and agents

Cully already supports [Team workflows](/team-workflows): project access, task ownership, portable checkpoints, review evidence and explicitly published lessons. Private notes remain separate. Broader Team enhancements are planned; see [product direction](/product-direction).
