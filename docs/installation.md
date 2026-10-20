---
title: Installer options
description: Choose an agent, set up PATH, pin a release or build Cully from source.
---

# Installer options

Follow the [quickstart](/quickstart) to run the memory stack on your laptop, or [connect to a remote MCP server](#connect-to-a-remote-memory-stack) to keep only the agent integration and advisor on your machine. The installer accepts `--agent claude`, `--agent codex`, `--agent cursor` or `--agent all`; every agent uses the single binary at `~/.local/bin/cully`. Without `--mcp-url`, run `cully setup --all` after installation to start the local memory stack and advisor daemon.

## macOS

For the full local memory stack, install and start Docker Desktop before running the installer from the [quickstart](/quickstart). The [remote MCP path](#connect-to-a-remote-memory-stack) does not need Docker. The installer clears the download quarantine flag on the `cully` binary. Open a new terminal afterwards so your zsh startup file is reloaded.

## Linux

For the full local memory stack, install Docker Engine with the Compose plugin and make sure your user can run `docker` without `sudo`. The [remote MCP path](#connect-to-a-remote-memory-stack) does not need Docker. Run the installer, then open a new terminal so your Bash startup file is reloaded.

Windows is not supported by the installer.

## Binary location and PATH

The only installed binary is `~/.local/bin/cully`. Agent paths are symlinks:

| Agent | Symlink target |
| --- | --- |
| Claude Code: `~/.claude/bin/cully` | `~/.local/bin/cully` |
| Codex: `~/.codex/bin/cully` | `~/.local/bin/cully` |
| Cursor: `~/.cursor/bin/cully` | `~/.local/bin/cully` |

The installer creates links for selected agents and existing agent directories, honoring `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `CURSOR_CONFIG_DIR`. Recognized older Cully copies become links; old Cully links are repointed without modifying their previous targets. Custom wrappers and system installations are preserved and reported. Later upgrades replace only the canonical binary, so all linked agents use the same version.

Install logs show the previous PATH selection, downloaded version, each link created or migrated, preserved paths, and the next setup command. Download or checksum mismatches leave existing installations in place.

The installer adds `~/.local/bin` to the zsh or Bash startup file, preserving existing settings. A piped installer cannot change its parent terminal's PATH or command cache. It explicitly prints the refresh and verification commands:

```sh
export PATH="$HOME/.local/bin:$PATH"
hash -r
command -v cully
cully version
```

Expected path: `~/.local/bin/cully`; expected version: the version printed by the installer.

## Connect to a remote memory stack

Your team can host Cully MCP, the private Cully data API, Mem0 and their databases on remote infrastructure. These services can run on separate hosts. Your machine needs only the Cully CLI, your coding agent, and the local advisor, skill and hooks installed by setup. Give the installer the public Cully MCP URL:

<InstallCommand template="curl -fsSL https://cully.net/install.sh | sh -s -- --agent {agent} --mcp-url https://mcp.example.com/mcp --oauth" />

The installer downloads the latest released CLI and runs setup with that URL. Setup registers the remote MCP connection, installs the selected agent's Cully skill and hooks, and starts the local advisor daemon. It does not start a local Docker stack or install Mem0 on your machine. Your agent sends memory tool calls to the remote MCP server; the advisor reads local session signals.

`--mcp-url` selects the remote server. Keep `--oauth` for an OAuth-protected team endpoint, as shown above; omit it for a trusted private single-user endpoint. `--agent` selects the integration; if omitted, setup detects installed coding agents. If the CLI is already installed, run `cully setup --agent AGENT --mcp-url URL --oauth` for a team endpoint. See [connect an agent](/agents) for sign-in and the [connection map](/team-deployment#connect-the-components) for remote services.

## Optional workflow skill

On your first interactive Cully launch or setup, Cully offers to create a `dev-workflow-rules` skill. Accept to describe your responsibilities, codebases, build/test/release habits, infrastructure aliases, intended read/write boundaries and advisor preferences. You see a preview and confirm before anything is saved. You can skip; Cully remembers that choice. Run `cully workflow` later, or `cully workflow --skip` to skip onboarding in advance.

The skill lives at `~/.cully/skills/dev-workflow-rules/SKILL.md` with private permissions and is linked into already configured agents' skill directories. Existing skills with that name are preserved. Edit the skill directly as your work changes. The advisor reads up to 12 KiB of this context for its next analysis, with common credential patterns redacted, and supplies it to your configured coding agent/model. Cully does not automatically save it to memory or add it to web research queries.

Use safe repository references, VM/cluster aliases and references to existing tool configuration. Do not include passwords, API tokens, private keys or credential-bearing URLs. The skill records your intended boundaries; it does not grant tool access or enable writes. The advisor remains read-only. Redirected commands, help/version queries and internal hooks do not prompt.

## Update Cully

```sh
cully update                     # install the latest stable CLI
cully update --check             # check without changing anything
cully update --version v0.15.0   # select a release, including a rollback
```

The updater verifies the release checksum and version before atomically replacing the CLI you invoked. Existing agent symlinks keep using that binary. A running local advisor restarts on the new version; open a new Cully terminal to use the updated wrapper. If the CLI is already at the selected version, nothing changes. If your installation directory requires administrator access, use the installer or package manager that owns it.

`cully update` updates the local CLI and running advisor. Refresh hooks and skills by rerunning `cully setup` with your original options. It preserves agent settings and MCP connections. It does not upgrade services, whether hosted locally or remotely: follow your deployment procedure for Cully MCP, the data API, Mem0 and databases. See [service deployment](/team-deployment) for component compatibility and upgrade steps.

Versions before this command was added can upgrade by rerunning the installer shown above.

## Build the CLI from source

If a prebuilt release cannot be downloaded, check GitHub release access or install from source with Go 1.26 or newer:

<InstallCommand template="curl -fsSL https://cully.net/install.sh | sh -s -- --from-source --agent {agent}" />

A source build can take several minutes while Go downloads its toolchain and dependencies. You can [inspect the installer](https://github.com/tavric-labs/cully/blob/main/install.sh) before running it.

## If the download stalls

The default installer downloads a prebuilt CLI from GitHub Releases and shows progress. It stops a binary download after two minutes or when the connection stalls, then prints a retry message; it does not silently switch to a source build. Check access to GitHub Releases and retry, or choose `--from-source` explicitly. Set `CULLY_VERSION` to a release tag when you need a particular version; a source build otherwise uses the current main branch.

## Check and adjust the installation

Run `cully status` to see the advisor, agent integration, MCP connection and continuity hooks. Run `cully setup --all` for the full laptop setup, or `cully setup --agent AGENT --mcp-url URL` to install integrations and start the advisor with an existing server. If only the server connection is missing, use `cully mcp add --agent AGENT --url URL`; add `--oauth` when that server requires sign-in. Cully keeps unrelated agent settings. For client-specific sign-in and commands, see [connect an agent](/agents).
