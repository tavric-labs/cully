---
title: Evaluate Cully
description: Score Cully as one agent, covering its hooks, skill, harness audit, advisor and memory, against ground truth and published metrics.
---

# Evaluate Cully

Cully is one agent from the outside: the hooks and skill it installs, the harness audit that reports them, the advisor that watches a session, and the memory it recalls. `cully eval` scores each part against ground truth. Every check runs real Cully code. None asks a model to grade a model.

```sh
cully eval                      # setup, harness and advisor; memory is skipped without a server
cully eval memory --mcp-url http://127.0.0.1:3393/mcp
cully eval advisor --project my-project --live 2 --agent claude
cully eval --json
```

It exits nonzero when any check fails, so it can gate a script or CI job. Setup, harness and the planted advisor checks run in a scratch home, restore your environment afterwards and never touch your agent configuration or start a process.

## What each suite checks

| Suite | Ground truth | What fails |
| --- | --- | --- |
| `setup` | Installs every agent into an empty home. | A missing or non-canonical skill or hook; a file written into the project; a rerun that changes anything; an uninstall that removes the user's own permissions or instructions, or leaves Cully's. The harness audit independently reads what setup wrote. |
| `harness` | Six planted directory layouts with known contents, including decoys such as empty skill folders and permissions without hooks. | Any component missed or reported falsely. |
| `advisor` | Planted sessions whose tool output is known byte for byte; planted loop journals whose answer follows the documented rules; planted file sizes and command histories for the PreToolUse guard; metric values worked out by hand. | Miscounted waste; advice before waste exists; advice in an ordinary session; a loop missed or invented; a guard nudge that fires on a small file, a limited read or a passing command, or stays silent on a large file or a repeated failure; a scorer that disagrees with the hand calculation. |
| `memory` | A dataset of relevant notes, same-project distractors, superseded notes and another project's notes, seeded into fresh projects on a live stack. | Recall, ranking or abstention below threshold; any note leaking across projects. |

The advisor suite also replays your own Claude transcripts when it finds them and reports measurements, such as advice precision and recall, redundancy and context coverage. Replay measurements are reported but never fail a run, because your history has no ground truth. `--live N` asks the real model-backed advisor about up to N of those checkpoints, half with a waste signal and half without, and fails if it cites a figure of 10 or more that does not appear in the signals it was given. That check catches invented numbers. It does not judge whether the advice was good.

## Where the methods come from

- **Context metrics: ContextBench** ([Li et al., 2026](https://arxiv.org/abs/2602.05892)). It scores the context an agent retrieves against a human-annotated gold context: Recall = |C^A ∩ C^G| / |C^G|, Precision = |C^A ∩ C^G| / |C^A| and F1. For efficiency it uses AUC-Cov, the mean over steps of recall of everything explored so far, and it defines Redundancy as the share of a step's context that was already seen. Memory scoring uses recall, precision and F1 over notes. Session replay uses Redundancy and AUC-Cov over files, computed as the paper defines them.
- **Memory abilities: LongMemEval** ([Wu et al., ICLR 2025](https://arxiv.org/abs/2410.10813)). It groups long-term memory into information extraction, multi-session reasoning, temporal reasoning, knowledge updates and abstention. Dataset cases carry these labels: extraction, multi-note, knowledge-update (the newer note must win over a superseded one) and abstention (silence is the right answer). We do not cover temporal reasoning.
- **Advisor design: LivePlan** ([Liu et al., 2026](https://arxiv.org/abs/2608.06701)). A rule-based monitor watches the trajectory without a model and consults an advisor model only when it flags a problem. The paper evaluates resolution, regressions on runs that already succeeded, and new solutions. We follow the same split. Loop detection is tested deterministically, a false alarm on a healthy session counts as a failure, and the model-backed advisor is tested separately.
- **Waste categories: AgentDiet** ([Xiao et al., FSE 2026](https://arxiv.org/abs/2509.23586)). It finds useless, redundant and expired information in agent trajectories. Redundancy is measured here. Useless and expired content are not.
- **Harness as the unit under test** ([Fan et al., 2026](https://arxiv.org/abs/2609.20804)). With the model fixed, varying context management, planning and action space changes accuracy and cost, and the effect depends on the model and the budget. This is why the harness audit reports presence, not benefit.

## What this does not prove

- The memory dataset is small and written by the Cully team. Scores catch regressions in this repository. They are not comparable with published benchmark numbers, and they do not show how Cully behaves on your notes.
- ContextBench's gold context is human-annotated and verified sufficient for the task. A transcript has none, so replay treats the files a session edited as gold. Replay Recall, Precision and AUC-Cov are proxies for finding the files the session changed. Redundancy needs no gold context. Evidence Drop needs a definition of utilized context that we do not have, so it is not reported.
- Replay advice precision and recall say that waste followed a suggestion. They do not say the agent acted on it or saved tokens.
- A passing harness audit shows a component exists. The ablation literature shows that its effect varies. To test an effect, run the same task with and without the component and compare cost and outcome.
- The grounding check catches invented figures. It cannot tell whether grounded advice was useful. That needs human judgement or a task-level experiment, such as resolution rate with and without advice, as LivePlan runs.

## Extend it

Add a memory case in `internal/evals/retrieval.json` when retrieval misses something that mattered. Add a planted loop or waste scenario in `internal/cully/eval.go` when the advisor misses a pattern or raises a false alarm. Plant the answer in the fixture first, so the eval can fail.
