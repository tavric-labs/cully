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
cully eval --repeat 3          # run setup, harness and improve three times; fail if results differ
cully eval --json
```

It exits nonzero when any check fails, so it can gate a script or CI job. Setup, harness and the planted advisor checks run in a scratch home, restore your environment afterwards and never touch your agent configuration or start a process.

## What each suite checks

| Suite | Ground truth | What fails |
| --- | --- | --- |
| `setup` | Installs every agent into an empty home. | A missing or non-canonical skill or hook; a file written into the project; a rerun that changes anything; an uninstall that removes the user's own permissions or instructions, or leaves Cully's. The harness audit independently reads what setup wrote. |
| `harness` | Six planted directory layouts with known contents, including decoys such as empty skill folders and permissions without hooks. | Any component missed or reported falsely. |
| `improve` | Planted failing runs and planted model plans for the [self-improvement loop](/self-improvement). | A failure that is not proposed once; a duplicate proposal; a fixed failure that is not resolved or a failed fix that is not reopened; a dismissed proposal that returns; a skipped suite that resolves a proposal; an MCP server or shell command kept; an unsafe skill name, credential or oversized plan accepted; a write after declining or a dry run; an MCP server added on approval. |
| `tasks` | Nine hand-worked sessions and six hand-worked before/after sessions for the task rules below. | A verified task reported unverified or the reverse; wrong step, orientation or duration counts; a rule that fires on a clean task or stays silent on a violation; medians of similar work computed wrongly; a comparison claimed from too few sessions, or sessions without edits counted as tasks. |
| `advisor` | Planted sessions whose tool output is known byte for byte; planted loop journals whose answer follows the documented rules; planted file sizes and command histories for the PreToolUse guard; metric values worked out by hand. | Miscounted waste; advice before waste exists; advice in an ordinary session; a loop missed or invented; a guard nudge that fires on a small file, a limited read or a passing command, or stays silent on a large file or a repeated failure; a scorer that disagrees with the hand calculation. |
| `memory` | A dataset of relevant notes, same-project distractors, superseded notes and another project's notes, seeded into fresh projects on a live stack. | Recall, ranking or abstention below threshold; duplicate results; any note leaking across projects. Unseeded results count as irrelevant rather than disappearing from the score. |

The advisor suite also replays your own Claude transcripts when it finds them and reports measurements, such as advice precision and recall, redundancy and context coverage. Replay measurements are reported but never fail a run, because your history has no ground truth. `--live N` asks the real model-backed advisor about up to N of those checkpoints, half with a waste signal and half without, and fails if it cites a figure of 10 or more that does not appear in the signals it was given. That check catches invented numbers. It does not judge whether the advice was good.

An explicit `--live N` request fails when no eligible checkpoints are available; it cannot pass without running. The Go tests also challenge the evaluators with deliberately broken implementations: an installer that loses a skill, empty and always-present harness audits, waste and loop detectors that are always quiet or always noisy, and retrieval stores that return nothing, duplicates, unknown records or reversed rankings. Each must make the corresponding evaluation fail; the same fixtures pass with the real implementation. These controls verify failure detection, not model quality.

## Task rules: did the work finish, and what did it cost?

Cully's aim is to help a developer finish work with less time and fewer tokens, so `tasks` scores sessions as tasks. Outcome alone hides cost: two agents can both finish while one takes twice the steps, which is why recent benchmarks report steps, cost and time next to the resolve rate ([JetBrains, 2026](https://blog.jetbrains.com/junie/2026/08/from-leaderboards-to-profiles/); [General Agent Evaluation, 2026](https://arxiv.org/abs/2602.22953); [SWE-Together, 2026](https://arxiv.org/abs/2606.29957)). A session counts as a task when it changed code. Each task gets:

| Rule | Broken when |
| --- | --- |
| `unverified` | The task edited code and no known passing check came after the last edit. A check with no reported outcome never counts as a pass. |
| `long-orientation` | More than 20 reads or searches came before the first edit. |
| `rework` | More than three checks failed in one task. |
| `loops` | The advisor's loop rules found the same failure or edit cycle repeating. |

The planted sessions fix each answer by hand, so the rules can fail. Your own recorded sessions are then summarized as measurements and never fail a run: the share of tasks verified, median time and steps, orientation before the first edit, loops, and which rules were broken how often. These count steps and minutes, not billed tokens; `cully impact --explain` and `cully savings` cover token estimates.

### Before and after a change

When you accept a change, Cully records when. `cully apply` records a project change; `cully improve --apply` records a change to Cully itself. For each change in the window, `tasks` compares the median time, steps and verified share of up to ten tasks before it with up to ten after it in the same project, and says so when fewer than three tasks sit on either side. A drop in time after a change is a signal, not proof: the work differs between sessions, so the comparison is uncontrolled. Matching genuinely similar tasks, for example by semantic similarity over task descriptions in Cully memory, needs task text stored with each session and a live memory server. It is not done yet.

## Where the methods come from

- **Context metrics: ContextBench** ([Li et al., 2026](https://arxiv.org/abs/2602.05892)). It scores the context an agent retrieves against a human-annotated gold context: Recall = |C^A ∩ C^G| / |C^G|, Precision = |C^A ∩ C^G| / |C^A| and F1. For efficiency it uses AUC-Cov, the mean over steps of recall of everything explored so far, and it defines Redundancy as the share of a step's context that was already seen. Memory scoring uses recall, precision and F1 over notes. Session replay uses Redundancy and AUC-Cov over files, computed as the paper defines them.
- **Memory abilities: LongMemEval** ([Wu et al., ICLR 2025](https://arxiv.org/abs/2410.10813)). It groups long-term memory into information extraction, multi-session reasoning, temporal reasoning, knowledge updates and abstention. Dataset cases carry these labels: extraction, multi-note, knowledge-update (the newer note must win over a superseded one) and abstention (silence is the right answer). We do not cover temporal reasoning.
- **Advisor design: LivePlan** ([Liu et al., 2026](https://arxiv.org/abs/2608.06701)). A rule-based monitor watches the trajectory without a model and consults an advisor model only when it flags a problem. The paper evaluates resolution, regressions on runs that already succeeded, and new solutions. We follow the same split. Loop detection is tested deterministically, a false alarm on a healthy session counts as a failure, and the model-backed advisor is tested separately.
- **Waste categories: AgentDiet** ([Xiao et al., FSE 2026](https://arxiv.org/abs/2509.23586)). It finds useless, redundant and expired information in agent trajectories. Redundancy is measured here. Useless and expired content are not.
- **Reliability, not only capability.** Recent work separates pass@k (some trial succeeds) from pass^k (every trial succeeds), and reports that agent scores fall sharply between the two ([SWE Atlas, 2026](https://arxiv.org/abs/2605.08366); [Towards More Standardized AI Evaluation, 2026](https://arxiv.org/abs/2602.18029)). These are preprints. `--repeat N` applies the idea to the evaluators themselves: the deterministic suites run N times and fail if any check changes, so a flaky check cannot pass by luck. The scheduled run uses two repeats. It does not repeat the model-backed `--live` pass.
- **Harness as the unit under test** ([Fan et al., 2026](https://arxiv.org/abs/2609.20804)). With the model fixed, varying context management, planning and action space changes accuracy and cost, and the effect depends on the model and the budget. This is why the harness audit reports presence, not benefit.

## What this does not prove

- The memory dataset is small and written by the Cully team. Scores catch regressions in this repository. They are not comparable with published benchmark numbers, and they do not show how Cully behaves on your notes.
- ContextBench's gold context is human-annotated and verified sufficient for the task. A transcript has none, so replay treats the files a session edited as gold. Replay Recall, Precision and AUC-Cov are proxies for finding the files the session changed. Redundancy needs no gold context. Evidence Drop needs a definition of utilized context that we do not have, so it is not reported.
- Replay advice precision and recall say that waste followed a suggestion. They do not say the agent acted on it or saved tokens.
- A passing harness audit shows a component exists. The ablation literature shows that its effect varies. To test an effect, run the same task with and without the component and compare cost and outcome.
- The grounding check catches invented figures. It cannot tell whether grounded advice was useful. That needs human judgement or a task-level experiment, such as resolution rate with and without advice, as LivePlan runs.

`--repeat` only catches nondeterminism inside the evaluators. A stable result still says nothing about quality beyond what the planted ground truth covers.

## Extend it

Add a memory case in `internal/evals/retrieval.json` when retrieval misses something that mattered. Add a planted loop or waste scenario in `internal/cully/eval.go` when the advisor misses a pattern or raises a false alarm. Plant the answer in the fixture first, so the eval can fail.
