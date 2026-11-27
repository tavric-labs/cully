# The research behind Cully

Cully reads your coding-agent sessions and proposes agents, background jobs, rules and skills. A proposal should rest on something better than taste, so this page lists what each behaviour rests on, what the source actually says, and where the support is thin or the choice is Cully's own.

**How to read the evidence.** Sources are of three kinds. *Paper* means a preprint or a peer-reviewed paper. *Vendor* means an engineering post by a company that sells the tool; it is practice, not independent evidence. *Practice* means a write-up by a team describing its own system. Most papers here are preprints. Where a claim comes from a vendor or practice post, the table says so.

**What was checked.** These sources were found and summarised in November 2026 from search results and abstracts. Their full texts were not all read. Before you quote a figure, open the source. Anything marked *unverified* below I could not confirm.

## What each Cully behaviour rests on

| What Cully does | What it rests on | Strength |
| --- | --- | --- |
| Reads transcripts read-only and keeps counts, not content | Tool output is most of what an agent reads (about 84 percent of an average turn in one study, arXiv 2508.21433) and much of it is redundant or expired (arXiv 2509.23586). Privacy is Cully's own rule, not a research result. | Paper (preliminary, small samples) |
| Sends a model a short digest, not a transcript | Quality falls as input grows and unevenly so (Chroma, *Context Rot*, 18 models; Liu et al., arXiv 2307.03172); Anthropic frames context as a finite budget to spend on the smallest set of useful tokens. | Paper plus vendor |
| Counts first, asks a model second, and grounds a verdict in counts | LLM judges agree with people over 80 percent of the time but show position, length and self-preference bias (Zheng et al., arXiv 2306.05685). Anthropic advises reading transcripts to validate graders. | Paper plus vendor |
| Flags the same call repeating, and long failing runs | Repeated identical actions snowball in SWE-agent trajectories (arXiv 2405.15793); endless repetition and failed localisation recur (arXiv 2504.21798); failed runs are longer and more variable but length alone is ambiguous (arXiv 2511.00197, arXiv 2604.02547). | Paper |
| Flags edits with no check afterwards | Task verification is one of three failure clusters in 1,600+ multi-agent traces (MAST, arXiv 2503.13657); agents tend to rate their own work too well (Anthropic, *Harness design for long-running apps*). | Paper plus vendor |
| Shows cache reads apart from fresh input and output | Input tokens drive agent cost, runs of one task vary up to 30 times, and more tokens do not mean more accuracy (arXiv 2604.22750). Cache reads are priced at a fraction of fresh input (Anthropic prompt caching docs; the multiplier differs by model, so check the pricing page). | Paper plus vendor docs |
| Estimates tokens by kind of work | The split of a turn's fresh input and output across its tool calls is **Cully's own estimate**, not a measured attribution: Claude reports usage once per turn. It is labelled as an estimate on the page. | Heuristic |
| Splits a session into steps and tells each step's story | Anthropic: transcripts record what the agent said and did, and the outcome is the final state; read both. A session often holds several jobs, and the research on trajectories analyses them per step. The per-step format is Cully's design. | Partly supported |
| Proposes the simplest tool first: a job, then a rule, a skill, an agent | Anthropic: find the simplest solution and add complexity only when needed (*Building effective agents*). A job runs one command with no model, so it costs no tokens. | Vendor |
| Keeps rules minimal and asks before writing them | Repository context files lowered agent success and raised cost by more than 20 percent in one study; model-written files hurt, short human-written files helped slightly (arXiv 2602.11988). Cully drafts a rule only from an observed failure and never saves one unapproved. | Paper (one study; the abstract and the body frame the effect differently) |
| Ships skills with only a short description always loaded | Skills load a name and description at start and the body only when needed (Anthropic, *Equipping agents for the real world with Agent Skills*). | Vendor |
| The handoff note, and starting a fresh session from it | A progress file beside version history carried long-running work across context windows (Anthropic, *Effective harnesses for long-running agents*); a fresh context with a structured handoff can beat compaction (Anthropic, *Harness design for long-running apps*); evolving a context playbook beats rewriting it whole (ACE, arXiv 2510.04618, ICLR 2026). | Vendor plus paper |
| Memory by meaning (Mem0 and PostgreSQL) | Mem0 reports large token and latency savings over full context on the LOCOMO benchmark (arXiv 2504.19413). These are the vendor's own measurements. | Paper by the vendor |

## Software factory: why Factory mode exists

A software factory is a setup where people state the intent and the checks and agents do the building. The sources agree on a few points, and Factory mode (a choice beside Edit and Plan only when you start a task) turns them on together:

