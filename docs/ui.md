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

- **How they are found.** Cully looks at your own running processes: the command line, the working folder, and whether the Cully terminal started it. It reads no conversation and no environment values. Sessions the UI started yourself are on the Agents tab, not here. By default the list shows only the agents you started Cully with (`cully run --ui claude` marks Claude; the agents the page starts count as Claude), and only sessions with a live terminal. **Show sessions from other agents** and **Show sessions whose terminal was closed** reveal the rest.
- **Terminal closed.** A session whose terminal window has been closed but whose process is still running is listed as detached. Background daemons and headless runs are not listed.
- **Any folder.** Add a git repository with **Use another folder**, or start from a session's folder. Agents only run in repositories you have added, and the folder must be yours.
- **One UI per user.** `cully run --ui` from any folder prints the same link; the folder you launched from is added to it.
- **Hiding folders.** `cully ui --hide-sessions-in /path` (repeatable, or `CULLY_UI_HIDE_SESSIONS_IN` with colon-separated paths) keeps sessions started in those folders out of the list, before any command runs there. Use it to keep work you do not want on a shared page, for example a separate company workspace.
- Detection works on Linux. On macOS it is best effort and untested.

## Advisor and Scout

The Dashboard has an **Advisor** and a **Scout** section.

- **Advisor.** Ask the advisor to review an agent's work. It gives up to three pieces of advice, each marked as a warning, a caution or a suggestion and tied to the agent it is for. **Send to task** passes the advice to that agent as a message; **Dismiss** removes it. The advisor follows Cully's usual limits: at most six requests an hour, two minutes apart.
- **Scout.** When you start a task with Scout turned on, Cully searches the web for current practice. The Scout section shows it working, each search and page it reads as it happens, and the findings with their sources. Clicking the scout or recall step on an agent opens it. Recall shows what Cully memory returned.

## Do it from the page

Everything below can be done here as well as in the terminal. Each button calls the same code as the command.

| In the terminal | On the page |
| --- | --- |
| `cully setup --agent claude` (without an MCP) | Dashboard, Setup: **Set up Claude**. A dialog says what changes: Cully adds its session-tracking entries and skill to the agent's user settings and leaves your other settings as they are. |
| `cully mcp add --agent claude --url URL` | Setup or the header: **Connect the Cully MCP**, then **Authenticate MCP** to sign in |
| Start the advisor | Setup: **Start the advisor** |
| `cully jobs run NAME` | Dashboard, Background jobs: **Run now** (only the program name is shown, never its arguments) |
| Run a scout | Dashboard, Scout: **Start a scout**, with live searches and findings; **Use in a new task** carries them into a new task |
| `cully suggestions` | Advisor, From terminal sessions (read only) |
| `cully handoff` | **Copy for another agent** on Activity and on an agent's Summary tab |
| `cully status`, `cully rescue` | Overview and Setup |
| `cully timeline` | Activity (replay playback is not on the page yet) |
| `cully impact` | Savings |

Not on the page yet: `cully apply N`, `cully improve`, `cully eval`, `cully update`, `cully task`, `cully workflow` and `cully terminal`.

These actions change your own agent settings, so they need the same login token as everything else, and a token holder can already run commands through an agent. The page is still plain HTTP on a network address by default; use `--tls-cert` and `--tls-key`, or an SSH tunnel to localhost, on a network you do not trust.

## Notifications

The bell in the header turns on three kinds of heads-up for when an agent needs you, new advice arrives, or sign-in is needed: a flashing tab title (works everywhere), a short sound, and desktop notifications. Browsers only allow desktop notifications on HTTPS or localhost, so over plain HTTP on a network address the menu says so and the other two still work. Your choices are kept in the browser. Nothing is sent anywhere.

## Where it left off

Handoff is a plain summary: what was asked, where the agent stopped, what changed, and whether the last check passed. The text written for another agent stays behind **Copy for another agent**, and **Continue in new agent** starts from a short prompt you can edit.

