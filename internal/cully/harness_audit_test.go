
func TestApplyEvidence(t *testing.T) {
	base := func() []harnessItem {
		return []harnessItem{
			{Component: "Skills", Hint: "skills hint"},
			{Component: "Subagents", Hint: "generic hint"},
			{Component: "Memory", Hint: "memory hint"},
		}
	}
	tests := []struct {
		name        string
		ev          harnessEvidence
		subPresent  bool
		subContains string
		skillsHint  string
	}{
		{"delegation present", harnessEvidence{Sessions: 2, BigTokens: 900, Delegations: 7}, true, "7 delegations in recent sessions", "skills hint"},
		{"delegation absent with waste", harnessEvidence{Sessions: 2, BigTokens: 1000, BashTokens: 600, ReadTokens: 400}, false, "Bash 300 and whole-file reads 200 tokens per session", "200 tokens per session"},
		{"no evidence", harnessEvidence{}, false, "generic hint", "skills hint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := applyEvidence(base(), tc.ev)
			skills, sub := items[0], items[1]
			if sub.Present() != tc.subPresent {
				t.Fatalf("Subagents present = %v, want %v", sub.Present(), tc.subPresent)
			}
			got := sub.Hint
			if sub.Present() {
				got = sub.Found[0]
			}
			if !strings.Contains(got, tc.subContains) {
				t.Errorf("Subagents = %q, want %q", got, tc.subContains)
			}
			if !strings.Contains(skills.Hint, tc.skillsHint) {
				t.Errorf("Skills = %q, want %q", skills.Hint, tc.skillsHint)
			}
			if items[2].Hint != "memory hint" {
				t.Errorf("unrelated item changed: %q", items[2].Hint)
			}
		})
	}
}
