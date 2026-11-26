---
name: cully-setup
description: Set up, check, repair or update Cully's memory stack (PostgreSQL, Mem0, the Cully MCP server and data API) and connect Claude Code, Codex or Cursor to it, or switch between local mode and a remote server. Use when the user asks to install or set up Cully, connect the Cully MCP, run Cully's Postgres or Mem0, fix "Cully MCP not connected" or "needs sign-in", or update or remove the stack.
---

<!-- cully:skill:managed -->
# Set up Cully's memory

Cully keeps memory in two ways. **Local mode** needs nothing installed: the browser UI keeps its session library, agents and advice in one SQLite file and recalls by keyword. It works, but it is less efficient: recall is by words not meaning, so agents may be given more context and use more tokens, and nothing is shared across machines. The **Cully MCP** adds PostgreSQL (the source of truth for notes) and Mem0 (semantic recall) behind an MCP server the agent calls. Your job is to get the user from one to the other safely, or to repair it.

## Safety first

- Never print, paste or log a credential. Setup generates database passwords, an MCP-to-data token, a Mem0 key and a signing secret; never `cat` the generated environment or config files, and never put a secret in a command line the user can see. If a token must be entered, the user types or pastes it themselves.
- Do not run `cully uninstall --purge-data`, `docker volume rm`, `docker system prune` or anything that deletes volumes unless the user asks for exactly that, after you have said it permanently deletes their memory.
- Image pulls and database startup are heavy. On a shared or company machine, say what will run and wait for a yes before starting the stack.
- Stay on the machine and scope the user named. Do not point this at a production database or cluster.
- `cully setup`, `cully run` and `cully ui` open long-running or interactive processes. Run setup yourself only when the user asked you to; for interactive ones (`cully run AGENT`, `cully handoff AGENT`) give the command to the user instead.

## 1. Find out which path the user wants

Ask one question if it is not clear, otherwise pick:

| Situation | Path |
| --- | --- |
| One person, one machine, wants private memory | **Full local stack**: `cully setup --all` |
| A Cully server already runs somewhere (team or another machine) | **Remote**: `cully setup --mcp-url URL` |
| Just trying Cully, or no Docker | **Stay in local mode**: do nothing; tell them what they give up (above) |

## 2. Check before changing anything (read only)

```sh
command -v cully && cully version        # is the CLI installed?
docker info >/dev/null 2>&1 && echo docker-ok      # full local stack only
docker compose version                              # needs the Compose plugin
cully status                                        # what Cully already sees
```

- No `cully`: tell the user to run the installer from the quickstart (docs: installation) and stop; do not pipe a download into a shell yourself.
- Docker missing or not running: the full stack needs Docker with the Compose plugin and permission to run `docker` without `sudo`. Offer the remote path or local mode instead of installing Docker for them.
- Already set up: if the agent has a working `cully` MCP (tools such as `cully_context`), do not run setup again; go to step 5.

## 3. Run setup

Full local stack, for the agents the user names (omit `--agent` to detect installed ones, or use `--agent all`):

```sh
cully setup --all --agent claude     # or codex, cursor, all
```

Remote server:

```sh
cully setup --agent claude --mcp-url https://HOST/mcp            # no sign-in
cully setup --agent claude --mcp-url https://HOST/mcp --oauth    # the server uses OAuth
```

Tell the user before running it that setup:

- downloads the matching release's Docker files and starts PostgreSQL, Mem0 (with its pgvector database), the private data API and the MCP server, and creates the schema (full stack only);
- generates private credentials once and reuses them on later runs, so running it again is safe and does not change a password behind an existing database;
- registers the MCP, the Cully skill, hooks and controls for the chosen agents and starts the advisor daemon;
- on an interactive local run can also install zsh with Oh My Zsh and pick a Nerd Font for the terminal, backing up existing settings; `--terminal-only` runs just that part if they want it separately.

It prints each stage and ends with `Setup complete`. Pulls and the first build take minutes: allow a long timeout and read the output for the failing stage rather than retrying blindly.

When it finishes: restart the agent. In Codex, review and trust the Cully hooks in `/hooks`.

## 4. A server that takes a token instead of OAuth

If the Cully MCP is behind a header token, not an OAuth sign-in, register it with the header so there is no sign-in step. The user supplies the token:

```sh
claude mcp add --transport http cully https://HOST/mcp --scope user --header "Authorization: Bearer <token>"
```

A server with no header set uses OAuth, and only the agent itself can finish it: in Claude Code run `/mcp`, choose `cully`, then Authenticate.

## 5. Verify, then show the user it works

1. In a new agent session, call a read tool (for example `cully_recent` or `cully_context`). A result, even an empty one, means the MCP is connected. An authentication error means sign-in is needed (step 4). A connection error means the stack or URL is down.
2. For a full local stack, `docker ps` shows the Cully containers healthy: the MCP server, data API, `db` (PostgreSQL), `mem0` and `mem0-db`. Report any that are restarting.
3. Open the browser UI with `cully ui url`. Its header changes from "Local mode (SQLite)" to the Cully MCP connected, and the Setup page shows ticks for the memory connection. Memory the UI kept locally before stays on the machine and is not lost.
4. Ask the agent to save a short note with `cully_log`, then recall it in a fresh session. That proves the whole path, PostgreSQL to Mem0.

## 6. When something is wrong

| Symptom | Likely cause and fix |
| --- | --- |
| `Cannot connect to the Docker daemon` | Docker is not running. Start Docker Desktop or the service, then run setup again. |
| `permission denied` on the Docker socket | The user is not in the `docker` group. Fix that for them to do; do not use `sudo` for setup. |
| A port is already in use | Another service holds it. Say which, and let the user stop it or choose; setup prints the MCP address it binds, on `127.0.0.1` by default. |
| Image pull fails or stalls | Network, a proxy, or a registry limit. Retry once, then report the exact error. |
| `Cully MCP needs sign-in` | OAuth server: `/mcp`, choose `cully`, Authenticate. Token server: step 4. |
| Setup succeeded but the agent has no Cully tools | The agent was not restarted, or in Codex the hooks were not trusted. |
| MCP connected but recall is empty | Mem0 indexes in the background, so text search may find a new note before semantic recall does. Wait a minute, then try again. |
| Want the newest version | `cully update` (add `--check` to only look). |
| Want it gone | `cully uninstall` stops the stack and removes the agent setup and **keeps the data**. `cully uninstall --purge-data` deletes the memory permanently, so ask first. |

## 7. Report back

End with what changed and the next step: which path ran, whether `Setup complete` appeared, the MCP address (never a secret), whether verification passed, and what the user must still do (restart the agent, sign in, trust the hooks).
