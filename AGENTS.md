# Cully development

- One Go module, with CLI, MCP and private data API entry points.
- Keep memory validation and authenticated owner scoping shared across transports.
- Preserve user-owned agent configuration; apply managed changes explicitly.
- `cully setup` owns local services, agent integrations and advisor startup; use `--mcp-url` for an existing server. Keep setup scripts, installer and docs on this single command.
- Run Go formatting, vet and race tests before publishing changes.
- Every PR must update `CHANGELOG.md` with a concise entry under `Unreleased` describing the final user-visible change, fix, docs or maintenance work. At release time, move those entries into the versioned section and keep `Unreleased` ready for the next PR. Follow `.github/PULL_REQUEST_TEMPLATE.md`; the changelog CI check enforces the file update.
- Keep credentials, transcripts and personal memory out of Git.
- See docs/architecture.md for the current system design.
- Setup and uninstall configure agents in user-level directories, independent of the current project. Never generate project `.cully` folders or agent instructions during ordinary setup; legacy cleanup requires `--migrate-project DIR`.
- The canonical Cully skill is `skills/cully/SKILL.md`. Agent installations live in their user-level configuration; keep project identity and authenticated memory scoping intact across projects.
