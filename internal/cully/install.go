package cully

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cullyskill "github.com/mcp-runtime/cully/skills/cully"
)

// Install registers cully for one or more coding agents. With no target it
// auto-detects coding agents installed on this machine and installs those
// integrations.
func Install(targets ...string) error {
	return InstallWithMCP("", false, targets...)
}

// InstallWithMCP installs local guidance and optionally registers one MCP URL
// for each selected agent. The operator deploys the server separately.
func InstallWithMCP(endpoint string, oauth bool, targets ...string) error {
	if err := installAgents(endpoint, oauth, targets...); err != nil {
		return err
	}
	fmt.Println("Starting or checking the advisor daemon")
	if err := StartDaemonDetached(); err != nil {
		return fmt.Errorf("advisor startup failed: %w; rerun cully setup with the same options to retry", err)
	}
	fmt.Println("Advisor ready; inspect with cully status")
	if endpoint == "" {
		fmt.Println("Connect shared memory with: cully mcp add --url URL")
	}
	return nil
}

// installAgents writes each agent's user-level integration without starting
// any process, so it can run against a scratch home.
func installAgents(endpoint string, oauth bool, targets ...string) error {
	if endpoint != "" {
		if err := validateMCPURL(endpoint); err != nil {
			return err
		}
	}
	if len(targets) == 0 {
		targets = detectedInstallTargets()
		fmt.Printf("Detected coding agents: %s\n", strings.Join(targets, ", "))
	}
	for _, target := range expandInstallTargets(targets) {
		fmt.Printf("Configuring %s hooks and controls for this user\n", target)
		switch target {
		case "claude":
			if err := installClaude(); err != nil {
				return err
			}
		case "codex":
			if err := installCodex(); err != nil {
				return err
			}
		case "cursor":
			if err := installCursor(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown install target %q (use claude, codex, cursor, or all)", target)
		}
		fmt.Printf("Installing the Cully skill for %s\n", target)
		if err := installMemorySkill(target); err != nil {
			return err
		}
		if endpoint != "" {
			fmt.Printf("Connecting %s to Cully MCP at %s\n", target, endpoint)
			if err := AddMCP(os.Stdout, target, endpoint, oauth); err != nil {
				return err
			}
		}
	}
	return nil
}

func installMemorySkill(target string) error {
	var dir string
	switch target {
	case "claude":
		dir = ConfigDir()
	case "codex":
		dir = CodexConfigDir()
	case "cursor":
		dir = CursorConfigDir()
	default:
		return fmt.Errorf("unknown skill target %q", target)
	}
	legacy := filepath.Join(dir, "skills", "cully", "SKILL.md")
	if err := removeOwnedFile(legacy, "<!-- cully:skill:managed -->"); err != nil {
		return err
	}
	removeWorkflowLink(filepath.Join(dir, "skills", "dev-workflow-rules"))
	path := runtimeSkillPath(target)
	wrote, err := writeOwnedFile(path, "<!-- cully:skill:managed -->", cullyskill.Content)
	if err != nil {
		return err
	}
	if !wrote {
		fmt.Printf("Preserved user-owned Cully skill -> %s\n", path)
	}
	linkWorkflowSkill(os.Stdout, dir)
	return nil
}

func detectedInstallTargets() []string {
	var ordered []string
	for _, spec := range agentCatalog() {
		ordered = append(ordered, spec.ID)
	}
	var out []string
	for _, id := range ordered {
		if codingAgentInstalled(id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		// Keep first-run installs useful even before config directories exist.
		out = []string{"claude"}
	}
	return out
}

// codingAgentInstalled deliberately ignores repository files: setup changes the
// user's agent configuration, so detection must have the same scope.
func codingAgentInstalled(id string) bool {
	spec, ok := lookupAgentSpec(id)
	return ok && (dirExists(spec.ConfigDir()) || commandExists(spec.Binary) || commandExists(spec.ID))
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// installClaude registers cully into settings.json (hooks),
// merging rather than overwriting so other settings/hooks are preserved.
// Idempotent. The merge is done in Go so the installer needs no jq.
func installClaude() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve own path: %w", err)
	}
	exe, _ = filepath.Abs(exe)
	settingsPath := filepath.Join(ConfigDir(), "settings.json")

	m, err := loadSettings(settingsPath)
	if err != nil {
		return err
	}
	if err := backup(settingsPath); err != nil {
		return err
	}

	setEventHook(m, "Stop", quote(exe)+" _internal analyze", "analyze")
	setEventHook(m, "SessionStart", quote(exe)+" _internal continuity claude start", "continuity claude start")
	setEventHook(m, "UserPromptSubmit", quote(exe)+" _internal continuity claude prompt", "continuity claude prompt")
	setEventHook(m, "Stop", quote(exe)+" _internal continuity claude stop", "continuity claude stop")
	setEventHook(m, "SessionEnd", quote(exe)+" _internal cleanup", "cleanup")
	setClaudePaneSignalHook(m, exe)
	setGuardHook(m, exe, "claude", claudeGuardMatcher)

	if err := writeSettings(settingsPath, m); err != nil {
		return err
	}
	if err := writeSlashCommand(exe); err != nil {
		fmt.Println("Slash command: could not write /cully —", err)
	} else {
		fmt.Println("Registered /cully (status · suggestions · apply).")
	}
	fmt.Printf("\033[32mInstalled.\033[0m Registered cully in %s\n", settingsPath)
	fmt.Println("Cully hooks stay inactive in native launches. Start through cully run to activate them.\nFor the terminal with live signals, start Claude with: cully run claude")
	fmt.Println("Accept a suggestion: cully apply <n>  (updates agent instructions, MCP, skills after you confirm)")
	return nil
}

func installCodex() error {
	if err := removeManagedSection(codexInstructionsPath(), "codex"); err != nil {
		return err
	}
	// Older Cully installs added a managed native footer. The status view is
	// now the only Cully display for Codex; preserve user-owned footer settings.
	if err := removeCodexStatusLine(codexConfigPath()); err != nil {
		return fmt.Errorf("remove legacy Codex status line: %w", err)
	}
	if err := installCodexContinuityHooks(); err != nil {
		return fmt.Errorf("configure Codex continuity hooks: %w", err)
	}
	if wrote, err := writeCodexPrompt(); err != nil {
		return fmt.Errorf("install Codex cully prompt: %w", err)
	} else if !wrote {
		fmt.Printf("Preserved existing Codex prompt -> %s\n", codexPromptPath())
	}
	fmt.Printf("\033[32mInstalled.\033[0m Registered Cully for Codex in %s\n", CodexConfigDir())
	fmt.Printf("Codex continuity hooks configured in %s; review them with /hooks.\n", codexHooksPath())
	fmt.Printf("Codex cully prompt available as /prompts:cully -> %s\n", codexPromptPath())
	fmt.Println("For the live status view, start Codex with: cully run codex")
	return nil
}

func installCursor() error {
	if err := installCursorContinuityHooks(); err != nil {
		return fmt.Errorf("configure Cursor continuity hooks: %w", err)
	}
	if wrote, err := writeCursorCommand(); err != nil {
		return fmt.Errorf("install Cursor cully command: %w", err)
	} else if !wrote {
		fmt.Printf("Preserved existing Cursor command -> %s\n", cursorCommandPath())
	}
	fmt.Printf("\033[32mInstalled.\033[0m Registered Cully for Cursor in %s\n", CursorConfigDir())
	fmt.Printf("Cursor continuity hooks configured in %s\n", cursorHooksPath())
	fmt.Printf("Cursor cully command available as /cully -> %s\n", cursorCommandPath())
	fmt.Println("For the live status view, start Cursor with: cully run cursor")
	return nil
}

// Uninstall removes cully's entries for one or more coding agents. With no
// target it preserves the historical behavior: uninstall Claude Code hooks only.
func Uninstall(targets ...string) error {
	if len(targets) == 0 {
		targets = []string{"claude"}
	}
	for _, target := range expandInstallTargets(targets) {
		switch target {
		case "claude":
			if err := uninstallClaude(); err != nil {
				return err
			}
		case "codex":
			if err := uninstallCodex(); err != nil {
				return err
			}
		case "cursor":
			if err := uninstallCursor(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown uninstall target %q (use claude, codex, cursor, or all)", target)
		}
		if err := removeManagedMemorySkill(target); err != nil {
			return err
		}
	}
	return nil
}

func removeManagedMemorySkill(target string) error {
	var configDir string
	switch target {
	case "claude":
		configDir = ConfigDir()
	case "codex":
		configDir = CodexConfigDir()
	case "cursor":
		configDir = CursorConfigDir()
	default:
		return fmt.Errorf("unknown skill target %q", target)
	}
	if err := removeCullySkillIfUnchanged(filepath.Join(configDir, "skills", "cully", "SKILL.md")); err != nil {
		return err
	}
	return removeCullySkillIfUnchanged(runtimeSkillPath(target))
}

// uninstallClaude removes cully's entries from settings.json and deletes
// transient state. It leaves the binary in place (the installer/user manages that).
func uninstallClaude() error {
	_ = StopDaemon()
	exe, _ := os.Executable()
	exe, _ = filepath.Abs(exe)
	settingsPath := filepath.Join(ConfigDir(), "settings.json")

	if _, err := os.Stat(settingsPath); err == nil {
		m, err := loadSettings(settingsPath)
		if err != nil {
			return err
		}
		_ = backup(settingsPath)

		removeEventHook(m, "Stop", quote(exe)+" _internal analyze", "analyze")
		removeEventHook(m, "UserPromptSubmit", "", "continuity claude prompt")
		removeEventHook(m, "SessionStart", quote(exe)+" _internal continuity claude start", "continuity claude start")
		removeEventHook(m, "Stop", quote(exe)+" _internal continuity claude stop", "continuity claude stop")
		removeEventHook(m, "SessionEnd", quote(exe)+" _internal cleanup", "cleanup")
		removeEventHook(m, "PostToolUse", "", "pane-signal claude")
		removeEventHook(m, "PreToolUse", "", "guard claude")
		if err := writeSettings(settingsPath, m); err != nil {
			return err
		}
	}

	_ = os.Remove(slashCommandPath())

	// transient state, consolidated under cullyDir()
	dir := cullyDir()
	_ = os.Remove(filepath.Join(dir, ".model-hint"))
	_ = os.Remove(filepath.Join(dir, ".session-report"))
	_ = os.Remove(filepath.Join(dir, ".cully-learning.json"))
	_ = os.Remove(filepath.Join(dir, ".cully-pending.json"))
	_ = os.Remove(filepath.Join(dir, ".cully-state"))
	_ = os.Remove(filepath.Join(dir, ".cully-snapshot"))
	_ = os.Remove(filepath.Join(dir, ".cully-chime-state"))
	_ = os.Remove(filepath.Join(dir, ".cully-debug.log"))
	_ = os.Remove(filepath.Join(dir, ".cully-daemon.pid"))
	_ = os.Remove(filepath.Join(dir, ".cully-daemon.log"))
	_ = os.Remove(filepath.Join(dir, "history.jsonl"))
	_ = os.RemoveAll(filepath.Join(dir, "cully-jobs"))
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			transient := strings.HasPrefix(name, ".sa-count-") ||
				strings.HasSuffix(name, ".report") || strings.HasSuffix(name, ".snapshot") ||
				strings.HasSuffix(name, ".chime") || strings.HasSuffix(name, ".signals") ||
				strings.HasSuffix(name, ".seen")
			if transient {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
	fmt.Println("\033[32mUninstalled.\033[0m Removed cully entries and state. Restart Claude Code to drop the status line.")
	return nil
}

func uninstallCodex() error {
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md"} {
		if err := removeManagedSection(filepath.Join(CodexConfigDir(), name), "codex"); err != nil {
			return err
		}
	}
	if err := removeOwnedFile(codexPromptPath(), codexCommandMarkerStart); err != nil {
		return err
	}
	if err := removeCodexStatusLine(codexConfigPath()); err != nil {
		return err
	}
	if err := uninstallCodexContinuityHooks(); err != nil {
		return err
	}
	fmt.Printf("\033[32mUninstalled.\033[0m Removed Cully Codex integration from %s\n", CodexConfigDir())
	return nil
}

func uninstallCursor() error {
	if err := removeOwnedFile(cursorCommandPath(), cursorCommandMarkerStart); err != nil {
		return err
	}
	if err := uninstallCursorContinuityHooks(); err != nil {
		return err
	}
	fmt.Printf("\033[32mUninstalled.\033[0m Removed Cully Cursor integration from %s\n", CursorConfigDir())
	return nil
}

func removeCullySkillIfUnchanged(path string) error {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(content)) != strings.TrimSpace(cullyskill.Content) {
		return nil
	}
	return os.Remove(path)
}

func loadSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON — fix or move it, then retry: %w", path, err)
	}
	return m, nil
}

