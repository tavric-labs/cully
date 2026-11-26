package cully

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mcpcat "github.com/mcp-runtime/cully/internal/transport/mcp"
)

// What a connected MCP server offers: its tools (with what each does and what
// it takes), its prompts and its resources. Claude Code names every tool and
// prompt it started with, so those names are always known after a task has
// run. Descriptions and parameters are not in that list: the Cully server's own
// are read from its source, and for any other server they are loaded on request
// by one short headless Claude call that is allowed to look at that server's
// tool definitions and call nothing. Resources are not reported at all.

type uiParam struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type uiToolDetail struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ReadOnly    bool      `json:"readOnly,omitempty"`
	Params      []uiParam `json:"params"`
}

type uiServerDetail struct {
	uiInfoServer
	Tools       []uiToolDetail `json:"tools"`
	Prompts     []string       `json:"prompts"`
	Resources   []string       `json:"resources"`
	DetailsFrom string         `json:"detailsFrom"` // where the descriptions came from, or empty
	CanLoad     bool           `json:"canLoad"`     // descriptions can be loaded from the server on request
	Note        string         `json:"note,omitempty"`
	Known       bool           `json:"known"` // a task has run, so the tool names are known
}

type uiInitNames struct {
	Tools    []string  `json:"tools"`
	Commands []string  `json:"commands"`
	Seen     time.Time `json:"seen"`
}

func (m *uiManager) rememberInit(tools, commands []string) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.initNames = &uiInitNames{Tools: tools, Commands: commands, Seen: time.Now().UTC()}
	uiDB().put("cache", "init", m.initNames)
}

func (m *uiManager) initKnown() *uiInitNames {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	if m.initNames == nil {
		var n uiInitNames
		if uiDB().get("cache", "init", &n) {
			m.initNames = &n
		}
	}
	return m.initNames
}

func uiReadCatalog() map[string][]uiToolDetail {
	out := map[string][]uiToolDetail{}
	uiDB().get("cache", "catalog", &out)
	return out
}

func (m *uiManager) serverDetail(agent, name string) (uiServerDetail, error) {
	if !uiMCPName.MatchString(name) {
		return uiServerDetail{}, errors.New("no such server")
	}
	info := m.harnessInfo(agent)
	var d uiServerDetail
	found := false
	for _, s := range info.MCP {
		if s.Name == name {
			d.uiInfoServer, found = s, true
		}
	}
	if !found {
		return uiServerDetail{}, errors.New("no such server")
	}
	d.Tools, d.Prompts, d.Resources = []uiToolDetail{}, []string{}, []string{}
	if agent != "claude" {
		d.Note = "Cully lists a server's tools only for the harness tasks run in."
		return d, nil
	}
	prefix := "mcp__" + name + "__"
	if n := m.initKnown(); n != nil {
		d.Known = true
		for _, t := range n.Tools {
			if strings.HasPrefix(t, prefix) {
				d.Tools = append(d.Tools, uiToolDetail{Name: strings.TrimPrefix(t, prefix), Params: []uiParam{}})
			}
		}
		for _, c := range n.Commands {
			if strings.HasPrefix(strings.TrimPrefix(c, "/"), prefix) {
				d.Prompts = append(d.Prompts, strings.TrimPrefix(strings.TrimPrefix(c, "/"), prefix))
			}
		}
	}
	sort.Slice(d.Tools, func(i, j int) bool { return d.Tools[i].Name < d.Tools[j].Name })
	sort.Strings(d.Prompts)
	d.Note = "Claude Code does not report a server's resources, so Cully cannot list them."
	if d.Status != "" && !strings.Contains(strings.ToLower(d.Status), "connected") {
		d.Note = "This server is " + d.Status + ", so it reported no tools. " + d.Note
	}
	if name == "cully" {
		if cat, err := mcpcat.Catalog("ui"); err == nil {
			d.Tools, d.DetailsFrom = d.Tools[:0], "Cully's own tool list"
			for _, t := range cat {
				x := uiToolDetail{Name: t.Name, Description: t.Description, ReadOnly: t.ReadOnly, Params: []uiParam{}}
				for _, p := range t.Params {
					x.Params = append(x.Params, uiParam{Name: p.Name, Type: p.Type, Required: p.Required, Description: p.Description})
				}
				d.Tools = append(d.Tools, x)
			}
			d.Known = true
			return d, nil
		}
	}
	if cached := uiReadCatalog()[agent+"|"+name]; len(cached) > 0 {
		byName := map[string]uiToolDetail{}
		for _, t := range cached {
			byName[t.Name] = t
		}
		for i, t := range d.Tools {
			if c, ok := byName[t.Name]; ok {
				d.Tools[i] = c
			}
		}
		d.DetailsFrom = "loaded from the server"
	}
	d.CanLoad = d.Known && len(d.Tools) > 0
	return d, nil
}

