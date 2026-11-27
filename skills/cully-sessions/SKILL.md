---
name: cully-sessions
description: Read and analyse past coding-agent sessions from Claude Code, Codex and Cursor (where each stores them, how to parse them, what to capture and what never to), then title and summarise sessions, find repeated work and failure patterns, and propose ready-made agents, background jobs or minimal rules from the evidence. Use when the user asks to analyse, summarise, title or compare sessions, to find what they keep repeating or where tokens went, or to decide whether work needs an agent or a background job.
---

<!-- cully:skill:managed -->
# Analyse coding-agent sessions

Cully's session library, titles, health scores and suggestions all come from reading the transcripts the harnesses already keep. This skill is how to do that reading yourself, safely and cheaply, and what to conclude from it. Every rule below names the source it rests on; `docs/research.md` in the Cully repository has the full list and says where a source is weak or Cully's own choice.

## Rules that keep this safe

- **Read only.** Never edit, move or delete a transcript. Codex's `resume` relies on its rollout files, and Cursor's sidebar index lives apart from its transcript files, so changing either breaks the harness.
- **Capture facts, not content.** Keep counts, tool names, exit status, file paths relative to the project, and a hash of a repeated call. Do not store prompts, replies, file contents, tool output or any credential. Redact anything shaped like a key before it reaches a summary.
- **Facts first, judgement second.** Count things with code; ask a model only for what counting cannot say (what the work was, whether it finished). Judges agree with people most of the time but favour long and first-position answers, and rate their own model's output higher, so ground a verdict in measured facts and check a sample by reading the transcript (Zheng et al. 2023, arXiv 2306.05685; Anthropic, *Demystifying evals for AI agents*).
- **Send a digest, not the transcript.** Tool output is most of what an agent reads: about 84 percent of an average turn in one study, and much of it is redundant or expired (arXiv 2508.21433; AgentDiet, arXiv 2509.23586). Give a judge the first request, the latest request, the last two replies, the files changed and the counts. More text does not make it more accurate, and quality falls as input grows (Chroma, *Context Rot*; Liu et al., *Lost in the Middle*, arXiv 2307.03172).

## Where each harness keeps sessions

| Harness | Location | Shape |
| --- | --- | --- |
| Claude Code | `~/.claude/projects/<folder>/<session-id>.jsonl`. The folder name is the working directory's path with separators turned into hyphens. | One JSON object per line. `user` and `assistant` records carry `message.content` (text, `tool_use`, `tool_result` blocks), a timestamp and `cwd`. Usage is on the assistant message. |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl`. `CODEX_HOME` moves the root. | One envelope per line: `{timestamp, type, payload}`. The first line is `session_meta`. Messages, `function_call` and `custom_tool_call` items, and their outputs sit in the payload. Exit codes appear in command output text. |
| Cursor | `~/.cursor/projects/<folder>/agent-transcripts/<id>/<id>.jsonl`. Older chats may be flat `.txt` files. Delegated runs sit under a `subagents/` folder. | Requests and tool calls only: no timestamps and no tool results, so time worked and tool errors are unavailable. Chat metadata is in a separate `state.vscdb`, so a transcript can exist that the sidebar does not list. |

Sources: Claude Code docs, *The .claude directory* (code.claude.com/docs/en/claude-directory); Codex session and rollout discussion (github.com/openai/codex/discussions/3827); Cursor forum threads on `agent-transcripts`. The Codex and Cursor layouts are documented by users and by Cully's own scanners, not by the vendors, so check a real file before trusting a field name and expect them to change.

**Retention.** Claude Code deletes session files older than `cleanupPeriodDays` at startup; the documented default is 30 days. If the user wants history beyond a month for analysis, tell them to raise it in `~/.claude/settings.json` themselves. Do not change it for them. A reported bug makes `0` stop transcripts being written at all, so never suggest `0`.

## What to capture per session

Measured, by counting:

