package cully

import (
	"fmt"
	"strings"
)

// paneAgent describes how the shared Cully terminal starts one coding agent.
// Every agent runs in the same terminal wrapper; only the launch command and
// the optional native-footer reader differ.
type paneAgent struct {
	// Name is the stable agent id used for hooks, advisor workers and state.
	Name string
	// Binary is the executable started inside the terminal.
	Binary string
	// NativeFooter is true when the agent prints an instrument footer that the
	// wrapper can read and move into the Cully panel (Codex today).
	NativeFooter bool
	// ResumeID extracts an explicit native session id from the arguments.
	ResumeID func(args []string) string
}

// paneAgents resolves launch entries from the single agent catalog, so the
// wrapper never repeats per-agent launch knowledge.
var paneAgents = func() map[string]paneAgent {
	m := make(map[string]paneAgent, len(agentCatalog()))
	for _, spec := range agentCatalog() {
		m[spec.ID] = paneAgent{
			Name:         spec.ID,
			Binary:       spec.Binary,
			NativeFooter: spec.NativeFooter,
			ResumeID:     spec.ResumeID,
		}
	}
	return m
}()

// lookupPaneAgent resolves a registered agent, or treats any other name as a
// generic agent whose executable has that name. This lets a coding agent that
// Cully does not know yet run in the same terminal with the common instruments.
func lookupPaneAgent(name string) (paneAgent, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t\n") {
		return paneAgent{}, fmt.Errorf("usage: cully run AGENT [ARGS...]")
	}
	if agent, ok := paneAgents[name]; ok {
		return agent, nil
	}
	return paneAgent{Name: safeAgentName(name), Binary: name}, nil
}

// safeAgentName reduces an executable path to a short id that is safe in file
// names and environment values.
func safeAgentName(binary string) string {
	if i := strings.LastIndex(binary, "/"); i >= 0 {
		binary = binary[i+1:]
	}
	var out strings.Builder
	for _, r := range strings.ToLower(binary) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	if out.Len() == 0 {
		return "agent"
	}
	return out.String()
}

func (a paneAgent) resumeID(args []string) string {
	if a.ResumeID == nil {
		return ""
	}
	return a.ResumeID(args)
}
