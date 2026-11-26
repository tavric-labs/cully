// Package cullysetupskill embeds the guidance an agent follows to set up Cully's memory stack.
package cullysetupskill

import _ "embed"

//go:embed SKILL.md
var Content string
