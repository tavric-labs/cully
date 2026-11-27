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

## Setting up Cully's memory with a skill

Cully installs a second skill, `cully-setup`, beside the main one for Claude Code, Codex and Cursor. Ask your agent to "set up Cully's memory" (or to fix "Cully MCP needs sign-in") and it follows a runbook: choose between the full local stack (`cully setup --all`: PostgreSQL, Mem0 with its pgvector database, the data API and the MCP server in Docker), an existing remote server (`cully setup --mcp-url URL`, with `--oauth` when it uses OAuth, or a header token), or staying in local mode; check Docker and the CLI read-only first; run setup; then verify by calling a memory tool, listing the containers and watching the UI header change from "Local mode (SQLite)" to connected. The skill never prints a credential and never purges data or volumes without being asked.

## Supervising, not just forwarding

Cully is a supervisor over harnesses: it does not run the model loop, it steers and checks the harness that does. Features drawn from reading how eleven open-source harnesses work (the paper "Harness Engineering", arXiv 2609.00006, and the source of Codex CLI, Gemini CLI, OpenCode, Mistral Vibe, OpenHands, Aider, mini-swe-agent, Hermes and Pi):

- **Stuck detection.** While a task runs, Cully watches its tool calls for the same call giving the same answer three times, the same call failing three times, or two calls taking turns. The task window shows a red line with a **Nudge** button that tells the task it is going in circles; the board card shows a "Stuck" chip. The agent's own todo-list updates are never counted. Nothing is stopped automatically.
- **Risk on every permission card.** Low, medium or high, from fixed rules over the command or path (no model is asked): reads are low; writing files, installing packages, building and testing are medium; deleting recursively, `sudo`, `git push`, reaching another machine, force-resetting git, deleting cluster or database data, writing a system or secrets file are high. A high-risk call is only ever allowed one at a time: "Allow for this task" is not offered.
- **Ask, don't block.** The default blocks (git push, curl, wget, ssh, scp, nc, kubectl) now become questions on a card when a task can ask you, which it can by default. A rule that asks wins over a broad allow, so they ask even when commands are allowed in general. Turn **Ask me about risky commands** off on the Permissions page to block them outright again; rules you add yourself are always blocks.
- **Limits per task.** Set "Stop at $" and "or after turns" when you start a task. Cully stops it at the limit with a typed reason (cost limit or turn limit), says so in the chat, and shows it on the board. What it did stays on its branch.
- **Approvals from anywhere.** A **Needs you** button in the header (and the top of the home page) lists every question any task is waiting on, with the tool, the risk label and exactly what it wants to do, and lets you Allow once or Deny right there; the task carries on at once. A new question also raises a notification (sound, tab flash, desktop) like a task waiting for a reply.
- **A shareable report.** From a task's More menu, **Open a shareable report** or **Download the report**: one self-contained page (no scripts) with the goal, the plan and how far it got, where it ended, the files changed with lines added and removed, whether the checks passed, the time, cost, tokens and tools used. It holds no file contents, no token values and no folder paths beyond the folder's name, so it is safe to attach to a pull request.
- **A first minute with nothing to set up.** Before you have started anything, the home page says what Cully already found on this machine: your sessions across Claude, Codex and Cursor, how many are analysed and how many need a second look, with links to see them. Hide it once you have seen it.
- **A checked contract.** The Harness overview lists what Cully can do with each harness and checks the installed Claude Code's `--help` for every flag Cully passes. If a release drops one, the features that need it turn themselves off with a plain explanation, instead of failing silently or passing a flag Claude Code would reject.

## Tasks that outlast the page

A task is the unit of work you care about, and it can be more than one session on more than one harness: the run Cully started, the session you resumed later, a Cursor or Codex session it was continued from. Cully keeps each task in its local database (state, title, what it was last doing, how many files it changed, and every session it has had with the harness of each), so work from today and this week is still there after the server restarts. The home page lists them under **Your tasks** (Today, This week, Earlier) as cards with harness badges. **Continue** resumes the task's last Claude session so the conversation carries on; when the work began in another harness, or that session is gone, a new run starts from a handoff of the task (what it was asked, where it stood, the sessions it had) in the folder it worked in, and it is added to the same task.

