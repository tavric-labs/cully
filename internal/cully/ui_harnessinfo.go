package cully

import (
	"bufio"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// What each supported harness has set up on this machine: the model it uses,
// its permission settings, the MCP servers it has connected, its skills, hooks,
// plugins, commands and instruction files. Only names and counts are read, and
// anything that could hold a secret (headers, environment values, command
// arguments, URL queries) is left out. Each harness keeps these in a different
// place, so each has its own reader.

type uiInfoServer struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`   // http, stdio, ...
	Target  string   `json:"target"` // address without credentials, or the program's name
	Status  string   `json:"status,omitempty"`
	Headers []string `json:"headers,omitempty"` // names of the headers set for it, never their values
	Tools   int      `json:"tools,omitempty"`   // tools it offered the last time a task started
	Prompts int      `json:"prompts,omitempty"` // prompts (slash commands) it offered
}

type uiInfoSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
}

type uiInfoHook struct {
	Event string   `json:"event"`
	Count int      `json:"count"`
	Runs  []string `json:"runs"` // the programs it runs, by name
}

type uiHarnessInfo struct {
	Agent        string         `json:"agent"`
	Name         string         `json:"name"`
	Installed    bool           `json:"installed"`
	Version      string         `json:"version,omitempty"`
	Model        string         `json:"model,omitempty"`
	Mode         string         `json:"mode,omitempty"` // its own permission or sandbox mode
	Allow        int            `json:"allow"`          // permission rules it already holds
	Deny         int            `json:"deny"`
	MCP          []uiInfoServer `json:"mcp"`
	Skills       []uiInfoSkill  `json:"skills"`
	Hooks        []uiInfoHook   `json:"hooks"`
	Plugins      []string       `json:"plugins"`
	Commands     []string       `json:"commands"`
	Subagents    []string       `json:"subagents"`
	Instructions []string       `json:"instructions"`       // instruction files that exist
	Notes        []string       `json:"notes"`              // what this harness does not expose
	TasksUseIt   bool           `json:"tasksUseIt"`         // tasks started here run in this harness
	StatusAt     int64          `json:"statusAt,omitempty"` // when the servers' states were last checked, unix seconds
}

// uiHarnessVersion runs `<harness> --version`; it is a variable so tests need no real program.
var uiHarnessVersion = func(agent string) string {
	bin := map[string]string{"claude": claudeExecutable(), "codex": "codex", "cursor": "cursor-agent"}[agent]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func uiHomeDir() string { h, _ := os.UserHomeDir(); return h }

func uiCodexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(uiHomeDir(), ".codex")
}

func uiCursorHome() string {
	if d := os.Getenv("CURSOR_HOME"); d != "" {
		return d
	}
	return filepath.Join(uiHomeDir(), ".cursor")
}

func uiSafeTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.Path
}

func uiProgramName(cmd string) string {
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return ""
	}
	return filepath.Base(f[0])
}

func uiFrontmatter(path string) (name, desc string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	in := false
	for i := 0; sc.Scan() && i < 40; i++ {
		line := sc.Text()
		if i == 0 && strings.TrimSpace(line) == "---" {
			in = true
			continue
		}
		if in && strings.TrimSpace(line) == "---" {
			break
		}
		if in {
			if k, v, ok := strings.Cut(line, ":"); ok {
				switch strings.TrimSpace(k) {
				case "name":
					name = strings.Trim(strings.TrimSpace(v), `"'`)
				case "description":
					desc = uiClipWords(strings.Trim(strings.TrimSpace(v), `"'`), 160)
				}
			}
		}
	}
	return name, desc
}

func uiSkillsIn(dir, scope string) []uiInfoSkill {
	var out []uiInfoSkill
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		p := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		n, d := uiFrontmatter(p)
		if n == "" {
			n = e.Name()
		}
		out = append(out, uiInfoSkill{Name: n, Description: d, Scope: scope})
	}
	return out
}

func uiMarkdownNames(dir string) []string {
	var out []string
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".md"))
	}
	sort.Strings(out)
	return out
}

// uiHooksFrom reads hook events from a JSON settings file, either at the top
// level or under "hooks", and lists the programs each one runs by name.
func uiHooksFrom(raw map[string]any) []uiInfoHook {
	if h, ok := raw["hooks"].(map[string]any); ok {
		raw = h
	}
	var out []uiInfoHook
	for ev, v := range raw {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		h := uiInfoHook{Event: ev}
		var walk func(any)
		walk = func(x any) {
			switch t := x.(type) {
			case map[string]any:
				if c, ok := t["command"].(string); ok {
					h.Count++
					if n := uiProgramName(c); n != "" && len(h.Runs) < 4 {
						h.Runs = append(h.Runs, n)
					}
				}
				for _, y := range t {
					walk(y)
				}
			case []any:
				for _, y := range t {
					walk(y)
				}
			}
		}
		walk(list)
		if h.Count == 0 {
			h.Count = len(list)
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Event < out[j].Event })
	return out
}

