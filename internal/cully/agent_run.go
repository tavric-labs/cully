package cully

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// RunAgent adds a panel only to interactive coding sessions. Utility commands
// and redirected streams retain the native CLI arguments, environment and IO.
func RunAgent(name string, args []string, input, output, stderr *os.File) error {
	return RunAgentOptions(name, args, input, output, stderr, false)
}

// RunAgentOptions is RunAgent with an explicit sidebar choice for callers
// that accept a --sidebar flag.
func RunAgentOptions(name string, args []string, input, output, stderr *os.File, sidebar bool) error {
	agent, err := lookupPaneAgent(name)
	if err != nil {
		return err
	}
	if os.Getenv("CULLY_NO_PANE") != "1" && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd())) && !agentUtilityCommand(agent.Name, args) {
		if err := MaybeWorkflowOnboarding(input, output); err != nil {
			return err
		}
		return agentRunResult(RunPaneOptions(name, args, input, output, sidebar))
	}
	cmd := exec.Command(agent.Binary, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, stderr
	return agentRunResult(runAgentCommand(cmd))
}

func agentUtilityCommand(agent string, args []string) bool {
	// Routing only: the original argv always goes intact to the executable.
	valueFlags := map[string]bool{}
	commands := ""
	switch agent {
	case "codex":
		commands = "agents exec e review login logout mcp mcp-server plugin app app-server remote-control completion update doctor sandbox debug apply a queue archive delete migrate-rollouts unarchive cloud exec-server features help"
		for _, flag := range strings.Fields("-c --config -m --model -p --profile -s --sandbox -a --ask-for-approval -C --cd --add-dir -i --image --enable --disable --local-provider --remote --remote-auth-token-env") {
			valueFlags[flag] = true
		}
	case "claude":
		commands = "auth mcp doctor install update upgrade setup-token plugin plugins agents help"
		for _, flag := range strings.Fields("--model --effort --agent --permission-mode --settings --setting-sources --add-dir --allowedTools --disallowedTools --tools --mcp-config --system-prompt --append-system-prompt --output-format --input-format --resume -r") {
			valueFlags[flag] = true
		}
	case "cursor":
		commands = "login logout status about update upgrade install mcp models agent help"
		for _, flag := range strings.Fields("--model --output-format --workspace --resume") {
			valueFlags[flag] = true
		}
	}
	utility, firstPosition := false, true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--":
			return utility // subsequent values are a prompt
		case "--help", "-h", "--version", "-V", "--no-alt-screen":
			return true
		case "--print", "-p":
			if agent == "claude" || agent == "cursor" {
				return true
			}
		}
		if valueFlags[arg] {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if firstPosition {
			for _, command := range strings.Fields(commands) {
				if arg == command {
					utility = true
				}
			}
			firstPosition = false
		}
	}
	return utility
}

// AgentExitError distinguishes a native CLI exit from a Cully startup failure.
// The native diagnostic has already been written to its own stderr.
type AgentExitError struct{ Err *exec.ExitError }

func (e *AgentExitError) Error() string { return e.Err.Error() }
func (e *AgentExitError) Unwrap() error { return e.Err }
func (e *AgentExitError) ExitCode() int { return agentExitCode(e.Err) }

func agentRunResult(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &AgentExitError{Err: exit}
	}
	return err
}