## The task window and the board

The agent window follows what other agent front ends do: the agent's own plan (its todo list) is pinned above the chat and ticked off as it goes, a line always says what it is doing now or that it is your turn, tool calls are typed rows with an icon, a spinner while they run and a tick or cross after, and the Changes column appears when there is something to review. The left panel's sections (Now, Context window, Usage limits, Tokens, Harness, Work so far, Other harnesses) fold, with the first three open by default and your choices remembered; the whole left column scrolls as one instead of in pieces; and **Hide panel** in the task header gives the chat the whole window.

Scrolling follows the same rule as other chat apps: only the conversation scrolls, never the whole page, and scrolling it does not drag the page along. It stays at the newest message while you are at the bottom; when you scroll up to read, a **Latest** button shows how many messages arrived meanwhile, and your own message always scrolls into view. A very long reply is shortened with Show all, code blocks scroll on their own, and Home and End jump to the start and end of the chat. The stage line (context, recall, scout) shows only while it is running or has failed.

The **Board** is a kanban: Up next, Working, Needs you and Finished side by side, each scrolling on its own. Press + in Up next to add a task, drag one from Up next to Working to start it, and a running one to Finished to stop it.

## Tools, permissions and suggested replies

The **Tools** page of the Harness lists every tool on the servers tasks connect to, grouped by server, and lets you choose for each one: **Ask** (the default: the task pauses and asks you in its window), **Allow** (it may use it freely) or **Block** (it never gets it). Bulk buttons set every read-only tool to Allow or every deleting tool to Block, and descriptions and parameters can be loaded for one server or all of them. The **Permissions** page was redesigned the same way: switches for what a task may do on its own with a plain-words summary on top, the extra rules and the never-allowed rules shown as readable chips ("Commands starting with git push") with quick-add suggestions, and one save bar.

The suggested replies under a task's message box come from that task: if its last message asked something, Yes and No; after edits with no test run, Run the tests; after a failing check, Fix the failing check; after a passing check, Commit; near a full context, Wrap up and hand off. They are worked out from facts the page already has, so they cost no tokens.

The task window hides its Changes and Summary column until a task has changed a file, so the chat fills the window, and keeps its secondary actions under a More menu. Task names are made from the first sentence of what you asked, without the "can you please", at most eight words.

The Session library's "Analysing your sessions" bar counts a session still in use as waiting and says so (this page's own session is one): a session is analysed 15 minutes after its last activity. Cully's own throwaway calls, such as the server check, are left out of the library.

**Usage limits** show "Not reported" when Claude Code has not said, never 0%, and keep a figure heard earlier until its window resets. Check now asks each account one tiny question on the smallest model and reads the limits from the answer.

## Asking you before a task uses a tool

A task runs without a person at a keyboard, so it used to refuse any tool use outside its allowed list. Now Cully asks you. When a task wants to use something it is not allowed to yet (a command such as `ssh`, a file outside its folder, a web fetch), the task window shows a card with the tool and exactly what it wants to do, and the task waits. **Allow once** lets that call through; **Allow for this task** lets the same exact call through again without asking, for the rest of that task; **Deny** refuses it and the task carries on without it. The card shows everything the task sent, in full, and your answer is tied to a hash of exactly that, so you can only allow what you were shown. A request too long to show (over 20,000 characters) or one that contains one of your tokens is refused instead of shown clipped or hidden. Nothing is allowed unless you click: an unanswered question is refused after ten minutes, and stopping the task refuses it at once. Tools on the Harness permissions always-blocked list never reach you; they are refused first. Behind the card, Claude is started with `--permission-prompt-tool` pointing at a small helper that talks to the Cully server over a private socket (mode 0600, in a private temporary folder) that exists only while the task runs.

## Any folder, with or without git

A task can start in any folder you can write in. In a git repository it gets its own branch and worktree. In a folder with no git it works in the folder directly, with no branch to review; the task shows "no git" where the branch would be. Cully works out which when the task starts, so you never have to set git up first. Your home folder and `/` are refused as too broad.

## Voice

