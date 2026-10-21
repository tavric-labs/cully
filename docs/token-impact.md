---
title: Measure token impact
description: Understand Cully’s impact ledger, observational savings report and advisor replay evaluation.
---

# Measure token impact

Cully’s own lookups and advisor calls consume tokens. These tools help you inspect that cost and investigate whether your setup is useful. They report different samples; none is a billing report or a controlled productivity benchmark.

| Question | Command | Source |
| --- | --- | --- |
| What did Cully observe or avoid? | `cully impact` | Local impact events from hooks and advisor activity |
| How much did Cully inject, compared with early exploration? | `cully savings` | Local Claude transcripts from sessions that edited code |
| Where are large tool results coming from? | `cully harness` | Recent sessions of the agent you use (Claude Code or Codex); see [harness](/harness) |
| Would the advisor flag later oversized output? | `cully eval advisor` | Planted sessions, plus replayed Claude transcript checkpoints |

## Read the impact ledger

```sh
cully impact
cully impact --days 7 --log
cully impact --explain
cully impact --days 30 --json
```

The default window is 14 days. The report counts lookups returning prior work, empty lookups, analyses skipped by cadence, loops caught and oversized output flagged. `--log` includes up to eight recent aggregate events. Events contain counters and token estimates, not retrieved notes or tool output.

“Memory used” means a lookup returned prior work. It does not prove the agent used it correctly or avoided rework. “Analysis avoided” estimates the instructions and signals not sent when an analysis was skipped. A loop count describes detection, not how much the loop cost or whether it was fixed.

The CLI can add an observational memory-saving estimate when `cully savings` has sufficient comparison data. Without that basis, it explicitly declines to claim memory savings. The terminal’s session impact uses current-session events without that historical memory-saving estimate. Its net figure subtracts recorded lookup-result cost from estimated avoided analysis tokens; it does not include every model call, tool schema or dollar charge.

## Inspect lookup cost and exploration

```sh
cully savings --days 14
cully savings --days 30 --project my-project --json
cully savings --dir /path/to/claude/projects
```

The default source is `~/.claude/projects`. `--project TEXT` matches transcript folder names containing that text; use a distinctive value to keep the sample relevant. Only sessions containing an edit enter the comparison.

Cost includes Cully result text, call arguments and tool schemas found in the transcript, estimated at roughly four characters per token. The report also shows empty-lookup cost and per-tool results. This approximation is not the model’s tokenizer or invoice.

Benefit compares mean read/search output before the first edit for sessions with a memory hit against sessions without Cully, or sessions with no hit when no no-Cully baseline exists. It labels confidence based on cohort sizes. Tasks, session length and other tools can differ between groups, so the observed difference cannot establish causation. A negative net result is useful evidence to reduce overhead, not something to hide.

## Evaluate the advisor

```sh
cully eval advisor --project personal --days 30 --max 20
cully eval advisor --project my-project --max 10 --json
```

The default is 30 days, up to 40 newest matching transcript files, filtered to folder names containing `personal`. The evaluator replays eligible sessions at 30%, 60% and 90% through the signal pipeline. It compares token-waste advice at each checkpoint with oversized output later in the same transcript, producing useful, low-value, missed and correct-quiet counts plus precision/recall proxies.

These are hindsight proxies on the selected history. They do not establish advice quality on new tasks, correctness or actual savings. Small samples can move the scores sharply. This is a diagnostic tool, not an automated acceptance judge for unrelated UI, detection or documentation changes.

```sh
cully eval advisor --project personal --max 10 --live 2 --agent claude
```

`--live N` additionally invokes the real model-backed advisor for up to N checkpoints through the chosen agent CLI. It consumes model tokens and can perform configured Cully recall and targeted web research. It sends signal summaries through the configured adapter rather than uploading raw transcript text. The live pass also checks that every figure of 10 or more in the advice appears in the signals the advisor was given. Use the default replay when you only need a local check. See [evaluate Cully](/evals) for the full method.

## A practical comparison

1. Pick a project and a fixed recent window. Read `cully impact`, `cully savings` and the harness baseline together.
2. Identify one concrete cost, such as empty lookups or repeated whole-file reads. Change one workflow component.
3. After comparable work, rerun the same reports. Check correctness and successful verification alongside the token estimates.

The advisor’s token-spend recommendations must cite measured signal figures; unsupported token advice should remain quiet. That prompt rule does not make every generated recommendation correct. See [advisor](/advisor), [privacy](/privacy) and the engineering [token-efficiency plan](/token-efficiency-plan).
