package cully

import "testing"

func TestBlockRulesMatchAnyPartOfACommandLine(t *testing.T) {
	deny := []string{"Bash(git push:*)", "Bash(rm -rf:*)", "Bash(make deploy)"}
	for cmd, want := range map[string]string{
		"git push origin main":             "Bash(git push:*)",
		"cd x && git push":                 "Bash(git push:*)",
		"echo hi; rm -rf /tmp/x":           "Bash(rm -rf:*)",
		"ls | xargs echo $(rm -rf build)":  "Bash(rm -rf:*)",
		"make deploy":                      "Bash(make deploy)",
		"git status":                       "",
		"echo git push":                    "",
		"make deploy-staging":              "",
		"git pushd":                        "",
		"go test ./... && git diff --stat": "",
	} {
		if got := uiRuleBlocksCommand(deny, cmd); got != want {
			t.Errorf("%q: got %q, want %q", cmd, got, want)
		}
	}
	if uiRuleBlocksCommand([]string{"Bash"}, "anything") != "Bash" {
		t.Error("a bare Bash rule blocks every command")
	}
}

func TestCodexFileChangesAreRatedByEveryPathAndFolderGrants(t *testing.T) {
	wt := "/work/tree"
	for _, c := range []struct {
		paths []string
		grant string
		want  string
	}{
		{[]string{"/work/tree/a.go"}, "", uiRiskMedium},
		{[]string{"/work/tree/a.go", "/etc/passwd"}, "", uiRiskHigh},
		{[]string{"/work/tree/a.go", "/work/tree/.env"}, "", uiRiskHigh},
		{[]string{"/work/tree/a.go"}, "/work", uiRiskHigh},
		{nil, "", uiRiskHigh},
		{[]string{"a.go"}, "", uiRiskMedium},
		{[]string{"../../etc/hosts"}, "", uiRiskHigh},
		{[]string{"sub/../../outside.txt"}, "", uiRiskHigh},
		{[]string{".env"}, "", uiRiskHigh},
	} {
		if got, _ := uiCodexFileRisk(c.paths, c.grant, wt); got != c.want {
			t.Errorf("%v grant %q: got %s, want %s", c.paths, c.grant, got, c.want)
		}
	}
}

func TestACommandWhoseProgramIsAVariableIsOpaque(t *testing.T) {
	for cmd, want := range map[string]bool{"$CMD push": true, "FOO=1 ${X} y": true, "git push": false, "echo $HOME": false} {
		if got := uiCommandOpaque(cmd); got != want {
			t.Errorf("%q: got %v, want %v", cmd, got, want)
		}
	}
}
