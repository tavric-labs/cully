---
title: Use cases
description: "What teams use Cully for: a flight recorder for agent sessions, verification-trail review of AI-assisted work, and playbooks new engineers can replay."
---

# Use cases

Inside the Cully terminal, supported hooks record coarse events in a bounded local journal, then turns that record into a timeline, a step-by-step replay, a drift check against Git, and a handoff the next agent can continue. Three jobs teams already have map directly onto those primitives. Coverage depends on the agent’s hooks; unsupported or missing events stay unknown.

## 1. The flight recorder: postmortems for agent sessions

When a session goes wrong, nobody should have to guess what the agent did. Cully keeps a black-box recording of the session: which files were read, edited, created and deleted, which commands ran, which checks passed or failed, and where the agent started looping.

| Question | Answer |
| --- | --- |
| What happened, in order | `cully timeline` gives the forensic timeline |
| What exactly did it touch and run | `cully replay` renders the recorded steps, with available paths and command programs |
| Why is it stuck | `cully rescue` collects the evidence and the recovery steps |
| Who continues, and with what context | `cully handoff --print` summarizes recorded evidence for the next agent |

A detected loop marks its span with ⚠, so the postmortem starts where the session derailed instead of at the beginning. The recording contains file paths and command names but never prompts, file contents, arguments or output, so it reads like a build log and can be pasted into an incident thread.

## 2. Review the session, not just the diff

A diff shows what changed. It does not show whether the change was verified, whether the agent looped before landing it, or whether files changed outside the agent's record. For AI-assisted engineering, the session is part of the review.

- `cully replay` shows the verification trail: every check, passed or failed, in order.
- The reconcile section compares the journal against live `git status`. Files Git shows as changed that the journal never saw are drift, and recorded deletions that still exist are called out.
- `cully replay --html` exports one self-contained offline file that can be attached to a pull request as review evidence.

The export helps a reviewer inspect the observed verification trail. It is not tamper-proof CI attestation, a transcript or proof that every operation was captured. No extra viewer installation is required.

## 3. Playbooks: onboarding from real sessions

Strong sessions are reusable. A new engineer can replay how a shipped change was actually made: what was read first, where the edits landed, which checks ran after them. Project workflow hints encode the same knowledge live ("you usually run checks after editing in this project"), so the playbook teaches during the session, not after it.

Save the durable lessons with [project memory](/memory) so they outlive every session, and let replay carry the concrete detail memory summaries leave out.

## What ties them together

One append-only, owner-only session ledger feeds all three. The record is bounded, coarse and private by design: times, tool kinds, pass or fail, project-relative paths and command programs. See [privacy](/privacy) for the full list of what is never recorded. And every view says plainly when its data is partial, for example when hooks started late or path recording is off, so a recording is never mistaken for a complete one.
