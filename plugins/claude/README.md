# Cully for Claude Code

Solo: a coding-agent harness runtime. Team: a project management harness runtime for people and AI agents. Needs the cully CLI on PATH.

This plugin adds Cully's hooks and skill to Claude Code. It is one of two install routes: `cully setup` installs the same hooks, so use **one** of them or each hook runs twice.

## Before you install

- Install the `cully` CLI first. The hooks run `cully` from your `PATH`: https://docs.cully.net/installation
- The plugin does not connect memory. You run the Cully MCP server on your own infrastructure, so connect it yourself with `cully mcp add --agent claude code --url URL`.
- Hooks send nothing off your machine. See https://docs.cully.net/privacy

## Install

```
/plugin marketplace add tavric-labs/cully
/plugin install cully@cully
```

Full guide: https://docs.cully.net/plugins

Source and issues: https://github.com/tavric-labs/cully