// uiToolDetailsCall asks a headless Claude, connected to only this server, to read its tool definitions.
var uiToolDetailsCall = func(cwd, server string, def json.RawMessage, tools []string) ([]uiToolDetail, error) {
	dir, err := os.MkdirTemp("", "cully-mcp-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cfg := filepath.Join(dir, "mcp.json")
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]json.RawMessage{server: def}})
	if err := os.WriteFile(cfg, b, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeExecutable(), "-p", "--model", "haiku", "--permission-mode", "dontAsk", "--no-session-persistence",
		"--tools", "ToolSearch", "--allowedTools", "ToolSearch", "--strict-mcp-config", "--mcp-config", cfg)
	cmd.Dir = cwd
	var names []string
	for _, t := range tools {
		names = append(names, "mcp__"+server+"__"+t)
	}
	cmd.Stdin = strings.NewReader("Use ToolSearch with the query select:" + strings.Join(names, ",") + " to load these tool definitions. Do not call any of the tools. Then reply with ONLY a JSON array, one object per tool, copied from the definitions: [{\"name\":\"<tool name without the mcp__ prefix>\",\"description\":\"<its description>\",\"params\":[{\"name\":\"\",\"type\":\"\",\"required\":true,\"description\":\"\"}]}]. No other text.\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	s := string(out)
	i, j := strings.Index(s, "["), strings.LastIndex(s, "]")
	if i < 0 || j < i {
		return nil, errors.New("the server's tool definitions could not be read")
	}
	var list []uiToolDetail
	if json.Unmarshal([]byte(s[i:j+1]), &list) != nil || len(list) == 0 {
		return nil, errors.New("the server's tool definitions could not be read")
	}
	return list, nil
}

// loadServerDetails reads and remembers the descriptions and parameters of one server's tools.
func (m *uiManager) loadServerDetails(agent, name string) (uiServerDetail, error) {
	if agent != "claude" {
		return uiServerDetail{}, errors.New("Cully can load tool details only for the harness tasks run in")
	}
	d, err := m.serverDetail(agent, name)
	if err != nil {
		return d, err
	}
	if len(d.Tools) == 0 || name == "cully" {
		return d, nil
	}
	def, ok := uiMCPDefsRaw()[name]
	if !ok {
		return d, errors.New("no such server")
	}
	var names []string
	for _, t := range d.Tools {
		names = append(names, t.Name)
	}
	if len(names) > 60 {
		names = names[:60]
	}
	cwd := ConfigDir()
	if repos := m.repoList(); len(repos) > 0 {
		cwd = repos[0].Path
	}
	list, err := uiToolDetailsCall(cwd, name, def, names)
	if err != nil {
		return d, errors.New("could not read the tool details: " + err.Error())
	}
	for i := range list {
		if list[i].Params == nil {
			list[i].Params = []uiParam{}
		}
		list[i].Description = uiClipWords(m.cfg.Tokens.redact(list[i].Description), 600)
	}
	all := uiReadCatalog()
	all[agent+"|"+name] = list
	uiDB().put("cache", "catalog", all)
	return m.serverDetail(agent, name)
}

