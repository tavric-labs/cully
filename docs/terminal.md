---
title: The Cully terminal
description: Run Claude Code, Codex, Cursor or any coding agent in one terminal with a live health bar and advisor.
---

# The Cully terminal

`cully run` starts a coding agent inside a terminal that Cully owns. The agent gets a slightly smaller screen. Cully uses the rest for a health bar and the advisor, so the agent's own clear-screen and cursor codes can never erase them.

It is an observation surface, not another terminal emulator you have to learn. You use your agent exactly as before.

## Supported environment

Cully supports **macOS and Linux**, using **zsh + Oh My Zsh** in the OS-provided terminal. Native font configuration targets [macOS Terminal](https://support.apple.com/guide/terminal/welcome/mac), GNOME Terminal, Ptyxis and GNOME Console on Linux. Warp and other third-party terminal apps are outside support; identified unsupported apps receive an instruction to use a native terminal. Unidentified client metadata does not prove support.

Setup applies a consistent preset automatically: MesloLGS Nerd Font Mono at **14 pt on macOS** or **12 pt on Linux**, plus the stock `robbyrussell` prompt for a new Oh My Zsh configuration. An existing Oh My Zsh configuration is reused. Bash, fish and other interactive shells are outside the supported setup. This does not restrict a coding agent's own shell tools.

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

## Sessions

A sidebar down the left edge of the window lists the named sessions running in that window. Click **+ New** to start another session of the same agent, click a name to switch to it, and click **Rename** then type a name and press Enter. Each session keeps its own agent process, input and scroll position, so switching back returns you to where you were. Sessions belong to the window: closing it ends them, and nothing runs in the background. The sidebar hides itself in windows narrower than 60 columns, and `CULLY_SIDEBAR=0` turns it off.

Keys go to the agent unless the sidebar has focus after you click it. With focus, Up/Down move, Enter switches, `n` creates, `r` renames and Esc returns to the agent. Cully does not take any agent shortcut by default. `CULLY_SHORTCUTS=1` adds F7 to move focus to the sidebar.

## Cully only inside `cully run`

Cully's hooks, skill guidance, panel and sidebar are active only in sessions started with `cully run AGENT`. Running `claude`, `codex` or `cursor-agent` directly gives the native experience: setup puts nothing in their instructions or skill folders, and every Cully hook exits before doing anything unless `cully run` started the session.

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

Below the health bar, the panel shows session instruments and two prioritized advisor comments. Click below the divider in the panel, or press `Ctrl+]` or `F6`, to open the full advisor. Select with the mouse or Up and Down, press Enter to add the suggestion to your prompt, and press Tab for every instrument. See the [advisor](/advisor) for what it analyzes and how you apply a suggestion. Mouse, wheel and pointer reporting stay enabled.

At 120 columns or wider, two columns show **Harness** and **Session**. Narrow screens stack the groups. Harness shows which setup components were found, gaps, recent oversized tool-output estimates, the model and pending checks. Session keeps the project, context, token counts, quota, Fast mode and Git changes together with observed memory use, analyses avoided, loops caught, estimated net tokens, MCP health and authentication. Open Tab details for tool counters.

Harness estimates come from recent local sessions of the running agent, using supported Claude Code or Codex history and labeling unsupported agents. They are a historical baseline, not the current session’s usage or proof of savings. Setup checks refresh every 30 seconds and background measurements every five minutes. See [audit your harness](/harness).

Drag the horizontal health bar between the agent and Cully up to expand the panel or down to shrink it, all the way to one divider row. The agent's terminal resizes with it. Cully keeps your chosen height for the rest of the launch, including advisor refreshes and window resizes; smaller windows clamp it to leave the agent usable.

The divider grip highlights when the pointer is over it, and Cully requests a vertical-resize cursor (OSC 22) while you hover or drag, restoring the default when you leave the bar or open the advisor. Cursor appearance depends on terminal support.

Scroll back through the agent's output with the mouse wheel over the agent or compact panel. Cully draws the agent from its own terminal emulator, which keeps the last 10,000 lines, because your terminal never receives that output and has no scrollback of it. Page Up and Page Down also read this history when the agent uses the normal screen. The panel shows how many lines back you are; typing, or scrolling down to the bottom, returns to the live screen, and new output does not move the lines you are reading. A fullscreen agent using the alternate screen receives wheel and page keys to navigate its own history.

Drag the divider directly in a supported native terminal. `Ctrl+\` toggles explicit resize mode when needed. Click below the divider to open the advisor.

Before your first drag, the panel grows only as far as its content needs. It always leaves the agent at least 12 rows on a normal screen (8 on a short one). Terminals shorter than 12 rows give the agent the whole screen.

## What each agent supplies

All agents get the health bar, harness overview, check and edit state, loops, Git changes, daemon health and advice. Tool counts remain in the expanded instruments. What differs is what the agent itself reports.

| Agent | Extra signals in the terminal | How Cully gets them |
| --- | --- | --- |
| Codex | Model, reasoning effort, context, tokens, quota, fast mode | Reads the native footer rows in memory, never a transcript |
| Claude Code | Tool events | Claude hooks. Cully installs no status line; Claude's own stays yours. |
| Cursor CLI | Tool events from shell, file edits and MCP calls | Cursor hooks |
| Other agents | Process and Git state only, until it has an adapter | None needed |

Cost and cache values are shown only when an agent reports them. Cully never invents them.

## What the terminal records

Hooks send each tool call to a local [session journal](/session-intelligence#the-session-journal). The journal powers loop detection, `cully timeline`, `cully replay`, `cully handoff`, `cully rescue` and `cully status`. It is stored in your Cully directory with owner-only permissions and pruned after 30 days. See [privacy](/privacy) for exactly what it holds.

The hooks are inactive outside the Cully terminal, so an ordinary agent session leaves no advisor files.

## Scroll the conversation

The mouse wheel reads Cully's emulator history over the agent or compact panel. Cully requests SGR mouse reporting and accepts legacy mouse reports from terminals that do not support it. Modified vertical wheel events keep their direction; fullscreen agents receive the original modifiers with coordinates constrained to their viewport. New output keeps the history view anchored; typing returns to live output. Page Up and Page Down provide access to emulator history when wheel events are unavailable; fullscreen agents retain their own page-key handling. Opening and closing the advisor, ending a drag or resizing the terminal preserves wheel reporting.

While the panel is active, Cully uses an alternate screen and owns the conversation history. Your OS terminal's scrollbar does not represent that history; use the wheel or Page Up/Down. Selection/copy still uses the host terminal's controls and needs a manual check with mouse reporting enabled. Set `CULLY_NO_PANE=1` for native agent I/O, including native scrollback behavior. The panel does not override the host's clipboard shortcuts or keymap.

Cully retains up to 10,000 lines of normal-screen history. Alternate-screen agents own their private history and receive wheel and page-key events inside their viewport. Page Up and Page Down navigate Cully's normal-screen history when wheel events are unavailable. Cully uses the terminal's existing colors and preserves input event order.

### Configure and verify your terminal

```sh
cully setup --terminal-only    # install required shell/font preset, then verify
cully terminal check          # read-only diagnostics and shell setup preview
cully terminal shell --dry-run # preview required shell changes
cully terminal configure --dry-run # preview native font settings
cully terminal shell          # install/configure required zsh + Oh My Zsh
cully terminal configure      # install and select the Nerd Font preset
cully terminal test           # verify icons, colors, wheel, divider and advisor
cully terminal restore --backup /absolute/path/to/native-font-backup.json
```

Regular `cully setup --all` and `cully setup --mcp-url URL` also apply the required shell and font configuration. These explicit commands authorize installation; Cully prints the changes and does not add confirmation prompts for each component. Package managers, `chsh` or macOS automation may request OS authentication or permission. If zsh is missing, setup uses the existing supported package manager; it does not install a package manager. Oh My Zsh comes from its [official repository](https://github.com/ohmyzsh/ohmyzsh). The font download is pinned to Nerd Fonts v3.4.0 and checked against its SHA-256 before installation.

Setup backs up an existing `.zshrc`, preserves its content and symlink, and adds initialization only when Oh My Zsh is not already sourced. It honors `ZDOTDIR` and `ZSH`. It selects zsh as the login shell with `chsh` when necessary; reopen the terminal afterwards. There is no Bash fallback. A download, syntax or shell-change failure reports an actionable error instead of claiming the setup completed.

Font settings are backed up separately in `~/.cully/terminal/`. Setup changes font family, size and the native terminal's custom-font switch; themes, keymaps, padding and other preferences stay intact. It prints the exact font restore command and, when needed, a shell backup restore command. Font restoration keeps the downloaded font available. Restoring a previous login shell uses the OS account settings or `chsh`.

The home icon `` (U+F015), folder and Git icons use Nerd Font glyphs. Installing Oh My Zsh alone does not provide those glyphs: the terminal must select the patched font. The live exercise prints these icons, accented/wide text and color samples. Press `u` only if all text and icons look correct, and `c` only if the color labels match. Then scroll up/down, drag the divider both ways, open/close the advisor, and press `q`. Missing confirmations and gestures remain **NOT VERIFIED**; Cully cannot infer correct glyph appearance from writing text successfully.

Interactive setup provisions the required shell/font preset and runs the exercise. `--skip-terminal-test` skips only the exercise. Headless or SSH service setup skips desktop provisioning and labels it untested; run `cully setup --terminal-only` in the local native terminal. `--prepare` prepares services without changing the terminal or shell. Ordinary launches never install software or rewrite preferences.

When Linux terminal metadata is missing, identify the app with `cully setup --terminal-only --terminal gnome-terminal` (or `ptyxis` / `gnome-console`). Do not use an override to claim a different terminal is supported. Model, token and tool statistics still need a real coding-agent session; the exercise verifies their display area only.
