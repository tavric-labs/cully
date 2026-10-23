package cully

import (
	"os"
	"path/filepath"
	"strings"
)

// Only Cully's launcher sets this for the child agent. Native agent launches
// must not receive automatic Cully context or create advisor artifacts.
func wrappedSessionActive() bool { return os.Getenv("CULLY_RUN_ACTIVE") == "1" }

func runtimeAgentDir(agent string) string {
	spec, ok := lookupAgentSpec(agent)
	if !ok {
		return ""
	}
	return filepath.Join(spec.ConfigDir(), ".cully-runtime")
}

func runtimeSkillPath(agent string) string {
	return filepath.Join(runtimeAgentDir(agent), "skills", "cully", "SKILL.md")
}

// The shell guard also prevents invoking the Cully binary at all during an
// ordinary native session. The Go entry points independently check activation.
func scopedHookCommand(command string) string {
	return `if [ "${CULLY_RUN_ACTIVE:-}" = "1" ]; then ` + command + `; fi`
}

// unscopedHookCommand returns the command inside scopedHookCommand's guard, so
// hooks written before or after scoping are recognised as the same entry.
func unscopedHookCommand(command string) string {
	const suffix = "; fi"
	prefix := strings.TrimSuffix(scopedHookCommand(""), suffix)
	if strings.HasPrefix(command, prefix) && strings.HasSuffix(command, suffix) {
		return strings.TrimSuffix(strings.TrimPrefix(command, prefix), suffix)
	}
	return command
}
