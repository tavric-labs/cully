---
title: The browser UI
description: Give Cully a task, let it spawn agents on your tokens, talk to them, and see the Cully dashboard in a browser.
---

# The browser UI

`cully ui` serves a page where you describe a task. Cully prepares context, starts a Claude agent in its own git worktree, and lets you keep talking to it. The same page shows what Cully already knows: status, timeline, replay, handoff, suggestions, impact and jobs.

## Start it

```sh
cully ui --repo ~/work/myrepo --tokens ~/.cully/tokens.env
```

It prints a URL ending in `?token=...`. Open it once; the browser keeps a cookie. The token is created on first run in `cully-logs/ui-token` (mode 0600) and stays the same across restarts.

`tokens.env` holds one line per account, `CLAUDE_CODE_OAUTH_TOKEN_1=...`, `CLAUDE_CODE_OAUTH_TOKEN_2=...` (from `claude setup-token`). Agents take them round-robin. Without a tokens file, agents use this user's Claude login.

## Open it from your agent

```sh
cully run --ui claude      # or: cully claude --ui, cully codex --ui, cully cursor --ui
```

Before the agent starts, Cully prints a link. The first time, it also starts `cully ui` in the background for the current repository; later runs in the same repository print the same link. Open it in a browser and start using it. `cully ui url` prints the link at any time and starts the UI if it is not running.

- **One server per repository.** Its address and process id are recorded in a small file in Cully's log directory, and its output goes to `ui.log`. Neither contains the sign-in token, which stays in `ui-token` (mode 0600).
- **Localhost only.** The background server listens on `127.0.0.1` with a free port. Over SSH, Cully adds the `ssh -L` command to forward that port.
- **Opt in.** Only `--ui`, or `CULLY_UI=1` in your environment, starts it. Utility commands such as `--version`, `mcp` and `-p` never print a link, and a failure to start the UI is reported without stopping the agent.
- **Needs a git repository,** because agents work in worktrees.

Stop the background server with `kill` on the process id in that record, or leave it running; it holds no agent state beyond what you start from the page.

## Reach it on a VM

Keep the default bind and use a tunnel:

```sh
ssh -L 7777:127.0.0.1:7777 you@vm
```

Then open `http://localhost:7777/?token=...`. To listen on the VM's address, pass `--addr 0.0.0.0:7777` with `--tls-cert` and `--tls-key`; without TLS Cully warns, because the token and agent output would cross the network unencrypted.

## What a task does

1. **Context.** The project's latest journal as a handoff, plus live git state.
2. **Recall** (optional). The advisor reads Cully memory for prior work on the task.
3. **Scout** (optional). The advisor searches the web for current practice and pitfalls.
4. **Worktree and agent.** `cully/<slug>-<id>` branch under `ui-worktrees/` in the Claude config directory, then `claude` in stream-JSON mode with the next token. A failed stage is shown and skipped; it never blocks the agent.

The agent shows as **needs you** when it has finished a turn and is waiting. Pick **plan only** to let it read and propose without editing.

## Sign in to the Cully MCP

The header shows **Cully MCP** with its state. When Claude, Codex or Cursor reports that the Cully MCP needs authentication, the chip turns amber, the home page shows a banner, and an agent that started without access shows a warning. **Authenticate MCP** opens a dialog for the three agents Cully wraps. Status comes from the same probes `cully status` uses.

The dialog runs the agent's own login command on the machine running Cully and shows the sign-in link it prints:

- **Claude** runs `claude mcp login cully --no-browser` and **Codex** runs `codex mcp login cully --no-browser`. Open the link, sign in and approve. If your browser is on the same machine as Cully, the page finishes by itself. If Cully runs on a VM and your browser is on your laptop, the approval ends on a `http://localhost…` page that cannot load, because that address points at your laptop. That is expected: copy the full address from the address bar and paste it into the dialog. Cully accepts only a localhost address that carries an authorization code or an error, and passes it to the login command.
- **Cursor** has no `--no-browser` option, so its login cannot take a pasted address. Run `cursor-agent mcp login cully` in a terminal on the machine whose browser you use, or forward the callback port with `ssh -L`.

Deep link: `#/mcp/claude`, `#/mcp/codex` or `#/mcp/cursor` opens the dialog directly.

## Every session on this machine

