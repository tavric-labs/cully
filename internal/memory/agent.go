package memory

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A named agent is private to its owner, like every other record. Cully keeps
// its definition as JSON and its notes as a short text; neither may hold a
// transcript, a command line or a credential, so both are checked.
var agentIDRE = regexp.MustCompile(`^[a-z0-9]{4,32}$`)

const (
	agentDefinitionMax = 16000
	agentNotesMax      = 2200
	agentNameMax       = 40
	agentListMax       = 20
)

type AgentInput struct {
	AgentID    string `json:"agent_id"`
	Name       string `json:"name"`
	Scope      string `json:"scope"` // project or global
	Approved   bool   `json:"approved"`
	Definition string `json:"definition"` // the agent's definition, as a JSON object
	Notes      string `json:"notes,omitempty"`
}

type AgentQueryInput struct {
	Limit int `json:"limit,omitempty"`
}

type AgentRefInput struct {
	AgentID string `json:"agent_id"`
}

type AgentRecord struct {
	AgentID    string    `json:"agent_id"`
	Name       string    `json:"name"`
	Scope      string    `json:"scope"`
	Approved   bool      `json:"approved"`
	Definition string    `json:"definition"`
	Notes      string    `json:"notes,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (v *AgentInput) Validate() error {
	v.Name = strings.TrimSpace(v.Name)
	if !agentIDRE.MatchString(v.AgentID) {
		return fmt.Errorf("%w: invalid agent id", ErrInvalid)
	}
	if n := len([]rune(v.Name)); n < 1 || n > agentNameMax {
		return fmt.Errorf("%w: an agent's name is 1-%d characters", ErrInvalid, agentNameMax)
	}
	if v.Scope != "project" && v.Scope != "global" {
		return fmt.Errorf("%w: scope must be project or global", ErrInvalid)
	}
	if len(v.Definition) == 0 || len(v.Definition) > agentDefinitionMax {
		return fmt.Errorf("%w: the definition is required and at most %d bytes", ErrInvalid, agentDefinitionMax)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(v.Definition), &obj); err != nil || obj == nil {
		return fmt.Errorf("%w: the definition must be a JSON object", ErrInvalid)
	}
	if len([]rune(v.Notes)) > agentNotesMax {
		return fmt.Errorf("%w: notes are at most %d characters", ErrInvalid, agentNotesMax)
	}
	for _, field := range []string{v.Definition, v.Notes} {
		for _, p := range secrets {
			if p.MatchString(field) {
				return fmt.Errorf("%w: remove credentials before saving", ErrInvalid)
			}
		}
	}
	return nil
}

func (v *AgentQueryInput) Validate() error {
	if v.Limit == 0 {
		v.Limit = agentListMax
	}
	if v.Limit < 1 || v.Limit > agentListMax {
		return fmt.Errorf("%w: limit is 1-%d", ErrInvalid, agentListMax)
	}
	return nil
}

func (v *AgentRefInput) Validate() error {
	if !agentIDRE.MatchString(v.AgentID) {
		return fmt.Errorf("%w: invalid agent id", ErrInvalid)
	}
	return nil
}