## Memory mode and sign-in

The header says how agents sign in: Claude subscription accounts (with a count) when you give a tokens file, or this machine's own Claude login. If no Cully MCP is registered for the agents you use, the header says **Local mode (SQLite)**: everything the UI keeps (session library, agents and their notes, evals, advice, usage) lives in one SQLite database, `cully.db` in the Cully folder, with mode 0600; nothing needs Postgres or Mem0 and nothing asks you to sign in. Local mode is less efficient than the Cully MCP: it recalls by keyword only, so agents can receive more context and use more tokens, and the page says so and shows how to connect the MCP. Files from earlier versions are imported once and renamed `*.imported`. Set `CULLY_MEMORY=local` or `mcp` to pin it. The sign-in dialog focuses on the agents you launch Cully with (`cully run --ui AGENT`, or `--agent`), and the others are tucked under Show other agents.

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

## Sessions, the library and health

**Sessions** shows the sessions open now first, then recent ones from the agent's own folders (`~/.claude/projects`, `~/.codex/sessions`), with Resume and Fork for Claude: the agent runs in the session's original folder, and a summary of what the session did is shown before you start. A session open in a terminal can only be forked, so two writers never share one session.

**Session library** keeps one record per Claude session in `~/.claude/cully-logs/session-index.json`: time worked (gaps over ten minutes do not count), requests, tool calls by tool, files changed, test runs and tool errors, counted from the transcript. No prompt or reply is stored. When you turn on naming, Claude reads each finished session in the background (up to 30 an hour, newest first, never one active in the last 15 minutes, never one in a hidden folder) and writes a title, a description, what could go better, whether it finished the job, whether it drifted, the goal in one sentence and how to tell it was met.

**Session health** scores each session from 0 to 100. Signals follow what coding-agent harnesses watch for ("Harness Engineering", arXiv 2609.00006): the same call repeated three or more times in a row, the same call failing three or more times, ending after changes with no test run, and the judged outcome. **Make an eval** turns a completed session into a test case (goal, checks, what it did) saved under `evals/`. **Evals** runs a case on a fresh agent in its own worktree and a judge scores it pass, partial or fail.

The **coach** reads all analysed sessions and suggests a job, skill, MCP server, habit or eval; it runs about once an hour while naming is on, or when you ask. Everything above is kept in the local database. Saving it to the Cully MCP is not built yet; the records carry a `synced` field for that.

## Keep going until checked

Turn on **Keep going until checked** when you start an agent and, after each turn, Cully makes sure the agent ran tests after changing files, steers it when it repeats the same call or the same failing call, and judges its work against the "done when" lines you wrote. It stops after three rounds (up to six), after 200 tool calls, or after eight identical calls in a row. Every message Cully sends is shown in the feed.

## Suggestions, background jobs and the Advisor

The Advisor is the one place Cully suggests things. The Advisor page opens with a **Suggestions** inbox that gathers every pending suggestion: agents, background jobs, tips from the coach, a workflow-rules update and the latest scout digest. The other pages (Agents, Background jobs, Workflow rules) show what you have approved.

An **agent** and a **background job** are different. An agent runs a model: it reads, reasons and reports, keeps notes between runs, and costs tokens. A background job is one fixed command that the Cully daemon starts at a time of day, with no model, no judgement and no tokens; it does the same chore every time and leaves a log. Cully proposes a job only for a chore you repeat that one command can do (tests, a linter, `git fetch --all`, a build), picks a quiet time, shows the exact command and lets you change the time. Approve adds it to `~/.cully/jobs.json` (see [scheduled jobs](scheduled-jobs.md)); Reject forgets it. Proposals come from the analysed sessions (their goals and problems, not your prompts, which the library never stores), about once a day once five new sessions are analysed, or when you press **Look for background jobs**. A proposed command is checked again when you approve it, against an allowlist of fixed shapes (git fetch or status, make with a known target, go test or build or vet, npm test or run, pytest); anything else is refused. See [scheduled jobs](scheduled-jobs.md).

