package cully

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
}

// uiPolicyInput is what the admin page sends. A nil Deny restores the default block list.
type uiPolicyInput struct {
	Presets map[string]bool `json:"presets"`
	Allow   []string        `json:"allow"`
	Deny    []string        `json:"deny"`
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
	Effective   struct {
		Allowed []string `json:"allowed"`
		Denied  []string `json:"denied"`
	} `json:"effective"`
}

func uiSettingsPath() string { return filepath.Join(cullyDir(), "ui-settings.json") }

func (m *uiManager) loadPolicyLocked() {
	if m.policyLoaded {
		return
	}
	m.policyLoaded = true
	if b, err := os.ReadFile(uiSettingsPath()); err == nil {
		var p uiPolicy
		if json.Unmarshal(b, &p) == nil {
			m.policy = p
		}
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
func (m *uiManager) toolPolicy(plan bool) (allowed, denied string) {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	m.loadPolicyLocked()
	var tools []string
	for _, pr := range uiToolPresets {
		if m.policy.presetOn(pr) && !(plan && pr.NotInPlan) {
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
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp := uiSettingsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, uiSettingsPath()); err != nil {
		return err
	}
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
	allowed, denied := m.toolPolicy(false)
	v.Effective.Allowed, v.Effective.Denied = uiSplitTools(allowed), uiSplitTools(denied)
	v.Deny = v.Effective.Denied
	return v
}
