# Cully for VS Code

A thin front end for the [Cully](https://cully.net) CLI. It runs the commands you already have, so it needs Cully installed and set up (`cully setup --all`).

- **Run agents** in an integrated terminal: Claude Code, Codex or Cursor Agent through `cully run`.
- **Status bar** shows the current session risk; click it for the menu.
- **Read-only views** in the Cully output channel: status, replay, handoff summary, harness audit, token impact and self-improvement suggestions.
- **Rescue** a stuck session and **set the session task**.

Setup, uninstall and terminal preference changes stay in the CLI; the extension never rewrites agent or terminal configuration.

## Settings

| Setting | Default | Purpose |
| --- | --- | --- |
| `cully.path` | `cully` | Path to the executable |
| `cully.statusBar` | `true` | Show status in the status bar |
| `cully.refreshSeconds` | `60` | Status refresh interval |

## Develop

Open this folder in VS Code and press F5. Package with `npx @vscode/vsce package`.
