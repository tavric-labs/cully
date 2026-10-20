---
title: Solo mode
description: A local coding-agent harness runtime, with session health, advice, replay, handoff and private memory.
---

# Solo mode

**The local runtime for your coding agents.**

Run your preferred agent inside Cully. It helps you notice repeated failures, context pressure and unchecked edits, recover useful context, understand what happened and continue in another agent. Your journal stays local and your saved memory stays owner-scoped.

```sh
cully setup --all
cully run codex       # or claude, cursor, another executable
```

Use `cully status` for observed health, `cully replay` for session activity, `cully rescue` when stuck, and `cully handoff --print` for continuation. Hooks provide supported signals; missing values remain unavailable. Local rule warnings work offline. Durable memory and model-backed advice need their configured services.

## Audit and improve the setup

Run `cully harness` for setup gaps and recent oversized-output estimates. The terminal shows Harness, Project & usage and Cully impact together; open the advisor’s Tab view for all instruments. Run `cully impact` for observed memory hits, skipped analyses and loops, and `cully savings` for a Claude transcript cost/exploration comparison. These are estimates and observations, not guaranteed savings. See [harness](/harness) and [token impact](/token-impact).

`cully workflow` previews an optional private profile of your build, test and release habits for the configured advisor model. `cully update --check` checks for a newer CLI; `cully update` verifies and installs it. Service upgrades still use your deployment tooling. See [installation](/installation).

The [mobile Self preview](/mobile) reads dated personal notes and offers local tasks and questions. It does not run coding agents or expose Team actions.

For an existing memory server, use `cully setup --mcp-url URL` and add `--oauth` if it requires sign-in. A shared server does not make your private notes visible to other people. `personal` and `company` are private organizational labels.

Start with [quickstart](/quickstart), [terminal](/terminal), [advisor](/advisor), [session intelligence](/session-intelligence) and [memory](/memory). For shared ownership, handoffs and review, use [Team mode](/team-workflows). Team mode adds authorized project records without importing your journal or notes. See [product vision](/product-direction).
