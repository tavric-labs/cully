---
title: Audit your harness
description: Check your agent setup and compare recent oversized tool-output estimates without changing configuration.
---

# Audit your harness

Your harness is the setup around the coding agent: instructions, permissions, memory, reusable skills, subagents, hooks and plugins. `cully harness` reports what it can find and suggests a next step for each gap. It changes nothing.

```sh
cully harness
cully harness --cwd /path/to/project
```

## Read the setup audit

| Component | What Cully looks for |
| --- | --- |
| Instructions | Project or global `CLAUDE.md` and `AGENTS.md`, project `.cursorrules` or Cursor rules |
| Guardrails | Claude permission keys or Codex approval/sandbox keys |
| Memory | A Cully reference in supported Claude, Codex, Cursor or project MCP configuration |
| Skills | Agent skill directories, including project `.claude`, `.codex`, `.cursor`, `.agents` and `.cully` locations |
| Subagents | Agent definitions, or actual delegations measured in recent Claude sessions |
| Hooks and scripts | Supported Claude, Codex or Cursor hook configuration keys |
| Plugins | Claude plugin configuration or installed plugin directories |

Detection combines supported agent configurations on this machine. A found file or key does not prove an integration is enabled, authenticated or delivering events. Check the panel’s observed MCP health separately. Plugins are optional; a gap is a prompt to review your setup, not a requirement to reach seven out of seven.

Run `cully setup` with your original options to refresh managed Cully integrations. Run `cully workflow` to install a starter private workflow skill. Review suggestions before making changes; the audit never installs anything.

## Compare measured output

Cully also streams recent local Claude transcripts for the exact project folder. It counts oversized tool results, their Bash and whole-file-read portions, and actual subagent calls. Results of roughly 2,000 estimated tokens or more count as oversized. Tokens are estimates based on output size, not billed usage.

```sh
cully harness --days 7 --max 20
cully harness --all --days 30
cully harness --all --only personal
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `--cwd DIR` | Current directory | Project to audit; relative paths are resolved |
| `--days N` | `30` | Recent-file window; must be positive |
| `--max N` | `40` | Maximum newest transcript files per project; must be positive |
| `--dir DIR` | Claude configuration’s `projects` directory | Alternate local Claude transcript root |
| `--all` | Off | Measure all project folders and rank the top ten by oversized output |
| `--only TEXT` | No filter | With `--all`, include folder names containing this text |

The sample uses file modification time and skips transcripts shorter than 20 nonempty lines or unreadable transcripts. Nested subagent transcripts are excluded to avoid counting the same work twice. `--all` measures projects separately before adding totals. Codex and Cursor transcript histories are not included.

Use tokens per session as a baseline: change one setup component, do a comparable amount of work, then rerun. Different tasks and session lengths can change the number. A smaller result does not establish that a component caused savings. With no eligible sessions, Cully says nothing was measured.

## In the terminal panel

The leftmost **Harness** column shows setup readiness, named gaps, recent output estimates, the model and pending verification. Active loops and nonzero errors also appear. Wide terminals place **Project & usage** and **Cully impact** beside it; narrow terminals stack them and shorten long gap lists to keep the panel bounded. Use the CLI for the complete audit and the advisor’s Tab view for full session instruments.

Setup checks refresh every 30 seconds. A background measurement samples up to 20 recent Claude sessions over 30 days, refreshed every five minutes. “Measuring…” means the first measurement is pending; “no recent sessions” means no eligible sample was found. These historical figures can appear while you run Codex or Cursor; they are not usage for the current agent session.

Transcript text stays local and is processed one line at a time. The panel keeps aggregate counts in memory, without saving or uploading source text. See [privacy](/privacy#harness-measurements), [the terminal](/terminal) and [the advisor](/advisor).
