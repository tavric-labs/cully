---
name: cully-spec
description: Turn a request into a short spec before any code is written: the intent, what is out of scope, observable acceptance scenarios, constraints, and a plan of small tasks. Use when a request is vague or large, when the user says "build", "add" or "design" something with several parts, or before starting work that will take more than a few edits.
---

<!-- cully:skill:managed -->
# Write the spec first

An agent that is not told an assumption will guess it. Writing the intent down first is the largest lever on whether the result is right (Factory, *Agent Native Development*), and spec-driven practice runs specify, plan, implement, validate (arXiv 2602.00180). Keep it short: instruction that grows until nobody can find what matters is a named failure, and long context files lowered agent success in one study (arXiv 2602.11988).

## Write it, in this order

1. **Intent** in one or two sentences: who gets what, and why.
2. **Out of scope**: what you will not touch. One line each.
3. **Acceptance scenarios**: two to five, each a thing a person could observe ("with an empty cart, checkout shows the empty message"). Not implementation steps. These are what "done" means, and they are what the verifier will use.
4. **Constraints**: only the ones that bind (compatibility, a file you must not change, a deadline). Skip anything the repository already enforces.
5. **Plan**: small tasks, each finishable and checkable alone. Small tasks are easier to build and to review (Qodo, *What is spec-driven development*).
6. **Open questions**: ask the person now if an answer would change the plan. Do not guess a decision that is theirs.

## Then

- Show the spec and wait for a yes unless the person told you to proceed. Changing a spec later is cheaper than changing code.
- Keep the scenarios apart from the code you write. A check the implementer wrote to its own measure proves little; scenarios held back from the implementer are how StrongDM's software factory keeps agents from gaming their tests. In Cully, tick "Keep these checks hidden from the agent" on the task.
- When the code changes the behaviour, change the spec in the same step. A spec that no longer matches the code is worse than none (spec drift).

## Do not

- Pad the spec with requirements to look thorough. Every line is something the agent will try to obey, and extra requirements make tasks harder (arXiv 2602.11988).
- Write implementation detail the agent can find by reading the code.