| Factory idea | Source | In Cully |
| --- | --- | --- |
| People write the spec and the scenarios; agents implement | StrongDM's Software Factory (practice); Factory, *Agent Native Development* (vendor); the SE 3.0 and SASE roadmaps (arXiv 2410.06107, arXiv 2509.06216) | You describe the goal and the acceptance scenarios; the brief tells the agent to write a spec and plan small tasks first. |
| Scenarios are held back from the builder, like a holdout set, so the agent cannot write to the test | StrongDM (practice). Critics ask who can trust code that agents both build and test (Stanford Law commentary), so this is a design choice, not a proven result. | Your scenarios are never sent to the agent. A reviewer holds them and says only what is missing. |
| Verification before done, by something other than the builder | Anthropic's planner, generator and evaluator split (vendor); MAST (paper) | The keep-going loop judges the work against your scenarios; an approved agent named "Independent reviewer" reviews the finished work. |
| Specify, plan, implement, validate, in small tasks | Spec-Driven Development (arXiv 2602.00180, a practitioner guide); Qodo and Thoughtworks write-ups (practice) | The brief asks for exactly that order and one commit per task. |
| The repository is the system of record; the agent map is short | OpenAI, *Harness engineering* (vendor) | The `cully-repo-map` skill keeps the map under about 100 lines and points to docs. |
| Humans review traces and outcomes, not every line | Factory and OpenAI (vendor) | The task page shows the graph and every tool call with its parameters. |

**What the factory sources do not show.** Almost none of this is independent evidence. StrongDM, Factory and OpenAI describe their own systems; the "no human reads the code" part drew the most scepticism and Cully does not adopt it: you still approve the result. Spec drift (the spec and the code parting ways) and instruction bloat are named failure modes of spec-driven work, which is why the skills are short and say to update the spec with the code.

## What ships with setup

- **Skills** installed for Claude, Codex and Cursor: `cully` (memory), `cully-setup`, `cully-sessions` (how to read each harness's sessions and what to capture), `cully-spec`, `cully-verify`, `cully-handoff`, `cully-repo-map`. A copy you edit is never overwritten or removed.
- **Ready-made agents**, offered before there is any history, each read only where it can be: Independent reviewer, Handoff keeper, Loop breaker.
- **A ready-made background job**: the folder's own test command at a quiet hour, which uses no tokens.
- Each card shows its sources. Nothing is created until you review and approve it.

## What is Cully's own choice, with no source behind it

Say this plainly so no one mistakes a number for a finding:

- The thresholds that count as repeated work (six sessions on three different days in one folder), and the 30-day window.
- The health score's weights and the exact tolerances for repeated calls or untested edits. The signals follow the research above; the numbers do not come from a study. The code comment that credits "Harness Engineering, arXiv 2609.00006" is **unverified**: only a secondary glossary links that number to a paper, so treat that citation as unconfirmed.
- The per-kind token split.
- Titling a task from its latest analysed session.

## Where each harness keeps sessions

Claude Code documents its own location and retention (30 days by default, then deleted at startup; check `cleanupPeriodDays`). Codex and Cursor layouts are not documented by their vendors; they come from users' reports and from Cully's own scanners, so they can change. See the `cully-sessions` skill for the paths and what each file does and does not record.

## Sources

Papers: [MAST, arXiv 2503.13657](https://arxiv.org/abs/2503.13657) · [SWE-agent, 2405.15793](https://arxiv.org/abs/2405.15793) · [SWE-smith, 2504.21798](https://arxiv.org/abs/2504.21798) · [trajectory study, 2511.00197](https://arxiv.org/abs/2511.00197) · [trajectory length, 2604.02547](https://arxiv.org/abs/2604.02547) · [token cost, 2604.22750](https://arxiv.org/abs/2604.22750) · [AgentDiet, 2509.23586](https://arxiv.org/abs/2509.23586) · [context management, 2508.21433](https://arxiv.org/abs/2508.21433) · [Lost in the Middle, 2307.03172](https://arxiv.org/abs/2307.03172) · [LLM-as-a-Judge, 2306.05685](https://arxiv.org/abs/2306.05685) · [Evaluating AGENTS.md, 2602.11988](https://arxiv.org/abs/2602.11988) · [ACE, 2510.04618](https://arxiv.org/abs/2510.04618) · [Mem0, 2504.19413](https://arxiv.org/abs/2504.19413) · [Spec-Driven Development, 2602.00180](https://arxiv.org/abs/2602.00180) · [SE 3.0, 2410.06107](https://arxiv.org/abs/2410.06107) · [SASE, 2509.06216](https://arxiv.org/abs/2509.06216)

Vendor and practice: [Anthropic, context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) · [long-running harnesses](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents) · [harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps) · [evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents) · [building effective agents](https://www.anthropic.com/engineering/building-effective-agents) · [Agent Skills](https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills) · [prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) · [Claude Code, .claude directory](https://code.claude.com/docs/en/claude-directory) · [OpenAI, harness engineering](https://openai.com/index/harness-engineering/) · [StrongDM Software Factory](https://factory.strongdm.ai/) · [Factory, what is a software factory](https://factory.ai/articles/what-is-a-software-factory) · [Chroma, Context Rot](https://research.trychroma.com/context-rot)
