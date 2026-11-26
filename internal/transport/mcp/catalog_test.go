package mcp

import (
	"slices"
	"testing"
)

func TestCatalogListsEveryRegisteredToolWithParameters(t *testing.T) {
	tools, err := Catalog("test")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	for _, reg := range toolRegistrations() {
		if !slices.Contains(names, reg.name) {
			t.Errorf("the catalog lacks %s", reg.name)
		}
	}
	var save *ToolInfo
	for i := range tools {
		if tools[i].Name == "cully_agent_save" {
			save = &tools[i]
		}
	}
	if save == nil || save.ReadOnly || len(save.Params) < 5 {
		t.Fatalf("cully_agent_save = %+v", save)
	}
	req := 0
	for _, p := range save.Params {
		if p.Required {
			req++
			if p.Type == "" {
				t.Errorf("parameter %s has no type", p.Name)
			}
		}
	}
	if req < 4 {
		t.Fatalf("expected the id, name, scope and definition to be required: %+v", save.Params)
	}
	for _, tool := range tools {
		if tool.Name == "cully_search" && !tool.ReadOnly {
			t.Error("cully_search should be read only")
		}
	}
}
