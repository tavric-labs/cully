# Cully mobile: carry the work forward

Research checked 19 October 2026. This is the product rationale for the mobile
preview and its next steps. The preview is Self only; Workspace remains planned.

The home page should help a developer return to several projects without first
reconstructing yesterday. A useful visit ends with one chosen next step, the
reason behind it, and something worth remembering. Saving more notes is useful
when those notes help the next decision.

## What we learned from other products

| Reference | Observed pattern | Cully application |
| --- | --- | --- |
| [Linear Pulse](https://linear.app/docs/pulse) | Daily/weekly project summaries, focused feeds, and optional audio summaries | A short morning overview with project scope, followed by its dated evidence |
| [Linear daily workflows](https://linear.app/learn/daily-workflows) | Daily priorities and weekly review of blocked or waiting work | Put a small continuation queue above the history; keep the selected focus visible |
| [Mem Heads Up](https://get.mem.ai/use-cases/content-development) | Related saved material resurfaces in the context of current work | Let a developer recall the approach, outcome and lesson behind a proposed next step |
| [Read For You](https://www.read.ai/for-you) | Time-bounded topic summaries and a daily recap, with links back to source moments | Organize the recap around useful threads and source notes rather than a long log |

These are product patterns, not evidence that any product improves learning or
developer productivity. Cully's implementation should be tested on its own users.
Its advantage is the existing structured work evidence: project, approach,
outcome, issue, learning, next steps and resumable session references.

## The home page

Order reflects the question the user is trying to answer, not the order in which
records were saved. Keep Today, Tasks and Advisor as the main destinations and a
shared floating Ask/mic composer. Detailed evidence belongs behind a deliberate
expansion or in the lower part of the page.

| Surface | Useful answer | Evidence and interaction |
| --- | --- | --- |
| Today hero | What matters when I return? | Date, connection/model state, a concise local-model draft, and a visible personal focus |
| Continue today | Where did I leave the work? | Up to three dated saved next steps; project and blocker context; choose a local task or ask for the reasoning |
| Recall how | Why did we choose this approach? | Expand the saved approach, outcome, issue and lesson; ask a scoped follow-up through Cully MCP |
| Learn next | What is worth revisiting from my own work? | Explicitly recorded lessons, a suggested recall question, and a small practical next exercise |
| Project threads | Which project does this belong to? | Project links from the retrieved evidence, preserving its date and source |
| Yesterday / week | What changed in the period? | Dated evidence with last-24-hours / rolling-seven-day views and honest sample coverage |
| Ask Cully | Can I find the detail in my own words? | Typed or on-device voice question, read-only MCP retrieval, then a grounded local-model answer |

The first preview fetches at most three notes from each of two disjoint windows:
the last 24 hours and the preceding six days. Those six previews are a sample,
not a complete project list or activity report. A rolling 24-hour window must
not be presented as the previous calendar day. Unloaded, unavailable and empty
are different states.

## What to carry into tomorrow

Carry forward evidence that changes the next useful action:

- The last planned next step and the project it belongs to.
- A reported blocker, what was tried, and any condition needed to proceed.
- An outcome or decision rationale that prevents repeating investigation.
- A recorded lesson and the situation where it applied.
- A source date and note/session reference so the detail can be retrieved.

A handoff is historical evidence, not live task status. Say "last planned" or
"reported on" until a newer authoritative state or the user confirms it.
Do not repeatedly resurrect an old blocker as a new urgent alert. Do not infer a
deadline, priority, completion or owner from a passing mention in a note.

The current app creates local tasks only when the user chooses to. A later
durable follow-up loop needs stable source-note links, completed/dismissed
states, and conflict handling before it can claim to track commitments across
devices. Date-based summaries alone cannot provide that state.

## Learning from work

Distinguish three things: a topic appeared, a lesson was recorded, and the user
demonstrated understanding. Only the second is directly supported by a saved
`learning` field. Neither a technology name nor a completed agent task proves
the user's knowledge.

Make a useful learning card concrete: the dated lesson, why it mattered in this
project, a question to answer without rereading, and an optional small experiment.
Clearly label the question and experiment as suggestions. For example, a saved
certificate-rotation lesson could lead to a question about overlapping trust
roots; it must not become a claim that the user does not understand TLS.

Reading recommendations should use relevant primary documentation, papers or
references already supplied by the user. A future external-research capability
must verify links and separate public topic queries from private note contents.
The preview does not have a web-research tool or a scheduled learning system;
it must not invent reading URLs or claim a review has been scheduled.

## Personal and company work

Self maps to authenticated owner-scoped personal memory. A project URL on a
personal note does not authorize reading a company workspace. A future Workspace
view must use explicit team/project access and its own data source and display
scope. Switching or losing access must clear derived content and cancel in-flight
work. Private lessons must never silently become shared team guidance.

## Experience and performance

Use Cully's forest, mint and gold palette, a strong Today headline, rounded
navigation, quiet cards and a floating voice dock. Keep tap targets and text
legible; depth comes from native surfaces and restrained accents. Motion must
serve a state change and honor accessibility settings. Do not use an always
animated background or blur that makes the test phone struggle.

Show retrieved evidence first. When weights are ready, run one bounded local
reflection for a fresh foreground brief. Avoid overlapping chat and analysis;
preserve the user's draft and cancel obsolete work on disconnect/background.
Show setup, listening, transcription, model work and errors as their real states.

## Delivery and validation

The current PR implements dated bounded retrieval, local reflection, personal
tasks, a shared voice dock and the first continuation/recall/learning surfaces.
It requires updated MCP and private data services; mixed-version deployments
must refuse unsupported date windows rather than echo a filter not applied.

Later work needs separate implementation and validation: a complete paginated
project overview, source-linked durable follow-ups, scheduled recaps, verified
reading recommendations, spaced review, and an authorized Workspace client.

Evaluate whether users choose a useful next step, can find the supporting note,
and dismiss stale suggestions. Test duplicates, conflicting later outcomes,
empty/partial periods, unavailable models, access removal and cancellation.
Measure briefing latency on the Motorola and memory/energy use before setting
budgets. Measure usefulness and continuation time with counts and a comparison;
avoid arbitrary productivity or knowledge scores.
