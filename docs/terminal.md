---
title: The Cully terminal
description: Run Claude Code, Codex, Cursor or any coding agent in one terminal with a live health bar and advisor.
---

# The Cully terminal

`cully run` starts a coding agent inside a terminal that Cully owns. The agent gets a slightly smaller screen. Cully uses the rest for a health bar and the advisor, so the agent's own clear-screen and cursor codes can never erase them.

It is an observation surface, not another terminal emulator you have to learn. You use your agent exactly as before.

## Start an agent

```sh
cully run claude [ARGS...]
cully run codex  [ARGS...]
cully run cursor [ARGS...]     # starts cursor-agent
cully run AGENT  [ARGS...]     # any executable on your PATH
```

`cully claude`, `cully codex` and `cully cursor` are shortcuts. Arguments after the agent name go to the agent unchanged, for example `cully run claude --continue` or `cully run codex resume`.

An agent Cully does not know yet still runs in the terminal with the common instruments. Add a dedicated adapter later only if the agent offers more signals.

Help, version, MCP management, login and other native utility commands run directly without opening the panel. Print/exec modes and piped or redirected streams work with their original arguments, stdin, stdout, stderr and exit status. For example:

```sh
cully codex mcp list
cully codex mcp get cully
cully codex exec --json "Review this change"
cully claude --print "Explain this function"
cully cursor mcp list
```

Cully forwards native errors too: a subcommand unsupported by the installed agent remains unsupported. Set `CULLY_NO_PANE=1` to run any command directly, including a future agent utility command. Codex's `--no-alt-screen` also runs directly to preserve its native terminal mode.

Interactive panel requirements: a terminal at least 20 columns by 8 rows, macOS or Linux. Exiting the panel stops the agent's process group.

## The health bar

The first row under the agent is a one-line health bar. Every value is measured. A value with no data is left out.

```text
── Codex │ oauth-service:main │ 27m │ Context 63% │ ⚠ Loop 3x │ ● 2 unchecked ── Ctrl+] advisor ──
```

| Segment | Meaning |
| --- | --- |
| Agent | Which agent runs here |
| `project:branch` | The project folder and Git branch |
| Time | How long the session has run |
| Context | Share of the context window used, when the agent reports a window size. Yellow from 75%, red from 90%. |
| `⚠ Loop 3x` | The journal shows the same command failing 3 times, or repeated edit and failed-check cycles. See [loop detection](/session-intelligence#loop-detection). |
| `● 2 unchecked` | Edits since the last check |

## The advisor panel

Below the health bar, the panel shows session instruments and two prioritized advisor comments. Click below the divider in the panel, or press `Ctrl+]` or `F6`, to open the full advisor. Select with the mouse or Up and Down, press Enter to add the suggestion to your prompt, and press Tab for every instrument. See the [advisor](/advisor) for what it analyzes and how you apply a suggestion. In Warp, open the advisor with `Ctrl+]` or `F6` first: mouse tracking is deferred so Warp scroll mode is not consumed while you work.

At 120 columns or wider, two columns show **Harness** and **Session**. Narrow screens stack the groups. Harness shows which setup components were found, gaps, recent oversized tool-output estimates, the model and pending checks. Session keeps the project, context, token counts, quota, Fast mode and Git changes together with observed memory use, analyses avoided, loops caught, estimated net tokens, MCP health and authentication. Open Tab details for tool counters.

Harness estimates come from recent local sessions of the running agent, using supported Claude Code or Codex history and labeling unsupported agents. They are a historical baseline, not the current session’s usage or proof of savings. Setup checks refresh every 30 seconds and background measurements every five minutes. See [audit your harness](/harness).

Drag the horizontal health bar between the agent and Cully up to expand the panel or down to shrink it, all the way to one divider row. The agent's terminal resizes with it. Cully keeps your chosen height for the rest of the launch, including advisor refreshes and window resizes; smaller windows clamp it to leave the agent usable.

The divider grip highlights when the pointer is over it, and Cully requests a vertical-resize cursor (OSC 22) while you hover or drag, restoring the default when you leave the bar or open the advisor. Cursor appearance depends on terminal support.

Scroll back through the agent's output with the mouse wheel over the agent area. Cully draws the agent from its own terminal emulator, which keeps the last 10,000 lines, because your terminal never receives that output and has no scrollback of it. The panel shows how many lines back you are; typing, or scrolling down to the bottom, returns to the live screen, and new output does not move the lines you are reading. An agent that uses the alternate screen, such as a full-screen interface, scrolls itself and receives the wheel instead. In Warp, mouse tracking stays off until you open the advisor or divider resizing, so Warp's own scrolling applies.

In Warp, press `Ctrl+\` to enable divider dragging, drag the bar, then press `Ctrl+\` again or Esc to restore normal host scrolling. Other terminals allow dragging immediately. Click below the divider to open the advisor.

Before your first drag, the panel grows only as far as its content needs. It always leaves the agent at least 12 rows on a normal screen (8 on a short one). Terminals shorter than 12 rows give the agent the whole screen.

## What each agent supplies

All agents get the health bar, harness overview, check and edit state, loops, Git changes, daemon health and advice. Tool counts remain in the expanded instruments. What differs is what the agent itself reports.

| Agent | Extra signals in the terminal | How Cully gets them |
| --- | --- | --- |
| Codex | Model, reasoning effort, context, tokens, quota, fast mode | Reads the native footer rows in memory, never a transcript |
| Claude Code | Context pressure | A hook-fed snapshot. The visible Claude status line is turned off inside the terminal, so there is one display. |
| Cursor CLI | Tool events from shell, file edits and MCP calls | Cursor hooks |
| Other agents | Process and Git state only, until it has an adapter | None needed |

Cost and cache values are shown only when an agent reports them. Cully never invents them.

## What the terminal records

Hooks send each tool call to a local [session journal](/session-intelligence#the-session-journal). The journal powers loop detection, `cully timeline`, `cully replay`, `cully handoff`, `cully rescue` and `cully status`. It is stored in your Cully directory with owner-only permissions and pruned after 30 days. See [privacy](/privacy) for exactly what it holds.

The hooks are inactive outside the Cully terminal, so an ordinary agent session leaves no advisor files.

## Scroll the conversation

The mouse wheel reads Cully's emulator history over the agent or compact panel, including in Warp. New output keeps the history view anchored; typing returns to live output. An agent using its own alternate screen receives its own mouse events. Opening and closing the advisor preserves wheel reporting. Warp users can hold Shift for terminal text selection, or use `CULLY_NO_PANE=1` for native agent IO. Mouse and scroll reporting must be enabled in Warp for it to deliver wheel events to full-screen applications.