## Scout history and the scout

Every scout result and every piece of advice is kept in the local database (newest 500) and shown at once. Turn on **Scout the web while you work**: while a session changed in the last 90 minutes, the advisor gives at most one search an hour, from the last week of analysed sessions. When you are away, nothing new is searched; every 12 hours the scouts found so far are merged into at most three suggestions, which appear at the top of the history and in the Suggestions inbox. A search is only sent when it uses general technical terms: Cully refuses any that contain a path, link, email, ticket number, or the name of a folder or repository, and drops one that is about the same as a search from the last week (by shared words, so a rewording counts as a repeat).

## Harness permissions

The harness is the program that runs the model (Claude Code). Tasks the page starts run headless, so a tool that is not allowed is refused. **Dashboard, More, Harness permissions** sets what they may do: groups of tools (read, edit, commands, Cully memory read, Cully memory save, web), extra tools by name, and a block list that always wins. Defaults match what agents had before; changes apply to agents started afterwards and are saved in `ui-settings.json`.

## Agents

An agent is a named worker with a role, instructions on how it works, a set of tools, a folder and short notes it keeps between runs. Create one on the **Agents** page, or start from a template (pull request watcher, test guardian, dependency reviewer, docs keeper). Its name, role and instructions are added to the system prompt at the start of every run, ahead of the task, so that part stays the same and the prompt cache can reuse it. Each run is an ordinary task in its own worktree, shown on the Tasks tab; at the end it saves up to three short facts, kept to 2,200 characters with the newest winning, and the next run reads them first. An agent runs when you press Run now, or every hour or day while the page server is running. Definitions, notes and run history are in `identities.json` in the Cully folder.

Cully also proposes agents. About once an hour while analysis is on, it reads the analysed sessions and suggests agents for work you repeat or skip. A proposal shows what it would do, why, and its tools; nothing exists until you press **Review and create**, change what you like and save.

Suggestions from the coach can be applied from the Advisor and Session library pages: to a task started from Cully (sent as a message), as a new task, or copied for a session open in a terminal, which the page cannot write to.

## Token use and savings

Each finished task writes one line of counts to `usage-log.jsonl` in the Cully folder: turns, fresh input, input served from the prompt cache, output, and whether it loaded every connected MCP server or only the ones you chose. No prompt or reply is stored. The **Savings** page adds these up for the last 7, 14 or 30 days: tokens handled, the share of input that came from the cache, tokens saved by the cache (about nine tenths of the cached tokens, because a cache read costs about a tenth of fresh input), a per-day chart, where the tokens went by folder, and the biggest tasks. Once there are tasks of both kinds it compares the input each turn carries with every server loaded against only the chosen ones, and shows what limiting servers saved.

## The harness

The **Harness** page is about the program that runs the model (Claude Code, Codex or Cursor). Tasks run in the one the page was opened through; sessions, the library, the advisor and the scout cover all three, and each session is marked with its harness. For each harness the page lists what it has set up on this machine: its version and model, its own permission mode, the MCP servers it has connected (with their status once a task has reported it, and whether tasks load each one), its skills, hooks, plugins, slash commands, subagents and instruction files. Only names and counts are read: headers, environment values, command arguments and URL queries are never shown. For the harness tasks run in there is also a **Permissions** tab: the groups of tools a task may use, the connected servers it loads, extra allowed tools and the always-blocked list.

Each task has a panel down the left side: what it is doing, how full its context window is and how many tokens are left, the harness's own 5-hour and 7-day usage limits with their reset times (as the harness reports them), tokens used by the task, today and this week, how much came from the prompt cache, the list price so far, and a link to how the harness underneath was set up. When the context passes 80 percent the panel and the task header offer a new task that starts from a handoff. Sessions of every harness show a context percentage where the harness records one (Claude estimates the window from the model name; Codex records the real one; Cursor records none).

