package cully

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// What Cully can and cannot apply to a Codex task. Codex asks Cully only about what it wants to do
// outside its own sandbox, so Cully's block list is applied to those requests, the edit group decides
// whether the sandbox is read only, and everything Cully cannot see is said so in the task.
const uiCodexLimits = "Codex asks Cully only before it leaves its sandbox, so your block list applies to those requests. Inside the sandbox it acts on its own and has no network. The MCP servers set up in Codex load unfiltered."

// uiCommandParts splits a shell command line into the simple commands it runs, with quotes and extra
// spaces removed, leading variable assignments and wrappers (env, sudo, command, exec, nohup, time,
// nice, xargs) stripped, and the text after sh -c, bash -c or eval treated as a command line of its own.
func uiCommandParts(command string, depth int) []string {
	if depth > 3 {
		return nil
	}
	repl := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "`", "\n", "$(", "\n", ")", "\n", "(", "\n", "{", "\n", "}", "\n", "&", "\n")
	var out []string
	for _, seg := range strings.Split(repl.Replace(command), "\n") {
		fields := strings.Fields(strings.NewReplacer("'", "", `"`, "", "\\", "").Replace(seg))
		for len(fields) > 0 {
			f := fields[0]
			if i := strings.Index(f, "="); i > 0 && !strings.ContainsAny(f[:i], "/-.") { // NAME=value
				fields = fields[1:]
				continue
			}
			switch f {
			case "env", "sudo", "command", "exec", "nohup", "time", "nice", "xargs", "builtin", "doas", "stdbuf":
				fields = fields[1:]
				for len(fields) > 0 && strings.HasPrefix(fields[0], "-") { // the wrapper's own flags
					fields = fields[1:]
				}
				continue
			}
			break
		}
		if len(fields) == 0 {
			continue
		}
		if i := strings.LastIndex(fields[0], "/"); i >= 0 && i < len(fields[0])-1 {
			fields[0] = fields[0][i+1:] // /usr/bin/git is git
		}
		out = append(out, strings.Join(fields, " "))
		// sh -c "...", bash -c "...", eval ...: look inside as well
		base := fields[0]
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		switch base {
		case "sh", "bash", "zsh", "dash", "ksh":
			for i, f := range fields {
				if f == "-c" || (strings.HasPrefix(f, "-") && strings.HasSuffix(f, "c") && !strings.HasPrefix(f, "--")) {
					out = append(out, uiCommandParts(strings.Join(fields[i+1:], " "), depth+1)...)
					break
				}
			}
		case "eval":
			out = append(out, uiCommandParts(strings.Join(fields[1:], " "), depth+1)...)
		}
	}
	return out
}

// uiCommandOpaque is true when the program to run is not written out, as in $CMD push or ${X}, so no
// rule can be matched against it.
func uiCommandOpaque(command string) bool {
	for _, p := range uiCommandParts(command, 0) {
		if strings.HasPrefix(p, "$") {
			return true
		}
	}
	return false
}

// uiRuleBlocksCommand returns the first rule of the form Bash(prefix:*), Bash(command) or Bash that
// matches any simple command in a shell command line, or "". It is a first line of defence only: a
// command it cannot see through still reaches the permission card, where you decide.
func uiRuleBlocksCommand(rules []string, command string) string {
	parts := uiCommandParts(command, 0)
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
		prefix = strings.Join(strings.Fields(prefix), " ")
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
		if !filepath.IsAbs(p) && worktree != "" {
			p = filepath.Join(worktree, p) // a relative path is relative to the worktree; ../ then shows as outside it
		}
		p = filepath.Clean(p)
		pj, _ := json.Marshal(map[string]string{"file_path": p})
		r, w := uiToolRisk("Edit", pj, worktree)
		if rank[r] > rank[risk] {
			risk, why = r, w
		}
	}
	return risk, why
}
