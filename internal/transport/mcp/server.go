// Package mcp exposes shared memory through the official Go MCP SDK.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type AuthConfig struct {
	ExperimentalTeam bool // reserved for existing workspace integration tests
	Mode             string
	Owner            string
	Verifier         auth.TokenVerifier
	Issuer           string
	Resource         string
}

func add[I any](server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error), name, description string, write bool, request func(I) memory.Request) {
	sdk.AddTool(server, &sdk.Tool{Name: name, Description: description, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: !write}}, func(ctx context.Context, _ *sdk.CallToolRequest, input I) (*sdk.CallToolResult, memory.Result, error) {
		owner, err := identity(ctx, write)
		if err != nil {
			return nil, memory.Result{}, err
		}
		req := request(input)
		out, err := service.Execute(ctx, owner, req)
		if err == nil && (req.Operation == "log" || req.Operation == "update") {
			out = acknowledgeWrite(out)
		}
		return nil, out, err
	})
}

// acknowledgeWrite drops the record text a write would echo back. The agent
// just sent it, so returning it again only spends the agent's context; the id
// and timestamps are enough to update or delete the record later, and the
// native agent session ID is kept because it is the link that resumes the work.
func acknowledgeWrite(out memory.Result) memory.Result {
	if out.Entry == nil {
		return out
	}
	e := out.Entry
	out.Entry = &memory.Entry{ID: e.ID, SessionRef: e.SessionRef, AgentSessionID: e.AgentSessionID, EntryType: e.EntryType, OccurredAt: e.OccurredAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
	return out
}

// toolRegistration is one MCP tool Cully serves. Handler registers exactly
// this table, so contract tests enumerate the same list clients discover.
type toolRegistration struct {
	name        string
	description string
	write       bool
	register    func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error))
}

func toolRegistrations() []toolRegistration {
	return []toolRegistration{
		{name: "cully_workspace_read", description: "Read an authorized team workspace: whoami, projects, board, inbox, task_get (portable checkpoint/review packet), lessons (up to three). Requires OAuth; private memory remains separate.", register: registerWorkspace},
		{name: "cully_workspace_write", description: "Explicit team changes: team_create, team_member, project_create, project_member, task_create, task_claim, task_checkpoint, task_release, task_state, task_submit, task_approve, learning_draft, learning_publish, learning_unshare, learning_edit, learning_delete, playbook_adopt. Requires OAuth and project roles. Supply the current version for task and learning changes. Use manual agent launch; evidence is reported, never an independent attestation. Publishing a lesson is an explicit sharing action.", write: true, register: registerWorkspace},
		{name: "cully_log", description: "Save a private, authenticated-owner-scoped work record.", write: true, register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.LogInput) memory.Request { return memory.Request{Operation: "log", Log: &v} })
		}},
		{name: "cully_search", description: "Search authoritative source records using PostgreSQL full-text search.", register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.SearchInput) memory.Request { return memory.Request{Operation: "search", Search: &v} })
		}},
		{name: "cully_recall", description: "Recall live source records through self-hosted Mem0 semantic memory. Requires configured Mem0 indexing.", register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.SearchInput) memory.Request { return memory.Request{Operation: "recall", Search: &v} })
		}},
		{name: "cully_recent", description: "List recent owned records, optionally filtered by session or time.", register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.RecentInput) memory.Request { return memory.Request{Operation: "recent", Recent: &v} })
		}},
		{name: "cully_context", description: "Get up to five concise, owner-scoped prior-work previews for a task. Filter by opaque session_ref when known. Use text or semantic mode with a query, or recent mode without one. Recent mode accepts optional since (inclusive) and until (exclusive) RFC3339 timestamps. Results include timestamp, session links and has_more; previews are not complete activity. Fetch full details with cully_get only when needed.", register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			sdk.AddTool(server, &sdk.Tool{Name: t.name, Description: t.description, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *sdk.CallToolRequest, input ContextInput) (*sdk.CallToolResult, ContextResult, error) {
				owner, err := identity(ctx, false)
				if err != nil {
					return nil, ContextResult{}, err
				}
				out, err := compactContext(ctx, service, owner, input)
				return nil, out, err
			})
		}},
		{name: "cully_session", description: "Start or update the current agent session with its native agent session ID, branch and an optional task name. A new task name saves one task record linked to the session; the same name again keeps or relinks that record without adding a duplicate. Omit task to keep the current link; pass clear_task to unlink it while keeping the task record.", write: true, register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.SessionInput) memory.Request { return memory.Request{Operation: "session", Session: &v} })
		}},
		{name: "cully_session_get", description: "Get one session by its opaque session_ref, including its task.", register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.SessionRefInput) memory.Request {
				return memory.Request{Operation: "session_get", SessionGet: &v}
			})
		}},
		{name: "cully_get", description: "Get one owned memory record.", register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.IDInput) memory.Request { return memory.Request{Operation: "get", ID: &v} })
		}},
		{name: "cully_update", description: "Update selected fields. Empty optional text clears a field.", write: true, register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.UpdateInput) memory.Request { return memory.Request{Operation: "update", Update: &v} })
		}},
		{name: "cully_delete", description: "Delete one owned record and queue deletion of its Mem0 projection.", write: true, register: func(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
			add(server, service, identity, t.name, t.description, t.write, func(v memory.IDInput) memory.Request { return memory.Request{Operation: "delete", ID: &v} })
		}},
	}
}

