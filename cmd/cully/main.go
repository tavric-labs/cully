// Command cully helps users guide coding agents and improve their workflow.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/mcp-runtime/cully/internal/cully"
)

var version = "dev"

const help = `Cully — the harness runtime for coding agents and software projects.

Usage:
  cully setup --all [--agent claude|codex|cursor|all] [--oauth] [--prepare]
                                         Start the full local stack, integrations and advisor
  cully setup [--agent AGENT] --mcp-url URL [--oauth]
                                         Use an existing server; set up integrations and advisor
  cully setup --terminal-only [--skip-terminal-test]
                                         Install zsh/Oh My Zsh, select a Nerd Font and verify the terminal
  cully terminal check|configure|shell|test|restore
                                         Inspect, repair, exercise or restore terminal preferences
  cully uninstall [--purge-data]           Stop local stack and remove managed agent setup
  cully uninstall [claude|codex|cursor|all] Remove one or all agent integrations
  cully update [--check] [--version TAG]   Update this machine's CLI and running advisor
  cully workflow [--skip]                Install starter dev-workflow-rules skill
  cully harness [--cwd DIR]               Audit instructions, guardrails, memory, skills and hooks
  cully status [directory]                Show session and agent status
  cully rescue [--cwd DIR] [--agent claude|codex|cursor] [--no-ai]
                                          Diagnose a stuck session from recorded evidence
  cully suggestions [--request N]         Review suggested improvements; --request prints a setup request for your agent
  cully impact [--days N] [--log] [--explain]
                                          What Cully saved and cost; --explain shows how Net tokens are built
  cully eval [setup|harness|advisor|memory] Score Cully as an agent against planted ground truth; see docs/evals.md
  cully improve [--run] [--apply N] [--dismiss N]
                                          Review how Cully scored itself every 5 hours; approve changes that improve it
  cully savings [--days N]                Measure token cost against exploration avoided, from transcripts
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
	// Only foreground entry points may ask first-run questions; utility calls,
	// workers, redirected streams and native agent commands keep their IO.
	onboard := len(args) == 0
	if len(args) > 0 {
		switch args[0] {
		case "setup", "status":
			onboard = true
			for _, arg := range args[1:] {
				if arg == "--help" || arg == "-h" {
					onboard = false
				}
			}
		}
	}
	if onboard {
		if err := cully.MaybeWorkflowOnboarding(os.Stdin, os.Stdout); err != nil {
			return err
		}
	}
	if len(args) == 0 {
		fmt.Print(help)
		return nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Print(help)
	case "setup":
		return runSetup(args[1:])
	case "terminal":
		return cully.RunTerminal(os.Stdin, os.Stdout, args[1:])
	case "uninstall":
		return runUninstall(args[1:])
	case "update":
		return cully.RunUpdate(os.Stdout, version, args[1:])
	case "harness":
		return cully.RunHarness(os.Stdout, args[1:])
	case "workflow":
		return cully.RunWorkflowOnboarding(os.Stdin, os.Stdout, args[1:])
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
		return cully.RunSuggestions(os.Stdout, args[1:])
	case "eval":
		return cully.RunEval(os.Stdout, args[1:])
	case "improve":
		return cully.RunImprove(os.Stdout, os.Stdin, args[1:])
	case "impact":
		return cully.RunImpact(os.Stdout, args[1:])
	case "savings":
		return cully.RunSavings(os.Stdout, args[1:])
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
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("Cully setup supports macOS and Linux only")
	}
	options, err := parseSetup(args)
	if err != nil {
		return err
	}
	fmt.Printf("Cully %s setup\n", version)
	if options.terminalOnly {
		return cully.SetupTerminal(os.Stdin, os.Stdout, options.skipTerminalTest, options.terminal)
	}
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
		if err := cully.SetupTerminal(os.Stdin, os.Stdout, options.skipTerminalTest, options.terminal); err != nil {
			return err
		}
		fmt.Println("Setup complete. Restart your coding agent to load the Cully skill, hooks and MCP tools.")
		printT3CodeHint(os.Stdout, exec.LookPath)
		return nil
	}
	if err := runSelfHost(options.target, options.oauth, options.prepare, options.auth); err != nil {
		return err
	}
	printT3CodeHint(os.Stdout, exec.LookPath)
	if options.prepare {
		return nil
	}
	return cully.SetupTerminal(os.Stdin, os.Stdout, options.skipTerminalTest, options.terminal)
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
	target, endpoint               string
	oauth, prepare                 bool
	terminalOnly, skipTerminalTest bool
	terminal                       string
	auth                           mcpAuthOptions
}

func parseSetup(args []string) (setupOptions, error) {
	var options setupOptions
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.StringVar(&options.target, "agent", "", "coding agent: claude, codex, cursor, or all; default: detect installed agents")
	all := fs.Bool("all", false, "start the full stack on this machine, configure agents, and start the advisor")
	fs.StringVar(&options.endpoint, "mcp-url", "", "connect to an already deployed Cully MCP server")
	fs.BoolVar(&options.oauth, "oauth", false, "server uses OAuth; print sign-in instructions")
	fs.BoolVar(&options.prepare, "prepare", false, "with --all, prepare local configuration without starting services")
	fs.BoolVar(&options.terminalOnly, "terminal-only", false, "install the required zsh/Oh My Zsh and font setup without services or agents")
	fs.StringVar(&options.terminal, "terminal", "", "native terminal program; default: detect")
	fs.BoolVar(&options.skipTerminalTest, "skip-terminal-test", false, "configure terminal settings without the interactive wheel/divider/advisor exercise")
	fs.StringVar(&options.auth.Image, "mcp-auth-image", "", "testing only: override the automatically selected latest mcp-auth release image")
	fs.StringVar(&options.auth.ConfigDir, "mcp-auth-config-dir", "", "mount this directory of mcp-auth configuration files read-only at /etc/mcp-auth")
	fs.StringVar(&options.auth.EnvFile, "mcp-auth-env", "", "environment file for mcp-auth, including its configuration-file paths and provider secrets")
	if err := fs.Parse(args); err != nil {
		return setupOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return setupOptions{}, fmt.Errorf("setup does not accept positional agents; use cully setup --agent codex --all or cully setup --agent codex --mcp-url URL")
	}
	if *all && options.endpoint != "" {
		return setupOptions{}, fmt.Errorf("choose either --all for a local stack or --mcp-url for a deployed stack")
	}
	if options.terminalOnly && (*all || options.endpoint != "" || options.target != "" || options.oauth || options.prepare || options.auth.specified()) {
		return setupOptions{}, fmt.Errorf("--terminal-only cannot be combined with stack or agent setup options")
	}
	if !*all && options.endpoint == "" && !options.terminalOnly {
		return setupOptions{}, fmt.Errorf("setup requires --all to start Cully on this machine, or --mcp-url URL to connect to a deployed Cully stack")
	}
	if options.prepare && !*all {
		return setupOptions{}, fmt.Errorf("--prepare requires --all")
	}
	if options.auth.specified() {
		if !*all || !options.oauth {
			return setupOptions{}, fmt.Errorf("mcp-auth options require --all --oauth")
		}
		if (options.auth.ConfigDir == "") != (options.auth.EnvFile == "") {
			return setupOptions{}, fmt.Errorf("provide --mcp-auth-config-dir and --mcp-auth-env together")
		}
	}
	if options.target != "" && options.target != "claude" && options.target != "codex" && options.target != "cursor" && options.target != "all" {
		return setupOptions{}, fmt.Errorf("choose --agent claude, codex, cursor, or all")
	}
	var emptyFlag string
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "agent" && options.target == "" || f.Name == "mcp-url" && options.endpoint == "" {
			emptyFlag = f.Name
		}
		if f.Name == "mcp-auth-image" && options.auth.Image == "" || f.Name == "mcp-auth-config-dir" && options.auth.ConfigDir == "" || f.Name == "mcp-auth-env" && options.auth.EnvFile == "" {
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
	case "terminal-check-output":
		return cully.RunTerminalCheckOutput(os.Stdin, os.Stdout)
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
	case "guard":
		if len(args) != 2 {
			return fmt.Errorf("internal guard requires an agent")
		}
		cully.RunGuardHook(args[1], os.Stdin, os.Stdout)
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
