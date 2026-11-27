---
name: cully-handoff
description: Write a short progress note at a milestone, before the context fills, or when stopping, so a fresh session can start from the real state of the work. Use when a task is long, when context is running low, when the user says to hand off or continue later, or at the end of a working block.
---

<!-- cully:skill:managed -->
# Leave the next session a way in

Work that spans context windows fails when each session starts blind: one long-running experiment found agents ran out of context mid-feature and left the next session with something half-built and undocumented. What helped was a progress file kept beside the version history (Anthropic, *Effective harnesses for long-running agents*). Notes kept outside the window are a named context technique (Anthropic, *Effective context engineering for AI agents*), and a fresh context with a structured handoff can beat compacting a long one (Anthropic, *Harness design for long-running apps*).

## Write it at a milestone, not at the end

A milestone is a feature that works, a decision made, or the point just before something risky. Do not wait for the limit; you may not get the chance.

## What goes in it (ten lines or fewer)

- **Goal**: one sentence.
- **State**: what works now, what is half-done, what is broken. Name files.
- **Decisions**: what was chosen and the reason, so it is not reopened.
- **Next**: the single next step, concrete enough to start on.
- **Check**: the command that shows the current state.
- **Do not repeat**: what was tried and failed.

## Where

Put it where the next session will look first and where it is versioned: a `PROGRESS.md` (or the project's existing notes file) committed with the work. If Cully's memory is connected, also save it with `cully_log` so a session in another harness finds it. Never put a credential, a prompt transcript or file contents in it.

## Do not

- Write a diary. If it would not change what the next session does, leave it out.
- Leave the state ambiguous. "Mostly working" is not a state; say what works.
