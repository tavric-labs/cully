package cully

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// What agents the page runs may do without asking. They run headless, so a
// tool that is not allowed is refused. The settings live in one file in the
// user's Cully folder and apply to agents started afterwards.

const uiPolicyMaxEntries = 50

var uiToolRule = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_*.-]{0,100}(\([^(),\n]{1,100}\))?$`)

type uiToolPreset struct {
	ID, Label, Description string
	Tools                  []string
	Default                bool
	NotInPlan              bool // left out for plan-only agents
}

var uiToolPresets = []uiToolPreset{
	{ID: "read", Label: "Read files", Description: "Look at files and search the project.", Tools: []string{"Read", "Glob", "Grep"}, Default: true},
	{ID: "edit", Label: "Edit files", Description: "Change and create files in the agent's own branch.", Tools: []string{"Edit", "Write"}, Default: true},
	{ID: "commands", Label: "Run commands", Description: "Run shell commands, such as tests. The block list below still applies.", Tools: []string{"Bash"}, Default: true},
	{ID: "memory-read", Label: "Cully memory: read", Description: "Look up what Cully already knows about the project.", Tools: strings.Split(uiCullyReadTools, ","), Default: true},
	{ID: "memory-save", Label: "Cully memory: save summaries", Description: "Let the agent save a short summary of its work. Never offered to plan-only agents.", Tools: strings.Split(uiCullyWriteTools, ","), Default: true, NotInPlan: true},
	{ID: "web", Label: "Web search and fetch", Description: "Search the web and read pages. Off unless you turn it on.", Tools: []string{"WebSearch", "WebFetch"}},
}

type uiPolicy struct {
	Presets map[string]bool `json:"presets,omitempty"` // only the ones changed from their default
	Allow   []string        `json:"allow,omitempty"`
	Deny    []string        `json:"deny"` // nil means the default block list
	// McpOnly names the connected MCP servers tasks load. Empty means all of them.
	McpOnly []string `json:"mcpOnly,omitempty"`
}

// uiPolicyInput is what the admin page sends. A nil Deny restores the default block list.
type uiPolicyInput struct {
	Presets map[string]bool `json:"presets"`
	Allow   []string        `json:"allow"`
	Deny    []string        `json:"deny"`
	McpOnly []string        `json:"mcpOnly"`
}

type uiToolPresetView struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	On          bool     `json:"on"`
	NotInPlan   bool     `json:"notInPlan,omitempty"`
}

type uiToolsView struct {
	Presets     []uiToolPresetView `json:"presets"`
	Allow       []string           `json:"allow"`
	Deny        []string           `json:"deny"`
	DefaultDeny []string           `json:"defaultDeny"`
	Mcp         struct {
		Available []string `json:"available"` // servers found in Claude's own config
		Only      []string `json:"only"`      // the ones tasks load; empty means all
	} `json:"mcp"`
	Effective struct {
		Allowed []string `json:"allowed"`
		Denied  []string `json:"denied"`
	} `json:"effective"`
}

func (m *uiManager) loadPolicyLocked() {
	if m.policyLoaded {
		return
	}
	m.policyLoaded = true
	var p uiPolicy
	if uiDB().get("settings", "policy", &p) {
		m.policy = p
	}
}

func (p uiPolicy) presetOn(pr uiToolPreset) bool {
	if v, ok := p.Presets[pr.ID]; ok {
		return v
	}
	return pr.Default
}

func uiSplitTools(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// toolPolicy returns the comma lists passed to --allowedTools and --disallowedTools.
func (m *uiManager) toolPolicy(plan bool) (allowed, denied string) { return m.toolPolicyFor(plan, nil) }

// toolPolicyFor is toolPolicy with the tool groups of one named agent in place
// of the global switches. The block list and extra allowed tools still apply.
func (m *uiManager) toolPolicyFor(plan bool, groups map[string]bool) (allowed, denied string) {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	m.loadPolicyLocked()
	var tools []string
	for _, pr := range uiToolPresets {
		on := m.policy.presetOn(pr)
		if groups != nil {
			on = groups[pr.ID]
		}
		if on && !(plan && pr.NotInPlan) {
			tools = append(tools, pr.Tools...)
		}
	}
	tools = append(tools, m.policy.Allow...)
	denied = uiDeniedTools
	if m.policy.Deny != nil {
		denied = strings.Join(m.policy.Deny, ",")
	}
	return strings.Join(uiDedupe(tools), ","), denied
}

func uiDedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func uiCleanRules(kind string, in []string) ([]string, error) {
	if len(in) > uiPolicyMaxEntries {
		return nil, fmt.Errorf("at most %d entries in the %s list", uiPolicyMaxEntries, kind)
	}
	out := []string{}
	for _, raw := range in {
		r := strings.TrimSpace(raw)
		if !uiToolRule.MatchString(r) {
			return nil, fmt.Errorf("%q is not a tool name; use a name like mcp__server__tool or Bash(git status:*)", raw)
		}
		out = append(out, r)
	}
	return uiDedupe(out), nil
}

func (m *uiManager) setToolPolicy(in uiPolicyInput) error {
	next := uiPolicy{}
	for id, on := range in.Presets {
		var found *uiToolPreset
		for i := range uiToolPresets {
			if uiToolPresets[i].ID == id {
				found = &uiToolPresets[i]
			}
		}
		if found == nil {
			return errors.New("unknown tool group " + id)
		}
		if on != found.Default {
			if next.Presets == nil {
				next.Presets = map[string]bool{}
			}
			next.Presets[id] = on
		}
	}
	allow, err := uiCleanRules("allowed", in.Allow)
	if err != nil {
		return err
	}
	if len(allow) > 0 {
		next.Allow = allow
	}
	if in.Deny != nil {
		deny, err := uiCleanRules("blocked", in.Deny)
		if err != nil {
			return err
		}
		next.Deny = deny
	}
	if len(in.McpOnly) > uiPolicyMaxEntries {
		return errors.New("too many connected tools chosen")
	}
	for _, n := range in.McpOnly {
		if !uiMCPName.MatchString(n) {
			return fmt.Errorf("%q is not a connected tool name", n)
		}
	}
	next.McpOnly = uiDedupe(in.McpOnly)
	uiDB().put("settings", "policy", next)
	m.policyMu.Lock()
	m.policy, m.policyLoaded = next, true
	m.policyMu.Unlock()
	return nil
}

func (m *uiManager) toolsView() uiToolsView {
	m.policyMu.Lock()
	m.loadPolicyLocked()
	p := m.policy
	m.policyMu.Unlock()
	v := uiToolsView{Presets: []uiToolPresetView{}, Allow: append([]string{}, p.Allow...), DefaultDeny: uiSplitTools(uiDeniedTools)}
	for _, pr := range uiToolPresets {
		v.Presets = append(v.Presets, uiToolPresetView{ID: pr.ID, Label: pr.Label, Description: pr.Description, Tools: pr.Tools, On: p.presetOn(pr), NotInPlan: pr.NotInPlan})
	}
	v.Mcp.Available = uiMCPNames()
	v.Mcp.Only = append([]string{}, p.McpOnly...)
	allowed, denied := m.toolPolicy(false)
	v.Effective.Allowed, v.Effective.Denied = uiSplitTools(allowed), uiSplitTools(denied)
	v.Deny = v.Effective.Denied
	return v
}

var uiMCPName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// uiClaudeJSONPath is where Claude keeps the MCP servers you added for yourself.
func uiClaudeJSONPath() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

func uiMCPDefs() map[string]json.RawMessage {
	b, err := os.ReadFile(uiClaudeJSONPath())
	if err != nil {
		return nil
	}
	var d struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &d) != nil {
		return nil
	}
	return d.Servers
}

func uiMCPNames() []string {
	names := []string{}
	for n := range uiMCPDefs() {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// mcpFilterArgs limits a task to the connected servers chosen on the permissions
// page. Every connected server adds its tool descriptions to every turn, so
// loading fewer saves tokens. Nothing is added when no choice was made or none
// of the chosen servers exist, so a task never starts with no way to reach Cully.
// The chosen definitions are copied to a private file in the Cully folder,
// because they can carry credentials and must not appear on a command line.
func (m *uiManager) mcpFilterArgs() []string {
	m.policyMu.Lock()
	m.loadPolicyLocked()
	only := append([]string{}, m.policy.McpOnly...)
	m.policyMu.Unlock()
	if len(only) == 0 {
		return nil
	}
	defs := uiMCPDefs()
	pick := map[string]json.RawMessage{}
	for _, n := range only {
		if d, ok := defs[n]; ok {
			pick[n] = d
		}
	}
	if len(pick) == 0 {
		return nil
	}
	b, err := json.Marshal(map[string]any{"mcpServers": pick})
	if err != nil {
		return nil
	}
	path := filepath.Join(cullyDir(), "ui-mcp.json")
	if os.WriteFile(path, b, 0o600) != nil {
		return nil
	}
	return []string{"--strict-mcp-config", "--mcp-config", path}
}
