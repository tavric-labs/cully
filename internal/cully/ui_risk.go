package cully

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// A risk label for what a task asks to do, worked out from the call itself with fixed rules (no model
// is asked, so it costs nothing and gives the same answer every time). It follows the shape OpenHands
// and Codex use: low, medium or high. High-risk calls are only ever allowed one at a time: "allow for
// this task" is not offered, however often the same call comes back.

const (
	uiRiskLow    = "low"
	uiRiskMedium = "medium"
	uiRiskHigh   = "high"
)

var (
	uiRiskSplit  = regexp.MustCompile(`&&|\|\||;|\||\n`)
	uiRiskPipeSh = regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b`)
	uiRiskRmRec  = regexp.MustCompile(`(?i)(^|\s)rm\s+(-[a-z]*r[a-z]*|--recursive)\b`)
	uiRiskSecret = regexp.MustCompile(`(?i)(^|/)(\.env[^/]*|id_rsa[^/]*|id_ed25519[^/]*|credentials[^/]*|\.netrc|\.npmrc|\.pypirc|\.aws/[^/]+|\.ssh/[^/]+|secrets?\.[a-z]+|[^/]*\.pem|[^/]*\.key)$`)
)

var uiRiskReadOnly = map[string]bool{"ls": true, "cat": true, "head": true, "tail": true, "pwd": true, "echo": true, "grep": true, "rg": true, "wc": true, "which": true, "date": true, "uname": true, "hostname": true, "uptime": true, "ps": true, "df": true, "du": true, "stat": true, "file": true, "tree": true, "sort": true, "uniq": true, "diff": true, "id": true, "whoami": true, "env": false}

var uiRiskGitRead = map[string]bool{"status": true, "diff": true, "log": true, "show": true, "branch": true, "blame": true, "ls-files": true, "rev-parse": true, "remote": true, "describe": true, "shortlog": true}

var uiRiskHighWords = []struct {
	re  *regexp.Regexp
	why string
}{
	{regexp.MustCompile(`(^|\s)sudo(\s|$)`), "uses sudo"},
	{regexp.MustCompile(`(?i)(^|\s)(ssh|scp|sftp|rsync|nc|ncat|telnet)(\s|$)`), "reaches another machine"},
	{regexp.MustCompile(`(?i)\bgit\s+push\b`), "pushes code to a remote"},
	{regexp.MustCompile(`(?i)\bgit\s+reset\s+--hard\b|\bgit\s+clean\s+-[a-z]*f|\bgit\s+checkout\s+--\s+\.|\bgit\s+branch\s+-D\b`), "throws away work in git"},
	{regexp.MustCompile(`(?i)\b(kubectl|helm)\s+(delete|apply|replace|scale|rollout|drain|uninstall)\b`), "changes a cluster"},
	{regexp.MustCompile(`(?i)\bdocker\s+(rm|rmi|system\s+prune|volume\s+rm|kill)\b|\bdocker\s+compose\s+down\b`), "removes containers or data"},
	{regexp.MustCompile(`(?i)(^|\s)(dd|mkfs[.\w]*|shred|fdisk|parted)(\s|$)`), "writes to a disk directly"},
	{regexp.MustCompile(`(?i)\b(chmod|chown)\s+(-[a-z]*R|--recursive)\b`), "changes permissions recursively"},
	{regexp.MustCompile(`(?i)\bdrop\s+(table|database)\b|\btruncate\s+table\b|\bdelete\s+from\b`), "deletes database data"},
	{regexp.MustCompile(`>\s*/(etc|dev/sd|dev/nvme|boot|usr|bin)\b`), "writes to a system location"},
	{regexp.MustCompile(`(?i)\b(npm|pip|pip3|cargo|gem)\s+publish\b|\bnpm\s+unpublish\b|\btwine\s+upload\b`), "publishes a package"},
}

var uiRiskMediumWords = []struct {
	re  *regexp.Regexp
	why string
}{
	{regexp.MustCompile(`(?i)\b(npm|yarn|pnpm|pip|pip3|go|cargo|gem|apt|apt-get|brew)\s+(install|add|get|update|upgrade|remove|uninstall)\b`), "installs or changes packages"},
	{regexp.MustCompile(`(?i)\bgit\s+(commit|checkout|switch|merge|rebase|stash|cherry-pick|apply|am|tag|add|rm|mv|pull|fetch)\b`), "changes the repository"},
	{regexp.MustCompile(`(?i)(^|\s)(mv|cp|mkdir|touch|ln|rm|tee|truncate|install)(\s|$)|\bsed\s+-i|>>?\s*\S`), "changes files"},
	{regexp.MustCompile(`(?i)\bdocker\s+(run|build|exec|compose)\b`), "runs containers"},
	{regexp.MustCompile(`(?i)\b(make|go\s+(build|run|test|generate)|cargo\s+(build|run|test)|npm\s+(run|test)|pytest|python3?|node|bash|sh)\b`), "runs project code"},
	{regexp.MustCompile(`(?i)\bcurl\b|\bwget\b`), "talks to the network"},
}

// uiBashRisk classifies a shell command, taking the riskiest of its parts.
func uiBashRisk(cmd string) (string, string) {
	if uiRiskPipeSh.MatchString(cmd) {
		return uiRiskHigh, "downloads a script and runs it"
	}
	if uiRiskRmRec.MatchString(cmd) {
		return uiRiskHigh, "deletes files recursively"
	}
	for _, h := range uiRiskHighWords {
		if h.re.MatchString(cmd) {
			return uiRiskHigh, h.why
		}
	}
	level, why := uiRiskLow, "only reads"
	for _, part := range uiRiskSplit.Split(cmd, -1) {
		fs := strings.Fields(strings.TrimSpace(part))
		if len(fs) == 0 {
			continue
		}
		first := filepath.Base(fs[0])
		ro := uiRiskReadOnly[first]
		if first == "git" && len(fs) > 1 {
			ro = uiRiskGitRead[fs[1]]
		}
		if first == "find" {
			ro = !strings.Contains(part, "-exec") && !strings.Contains(part, "-delete")
		}
		if !ro {
			level, why = uiRiskMedium, "runs a command Cully cannot show to be read-only"
		}
	}
	for _, m := range uiRiskMediumWords {
		if m.re.MatchString(cmd) {
			return uiRiskMedium, m.why
		}
	}
	if strings.ContainsAny(cmd, "`") || strings.Contains(cmd, "$(") {
		if level == uiRiskLow {
			level, why = uiRiskMedium, "builds part of its command at run time"
		}
	}
	return level, why
}