## Workflow rules and reviewing agents

**Workflow rules** keeps one skill, `dev-workflow-rules`, that tells every harness how you work: the projects you are on, the harnesses you use, habits that help and things to avoid. About once a day, when several new sessions have been named, Cully drafts an update from your sessions and shows exactly what would change as a diff. Nothing is written until you press **Approve and save**; then it is written to the skills folder of each harness that has one (Claude, Codex, and Cursor if it has a skills folder), and the file it replaces is kept beside it as `SKILL.md.prev`. A draft with a secret in it, or without a proper skill header, is refused. While a draft waits, the Dashboard badge, the Overview and a browser notification ask you to review it. The page can only notify while it is open.

**Reviewing agents.** When you start a task with *Keep going until checked*, you can ask up to two of your approved agents to review the finished work. A reviewer is read only, sees the task's goal and change and may read the task's worktree, and ends with `VERDICT: ok` or `VERDICT: concerns`. Concerns are sent back into the task as a message and count as a round; if there are none the task finishes.

Claude Code compacts its own history before the context window fills, so a full window is not treated as a problem. The task panel shows how many times it has been compacted, and offers a fresh task from a handoff only after three or more compactions, when detail from early on is likely gone.

## Saving agents to Cully memory

In local mode an agent is a record in the database (its workspace folder stays on disk). When the Cully MCP is connected, Cully also saves each approved agent to Cully memory (Postgres) through the new `cully_agent_save` tool, so it follows you to another machine; `cully_agents` lists your agents and `cully_agent_delete` removes one. The page has no MCP login of its own: a short headless Claude call, allowed to use only that one tool, makes the save, at most every ten minutes and only for agents that changed. The local database stays the source of truth, a failed save is retried, and the saved definition never includes a path on this machine. Migration 011 adds the table; it is private to the owner like every other record. Restoring agents from memory onto a new machine is not built yet.

## Evals

An eval is a small test: a task, a few checks in plain words and a judge that says pass, partly or fail, run again after something changes to show whether it got better or worse. The Evals page explains this and has two parts. **Your agents**: Cully drafts up to eight cases per agent from its role and rules (some where it should do its job, one where it should refuse, one awkward edge case); you approve the ones to keep, run them, and each answer is judged. A run only plans and answers; it never changes files and uses no notes. The page flags an agent whose instructions or tools changed since its last run. **From your sessions**: cases made from finished sessions, as before.

## Activity

Activity opens on a feed, newest first and grouped by day: tasks that finished, failed or need an answer, runs of your agents, eval verdicts, and what the scout and advisor found, each linking to where the detail is. Filter by Tasks, Agents, Evals, Scout or Advice. The **This folder** tab keeps the file and command timeline of the current folder.

## Servers, tools and prompts

On the Harness page each connected MCP server is a card with a switch for whether tasks load it. Open a card to see its tools, prompts and resources in a scrollable list with search. Claude Code reports every tool and prompt name when a task starts, so those are known after the first task. Cully's own server lists every tool with what it does and what it takes, read from its source. For any other server, **Load descriptions and parameters** asks your Claude once to read that server's tool definitions (a few thousand tokens) and remembers the answer. Claude Code does not report resources, so the Resources tab says so.

## Security model

- Every API call needs the token (cookie or bearer); changes also need the `X-Cully-UI` header; loopback binds reject other `Host` names.
- Agents run only in repositories you pass with `--repo`, in worktrees, with tools limited to Read, Glob, Grep, Edit, Write and Bash. They are told not to push.
- Token values stay in the agent's environment; the UI shows only a token number, and agent output is scrubbed of token values.
- Agent output is rendered as text only, under a strict content security policy.

## What is not stored

Agent transcripts live in memory and are lost when `cully ui` stops. The journal receives only what it always does: event class, outcome, project-relative path and a command hash. Worktrees and branches stay on disk until you remove them.