func uiMCPDefsRaw() map[string]json.RawMessage { return uiMCPDefs() }

// uiProbeCall starts the harness for one throwaway turn and returns its stream. The start line lists every
// tool and prompt it loaded and the status of each connected server, which is what the Harness page shows.
var uiProbeCall = func() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeExecutable(), "-p", "--output-format", "stream-json", "--verbose", "--max-turns", "1", "--model", "haiku", "--permission-mode", "plan", "--no-session-persistence")
	cmd.Dir = ConfigDir()
	cmd.Stdin = strings.NewReader("Reply with the single word ok.\n")
	return cmd.Output()
}

// probeHarness records what the harness reports at start. It costs one very small turn.
func (m *uiManager) probeHarness() (int, error) {
	out, err := uiProbeCall()
	if err != nil && len(out) == 0 {
		return 0, errors.New("could not start Claude to check the servers")
	}
	servers := 0
	for _, line := range strings.Split(string(out), "\n") {
		p := parseStreamLine([]byte(line))
		if len(p.InitTools) > 0 {
			m.rememberInit(p.InitTools, p.InitCommands)
		}
		if len(p.MCP) > 0 {
			m.rememberMCP(p.MCP)
			servers = len(p.MCP)
		}
	}
	m.infoMu.Lock()
	delete(m.infoCache, "claude") // the inventory shows the new statuses at once
	m.infoMu.Unlock()
	if servers == 0 {
		return 0, errors.New("Claude did not report its servers")
	}
	return servers, nil
}

// probeIfDue checks the servers once when the page starts if nothing is known or what is known is old.
func (m *uiManager) probeIfDue() {
	if n := m.initKnown(); n != nil && time.Since(n.Seen) < 12*time.Hour {
		return
	}
	_, _ = m.probeHarness()
}

// uiHarnessTools is every tool the harness has: its own built-in tools and each
// connected server's, with descriptions where they have been loaded. The Tools
// section of the Harness page lists them in one place so access can be set tool by tool.
type uiHarnessToolsServer struct {
	uiInfoServer
	Tools       []uiToolDetail `json:"tools"`
	DetailsFrom string         `json:"detailsFrom,omitempty"`
	CanLoad     bool           `json:"canLoad"`
}

type uiHarnessTools struct {
	Builtin []string               `json:"builtin"`
	Servers []uiHarnessToolsServer `json:"servers"`
	Known   bool                   `json:"known"` // a task has run, so tool names are known
	Note    string                 `json:"note,omitempty"`
}

func (m *uiManager) harnessTools(agent string) uiHarnessTools {
	out := uiHarnessTools{Builtin: []string{}, Servers: []uiHarnessToolsServer{}}
	if agent != "claude" {
		out.Note = "Cully lists tools only for the harness tasks run in."
		return out
	}
	if n := m.initKnown(); n != nil {
		out.Known = true
		for _, t := range n.Tools {
			if !strings.HasPrefix(t, "mcp__") {
				out.Builtin = append(out.Builtin, t)
			}
		}
		sort.Strings(out.Builtin)
	}
	for _, s := range m.harnessInfo(agent).MCP {
		d, err := m.serverDetail(agent, s.Name)
		if err != nil {
			continue
		}
		out.Servers = append(out.Servers, uiHarnessToolsServer{uiInfoServer: d.uiInfoServer, Tools: d.Tools, DetailsFrom: d.DetailsFrom, CanLoad: d.CanLoad})
	}
	return out
}

func (s *uiServer) harnessToolsGet(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		agent = "claude"
	}
	if !uiKnownAgent(agent) {
		uiError(w, http.StatusBadRequest, "unknown harness")
		return
	}
	uiJSON(w, http.StatusOK, s.m.harnessTools(agent))
}
