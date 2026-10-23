package cully

import "strings"

func cullyMCPServerName(name string) bool {
	name = strings.ToLower(name)
	return name == "cully" || strings.HasPrefix(name, "cully-") || strings.HasPrefix(name, "cully_") || strings.HasSuffix(name, "-cully") || strings.HasSuffix(name, "_cully")
}
