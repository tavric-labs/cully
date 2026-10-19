// Command cully helps users guide coding agents and improve their workflow.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/mcp-runtime/cully/internal/cully"
)

var version = "dev"

const help = `Cully — your agent's copilot, your team's workspace.

Usage:
  cully setup --all [--agent claude|codex|cursor|all] [--oauth] [--prepare]
                                         Start the full local stack, integrations and advisor
  cully setup [--agent AGENT] --mcp-url URL [--oauth]
                                         Use an existing server; set up integrations and advisor
  cully uninstall [--purge-data]           Stop local stack and remove managed agent setup
  cully uninstall [claude|codex|cursor|all] Remove one or all agent integrations
  cully status [directory]                Show session and agent status
  cully rescue [--cwd DIR] [--agent claude|codex|cursor] [--no-ai]
                                          Diagnose a stuck session from recorded evidence
  cully suggestions                       Review suggested improvements
  cully task [NAME]                       Show or set the session task (--clear removes it)
  cully workspace --mcp-url URL --input FILE
                                         Team tasks, handoffs, review and shared lessons (OAuth)
  cully run AGENT [ARGS...]               Run any coding agent in the Cully terminal
  cully claude|codex|cursor [ARGS...]     Shortcuts for cully run claude, codex and cursor-agent
  cully timeline [--all] [--session ID] [--cwd DIR]
                                         Show what happened in a coding session
  cully replay [--session ID] [--cwd DIR] [--files] [--speed N|--instant] [--json] [--html [FILE]]
                                         Replay what the agent did: steps, files, commands, checks
  cully handoff [AGENT] [--print] [--cwd DIR]
                                         Hand the session to another coding agent
  cully apply <n> [--dry-run] [--yes] [--cwd DIR]
  cully mcp add --url URL [--agent claude|codex|cursor] [--oauth]
  cully version

Solo: session health, advice, replay, handoff and private memory.
Team: project tasks, ownership, checkpoints, review evidence and shared lessons.
Works around Claude Code, Codex, Cursor and other coding agents.
Docs: https://docs.cully.net
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit *cully.AgentExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(help)
		return nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Print(help)
	case "setup":
		return runSetup(args[1:])
	case "uninstall":
		return runUninstall(args[1:])
	case "status":
		if len(args) > 2 {
			return fmt.Errorf("usage: cully status [directory]")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if len(args) == 2 {
			cwd = args[1]
		}
		cully.RunStatus(os.Stdout, cwd)
	case "rescue":
		return cully.RunRescue(os.Stdout, args[1:])
	case "task":
		return cully.RunTask(os.Stdout, args[1:])
	case "workspace":
		return runWorkspace(args[1:])
	case "suggestions":
		cully.RunList(os.Stdout)
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: cully run AGENT [ARGS...]")
		}
		return cully.RunAgent(args[1], args[2:], os.Stdin, os.Stdout, os.Stderr)
	case "claude", "codex", "cursor":
		return cully.RunAgent(args[0], args[1:], os.Stdin, os.Stdout, os.Stderr)
	case "timeline":
		return cully.RunTimeline(os.Stdout, args[1:])
	case "replay":
		return cully.RunReplay(os.Stdout, args[1:])
	case "handoff":
		return cully.RunHandoff(args[1:], os.Stdin, os.Stdout)
	case "apply":
		n, yes, dryRun, cwd, err := parseApply(args[1:])
		if err != nil {
			return err
		}
		return cully.RunApply(n, cwd, yes, dryRun)
	case "version", "--version", "-v":
		fmt.Println("cully", version)
	case "mcp":
		if len(args) < 2 || args[1] != "add" {
			return fmt.Errorf("usage: cully mcp add --url URL [--agent claude|codex|cursor] [--oauth]")
		}
		fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
		agent := fs.String("agent", "", "coding agent; default: detect configured agents")
		endpoint := fs.String("url", "", "MCP URL of your deployment (required)")
		oauth := fs.Bool("oauth", false, "print sign-in instructions for an OAuth-protected server")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if len(fs.Args()) != 0 {
			return fmt.Errorf("mcp add: unexpected arguments")
		}
		if *endpoint == "" {
			return fmt.Errorf("mcp add: --url is required")
		}
		return cully.AddMCP(os.Stdout, *agent, *endpoint, *oauth)
	case "_internal":
		return runInternal(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run cully help", args[0])
	}
	return nil
}

func runSetup(args []string) error {
	options, err := parseSetup(args)
	if err != nil {
		return err
	}
	fmt.Printf("Cully %s setup\n", version)
	if options.endpoint != "" {
		fmt.Println("Setting up Cully with an existing MCP server")
		var targets []string
		if options.target != "" {
			targets = append(targets, options.target)
		}
		if err := cully.InstallWithMCP(options.endpoint, options.oauth, targets...); err != nil {
			return err
		}
		if err := checkSetupMCP(os.Stdout, options.endpoint, options.oauth); err != nil {
			return err
		}
		fmt.Println("Setup complete. Restart your coding agent to load the Cully skill, hooks and MCP tools.")
		printT3CodeHint(os.Stdout, exec.LookPath)
		return nil
	}
	if err := runSelfHost(options.target, options.oauth, options.prepare); err != nil {
		return err
	}
	printT3CodeHint(os.Stdout, exec.LookPath)
	return nil
}

// printT3CodeHint tells T3 Code users that Cully already works in it. T3 Code
// starts the real agent CLIs, so setup needs no T3 Code configuration. The
// hint is informational and never changes anything.
func printT3CodeHint(w io.Writer, lookPath func(string) (string, error)) {
	if _, err := lookPath("t3"); err != nil {
		return
	}
	fmt.Fprintln(w, "T3 Code detected: agents it starts use the Cully MCP and hooks configured above. See https://docs.cully.net/t3code")
}

type setupOptions struct {
	target, endpoint string
	oauth, prepare   bool
}

func parseSetup(args []string) (setupOptions, error) {
	var options setupOptions
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.StringVar(&options.target, "agent", "", "coding agent: claude, codex, cursor, or all; default: detect installed agents")
	all := fs.Bool("all", false, "start the full stack on this machine, configure agents, and start the advisor")
	fs.StringVar(&options.endpoint, "mcp-url", "", "connect to an already deployed Cully MCP server")
	fs.BoolVar(&options.oauth, "oauth", false, "server uses OAuth; print sign-in instructions")
	fs.BoolVar(&options.prepare, "prepare", false, "with --all, prepare local configuration without starting services")
	if err := fs.Parse(args); err != nil {
		return setupOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return setupOptions{}, fmt.Errorf("setup does not accept positional agents; use cully setup --agent codex --all or cully setup --agent codex --mcp-url URL")
	}
	if *all && options.endpoint != "" {
		return setupOptions{}, fmt.Errorf("choose either --all for a local stack or --mcp-url for a deployed stack")
	}
	if !*all && options.endpoint == "" {
		return setupOptions{}, fmt.Errorf("setup requires --all to start Cully on this machine, or --mcp-url URL to connect to a deployed Cully stack")
	}
	if options.prepare && !*all {
		return setupOptions{}, fmt.Errorf("--prepare requires --all")
	}
	if options.target != "" && options.target != "claude" && options.target != "codex" && options.target != "cursor" && options.target != "all" {
		return setupOptions{}, fmt.Errorf("choose --agent claude, codex, cursor, or all")
	}
	var emptyFlag string
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "agent" && options.target == "" || f.Name == "mcp-url" && options.endpoint == "" {
			emptyFlag = f.Name
		}
	})
	if emptyFlag != "" {
		return setupOptions{}, fmt.Errorf("--%s requires a value", emptyFlag)
	}
	return options, nil
}

func parseApply(args []string) (n int, yes, dryRun bool, cwd string, err error) {
	if len(args) == 0 {
		err = fmt.Errorf("usage: cully apply <n> [--dry-run] [--yes] [--cwd DIR]")
		return
	}
	n, err = strconv.Atoi(args[0])
	if err != nil || n < 1 {
		err = fmt.Errorf("apply: suggestion number must be a positive integer")
		return
	}
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.BoolVar(&yes, "yes", false, "apply without a prompt")
	fs.BoolVar(&dryRun, "dry-run", false, "show plan only")
	fs.StringVar(&cwd, "cwd", "", "project directory")
	err = fs.Parse(args[1:])
	if err == nil && len(fs.Args()) != 0 {
		err = fmt.Errorf("apply: unexpected arguments")
	}
	return
}

// Internal entry points are installed by Cully; they are not user commands.
func runInternal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing internal action")
	}
	switch args[0] {
	case "statusline":
		cully.RunStatusline(os.Stdin, os.Stdout)
	case "analyze":
		cully.RunAnalyze(os.Stdin)
	case "cleanup":
		cully.RunCleanup(os.Stdin)
	case "continuity":
		if len(args) != 3 {
			return fmt.Errorf("internal continuity requires agent and event")
		}
		cully.RunContinuityHook(args[1], args[2], os.Stdin, os.Stdout)
	case "codex-signal":
		cully.RunSignalHook(os.Stdin)
	case "pane-signal":
		if len(args) != 2 {
			return fmt.Errorf("internal pane-signal requires an agent")
		}
		cully.RunPaneSignalHook(args[1], os.Stdin)
	case "daemon":
		cully.RunDaemon()
	case "stop-daemon":
		return cully.StopDaemon()
	case "worker":
		if len(args) != 4 {
			return fmt.Errorf("internal worker requires signals, session and directory")
		}
		cully.RunWorker(args[1], args[2], args[3])
	case "self-hosted-credentials":
		return runSelfHostCredentials(args[1:])
	case "self-hosted-ports":
		return runSelfHostPorts(args[1:])
	default:
		return fmt.Errorf("unknown internal action %q", args[0])
	}
	return nil
}