The **Sessions** tab lists every Claude, Codex and Cursor session open on this computer, in any folder and however it was started. Sessions are grouped by folder, with the git branch and remote. **New agent here** starts a new agent in that folder, and **View history** shows that folder in the Dashboard.

- **How they are found.** Cully looks at your own running processes: the command line, the working folder, and whether the Cully terminal started it. It reads no conversation and no environment values. Sessions the UI started yourself are on the Agents tab, not here.
- **Terminal closed.** A session whose terminal window has been closed but whose process is still running is listed as detached. Background daemons and headless runs are not listed.
- **Any folder.** Add a git repository with **Use another folder**, or start from a session's folder. Agents only run in repositories you have added, and the folder must be yours.
- **One UI per user.** `cully run --ui` from any folder prints the same link; the folder you launched from is added to it.
- **Hiding folders.** `cully ui --hide-sessions-in /path` (repeatable, or `CULLY_UI_HIDE_SESSIONS_IN` with colon-separated paths) keeps sessions started in those folders out of the list, before any command runs there. Use it to keep work you do not want on a shared page, for example a separate company workspace.
- Detection works on Linux. On macOS it is best effort and untested.

## Advisor and Scout

The Dashboard has an **Advisor** and a **Scout** section.

- **Advisor.** Ask the advisor to review an agent's work. It gives up to three pieces of advice, each marked as a warning, a caution or a suggestion and tied to the agent it is for. **Send to <agent>** passes the advice to that agent as a message; **Dismiss** removes it. The advisor follows Cully's usual limits: at most six requests an hour, two minutes apart.
- **Scout.** When you start a task with Scout turned on, Cully searches the web for current practice. The Scout section shows it working, each search and page it reads as it happens, and the findings with their sources. Clicking the scout or recall step on an agent opens it. Recall shows what Cully memory returned.

## Where it left off

Handoff is a plain summary: what was asked, where the agent stopped, what changed, and whether the last check passed. The text written for another agent stays behind **Copy for another agent**, and **Continue in new agent** starts from a short prompt you can edit.

## Memory mode and sign-in

The header says how agents sign in: Claude subscription accounts (with a count) when you give a tokens file, or this machine's own Claude login. If no Cully MCP is registered for the agents you use, the header says **Local disk mode**: session history is kept in files on this machine and nothing asks you to sign in. Set `CULLY_MEMORY=local` or `mcp` to pin it. The sign-in dialog focuses on the agents you launch Cully with (`cully run --ui AGENT`, or `--agent`), and the others are tucked under Show other agents.

## Agents working together

Agents never message each other directly. Cully relays, so every message shows in both feeds and you stay in the loop.

- **Conflict alerts.** When a second agent edits a file another live agent already edited, Cully tells both of them, naming the other agent, and asks each to keep changes to that file minimal and expect a merge. Each pair is told once per file. The message wakes an idle agent, which answers it. Read-only (plan) agents never take part. Turn it off with `cully ui --no-conflict-alerts`.
- **Send to agent.** From any agent, send its latest reply or its diff to another live agent, with an optional note. The receiving agent sees it as a message from the named agent and responds.
- **Review.** Starts a read-only agent on the same repository with the author's task and diff in its prompt, and read access to the author's worktree. Its findings come back in its own chat; use Send to agent to pass them to the author.

Fan-out (one prompt to several agents, compared side by side) is not built yet.

## Board, diff, handoff

- **Board** lists live agents with the files each has edited and warns when two touch the same file. New agents are told what the others are doing.
- **Diff** is the whole change against the commit the worktree started from, so earlier turns are not missed.
- **Continue in new agent** puts the handoff in the task box for you to edit before starting.

## Security model

- Every API call needs the token (cookie or bearer); changes also need the `X-Cully-UI` header; loopback binds reject other `Host` names.
- Agents run only in repositories you pass with `--repo`, in worktrees, with tools limited to Read, Glob, Grep, Edit, Write and Bash. They are told not to push.
- Token values stay in the agent's environment; the UI shows only a token number, and agent output is scrubbed of token values.
- Agent output is rendered as text only, under a strict content security policy.

## What is not stored

Agent transcripts live in memory and are lost when `cully ui` stops. The journal receives only what it always does: event class, outcome, project-relative path and a command hash. Worktrees and branches stay on disk until you remove them.