func Handler(service memory.Service, cfg AuthConfig, path, version string) http.Handler {
	server := sdk.NewServer(&sdk.Implementation{Name: "Cully", Version: version}, &sdk.ServerOptions{Instructions: "Cully provides Solo for one local owner and IC for signed-in individual contributors using shared infrastructure. Both keep memory private to its authenticated owner. Team project management is not enabled in these modes. At the start of substantive work, use cully_context with a focused task query and an opaque session_ref when known to get a few concise prior-work previews. Use semantic mode when text search misses, and cully_get only for a record whose full details matter. Before finishing substantive work, use cully_log to save one concise task summary with approach, outcome, issues or missed steps, and next steps. Include the opaque session_ref and native agent_session_id supplied by an installed Cully hook when available so the conversation can be resumed. When the user accepts a task, call cully_session with that session_ref and the task name. Avoid duplicate records, raw transcripts and credentials. Search before answering history questions. Remember personal context when the user asks. Local session controls use the cully CLI."})
	identity := func(ctx context.Context, write bool) (string, error) {
		if cfg.Mode == "none" {
			return cfg.Owner, nil
		}
		ti := auth.TokenInfoFromContext(ctx)
		if ti == nil || ti.UserID == "" {
			return "", memory.ErrForbidden
		}
		scope := "tools:read"
		if write {
			scope = "tools:write"
		}
		if !slices.Contains(ti.Scopes, scope) {
			return "", memory.ErrForbidden
		}
		return ti.UserID, nil
	}
	for _, t := range toolRegistrations() {
		toolIdentity := identity
		if strings.HasPrefix(t.name, "cully_workspace_") {
			if !cfg.ExperimentalTeam {
				continue
			}
			toolIdentity = func(ctx context.Context, write bool) (string, error) {
				if cfg.Mode != "oauth" || cfg.Issuer == "" {
					return "", memory.ErrForbidden
				}
				subject, err := identity(ctx, write)
				if err != nil {
					return "", err
				}
				return workspace.Principal(cfg.Issuer, subject), nil
			}
		}
		t.register(t, server, service, toolIdentity)
	}
	mux := http.NewServeMux()
	endpoint := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 256 << 10})
	if cfg.Mode == "oauth" {
		u, _ := url.Parse(cfg.Resource)
		metadataPath := "/.well-known/oauth-protected-resource" + u.Path
		metadata := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(oauthex.ProtectedResourceMetadata{Resource: cfg.Resource, AuthorizationServers: []string{cfg.Issuer}, ScopesSupported: []string{"tools:read", "tools:write"}, BearerMethodsSupported: []string{"header"}, ResourceName: "Cully"})
		}
		mux.HandleFunc("GET "+metadataPath, metadata)
		if metadataPath != "/.well-known/oauth-protected-resource" {
			mux.HandleFunc("GET /.well-known/oauth-protected-resource", metadata)
		}
		mux.Handle(path, auth.RequireBearerToken(cfg.Verifier, &auth.RequireBearerTokenOptions{ResourceMetadataURL: u.Scheme + "://" + u.Host + metadataPath})(endpoint))
	} else {
		mux.Handle(path, endpoint)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func registerWorkspace(t toolRegistration, server *sdk.Server, service memory.Service, identity func(context.Context, bool) (string, error)) {
	sdk.AddTool(server, &sdk.Tool{Name: t.name, Description: t.description, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: !t.write}}, func(ctx context.Context, _ *sdk.CallToolRequest, v workspace.Input) (*sdk.CallToolResult, memory.Result, error) {
		if v.Write() != t.write {
			return nil, memory.Result{}, memory.ErrInvalid
		}
		actor, err := identity(ctx, t.write)
		if err != nil {
			return nil, memory.Result{}, err
		}
		out, err := service.Execute(ctx, actor, memory.Request{Operation: "workspace", Workspace: &v})
		return nil, out, err
	})
}
