---
title: Quickstart
description: Install Cully, try a task handoff, and use Mem0 recall and advisor suggestions.
---

# Quickstart

Set up Cully on your laptop with Docker Compose and Claude Code, Codex or Cursor. Then try one task and pick it up in a new session. If your memory services already run elsewhere, use the [remote MCP installation path](/installation#connect-to-a-remote-memory-stack); it installs the advisor and agent integration locally without starting Docker.

## 1. Install Cully

Pick your agent, then copy the command:

<InstallCommand />

The installer puts Cully at `~/.local/bin/cully`, links it into agent `bin` directories and adds the canonical directory to your shell's PATH when needed. The setup command in step 2 installs the integrations and starts all components, including the advisor. If your current shell cannot find `cully`, run `~/.local/bin/cully setup --all` directly. Setup installs the required zsh environment; reopen the native terminal afterwards.

For source builds, version pinning or custom PATH setup, see [installer options](/installation).

## 2. Set up Cully once for your user

With Docker running, connect the agent you installed:

<InstallCommand kind="setup" />

Use the same agent you picked in step 1, or run bare `cully setup --all` to detect installed agents automatically. This one command downloads the matching local stack, creates private credentials that stay stable across restarts, and starts PostgreSQL, Mem0, the private data API and Cully MCP in Docker. It connects your agent to MCP, configures the Cully skill, session hooks and [agent-specific controls](/agents), and starts the local advisor daemon if needed. Setup only reports completion after the advisor is running. A component failure exits with an error; fix it and rerun the same setup command to retry. See [what runs on your laptop](/hosting#what-starts-on-your-laptop).

Setup writes to your home and agent user configuration, regardless of where you run it. No project `.cully` directory or repository changes are required. It works across your projects while keeping memory associated with each active repository. See [paths and migration from older installs](/installation#one-setup-across-projects).

When `Setup complete` appears, restart your agent. In Codex, review and trust the installed Cully hooks in `/hooks` when asked. You do not need a repository checkout or OAuth for this laptop setup.

Local interactive setup also installs the required zsh + Oh My Zsh environment and selects a Nerd Font preset in your native macOS/Linux terminal, backing up existing shell and font settings. Cully prints the changes and applies them automatically; OS authentication may still be required. Warp and other third-party terminal apps are outside support. Follow the [live terminal check](/terminal#configure-and-verify-your-terminal), confirming Unicode/icons with `u` and colors with `c`, then wheel, divider and advisor gestures before pressing `q`. `--skip-terminal-test` skips the exercise. Headless/SSH service setup leaves desktop settings alone and marks them untested. Use `cully setup --terminal-only` locally to configure the terminal without restarting services.

Setup prints each stage, including image pulls and builds, database startup, migrations and agent connection. It confirms that private credentials are ready without showing their values. Mem0's local embedding model is downloaded into its Docker image on the first build.

## How Cully keeps sessions focused

| Need | What Cully does | Details |
| --- | --- | --- |
| Start without rereading an old conversation | `cully_context` gives the agent three short note previews by default. It opens a full record with `cully_get` only if needed. | [Bounded context](/session-optimization#what-cully-optimizes) |
| Find a decision phrased differently | Mem0 indexes authored note fields by meaning. Cully checks recall results against the owner’s live PostgreSQL records. | [Mem0 recall](/mem0#what-to-expect) |
| Leave a usable handoff | The session hook asks the agent to look up relevant work. The Cully skill asks for one `cully_log` note with the task, approach, result, checks, gaps and next step. Claude Code and Cursor also have turn-end reminders; Codex has no blocking stop hook. No transcript is uploaded. | [Saved notes](/memory#what-you-can-save) |
| Catch repeated effort in the current session | The local advisor uses available agent signals to suggest context controls, narrower searches, useful tools or a missing check. You review changes before applying them. | [Advisor suggestions](/advisor#preview-a-suggestion) |

These can reduce repeated reading and material in the model’s input. They are not a measured promise of lower billed tokens or faster work; MCP and advisor calls also have a cost. The [session optimization guide](/session-optimization) explains the full flow and each agent’s limits.

## 3. Work on a real task

Open a project in your connected agent and work as usual. You do not need to mention Cully in every prompt. Its [hooks and skill](/session-optimization#what-happens-during-a-task) guide the agent during substantive work. On your first task, no prior notes may exist yet.

After a useful change or decision, ask “What did you save with Cully for this task?” Look for a successful `cully_log` result. The hook prompts the agent to make that MCP call; it does not write a note itself, and routine replies are skipped. When the client supplies a session ID, the hook gives the agent an opaque `session_ref` to group notes and a native `agent_session_id` to reopen the coding agent's conversation. No transcript is saved. If MCP is unavailable, the agent should say that shared memory was not saved.

After a successful write, PostgreSQL holds the source note. Mem0 indexes it in the background, so [text search may find a new note first](/mem0#what-to-expect). The [memory tools](/memory#find-and-change-notes) let you search or correct it explicitly.

## 4. Continue in a new session

Start a fresh session in the same project, or use another agent connected to the same Cully server. Ask, for example, “Continue [task] in this repository. What changed, which checks passed and what remains?” The agent should ground its answer in the saved note. If text search misses a decision, it can try Mem0's semantic mode; if a preview is insufficient, it can open that one full note.

Ask the new agent which Cully note supports its answer; it should give you a date or record ID. If MCP is unavailable, the agent can keep working but cannot retrieve shared notes until it reconnects.

## 5. Check the local advisor

The memory hooks handle continuity. Separately, the advisor watches session signals your agent makes available. Run your agent through the [Cully terminal](/terminal) for one health bar and advisor panel on every agent. Cully does not compact a session or run tests for you. See the [agent-specific controls](/session-optimization#use-the-agent-s-native-controls).

In a terminal, run:

```sh
cully status
cully suggestions
```

You can also use Cully inside your agent:

| Agent | In-session control |
| --- | --- |
| Claude Code | Start `cully run claude` for the health bar and advisor panel, or run `/cully suggestions`. |
| Codex | Start `cully run codex` for the health bar and advisor panel, or run `/prompts:cully suggestions`. |
| Cursor | Start `cully run cursor` for the health bar and advisor panel, or run the user-level `/cully suggestions` command. |

If Cully lists a numbered improvement, use its number with `cully apply 1 --dry-run` to inspect the proposed change. Review it before applying. `cully status` also shows whether the advisor daemon is running; if startup failed, run `cully setup --agent codex --all` with your agent name to retry. See the [advisor commands](/advisor#commands) for the rest.

To stop and disconnect the local stack later, run `cully uninstall`. It keeps
your notes by default. The [self-hosting guide](/hosting#uninstall-the-laptop-stack)
explains the explicit data deletion option.

## Check your setup and keep it current

```sh
cully harness         # read-only setup audit and recent output baseline
cully workflow        # optional private profile; installs a starter to fill in
cully update --check  # check for a newer CLI without changing it
```

Read [audit your harness](/harness) to interpret gaps and measurements. Run `cully update` to install a verified CLI release; upgrade memory services with your deployment tooling. See [installation and updates](/installation).
