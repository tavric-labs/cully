---
name: cully-verify
description: Verify finished work against evidence before saying it is done: run the project's own checks, quote the result, compare the change with the acceptance scenarios, and say plainly what is unproven. Use before declaring any task complete, fixed or passing, before a commit or pull request, and when asked to review or check work.
---

<!-- cully:skill:managed -->
# Verify before you say done

Agents tend to rate their own work too generously, which is why long-running setups split building from evaluating (Anthropic, *Harness design for long-running apps*), and weak verification is one of the main failure families in multi-agent traces (MAST, arXiv 2503.13657). A claim with no check behind it is a guess.

## Do this every time

1. **Find the project's own checks** (README, CONTRIBUTING, Makefile, CI config). Use those, not ones you invent.
2. **Run them after your last edit**, not before. A passing run from before the change proves nothing about it.
3. **Quote the result**: the command and the pass or fail line. If you did not run it, say so.
4. **Compare with the acceptance scenarios** if there are any. For each, say how you saw it hold: a test, a run, a screenshot, a read of the code. "Looks right" is not a way.
5. **Look at the diff** for changes you did not mean to make: files outside the task, debug output, a loosened test.
6. **Report in three parts**: proven (with evidence), claimed but unproven, not checked and why.

## Judging someone else's work

- Judge from what ran, not from how confident the write-up sounds. Model judges favour longer answers and answers from their own model (Zheng et al., arXiv 2306.05685), so read the evidence yourself.
- Read a sample of the transcript when a grade surprises you (Anthropic, *Demystifying evals for AI agents*).
- Do not edit the work you are checking. Report what is wrong and where.

## Do not

- Make a test pass by weakening the test, skipping it or special-casing its input.
- Say "should work". Say what you ran.
