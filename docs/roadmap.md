# Roadmap

Cully has two modes: [Solo](/solo), the local harness runtime for your coding agents, and [Team](/team-workflows), a shared workspace for project tasks, handoffs, review and lessons. See [architecture](architecture.md) and [current capabilities](capabilities.md) for the implementation.

[Product direction](product-direction.md) describes feature proposals in detail, with what each reuses and what is missing. This page lists engineering work.

## Team workflow and next gates

The [manual-launch workflow](/team-workflows) implements project roles, atomic claims, task lifecycle/dependencies, board/inbox, checkpoints, reported evidence review, project lesson publication and playbook adoption. Private memory remains separate. Tests cover concurrent claims, access removal, private drafts, stale versions and self-approval.

Next, run the [two-person cross-agent demo](/product-direction#demo-and-commercial-validation) with real signed-in clients, and measure handoff/review usefulness. Add web views, configurable leases/review policies, playbook maintenance, authorized semantic projections and normalized storage as needs emerge. Managed execution remains gated on adapter-enforced capabilities; ranking and comparisons require measured outcomes. See [planned team stages](/product-direction#planned-team-stages). These additions have no release dates.

## Planned work

1. Verify the [team deployment guide](team-deployment.md) with an agent sign-in, tool write and read, full-text search, and Mem0 recall.
2. Measure end-to-end token use, latency and continuity quality with and without `cully_context`, the terminal and the installed hooks (Cully Bench). Improve project and task identity and duplicate handling only from those results, without storing transcripts or giving the advisor an OAuth credential.
3. Add pre-tool hooks and a small rule format for guardrails that warn or ask before a risky tool call.
4. Provide a lighter local install that runs the terminal, journal, replay, handoff and rescue without the Docker memory stack.
5. Measure advisor latency and project isolation before changing its job scheduling. Coalesce repeated work and use bounded concurrency where measurements justify it.
6. Measure PostgreSQL search plans and Mem0 recall latency. Keep source-record hydration and owner checks in Cully as the system scales.
7. Consider richer Mem0 extraction only with a clear source-to-fact attribution and edit/delete contract. Current projections embed authored summaries with `infer=false`.

Changes to storage and authorization need tests for owner isolation, recovery and failure behavior. The local advisor and terminal must remain usable when the hosted service is unavailable.
