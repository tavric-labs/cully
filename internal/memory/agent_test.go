package memory

import (
	"errors"
	"strings"
	"testing"
)

func validAgent() AgentInput {
	return AgentInput{AgentID: "ab12cd34", Name: "Release watcher", Scope: "project", Approved: true, Definition: `{"role":"Checks pull requests","mode":"plan"}`, Notes: "- PR 12 waits on CI"}
}

func TestAgentInputIsValidatedAndPrivateData(t *testing.T) {
	ok := validAgent()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AgentInput){
		"bad id":          func(v *AgentInput) { v.AgentID = "A/../b" },
		"no name":         func(v *AgentInput) { v.Name = "  " },
		"long name":       func(v *AgentInput) { v.Name = strings.Repeat("a", 41) },
		"bad scope":       func(v *AgentInput) { v.Scope = "everywhere" },
		"no definition":   func(v *AgentInput) { v.Definition = "" },
		"not an object":   func(v *AgentInput) { v.Definition = `["x"]` },
		"not json":        func(v *AgentInput) { v.Definition = `role: x` },
		"huge definition": func(v *AgentInput) { v.Definition = `{"x":"` + strings.Repeat("a", agentDefinitionMax) + `"}` },
		"long notes":      func(v *AgentInput) { v.Notes = strings.Repeat("n", agentNotesMax+1) },
		"secret in notes": func(v *AgentInput) { v.Notes = "api_key = sk-abcdefghijklmnopqrstuvwxyz0123" },
		"secret in def": func(v *AgentInput) {
			v.Definition = `{"instructions":"use token ghp_abcdefghijklmnopqrstuvwxyz0123456789"}`
		},
	} {
		v := validAgent()
		mutate(&v)
		if err := v.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestAgentRequestsNeedExactlyOneMatchingInput(t *testing.T) {
	save := validAgent()
	good := []Request{
		{Operation: "agent_save", AgentSave: &save},
		{Operation: "agent_list", AgentQuery: &AgentQueryInput{}},
		{Operation: "agent_delete", AgentRef: &AgentRefInput{AgentID: "ab12cd34"}},
	}
	for _, r := range good {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", r.Operation, err)
		}
	}
	both := Request{Operation: "agent_save", AgentSave: &save, AgentRef: &AgentRefInput{AgentID: "ab12cd34"}}
	if err := both.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("two inputs: %v", err)
	}
	wrong := Request{Operation: "log", AgentSave: &save}
	if err := wrong.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("an agent input under another operation: %v", err)
	}
	q := AgentQueryInput{Limit: 99}
	if err := (&Request{Operation: "agent_list", AgentQuery: &q}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("limit 99: %v", err)
	}
}