func writeSettings(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func backup(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	dst := fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102150405"))
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("Backed up settings.json -> %s\n", dst)
	return nil
}

// setEventHook appends our hook for the given event, removing any prior cully
// entry (matched by exact command or by subcommand) first so re-running install
// never duplicates it. Foreign hooks are preserved.
func setEventHook(m map[string]any, event, cmd, sub string) {
	cmd = scopedHookCommand(cmd)
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	list := filterOutCullyCommand(toList(hooks[event]), cmd, sub)
	list = append(list, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": cmd}},
	})
	hooks[event] = list
	m["hooks"] = hooks
}

// setGuardHook registers the synchronous PreToolUse guard for tools matching
// matcher. It only ever adds context, so a short timeout is safe.
func setGuardHook(m map[string]any, exe, agent, matcher string) {
	sub := "guard " + agent
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	list := filterOutCullyCommand(toList(hooks["PreToolUse"]), "", sub)
	list = append(list, map[string]any{
		"matcher": matcher,
		"hooks":   []any{map[string]any{"type": "command", "command": scopedHookCommand(quote(exe) + " _internal " + sub), "timeout": 5}},
	})
	hooks["PreToolUse"] = list
	m["hooks"] = hooks
}

// setClaudePaneSignalHook registers the asynchronous PostToolUse hook that
// feeds the Cully terminal panel.
func setClaudePaneSignalHook(m map[string]any, exe string) {
	setEventHook(m, "PostToolUse", quote(exe)+" _internal pane-signal claude", "pane-signal claude")
	groups := toList(m["hooks"].(map[string]any)["PostToolUse"])
	handler := toList(groups[len(groups)-1].(map[string]any)["hooks"])[0].(map[string]any)
	handler["async"] = true
	handler["timeout"] = 5
}

