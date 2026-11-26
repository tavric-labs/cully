# Dashboard UI and UX improvement plan

Status: proposal for review. Nothing here is built yet.
Written after reviewing every Dashboard panel on the live server (port 7778).

## What you told us the dashboard must be

1. **No internals.** Nothing a user cannot act on: no hook names, no `cully_*` tool names, no CLI instructions where a button exists.
2. **Show better, not more.** Curated facts that help a decision, with detail one click away.
3. **Visual, not dumped text.** Numbers, bars and timelines; no monospace walls.
4. **Open a session and see what it did.**
5. **Keep the Cully look and the good parts.** You liked Background jobs' empty state; it stays as the model for empty states.

## Diagnosis: what is wrong today

| Area | What I saw | Why it hurts |
| --- | --- | --- |
| Navigation | Ten flat items with equal weight and two-line blurbs. Landing on Dashboard drops you into Advisor. | Live agent work (Advisor, Scout, Where it left off) is mixed with project history (Timeline, Replay, Savings) and system settings (Jobs). No "what needs me" view. |
| Status | Paragraphs such as "Stop/analyze ✓", "SessionEnd/cleanup ✓", `mcp: atlassian cap-verify … gerrit gitlab tgit`, `skills: .system .trash …`, "cully advisor daemon not running: rerun cully setup". | Pure implementation detail. The one real answer (is my setup healthy?) is buried. It also lists company tool names by default. |
| Timeline | One sentence in a monospace box: "Run your agent with `cully run AGENT`". | Raw CLI text in a UI that has its own Start button. |
| Savings | "CULLY IMPACT (last 14 days, 1 sessions)" then plain lines. | No hierarchy or visual, an ALL-CAPS title, "1 sessions", unexplained terms such as "Analyses avoided". |
| Replay, Terminal tips, Jobs | Mostly empty for most people. | Half the navigation leads to empty screens. |
| Folder | A folder picker appears only with two or more folders. | You cannot tell which folder a panel describes. |
| Consistency | Some empty states are cards, some raw text. "Copy" appears on panels where it is unclear what it copies. Title case and sentence case are mixed. | The UI feels like several tools. |

## Proposed structure

Cut ten items to six, in three groups, with a new landing page.

```
Dashboard
  Overview            new: what needs you, at a glance
  ─ Work
    Advisor           advice, per agent, send to agent
    Scout             live searches and findings
    Activity          Timeline and Replay merged (one view, two modes)
  ─ Health
    Setup             was Status; a checklist, not paragraphs
    Savings           what Cully saved and cost
  More
    Background jobs   unchanged (keeps its empty state)
```

Folded in: Where it left off becomes the first tab of **Activity** for a folder or session. The stuck-session check becomes a banner on Overview when something is stuck, not a page. Terminal tips become a source tab inside Advisor.

## Page designs

### Overview (new landing)

```
Good morning. 2 things need you.
┌───────────────────────────────────────────────────────────────┐
│ ● Fix flaky gateway test     waiting for you      [Open]      │
│ ✦ 3 pieces of advice ready for "Fix flaky gateway test" [Review]│
│ 🔒 Cully MCP needs sign-in (Claude)               [Sign in]   │
└───────────────────────────────────────────────────────────────┘
Open now            4 sessions in 2 folders          [See sessions]
This week           Saved ≈ 2.7k tokens   ▂▃▅▇▆▃▂      [Details]
Setup               All set                           [Details]
```

Every row is a decision or a shortcut. Nothing appears that you cannot act on. Empty means "Nothing needs you."

### Setup (was Status)

A checklist per agent you use, not per internal part.

```
Claude    ● Ready      Session tracking  ✓   Advisor  ✓   Memory  ⚠ needs sign-in [Sign in]
Codex     ● Ready      ...
Cursor    ● Ready      Terminal signals  ✗   [Fix]
Details ▸   (hook names, tools, skills live here, collapsed; tool names hidden by default)
```

Plain words, one button per problem, internals collapsed.

### Activity (Timeline plus Replay)

A vertical timeline grouped by turn, with icons for edit, read, command and check. File chips link to the diff, checks show pass or fail, and failures are expanded. A "Replay" toggle plays it back. Filters: files, commands, checks. Empty state: "No activity yet" with **Start an agent**.

### Savings

No cards. One hero sentence and one bar.

```
Cully saved about 2.7k tokens in the last 14 days            [7d] [14d] [30d]
saved ██████████████████████░░░░  cost  0
Memory lookups 0    Analyses avoided 1    Loops caught 0    Oversized output 0
How this is measured ▸
```

Plain-language labels, an explainer in the disclosure, a day-by-day sparkline once there is data.

### Where it left off

Group the 37-file list by folder with a +/− bar: `internal/cully  24 ████████`, `docs  3`, `cmd  2`, `Local settings  3`. The files are one click away. End with the checks result and a next-step button.

### Session detail (open a session and see what it did)

Click a session on the Sessions tab. Show **what it did, not what was said**: tools used, files touched, commands by program, checks passed or failed, busy or idle, uncommitted files, last commit, recorded history. Claude and Codex come from their local transcripts, reduced to actions and relative paths; Cursor shows "not available yet". Buttons: **Continue in new agent**, **View history**.

