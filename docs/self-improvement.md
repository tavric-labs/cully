---
title: Let Cully improve itself
description: Cully scores itself every five hours, proposes changes from failing checks, and writes a change only after you approve it.
---

# Let Cully improve itself

Cully's own evaluation runs on a schedule. When a check fails, Cully proposes a change to itself. Nothing is written until you approve it. This is separate from the [advisor](/advisor), which watches a session in your project and, when you accept a suggestion, changes that project with `cully apply`. Self-improvement changes Cully's own guidance and nothing in your projects.

```sh
cully improve                      # last eval, trend and pending proposals
cully improve --run                # evaluate now instead of waiting
cully improve --apply 1 --dry-run  # preview what proposal 1 would write
cully improve --apply 1            # approve it; Cully writes it
cully improve --dismiss 1          # skip it
```

## How it works

1. The advisor daemon runs `cully eval` for the setup, harness, improve, tasks and advisor suites every five hours, twice each so a flaky check is reported. Set `CULLY_EVAL_INTERVAL` (for example `12h`, never below 30 minutes) to change the interval, or `off` to disable it. The last run is remembered across restarts. The memory suite needs a server and is not scheduled.
2. Each run is saved to `eval-history.jsonl` with counts and the names of failing checks, never contents. `cully improve` shows the trend in failing checks over recent runs.
3. A failing suite becomes one **pending proposal**. The same failure is not proposed twice. A failure that goes away resolves its proposal, and an applied change that did not hold reopens it. A dismissed proposal stays dismissed. `cully suggestions` mentions pending proposals.
4. When you run `cully improve --apply N`, Cully asks your configured agent model to plan a change from the failing checks. It shows the plan, and writes it only after you confirm (or pass `--yes`).

## What a change may write

An approved change can add rules to `~/.cully/skills/cully/SKILL.md` and install one small skill under `~/.cully/skills/`. The coding agents you have set up read those files.

It never adds MCP servers or runs commands: if the planner proposes either, Cully ignores them and says so. It refuses a skill name that is not a plain folder name, anything that looks like a credential, and plans over 12 KiB. It cannot change Cully's code. When the cause is a code defect, the plan's notes say so and the proposal stays pending.

Hooks are not written automatically: a hook runs in every session, so adding one stays a decision you make through `cully setup`.

## How it is checked

The `improve` suite in [`cully eval`](/evals) tests this loop against planted failures: one proposal per failure, no duplicates, resolved and reopened proposals, dismissal, skipped suites, and the approval gate. Declining and a dry run must write nothing, and an approval must add no MCP server. The Go tests also confirm that the suite fails against a loop that never proposes, duplicates, never resolves or lets everything through.

## What this does not prove

- A passing `improve` suite shows the loop is safe and keeps its books straight. It does not show that a proposal improves Cully. The next scheduled run does: the failing-check count in `cully improve` should fall after a change.
- The planner is a model, so two plans for the same failure can differ. You review each one before it is written.
- Evaluations that read your Claude history, such as the advisor replay and the task measurements, report without ground truth and never fail a run, so they never create proposals. The `tasks` suite also compares time before and after each applied change, which shows whether a change helped; it is uncontrolled and informs you without creating a proposal.