The **Speak** button beside the task box starts a task by voice. Click it, say the task, and stop talking: it stops after 1.6 seconds of quiet (or click again, or press Escape; it gives up after 10 seconds of silence and stops at two minutes). The audio goes to the Cully server, which turns it into text with a speech model on that machine, and the text becomes the task. With **Start when I stop talking** on (the default) the task starts at once; turn it off to check the text first.

The first click offers **Set up voice**: Cully creates a private Python environment under its folder (`voice/`), installs faster-whisper and downloads the `distil-large-v3` model, about 1.5 GB, which takes a few minutes. The model runs on the server's CPU, one worker stays loaded while you use it and stops after ten idle minutes. The audio is written to a private temporary file, deleted straight away and never kept or sent anywhere. To use a different engine, set `CULLY_STT_CMD` to a JSON list that prints the text, with `{file}` where the audio goes, for example `["whisper-cli","-m","/models/ggml-large-v3.bin","-f","{file}","-nt","-np"]`.

The browser lets a page use the microphone only over https or on localhost, so over plain `http://10.x.x.x` the button explains this instead of recording. Cully cannot get around that: the microphone is on your computer and only the browser can open it. Allow it once, per browser: in Chrome or Edge open `chrome://flags/#unsafely-treat-insecure-origin-as-secure` (`edge://flags/...`), add the address you use (for example `http://10.162.0.20:7778`), set it to Enabled and relaunch; in Firefox set `media.devices.insecure.enabled` and `media.getusermedia.insecure.enabled` to true in `about:config`. Or use an SSH tunnel (`ssh -L 7777:127.0.0.1:7777 HOST`, then `http://127.0.0.1:7777`), or start the server with `--tls-cert` and `--tls-key` and open the https address.

## Continue any session as a task

Every session on the Sessions page, from Claude, Codex or Cursor, has **Continue as a task**. It starts a new task in the harness Cully runs tasks in (the one you opened the page with, Claude by default) and gives it the session's handoff as its first message: the goal, what it did, where it ended, whether it drifted or left changes untested, what would have gone better, and the path to the full record, which the task is allowed to read. You can add what to do next. The handoff is built from the analysis the library already did in the background; a session not analysed yet gets only its size and checks, and the dialog says so. The original session is not touched, so work begun in Cursor carries on in Claude. A folder with no git works too: the task runs in it directly, with no worktree. Resume and Fork of a Claude session also work in a folder with no git, for the same reason.

## Suggestions, background jobs and the Advisor

The Advisor is the one place Cully suggests things. The Advisor page opens with a **Suggestions** inbox that gathers every pending suggestion: agents, background jobs, tips from the coach, a workflow-rules update and the latest scout digest. The other pages (Agents, Background jobs, Workflow rules) show what you have approved.

An **agent** and a **background job** are different. An agent runs a model: it reads, reasons and reports, keeps notes between runs, and costs tokens. A background job is one fixed command that the Cully daemon starts at a time of day, with no model, no judgement and no tokens; it does the same chore every time and leaves a log. Cully proposes a job only for a chore you repeat that one command can do (tests, a linter, `git fetch --all`, a build), picks a quiet time, shows the exact command and lets you change the time. Approve adds it to `~/.cully/jobs.json` (see [scheduled jobs](scheduled-jobs.md)); Reject forgets it. Before proposing anything, the Advisor checks what you already have. It is told your existing jobs and agents, and Cully drops a proposal that repeats one: the same command, the same program on the same folder as a job already waiting, or something you rejected. When the idea matches an existing job or agent but would change it (another target, a new time, different tools), it is offered as an **update** of that job or agent, shown as now and would-become, and approving it changes that one instead of adding a second. Cully proposes rarely and one idea at a time: an agent or a job is only proposed for a pattern seen in at least six sessions over at least three days (ten for "changed files and never ran tests"), never from the Claude folder itself, only the strongest pattern is offered, nothing new is offered while two proposals (one for jobs) wait for an answer, and it looks again at most every three days once ten more sessions have been analysed. Proposals about the same job collapse into one. Every proposal shows what it rests on ("Based on 7 sessions over 5 days", with the sessions listed) and is dropped if it is not about the repeated work, so a generic idea that merely sounds useful never appears. Only sessions from the last 30 days count. An agent is proposed only for the same kind of task done again and again; skipped tests become a tip, or a job that runs the tests, never an agent. Proposals come from the analysed sessions (their goals and problems, not your prompts, which the library never stores), about once a day once five new sessions are analysed, or when you press **Look for background jobs**. A proposed command is checked again when you approve it, against an allowlist of fixed shapes (git fetch or status, make with a known target, go test or build or vet, npm test or run, pytest); anything else is refused. See [scheduled jobs](scheduled-jobs.md).

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