### Advisor and Scout polish

- Advisor: filter Open, Sent, Dismissed; sort by severity; confirm after Send with an undo note; Terminal tips as a second tab.
- Scout: a timeline of searches, a deduplicated sources list, and an indicator showing which findings went into the agent's prompt.

## Cross-cutting UX

- **One folder switcher in the header** (name, branch, remote). It replaces the old repo count, and every panel states which folder it shows.
- **Four shared components** used everywhere: empty state (title, one sentence, one action), loading skeleton, error with a fix, and toolbar. Retire the raw `pre` blocks.
- **Copy and labels:** sentence case; buttons name the result ("Copy handoff for another agent", not "Copy"); no internal names; no CLI instruction where a button can do it.
- **Live updates** over server events instead of polling for advice, scout and sessions; a tab title and badge count for "needs you".
- **Keyboard:** `g` then `o`, `a`, `s` to jump; `?` for help; focus order and visible focus checked in all three themes.
- **Accessibility and responsive:** AA contrast in Cully, Harbor and Graphite; reduced motion respected; status always has text or an icon, never color alone; phone layout for Overview.
- **Privacy and boundaries:** hide-folders applies to every panel including Activity; tool and skill names are collapsed by default; activity views never show prompts or file contents.

## Phases

| Phase | Scope | Size | Done when |
| --- | --- | --- | --- |
| 0 Quick wins | Rewrite Status, Timeline, Replay, Terminal tips and Savings as friendly views using the shared empty, loading and error components. Hide internals. Group the handoff file list. | 1–2 days | No panel shows raw CLI text; every empty state offers an action. |
| 1 Structure | Overview landing, six-item navigation, folder switcher, consistent toolbar and copy. | 2–3 days | Opening Dashboard answers "what needs me" in five seconds. |
| 2 Session detail | Activity view from Claude and Codex transcripts, richer session cards (busy or idle, uncommitted work, last commit), Continue in new agent. | 3–4 days | Click a session and see what it did without leaving the page. |
| 3 Polish | Live updates, keyboard shortcuts, accessibility and responsive pass, skeletons, notifications. | 2–3 days | Contrast, focus and phone checks pass in all themes. |

Each phase ships on its own, test first, with screenshots of every changed screen in all three themes.

## Risks

- **Transcript parsing** differs per agent. Claude and Codex are straightforward; Cursor stores chats in SQLite, so it ships later.
- **Which transcript belongs to which process** is a guess when two sessions share a folder; the page will say so.
- **Process detection** is Linux-first; macOS is best effort.
- **Merging panels** changes URLs; keep old links redirecting.

## Decisions taken (you delegated them)

1. Overview is the landing page for the Dashboard.
2. Activity shows actions only (files, commands, checks), never prompts or replies.
3. Replay folds into Activity (playback not built yet), Terminal tips become a tab in Advisor, the stuck-session check is part of Overview; old links redirect.
4. Order: Phase 0 and 1 first, then session detail.

## Progress

Done: Overview, six-item navigation with groups, folder switcher in the header, Setup checklist with Enable buttons, Savings visual, Activity timeline with grouped files, notifications, start-a-scout, jobs list with Run now, sessions filtered to the agents in use.
Not done: Session detail (open a session and see what it did), richer session cards (busy or idle, uncommitted work, last commit), replay playback, live updates over server events, keyboard shortcuts, the accessibility and responsive pass.

## Original questions (answered above)

1. **Overview as the landing page,** replacing Advisor as the default?
2. **Activity view:** actions only (my recommendation, matching Cully's privacy line), or also the agent's last reply?
3. **Cut or keep** Replay and Terminal tips as separate pages, or fold them as proposed?
4. **Order:** start with Phase 0 plus 1, or with Session detail first?


## Continuing a session: decision after review

The advisor nudge "continue in the session you already have open" is the wrong main path. It spends a rate-limited model call on what is a deterministic fact, and it shows up on a surface people do not look at when they want to continue.

What the agents really offer: native **resume** (`claude --resume`, `codex resume`, `cursor-agent --resume`) and **fork** (`claude --fork-session`, `codex fork`). Nothing can write into a session already running in another terminal, and nothing can merge two sessions' histories.

Design:

1. **Sessions gets a Recent list** (last 7 days, "Show older" adds more) for the agent in use. Each row offers, in this order:
   - **Fork**: a new session that starts from that session's history; the original stays untouched. This is the default when the session is open in a terminal right now, because resuming it would write to the same session twice.
   - **Resume**: continue the same session. Disabled while it is open in a terminal.
   - **Copy handoff**: a short summary to paste into another session or another agent. This is the honest replacement for "merge"; it loses detail, so it is the fallback.
2. **Claude** sessions resume or fork from the page, as an agent the page runs. **Codex and Cursor** sessions show the exact command with a Copy button, because the page cannot drive them.
3. The advisor keeps judgment advice (what to do next). The "copy this session's handoff" card stays only as a low-priority fallback when another session overlaps. A plain "You left work in this folder" prompt on Overview comes from facts, not the model.
4. Titles come from the agent's own session title when it records one; otherwise the row shows the folder and time and no prose.
