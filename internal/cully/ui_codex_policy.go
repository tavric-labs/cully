package cully

import (
	"encoding/json"
	"strings"
)

// What Cully can and cannot apply to a Codex task. Codex asks Cully only about what it wants to do
// outside its own sandbox, so Cully's block list is applied to those requests, the edit group decides
// whether the sandbox is read only, and everything Cully cannot see is said so in the task.
const uiCodexLimits = "Codex asks Cully only before it leaves its sandbox, so your block list applies to those requests. Inside the sandbox it acts on its own and has no network. The MCP servers set up in Codex load unfiltered."

// uiRuleBlocksCommand returns the first rule of the form Bash(prefix:*), Bash(command) or Bash that
// matches any part of a shell command line, or "".
func uiRuleBlocksCommand(rules []string, command string) string {
	repl := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "`", "\n", "$(", "\n", ")", "\n")
	var parts []string
	for _, seg := range strings.Split(repl.Replace(command), "\n") {
		if seg = strings.TrimSpace(seg); seg != "" {
			parts = append(parts, seg)
		}
	}
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "Bash" {
			return rule
		}
		inner, ok := strings.CutPrefix(rule, "Bash(")
		if !ok || !strings.HasSuffix(inner, ")") {
			continue
		}
		inner = strings.TrimSuffix(inner, ")")
		prefix, wild := strings.CutSuffix(inner, ":*")
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		for _, p := range parts {
			if (wild && (p == prefix || strings.HasPrefix(p, prefix+" "))) || (!wild && p == prefix) {
				return rule
			}
		}
	}
	return ""
}

// uiCodexPolicy works out, from Cully's tool policy, the block rules to apply to Codex's requests and
// whether the sandbox must be read only.
func (m *uiManager) uiCodexPolicy(plan bool, groups map[string]bool) (deny []string, readOnly bool) {
	allowed, denied, _ := m.toolPolicyAsk(plan, groups, true)
	for _, r := range strings.Split(denied, ",") {
		if r = strings.TrimSpace(r); r != "" {
			deny = append(deny, r)
		}
	}
	editOn := false
	for _, t := range strings.Split(allowed, ",") {
		if t = strings.TrimSpace(t); t == "Edit" || t == "Write" {
			editOn = true
		}
	}
	return deny, plan || !editOn
}

// uiCodexFileRisk rates a Codex file-change request by every path it names and by any folder it asks
// to be granted, not by the first path only. Unknown paths and a folder grant are high risk.
func uiCodexFileRisk(paths []string, grantRoot, worktree string) (risk, why string) {
	rank := map[string]int{uiRiskLow: 0, uiRiskMedium: 1, uiRiskHigh: 2}
	risk, why = uiRiskLow, "changes files in the workspace"
	if grantRoot != "" {
		return uiRiskHigh, "asks for write access to a whole folder, not to one file"
	}
	if len(paths) == 0 {
		return uiRiskHigh, "did not say which files it will change"
	}
	for _, p := range paths {
		pj, _ := json.Marshal(map[string]string{"file_path": p})
		r, w := uiToolRisk("Edit", pj, worktree)
		if rank[r] > rank[risk] {
			risk, why = r, w
		}
	}
	return risk, why
}