func uiReadJSONMap(path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

func uiServersFromJSON(defs map[string]any) []uiInfoServer {
	var out []uiInfoServer
	for name, v := range defs {
		d, _ := v.(map[string]any)
		s := uiInfoServer{Name: name, Kind: "stdio"}
		if t, ok := d["type"].(string); ok && t != "" {
			s.Kind = t
		}
		if hd, ok := d["headers"].(map[string]any); ok {
			for k := range hd {
				s.Headers = append(s.Headers, k)
			}
			sort.Strings(s.Headers)
		}
		if u, ok := d["url"].(string); ok {
			if s.Kind == "stdio" {
				s.Kind = "http"
			}
			s.Target = uiSafeTarget(u)
		} else if c, ok := d["command"].(string); ok {
			s.Target = uiProgramName(c)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var uiTOMLTable = regexp.MustCompile(`^\[mcp_servers\.("?)([^\]"]+)("?)\]$`)

func uiCodexConfig() (model, approval, sandbox string, servers []uiInfoServer) {
	b, err := os.ReadFile(filepath.Join(uiCodexHome(), "config.toml"))
	if err != nil {
		return
	}
	cur := -1
	top := true
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			top = false
			cur = -1
			if m := uiTOMLTable.FindStringSubmatch(line); m != nil {
				servers = append(servers, uiInfoServer{Name: m[2], Kind: "stdio"})
				cur = len(servers) - 1
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		switch {
		case top && k == "model":
			model = v
		case top && k == "approval_policy":
			approval = v
		case top && k == "sandbox_mode":
			sandbox = v
		case cur >= 0 && k == "url":
			servers[cur].Kind, servers[cur].Target = "http", uiSafeTarget(v)
		case cur >= 0 && k == "command":
			servers[cur].Target = uiProgramName(v)
		}
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return
}

func (m *uiManager) harnessInfo(agent string) uiHarnessInfo {
	m.infoMu.Lock()
	if c, ok := m.infoCache[agent]; ok && time.Since(c.at) < time.Minute {
		m.infoMu.Unlock()
		return c.info
	}
	m.infoMu.Unlock()
	last := m.lastMCPStatus()
	info := uiHarnessInfo{Agent: agent, Name: uiHarnessName(agent), MCP: []uiInfoServer{}, Skills: []uiInfoSkill{}, Hooks: []uiInfoHook{}, Plugins: []string{}, Commands: []string{}, Subagents: []string{}, Instructions: []string{}, Notes: []string{}}
	info.Version = uiHarnessVersion(agent)
	info.Installed = info.Version != ""
	info.TasksUseIt = agent == "claude"
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	switch agent {
	case "claude":
		cfg := uiReadJSONMap(filepath.Join(ConfigDir(), "settings.json"))
		if v, ok := cfg["model"].(string); ok {
			info.Model = v
		}
		if p, ok := cfg["permissions"].(map[string]any); ok {
			if a, ok := p["allow"].([]any); ok {
				info.Allow = len(a)
			}
			if d, ok := p["deny"].([]any); ok {
				info.Deny = len(d)
			}
			info.Mode, _ = p["defaultMode"].(string)
		}
		if h := uiHooksFrom(map[string]any{"hooks": cfg["hooks"]}); cfg["hooks"] != nil {
			info.Hooks = h
		}
		if pl, ok := cfg["enabledPlugins"].(map[string]any); ok {
			for k := range pl {
				info.Plugins = append(info.Plugins, k)
			}
			sort.Strings(info.Plugins)
		}
		var defs map[string]any
		if b, err := os.ReadFile(uiClaudeJSONPath()); err == nil {
			var all struct {
				S map[string]any `json:"mcpServers"`
			}
			if json.Unmarshal(b, &all) == nil {
				defs = all.S
			}
		}
		info.MCP = uiServersFromJSON(defs)
		for i := range info.MCP {
			if st, ok := last[info.MCP[i].Name]; ok {
				info.MCP[i].Status = st
			}
		}
		// Connectors added in claude.ai are not in the config file, but Claude reports them at start.
		have := map[string]bool{}
		for _, sv := range info.MCP {
			have[sv.Name] = true
		}
		var extra []string
		for name := range last {
			if !have[name] {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			info.MCP = append(info.MCP, uiInfoServer{Name: name, Kind: "connector", Target: "Added in claude.ai", Status: last[name]})
		}
		if n := m.initKnown(); n != nil {
			for i := range info.MCP {
				prefix := "mcp__" + uiToolNameOf(info.MCP[i].Name) + "__"
				for _, t := range n.Tools {
					if strings.HasPrefix(t, prefix) {
						info.MCP[i].Tools++
					}
				}
				for _, c := range n.Commands {
					if strings.HasPrefix(strings.TrimPrefix(c, "/"), prefix) {
						info.MCP[i].Prompts++
					}
				}
			}
		}
		if len(last) == 0 {
			info.Notes = append(info.Notes, "Server status shows once a task has started: Claude reports it then.")
		}
		info.StatusAt = m.lastMCPSeen()
		info.Skills = uiSkillsIn(filepath.Join(ConfigDir(), "skills"), "yours")
		info.Commands = uiMarkdownNames(filepath.Join(ConfigDir(), "commands"))
		info.Subagents = uiMarkdownNames(filepath.Join(ConfigDir(), "agents"))
		if exists(filepath.Join(ConfigDir(), "CLAUDE.md")) {
			info.Instructions = append(info.Instructions, "CLAUDE.md")
		}
		info.Notes = append(info.Notes, "Skills and commands that come from plugins are not listed; the plugins are.")
	case "codex":
		model, approval, sandbox, servers := uiCodexConfig()
		info.Model, info.MCP = model, servers
		info.Mode = strings.Trim(approval+" / "+sandbox, " /")
		info.Skills = uiSkillsIn(filepath.Join(uiCodexHome(), "skills"), "yours")
		info.Commands = uiMarkdownNames(filepath.Join(uiCodexHome(), "prompts"))
		if h := uiReadJSONMap(filepath.Join(uiCodexHome(), "hooks.json")); h != nil {
			info.Hooks = uiHooksFrom(h)
		}
		if exists(filepath.Join(uiCodexHome(), "AGENTS.md")) {
			info.Instructions = append(info.Instructions, "AGENTS.md")
		}
		info.Notes = append(info.Notes, "Codex does not report whether a server is connected until a session uses it.")
	case "cursor":
		info.MCP = uiServersFromJSON(func() map[string]any {
			m, _ := uiReadJSONMap(filepath.Join(uiCursorHome(), "mcp.json"))["mcpServers"].(map[string]any)
			return m
		}())
		info.Commands = uiMarkdownNames(filepath.Join(uiCursorHome(), "commands"))
		info.Skills = uiSkillsIn(filepath.Join(uiCursorHome(), "skills"), "yours")
		if h := uiReadJSONMap(filepath.Join(uiCursorHome(), "hooks.json")); h != nil {
			info.Hooks = uiHooksFrom(h)
		}
		if cfg := uiReadJSONMap(filepath.Join(uiCursorHome(), "cli-config.json")); cfg != nil {
			if p, ok := cfg["permissions"].(map[string]any); ok {
				if a, ok := p["allow"].([]any); ok {
					info.Allow = len(a)
				}
				if d, ok := p["deny"].([]any); ok {
					info.Deny = len(d)
				}
			}
		}
		info.Notes = append(info.Notes, "Cursor does not record the model, token use or limits of its chats.")
	}
	// Lists are always lists, never null, so the page can count them.
	if info.MCP == nil {
		info.MCP = []uiInfoServer{}
	}
	if info.Skills == nil {
		info.Skills = []uiInfoSkill{}
	}
	if info.Hooks == nil {
		info.Hooks = []uiInfoHook{}
	}
	if info.Plugins == nil {
		info.Plugins = []string{}
	}
	if info.Commands == nil {
		info.Commands = []string{}
	}
	if info.Subagents == nil {
		info.Subagents = []string{}
	}
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	if m.infoCache == nil {
		m.infoCache = map[string]struct {
			at   time.Time
			info uiHarnessInfo
		}{}
	}
	m.infoCache[agent] = struct {
		at   time.Time
		info uiHarnessInfo
	}{time.Now(), info}
	return info
}

// lastMCPStatus is what the most recent task reported about each connected server.
func (m *uiManager) lastMCPStatus() map[string]string {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	out := map[string]string{}
	for k, v := range m.lastMCP {
		out[k] = v
	}
	return out
}

func (m *uiManager) rememberMCP(servers []uiMCPServer) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.lastMCP = map[string]string{} // the newest report replaces the old: a removed server must not linger
	for _, s := range servers {
		m.lastMCP[s.Name] = s.Status
	}
	m.lastMCPAt = time.Now().Unix()
}

func (m *uiManager) lastMCPSeen() int64 {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	return m.lastMCPAt
}

// uiToolNameOf is how Claude writes a server's name inside a tool name: anything but letters, digits,
// dashes and underscores becomes an underscore ("claude.ai Claude Docs" is claude_ai_Claude_Docs).
func uiToolNameOf(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}
