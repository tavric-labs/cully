# Token efficiency plan: make Cully save more than it costs

Audience: the engineer picking this up. Read the whole page before starting.

## Why this exists

Cully is meant to save tokens by carrying work across sessions and by stopping loops and context blow-ups. Measured on a real machine (`cully savings --days 30`, 10 editing sessions), it was costing more than it saved:

| Cost | Tokens | Cause |
| --- | --- | --- |
| Results injected into the agent | about 36k in 100 calls | `cully_log` (79 calls, about 25k) and `cully_update` (12 calls, about 6k) echoed the saved record back |
| Call arguments | about 27k | the saved summaries themselves (needed) |
| Empty lookups | small | all 3 `cully_context` calls returned no notes, so there was no benefit to offset any cost |

Savings were zero because no session had a `cully_context` hit. Today's honest answer: Cully costs more than it returns for this user.

## What is already done

1. `cully savings [--days N] [--project TEXT] [--json]` reports measured cost and an observational benefit estimate from Claude Code transcripts. It stores counts only, never transcript text. Benefit is the difference in tokens read or searched before the first edit between sessions with and without a Cully hit. It is not causal and is labelled with a confidence level.
2. `cully_log` and `cully_update` now return a compact acknowledgement (id, section, project, session ref, timestamps) instead of echoing the record. About 300 to 500 tokens saved per write. The data API and mobile clients are unchanged.

## What the research says (read the caveats)

Source: a web research pass done on 2026-10-20. Most figures are vendor claims from search snippets; primary pages were not opened. Verify before quoting anything externally.

- Memory vendors report more than 90% token reduction, but against stuffing the whole chat history into context. A coding agent does not do that. Cully's real baseline is "no memory, the agent re-explores the repo", so do not reuse those numbers.
- A preprint (arXiv 2602.11988) found repo context files raised inference cost by more than 20% on average with no clear success gain, because agents obey injected context and do extra work. Injected recall has the same risk. Treat every injected token as guilty until measured.
- CLI output filters (RTK-style) report about 56% average and up to 90% reduction on command output only. Code-graph tools report 6x to 70x on narrow slices, with weak session-level results in one field test. Do not bundle these; recommend them from the advisor and measure.
- MCP tool schemas are a known cost. Claude Code defers them when they exceed about 10% of context. Fewer tools with shorter descriptions is cheaper regardless. Cully exposes 13 tools.
- Prompt caching is prefix-based. Volatile text early in the prefix invalidates the cache for the rest of the context. Inject once at session start, in a stable order. Never add timestamps or reordered lists per turn.

## Work plan

Do these in order. Each item is one commit on `main` with a CHANGELOG entry and tests. Run `gofmt`, `go vet` and `go test -race ./...` before every commit.

1. **Baseline.** Run `cully savings --days 30 --json > baseline.json` on two or three machines. Keep the files outside Git. This is the number to beat.
2. **Shrink every read response.** `cully_context` and `cully_search` previews should be at most about 60 tokens per note: summary truncated, no tags, no echoed filters. Add a test that fails if a five-note context result exceeds a token budget (use characters divided by four).
3. **Stop empty lookups.** Make `cully_context` return a single short line when nothing matches, and update the skill text so agents skip it on trivial tasks. The goal is that wasted-on-empty tokens stay under a quarter of total cost, which `cully savings` already warns about.
4. **Audit the tool schema.** Measure the schema size, then merge or shorten tools. A candidate is folding `session`, `session_get` and `projects` into fewer tools. Keep names stable or document the migration, since installed skills reference them.
5. **Cache-safe injection.** Check what the SessionStart hook adds. It must be injected once, small, and byte-identical across turns. Add a test that the hook output is deterministic for the same input.
6. **Prove a hit saves tokens.** The estimate needs sessions with a real `cully_context` hit. Make continuation hits more likely: save better notes (task, outcome, next steps), and make `cully_context` rank by the current project and branch. Re-run `cully savings` after two weeks.
7. **Codex support for `cully savings`.** Codex rollout files under `~/.codex/sessions/` carry per-turn token counts. Add a parser behind the same report. Normalise cached tokens: OpenAI counts them inside input tokens, Anthropic counts them separately.
8. **Advisor, token-aware.** Add advisor tips that use measured data: warn when Cully calls in this session exceeded a token budget; suggest RTK or tool search settings when command output dominates; suggest a handoff only when context pressure is high and a handoff is cheaper than continuing. Each tip must say what it measured.
9. **Honest A/B.** For 20 repeatable tasks, run each with Cully on and off in randomised order, repeated, and compare total tokens, steps and pass or fail. A saving that costs correctness is not a saving. Publish the result in the docs, including if Cully loses.

## Rules for this work

- Every claim needs a measured number or a source. Say "estimated" when it is.
- Never store transcript text or credentials in a report. `cully savings` keeps counts only.
- A change that cannot show a lower token count in a test or in `cully savings` is not finished.

## Out of scope for now

Bundling RTK, graphify or context-mode; replacing mem0; changing the data API response shapes used by the mobile clients.
