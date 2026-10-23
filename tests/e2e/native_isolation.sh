#!/usr/bin/env bash
# After setup, a direct claude, codex or cursor-agent launch must stay native:
# nothing Cully-owned is discoverable by the agent, and every Cully hook is
# gated on `cully run`, so it exits silently without that marker.
# Usage: native_isolation.sh CLAUDE_DIR CODEX_DIR CURSOR_DIR
set -euo pipefail
claude_dir="$1" codex_dir="$2" cursor_dir="$3"

# Setup configures the agents it detects; Codex is always present in CI.
test -f "$codex_dir/.cully-runtime/skills/cully/SKILL.md"
for dir in "$claude_dir" "$codex_dir" "$cursor_dir"; do
  [[ -d "$dir" ]] || continue
  test ! -e "$dir/skills/cully"
  test ! -e "$dir/skills/dev-workflow-rules"
  [[ ! -d "$dir/.cully-runtime" ]] || test -f "$dir/.cully-runtime/skills/cully/SKILL.md"
done
test ! -e "$codex_dir/AGENTS.md"
if [[ -f "$claude_dir/settings.json" ]]; then
  ! grep -Fq 'statusLine' "$claude_dir/settings.json"
fi

python3 - "$claude_dir/settings.json" "$codex_dir/hooks.json" "$cursor_dir/hooks.json" <<'PY'
import json, os, subprocess, sys

gate = 'if [ "${CULLY_RUN_ACTIVE:-}" = "1" ]; then '
total = 0
checked = 0
for path in sys.argv[1:]:
    if not os.path.exists(path):
        continue
    checked += 1
    with open(path) as f:
        hooks = json.load(f).get("hooks", {})
    commands = []
    for groups in hooks.values():
        for group in groups:
            if "command" in group:
                commands.append(group["command"])
            commands += [h["command"] for h in group.get("hooks", []) if "command" in h]
    if not commands:
        sys.exit(f"{path}: no Cully hooks to check")
    env = {k: v for k, v in os.environ.items() if not k.startswith("CULLY_")}
    for command in commands:
        total += 1
        if not command.startswith(gate):
            sys.exit(f"{path}: hook runs outside cully run: {command}")
        result = subprocess.run(["sh", "-c", command], input="{}", capture_output=True, text=True, env=env, timeout=30)
        if result.returncode != 0 or result.stdout or result.stderr:
            sys.exit(f"{path}: direct launch saw output from {command}: rc={result.returncode} {result.stdout!r} {result.stderr!r}")
if checked == 0:
    sys.exit("no agent hook configuration found")
print(f"{total} hooks are silent outside cully run")
PY