func removeEventHook(m map[string]any, event, cmd, sub string) {
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	list := filterOutCullyCommand(toList(hooks[event]), cmd, sub)
	if len(list) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = list
	}
	if len(hooks) == 0 {
		delete(m, "hooks")
	} else {
		m["hooks"] = hooks
	}
}

func filterOutCullyCommand(groups []any, cmd, subcommand string) []any {
	out := make([]any, 0, len(groups))
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			out = append(out, g)
			continue
		}
		inner := toList(gm["hooks"])
		kept := make([]any, 0, len(inner))
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok {
				if c, _ := hm["command"].(string); c == cmd || isCullySubcommand(c, subcommand) {
					continue
				}
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 {
			continue
		}
		gm["hooks"] = kept
		out = append(out, gm)
	}
	return out
}

func isCullySubcommand(cmd, subcommand string) bool {
	if subcommand == "" {
		return false
	}
	cmd = unscopedHookCommand(cmd)
	return strings.HasSuffix(cmd, " "+subcommand) && strings.Contains(cmd, "cully")
}

func toList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// quote single-quotes a path for safe use in a shell command string.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func slashCommandPath() string {
	return filepath.Join(ConfigDir(), "commands", "cully.md")
}

// cullyCommandMD is the /cully slash-command definition. {{EXE}} is replaced
// with the absolute, shell-quoted binary path at install time so it works under a
// custom CLAUDE_CONFIG_DIR. The embedded shell defaults a bare /cully to the
// status view, and passes status, suggestions and apply arguments through.
//
// $ARGUMENTS is text-substituted by Claude Code, so multi-word input like
// `apply 1` must be re-split into separate argv entries. We route through an
// inner `sh -c` where the substituted tokens arrive as positional parameters
// ($@) rather than being re-expanded from a variable: an unquoted ${VAR}
// word-splits in bash but NOT in zsh (macOS default), which previously collapsed
// `apply 1` into a single argument and produced `unknown subcommand "apply 1"`.
// Passing the tokens as positionals splits identically in every POSIX shell.
//
// CRITICAL: the template must not contain ANY $<digit> ($0-$9) — Claude Code
// substitutes those placeholders with the slash command's own arguments even
// inside single quotes, ZERO-indexed: a previous `b=$1` became `b=1` (second
// arg) and `exec "$0"` became `exec "apply"` (first arg). The binary path is
// therefore carried in the $CULLY_BIN env var — named variables, $#, and $@
// all survive substitution (only $ARGUMENTS and $<digit> are rewritten).
// CULLY_ASSUME_YES=1 tells `apply` there is no interactive stdin here, so it
// must not wait on a y/N prompt that would read EOF and cancel.
const cullyCommandMD = "---\n" +
	"description: Manage Cully — status, suggestions and apply\n" +
	"argument-hint: \"[status | suggestions | apply <n>]\"\n" +
	"allowed-tools: Bash\n" +
	"---\n\n" +
	"Run the cully control below, then explain the output plainly to the user:\n" +
	"summarize what each section means, call out anything in the warning/caution\n" +
	"colors first, and if they asked to `apply <n>` state exactly what changed.\n\n" +
	"!`CULLY_BIN={{EXE}} sh -c 'if [ \"$#\" -eq 0 ]; then exec \"$CULLY_BIN\" status; else CULLY_ASSUME_YES=1 exec \"$CULLY_BIN\" \"$@\"; fi' cully $ARGUMENTS 2>&1`\n"