Activity opens on a feed of cards, newest first and grouped by day: tasks that finished, failed or need an answer, runs of your agents, eval verdicts, and what the scout and advisor found. A strip of counts for the last 24 hours sits on top, and anything that needs a look is pulled to the front. Each card shows what it was, how it went, a preview and its folder; click it to see the full text (as formatted Markdown, with the links) and the facts behind it, with a button to the page where it lives. Filter by Tasks, Agents, Evals, Scout or Advice. The **This folder** tab keeps the file and command timeline of the current folder.

## The Harness page

**Overview** opens with **What to look at**: servers that need you to sign in or failed to connect, a warning when tasks load many tools (each tool description is sent every turn), servers not checked yet, and a missing instruction file, each with a button to the place to fix it. Under it the three harnesses sit side by side (model, permission mode, servers connected, skills, hooks, plugins, instruction files).

**Connected servers** is a list in the style of other MCP clients (Claude Desktop connectors, Cursor's MCP settings): one row per server with a state dot, name and address, tool count, a button for the one thing to do (How to sign in, Check again) and the switch for whether tasks load it. Servers that need you are grouped first; connectors you added in claude.ai sit in a collapsed group, since tasks do not need them. A row opens in place to say why a server is in its state and what to do: a server with no header credentials set uses OAuth sign-in, which only Claude Code can do (`claude`, then `/mcp`), and the steps and commands are there to copy; a server with headers set whose sign-in failed says its headers (names only, never values) were refused; a server that failed to connect says that is often temporary. States are checked when the page opens if they are over ten minutes old, and the top of the page says how old they are, with Check now. Before this change a server shown as a card had a coloured edge for its state (connected, needs sign-in, failed, not checked), whether it is a web address or a program, how many tools and prompts it offers, and what to do when it is not connected. Ones that need you come first; filter by state or search by name. The switch on a card decides whether tasks load that server, and the line above the cards counts the tools tasks will carry.

Opening a server shows its state and address, then its tools. A long list is grouped by service (for example `jira` and `confluence`) or, when there is no such prefix, by what the tools do. Each tool is marked **Reads**, **Changes things** or **Deletes**, a guess from its name that is there to help you scan a list of ninety; filter by it, search by name or description, and **Copy tool name** gives the `mcp__server__tool` name to use on the Permissions page. **Load descriptions and parameters** asks your Claude once to read the definitions.

## Servers, tools and prompts

On the Harness page each connected MCP server is a card with a switch for whether tasks load it. Open a card to see its tools, prompts and resources in a scrollable list with search. Claude Code reports every tool and prompt name when a task starts, so those are known after the first task. Cully's own server lists every tool with what it does and what it takes, read from its source. For any other server, **Load descriptions and parameters** asks your Claude once to read that server's tool definitions (a few thousand tokens) and remembers the answer. Claude Code does not report resources, so the Resources tab says so.

## Security model

- Every API call needs the token (cookie or bearer); changes also need the `X-Cully-UI` header; loopback binds reject other `Host` names.
- Agents run only in repositories you pass with `--repo`, in worktrees, with tools limited to Read, Glob, Grep, Edit, Write and Bash. They are told not to push.
- Token values stay in the agent's environment; the UI shows only a token number, and agent output is scrubbed of token values.
- Agent output is rendered as text only, under a strict content security policy.

## What is not stored

Agent transcripts live in memory and are lost when `cully ui` stops. The journal receives only what it always does: event class, outcome, project-relative path and a command hash. Worktrees and branches stay on disk until you remove them.
