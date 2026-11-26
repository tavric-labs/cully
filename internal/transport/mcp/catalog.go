package mcp

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/mcp-runtime/cully/internal/memory"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolInfo describes one tool the Cully MCP server offers, as a client sees it.
type ToolInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	ReadOnly    bool        `json:"readOnly"`
	Params      []ParamInfo `json:"params"`
}

type ParamInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// Catalog lists every tool the server registers with its description and
// parameters, by asking a server built exactly like the real one over an
// in-memory connection. No handler runs and no data is touched: the tools are
// registered with a nil service and are only listed.
func Catalog(version string) ([]ToolInfo, error) {
	ctx := context.Background()
	server := sdk.NewServer(&sdk.Implementation{Name: "Cully", Version: version}, nil)
	identity := func(context.Context, bool) (string, error) { return "", memory.ErrForbidden }
	for _, t := range toolRegistrations() {
		t.register(t, server, memory.Service{}, identity)
	}
	st, ct := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "cully-catalog", Version: version}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	var out []ToolInfo
	for _, t := range res.Tools {
		info := ToolInfo{Name: t.Name, Description: t.Description, ReadOnly: t.Annotations != nil && t.Annotations.ReadOnlyHint, Params: []ParamInfo{}}
		raw, _ := json.Marshal(t.InputSchema)
		var schema struct {
			Properties map[string]struct {
				Type        any    `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		if json.Unmarshal(raw, &schema) == nil {
			req := map[string]bool{}
			for _, r := range schema.Required {
				req[r] = true
			}
			for n, p := range schema.Properties {
				typ := ""
				switch v := p.Type.(type) {
				case string:
					typ = v
				case []any:
					for _, x := range v {
						if s, ok := x.(string); ok && s != "null" {
							typ = s
						}
					}
				}
				info.Params = append(info.Params, ParamInfo{Name: n, Type: typ, Required: req[n], Description: p.Description})
			}
			sort.Slice(info.Params, func(i, j int) bool {
				if info.Params[i].Required != info.Params[j].Required {
					return info.Params[i].Required
				}
				return info.Params[i].Name < info.Params[j].Name
			})
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
