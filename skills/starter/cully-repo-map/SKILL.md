---
name: cully-repo-map
description: Keep a repository legible to agents with a short map file that points to where things are documented, and a few mechanical checks that keep it current. Use when starting in an unfamiliar repository, when asked to write or improve AGENTS.md or CLAUDE.md, or when agents keep re-discovering the same facts about a project.
---

<!-- cully:skill:managed -->
# A map, not an encyclopedia

OpenAI's team that built a product with Codex kept a short AGENTS.md of roughly a hundred lines that works as a table of contents, with the real knowledge in a structured `docs/` folder that the repository treats as its system of record, and with linters and CI keeping it current (OpenAI, *Harness engineering*). A study of repository context files found they often made agents less successful and raised cost by more than 20 percent; model-written ones hurt most and short human-written ones helped slightly (arXiv 2602.11988). So: short, human-approved, and only what the agent cannot work out alone.

## Write or review the map

1. **Read the repository first.** Do not describe what the code already shows.
2. **Keep only what an agent cannot discover**: how to run the tests and the linter, the one build quirk, a rule that is easy to break, where the deeper docs live.
3. **Point, do not paste.** One line per pointer: "Architecture: docs/architecture.md".
4. **Aim for under about 100 lines.** If you are over, move detail into `docs/` and link it.
5. **Show the person the diff and get a yes** before saving. Never write it unasked.

## Keep it true

- A link or command in the map that no longer works is worse than nothing. Prefer a small check in CI that fails on a dead link or a command that does not run.
- When an agent keeps asking or failing on the same thing, that is the signal to add one line, not before.
- Remove a line when the code makes it unnecessary.

## Do not

- Generate a long file from the code and commit it. That is the case the study found harmful.
- Fill it with style advice a formatter already enforces.
