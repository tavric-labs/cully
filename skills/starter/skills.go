// Package starterskills embeds the small skills Cully installs with setup: how to write a spec, verify before
// saying done, hand work off between sessions, and keep a repository legible to agents. Each is short on purpose.
package starterskills

import "embed"

//go:embed cully-spec/SKILL.md cully-verify/SKILL.md cully-handoff/SKILL.md cully-repo-map/SKILL.md
var files embed.FS

// Names lists the skills in a stable order.
var Names = []string{"cully-spec", "cully-verify", "cully-handoff", "cully-repo-map"}

// Content returns one skill's SKILL.md.
func Content(name string) (string, bool) {
	b, err := files.ReadFile(name + "/SKILL.md")
	if err != nil {
		return "", false
	}
	return string(b), true
}