| Fact | Why it is worth having |
| --- | --- |
| Requests the person made, in order | Separates one job from several; a session often holds more than one (see steps below). |
| Tool calls by name, grouped as file edit, file read or search, shell, web, subagent, or MCP server | Shows where the work and the tokens went. |
| Tool errors, and the longest run of an identical call (name plus arguments hashed) | Repeated identical actions snowball into failure in agent trajectories (SWE-agent, arXiv 2405.15793; SWE-smith, arXiv 2504.21798). |
| Files edited, and whether a test or check ran after the last edit | Weak verification is a main failure family in multi-agent traces (MAST, arXiv 2503.13657) and agents tend to rate their own work too well (Anthropic, *Harness design for long-running apps*). |
| Active time and tool-call count | Failed runs are longer and more variable than successful ones, but length alone is ambiguous (arXiv 2511.00197; arXiv 2604.02547). |
| Tokens: fresh input, cache read, cache write, output | Input drives cost, runs of the same task vary up to 30 times in tokens, and more tokens do not mean more accuracy (arXiv 2604.22750). A cache read costs a fraction of fresh input, so report them apart (Anthropic prompt caching docs). |

Judged, by one model call over a digest:

- **Title:** 3 to 9 words naming the action and the deliverable, never generic, no secrets or links.
- **Summary:** four to six sentences: what was built, found or decided; what was verified and how; what failed; what remains.
- **Steps:** one line per separate job the person asked for, each with a status of done, partly or open and a one or two sentence story. A follow-up that refines the same job belongs to that step.
- **Verdict:** finished (yes, partly, no, unclear), on task or drifted, and up to three checks that would show the goal was met.

Distinguish planned work and claimed results from confirmed outcomes. A reply that says "tests pass" with no test run in the record is a claim.

## From sessions to a proposal

Use the plainest tool that does the job. Anthropic's advice is to find the simplest solution and add complexity only when needed (*Building effective agents*), so in this order:

1. **A background job** when one fixed command does the chore (run the tests, a lint, a build). It uses no model and no tokens.
2. **A minimal rule** when one observed failure keeps recurring. In one study, repository context files lowered task success and raised cost by more than 20 percent; the model-written ones hurt, and short human-written ones helped slightly (arXiv 2602.11988). Draft only what a real failure justifies, in a line or two, and let the person approve it.
3. **A skill** when the agent needs steps it can load on demand. Only the name and description are always loaded, so an idle skill is cheap (Anthropic, *Equipping agents for the real world with Agent Skills*).
4. **An agent** only for work that needs reading and judgement and that repeats. Count it: the same kind of goal in the same folder at least six times on at least three different days is Cully's own bar, a heuristic and not a published figure. Never propose from one-shot advisor calls (one request, no tools); they are tooling, not work a person repeats.

If an agent already covers that folder or job, propose an update to it, not a second one.

## Ready-made starters worth offering

Each is read-only or advisory, short, and tied to a documented failure. Offer them during setup even before there is history, and let the person approve each.

- **Independent reviewer.** After a change, runs the project's own checks and reports what is proven and what is only claimed. A separate evaluator addresses self-assessment bias (Anthropic, *Harness design for long-running apps*; MAST).
- **Handoff keeper.** At a milestone, writes a short progress note and a next step so a fresh session can start from the state of the work. Progress files with version history carry work across context windows, and notes outside the window are a named context technique (Anthropic, *Effective harnesses for long-running agents*; *Effective context engineering*; ACE, arXiv 2510.04618).
- **Loop breaker.** Reads sessions where the same call failed three or more times and writes the one change that would have stopped it (arXiv 2405.15793; arXiv 2504.21798).
- **Nightly checks** as a background job: the repository's test command at a quiet hour.

## Report back

State what you read (which harness, how many sessions, which period), what you counted, what you judged, and what you could not see (for example Cursor has no results or timestamps). End with proposals in the order above and the evidence for each, never an action taken without the person's yes.
