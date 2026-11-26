# Invoking an agent from Cully, and saving tokens

Status: findings and a plan. Written 2026-11-26 after reading arXiv 2609.00006 ("Harness Engineering", 83 pages, all of it by three readers) and other agent-design work. Only token capture is built so far.

## What the research says

Cully is what the paper calls an orchestrator: it wraps an existing harness (Claude Code) from outside and implements no editing loop of its own. So its job is what happens before the first model call, around each turn, and after the last one.

From "Harness Engineering" (Sections 6, 7, 10, 11, 14, 16):

- **Start simple, add only what a failure demands (R1, R3, R18).** A 50-line loop with one bash tool scores as well as elaborate loops on benchmarks. Ship the cheap caps (a dozen lines), not elaborate stuck detection.
- **Outer verification loop.** OpenHands' `/goal` judge and Hermes' verify-on-stop guard sit outside the turn loop: if files changed and no fresh test evidence exists, turn a final answer into a continuation. This is what Cully's "Keep going until checked" does.
- **Budgets, with a grace call.** Hermes: 90 iterations plus a final summarising call. Gemini CLI: turn cap 100, five identical calls abort. OpenCode: three identical calls raise a prompt. Hermes warns at 2 identical failures and halts at 8. Cully's 200 calls, warn at 3 and stop at 8 are in line; a final "summarise where you got to" call at the limit is the thing to add.
- **Static prefix first, dynamic context after (Section 7.2).** Every system splits the prompt into a stable cached prefix and a per-turn suffix. Hermes builds a bit-identical prefix and freezes its memory snapshot so mid-session writes do not break the cache.
- **Discover context files, do not embed code (R6, R8, R16).** None of eleven systems uses vector retrieval for code or as primary memory. Hermes uses lexical full-text search.
- **Mark injected context as data (Section 7.2).** OpenHands wraps repo context in untrusted-content tags; Mistral Vibe ranks external data lowest.
- **Deferred tool loading past about 15 tools (R4).** Claude Code's tool search cuts the initial prompt by about 40 percent; Hermes collapses large MCP catalogs once their schemas would pass 10 percent of the context window.
- **Compaction:** threshold at a fixed buffer, keep a verbatim recent tail, merge summaries incrementally (R7). An empirical study (arXiv 2609.20804, abstract only) found rule-based elision before LLM summarisation is the most efficient.
- **Single agent for coding (R12).** Multi-agent costs about 15 times the tokens of chat and coding has few parallel subtasks. Anthropic's own multi-agent post says it is a poor fit for coding. Use sub-agents for read-heavy exploration and independent review.
- **Headless means pre-decided permissions.** Background agents in Claude Code get rules and a classifier only; OpenCode auto-rejects prompts in headless runs. Cully's allow and block lists, now editable, are the right shape; put policy in data, not prose (R11).
- **Verification ranking.** Executable checks first, an end-to-end smoke run next, an LLM judge last. The Agent SDK post calls the judge "generally not a very robust method" and SWE-Critix (abstract only) found untrained judging of long trajectories close to chance. Use a different prompt from the worker and spot-check against human labels.
- **Evals:** grade the outcome, use transcripts for diagnosis, report pass^k, start every trial from a clean worktree, retire saturated cases (Anthropic, "Demystifying evals").
- **Long-running work:** a progress file plus git log read at the start of every session, one feature per session, a JSON feature list that starts all failing (Anthropic, "Effective harnesses for long-running agents").
- **Measure harness changes** by predicting the effect then checking it on your own sessions (arXiv 2604.25850, abstract only).

Caveats: the paper reads source code and measures nothing at run time; benchmark numbers in it are self-reported; several 2026 papers were read at abstract level only.

## How Cully should invoke an agent

1. **Intake.** State the goal and "done when" as checkable lines (already in the composer). Choose a tool profile and budget.
2. **Brief, not dump.** One short block: task, acceptance checks, how to build and test, the project's own instruction files, and pointers to the journal and memory instead of their contents. The agent fetches more only when it needs it.
3. **Stable prefix.** Put Cully's rules first and keep them byte-identical between tasks; put the task and context after.
4. **Plan only when needed.** Multi-file or ambiguous work gets a plan-first turn; small fixes do not.
5. **Run in a worktree** with small commits and a progress note.
6. **Verify.** Tests and lint first, then the judge (already built), then a different-prompt review.
7. **Hand off.** A short record of what was done, what was not, and how to resume.

## Saving tokens

| Step | What | Status |
| --- | --- | --- |
| 1 | Capture what each task really uses: input, cache reads, output, and the share served from the prompt cache. A cache read costs about a tenth of fresh input, so the cached share is the measurable saving. | Built: shown on each task |
| 2 | Load only the MCP servers a task needs. Each connected server adds tool schemas to every turn; a machine with a dozen servers pays for all of them. Pass a filtered MCP config for UI-run tasks, chosen on the Agent permissions page. | Not built |
| 3 | Keep the prompt prefix stable so the cache hits, and put variable text after it. | Not built |
| 4 | Journal and memory as pointers the agent follows on demand, not text pasted in. | Not built |
| 5 | Run Cully's own background work (naming, judging, coaching, scouting) on a cheaper model. | Not built |
| 6 | Elide old tool output by rule before any summarising; a final summarising call at the budget. | Not built |
| 7 | Report savings over time: cached share per task, per week, and what a plain run of the same kind of task used. | Not built |

Next: step 2, because it is the largest saving and independent of the others.