// uiToolRisk classifies any tool call. worktree is the task's own folder: a file outside it is riskier.
func uiToolRisk(tool string, input json.RawMessage, worktree string) (string, string) {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok {
				return s
			}
		}
		return ""
	}
	outside := func(p string) bool {
		if p == "" || worktree == "" || !filepath.IsAbs(p) {
			return false
		}
		rel, err := filepath.Rel(worktree, p)
		return err != nil || strings.HasPrefix(rel, "..")
	}
	switch tool {
	case "Bash":
		return uiBashRisk(str("command"))
	case "Read", "Glob", "Grep", "NotebookRead":
		if p := str("file_path", "path", "pattern"); uiRiskSecret.MatchString(p) {
			return uiRiskMedium, "may read a secret"
		}
		return uiRiskLow, "only reads"
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		p := str("file_path", "notebook_path", "path")
		switch {
		case uiRiskSecret.MatchString(p):
			return uiRiskHigh, "changes a file that holds secrets or credentials"
		case outside(p):
			return uiRiskHigh, "changes a file outside the task's own folder"
		}
		return uiRiskMedium, "changes a file"
	case "WebFetch", "WebSearch":
		return uiRiskMedium, "sends a request out of this machine"
	}
	if strings.HasPrefix(tool, "mcp__") {
		switch toolKindGo(tool) {
		case "delete":
			return uiRiskHigh, "an MCP tool whose name says it deletes"
		case "read":
			return uiRiskLow, "an MCP tool whose name says it reads"
		}
		return uiRiskMedium, "an MCP tool that may change something"
	}
	return uiRiskMedium, "a tool Cully has no rule for"
}

// toolKindGo is the Go twin of the page's toolKind: whether a tool's name says it reads, changes or deletes.
func toolKindGo(name string) string {
	del := map[string]bool{"delete": true, "remove": true, "erase": true, "drop": true, "destroy": true, "stop": true, "cancel": true, "purge": true, "revoke": true}
	chg := map[string]bool{"create": true, "add": true, "update": true, "edit": true, "move": true, "transition": true, "assign": true, "upload": true, "send": true, "merge": true, "play": true, "retry": true, "trigger": true, "promote": true, "resolve": true, "approve": true, "set": true, "link": true, "copy": true, "publish": true, "run": true, "write": true, "save": true, "log": true, "start": true, "push": true, "submit": true, "commit": true, "reply": true}
	rd := map[string]bool{"get": true, "list": true, "search": true, "read": true, "describe": true, "find": true, "check": true, "download": true, "whoami": true, "health": true, "help": true, "show": true, "view": true, "query": true, "sample": true, "validate": true, "discover": true, "wait": true, "lookup": true, "recall": true, "recent": true, "context": true}
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		switch {
		case del[w]:
			return "delete"
		case chg[w]:
			return "change"
		case rd[w]:
			return "read"
		}
	}
	return "other"
}