func writeSlashCommand(exe string) error {
	path := slashCommandPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := strings.ReplaceAll(cullyCommandMD, "{{EXE}}", quote(exe))
	return os.WriteFile(path, []byte(body), 0o644)
}

func expandInstallTargets(targets []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range targets {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if t == "all" {
			for _, expanded := range []string{"claude", "codex", "cursor"} {
				if !seen[expanded] {
					seen[expanded] = true
					out = append(out, expanded)
				}
			}
			continue
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func managedStart(id string) string { return "<!-- cully:" + id + ":start -->" }
func managedEnd(id string) string   { return "<!-- cully:" + id + ":end -->" }

func removeManagedSection(path, id string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	text := string(b)
	start, end := managedStart(id), managedEnd(id)
	s := strings.Index(text, start)
	if s < 0 {
		return nil
	}
	e := strings.Index(text[s:], end)
	if e < 0 {
		return nil
	}
	e += s + len(end)
	text = strings.TrimSpace(text[:s] + text[e:])
	if text == "" || mdcFrontmatterOnly(text) {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(text+"\n"), 0o644)
}

func mdcFrontmatterOnly(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "---\n") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, "---"))
	if i := strings.Index(rest, "---"); i >= 0 {
		return strings.TrimSpace(rest[i+3:]) == ""
	}
	return false
}

func cullySkill() string {
	return cullyskill.Content
}
