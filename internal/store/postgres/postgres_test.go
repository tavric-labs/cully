package postgres

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcp-runtime/cully/internal/identity"
	"github.com/mcp-runtime/cully/internal/mem0"
	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/store/remote"
	"github.com/mcp-runtime/cully/internal/transport/datahttp"
	mcpt "github.com/mcp-runtime/cully/internal/transport/mcp"
	"github.com/mcp-runtime/cully/migrations"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type signedTransport struct{ token string }

func (s signedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+s.token)
	return http.DefaultTransport.RoundTrip(r)
}
func TestEndToEndMCPDatabase(t *testing.T) {
	store := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer, resource := "https://issuer.test/mcp-auth", "https://public.test/cully/mcp"
	verify := identity.Verifier(func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, issuer, resource)
	data := httptest.NewServer(datahttp.Handler(memory.Service{Store: store}, "private-secret", nil))
	defer data.Close()
	httpStore, err := remote.New(data.URL, "private-secret")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mcpt.Handler(memory.Service{Store: httpStore}, mcpt.AuthConfig{Mode: "oauth", Verifier: verify, Issuer: issuer, Resource: resource}, "/mcp", "test"))
	defer server.Close()
	connect := func(owner, scope string) *sdk.ClientSession {
		t.Helper()
		raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": resource, "sub": owner, "exp": time.Now().Add(time.Hour).Unix(), "scope": scope}).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "e2e", Version: "1"}, nil)
		session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: signedTransport{raw}}, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	call := func(session *sdk.ClientSession, name string, input any) memory.Result {
		t.Helper()
		r, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
		if err != nil || r.IsError {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
		data, err := json.Marshal(r.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var out memory.Result
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	a := connect("owner-a", "tools:read tools:write")
	b := connect("owner-b", "tools:read tools:write")
	agentID := uuid.NewString()
	entry := call(a, "cully_log", memory.LogInput{Summary: "Port memory services to Go", Assistant: "codex", Section: "company", AgentSessionID: &agentID}).Entry
	if entry == nil || entry.AgentSessionID == nil || *entry.AgentSessionID != agentID {
		t.Fatal("resumable agent session ID missing from saved entry")
	}
	id := memory.IDInput{EntryID: entry.ID}
	if call(b, "cully_get", id).Entry != nil {
		t.Fatal("cross-owner MCP read")
	}
	found := call(a, "cully_search", memory.SearchInput{Query: "memory"}).Entries
	if len(found) != 1 || found[0].AgentSessionID == nil || *found[0].AgentSessionID != agentID {
		t.Fatal("MCP search lost resumable agent session ID")
	}
	if len(call(b, "cully_search", memory.SearchInput{Query: "memory"}).Entries) != 0 {
		t.Fatal("cross-owner MCP search")
	}
	summary := "Go memory services verified"
	if call(a, "cully_update", memory.UpdateInput{EntryID: entry.ID, Summary: &summary}).Entry.Summary != summary {
		t.Fatal("MCP update failed")
	}
	reader := connect("owner-a", "tools:read")
	denied, err := reader.CallTool(ctx, &sdk.CallToolParams{Name: "cully_delete", Arguments: id})
	if err != nil || !denied.IsError {
		t.Fatal("read-only token deleted memory")
	}
	if !call(a, "cully_delete", id).Deleted {
		t.Fatal("MCP deletion failed")
	}
	if call(a, "cully_get", id).Entry != nil {
		t.Fatal("deleted source still returned")
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CULLY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CULLY_TEST_DATABASE_URL not set; CI uses a disposable PostgreSQL database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "cully_test_" + uuid.NewString()
	name := `"` + schema + `"`
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+name+" CASCADE") })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name + ",public"
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return &Store{Pool: pool}
}
func execute(t *testing.T, s *Store, owner string, r memory.Request) memory.Result {
	t.Helper()
	v, e := (memory.Service{Store: s}).Execute(context.Background(), owner, r)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestDatabaseMemoryLifecycle(t *testing.T) {
	s := testStore(t)
	project := "git@github.com:MCP-Runtime/Cully.git"
	ref := "codex-0123456789abcdef"
	input := memory.LogInput{Summary: "OAuth audience validation", Assistant: "Codex", Section: "company", ProjectURL: &project, SessionRef: &ref, OccurredAt: "2026-10-06T15:00:00+05:30", Tags: []string{"Auth"}}
	logged := execute(t, s, "owner-a", memory.Request{Operation: "log", Log: &input}).Entry
	if logged == nil || logged.ID == "" || logged.OccurredAt.Format(time.RFC3339) != "2026-10-06T15:00:00+05:30" {
		t.Fatalf("bad entry %+v", logged)
	}
	if logged.ProjectURL == nil || *logged.ProjectURL != "https://github.com/mcp-runtime/cully" {
		t.Fatal("project not normalized")
	}
	if logged.SessionRef == nil || *logged.SessionRef != ref {
		t.Fatal("session reference lost")
	}
	id := memory.IDInput{EntryID: logged.ID}
	if execute(t, s, "owner-b", memory.Request{Operation: "get", ID: &id}).Entry != nil {
		t.Fatal("cross-owner get")
	}
	if execute(t, s, "owner-b", memory.Request{Operation: "delete", ID: &id}).Deleted {
		t.Fatal("cross-owner delete")
	}
	summary := "OAuth fix and lesson"
	if execute(t, s, "owner-b", memory.Request{Operation: "update", Update: &memory.UpdateInput{EntryID: logged.ID, Summary: &summary}}).Entry != nil {
		t.Fatal("cross-owner update")
	}
	for _, search := range []memory.SearchInput{{Query: "OAuth"}, {Query: "audience"}} {
		r := execute(t, s, "owner-a", memory.Request{Operation: "search", Search: &search})
		if len(r.Entries) != 1 {
			t.Fatalf("search failed %+v", r)
		}
		r = execute(t, s, "owner-b", memory.Request{Operation: "search", Search: &search})
		if len(r.Entries) != 0 {
			t.Fatal("cross-owner search")
		}
	}
	bySession := execute(t, s, "owner-a", memory.Request{Operation: "recent", Recent: &memory.RecentInput{SessionRef: &ref}})
	if len(bySession.Entries) != 1 || bySession.Entries[0].ID != logged.ID {
		t.Fatal("session filter failed")
	}
	if entries := execute(t, s, "owner-b", memory.Request{Operation: "recent", Recent: &memory.RecentInput{SessionRef: &ref}}).Entries; len(entries) != 0 {
		t.Fatal("cross-owner session filter")
	}
	learning := "Validate the public resource"
	updated := execute(t, s, "owner-a", memory.Request{Operation: "update", Update: &memory.UpdateInput{EntryID: logged.ID, Learning: &learning, Summary: &summary}}).Entry
	if updated.Learning == nil || *updated.Learning != learning {
		t.Fatal("update lost")
	}
	empty := ""
	updated = execute(t, s, "owner-a", memory.Request{Operation: "update", Update: &memory.UpdateInput{EntryID: logged.ID, Learning: &empty}}).Entry
	if updated.Learning != nil {
		t.Fatal("field was not cleared")
	}
	category := "reading"
	execute(t, s, "owner-a", memory.Request{Operation: "log", Log: &memory.LogInput{Summary: "Read a book", Assistant: "chatgpt", Section: "personal", Category: &category}})
	personal := "personal"
	r := execute(t, s, "owner-a", memory.Request{Operation: "recent", Recent: &memory.RecentInput{Section: &personal}})
	if len(r.Entries) != 1 || r.Entries[0].ProjectURL != nil {
		t.Fatal("personal section lost")
	}
	projects := execute(t, s, "owner-a", memory.Request{Operation: "projects", Projects: &memory.ProjectsInput{}})
	if len(projects.Projects) != 1 || projects.Projects[0].EntryCount != 1 {
		t.Fatal("project summary failed")
	}
	// Reapplying the fresh schema must preserve authoritative records.
	if err := migrations.Apply(context.Background(), s.Pool); err != nil {
		t.Fatal(err)
	}
	var obsoleteColumns int
	if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='cully_entries' AND column_name='embedding'").Scan(&obsoleteColumns); err != nil || obsoleteColumns != 0 {
		t.Fatalf("obsolete embedding column remains: %d %v", obsoleteColumns, err)
	}
	if execute(t, s, "owner-a", memory.Request{Operation: "get", ID: &id}).Entry.ID != logged.ID {
		t.Fatal("schema reapply lost memory")
	}
	if !execute(t, s, "owner-a", memory.Request{Operation: "delete", ID: &id}).Deleted {
		t.Fatal("delete failed")
	}
}
func TestDurableMem0Projection(t *testing.T) {
	s := testStore(t)
	var stored []mem0.Hit
	var outage bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outage {
			w.WriteHeader(503)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /memories", "POST /search":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": stored})
		case "POST /memories":
			var v struct {
				UserID   string         `json:"user_id"`
				RunID    string         `json:"run_id"`
				Metadata map[string]any `json:"metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&v)
			stored = []mem0.Hit{{ID: "projection", UserID: v.UserID, RunID: v.RunID, Metadata: v.Metadata}}
			_, _ = w.Write([]byte(`{}`))
		case "DELETE /memories/projection":
			stored = nil
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected Mem0 call %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	s.Mem0, _ = mem0.New(server.URL, "test-key")
	s.Mem0Enabled = true
	e := execute(t, s, "owner-a", memory.Request{Operation: "log", Log: &memory.LogInput{Summary: "Remember OAuth", Assistant: "codex", Section: "company"}}).Entry
	outage = true
	if err := s.processMem0Job(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM cully_mem0_jobs").Scan(&count); err != nil || count != 1 {
		t.Fatal("outage lost durable job")
	}
	outage = false
	if _, err := s.Pool.Exec(context.Background(), "UPDATE cully_mem0_jobs SET next_attempt=now()"); err != nil {
		t.Fatal(err)
	}
	if err := s.processMem0Job(context.Background()); err != nil {
		t.Fatal(err)
	}
	recalled := execute(t, s, "owner-a", memory.Request{Operation: "recall", Search: &memory.SearchInput{Query: "OAuth"}})
	if len(recalled.Entries) != 1 {
		t.Fatalf("recall failed %+v", recalled)
	}
	if len(execute(t, s, "owner-b", memory.Request{Operation: "recall", Search: &memory.SearchInput{Query: "OAuth"}}).Entries) != 0 {
		t.Fatal("Mem0 leaked another owner")
	}
	execute(t, s, "owner-a", memory.Request{Operation: "delete", ID: &memory.IDInput{EntryID: e.ID}})
	if len(execute(t, s, "owner-a", memory.Request{Operation: "recall", Search: &memory.SearchInput{Query: "OAuth"}}).Entries) != 0 {
		t.Fatal("deleted source returned before projection cleanup")
	}
	if err := s.processMem0Job(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatal("Mem0 deletion not propagated")
	}
}

func TestDatabaseSessionsAndTasks(t *testing.T) {
	s := testStore(t)
	project := "https://github.com/mcp-runtime/cully"
	branch := "feat/oauth-validation"
	task := "Oauth validation"
	ref := "claude-0123456789abcdef"
	start := func(owner string, task *string) memory.Result {
		in := memory.SessionInput{SessionRef: ref, Assistant: "claude", Section: "company", ProjectURL: &project, Branch: &branch, Task: task}
		return execute(t, s, owner, memory.Request{Operation: "session", Session: &in})
	}
	first := start("owner-a", &task)
	if first.Session == nil || first.Session.Task == nil || *first.Session.Task != task || first.Entry == nil || first.Entry.EntryType != "task" {
		t.Fatalf("task not created and linked: %+v", first)
	}
	if first.Session.TaskID == nil || *first.Session.TaskID != first.Entry.ID {
		t.Fatal("session not linked to its task entry")
	}
	again := start("owner-a", &task)
	if again.Entry != nil || again.Session.TaskID == nil || *again.Session.TaskID != *first.Session.TaskID {
		t.Fatal("same task name must not create a second entry")
	}
	personalTask := "Private work"
	wrongSection := memory.SessionInput{SessionRef: ref, Assistant: "claude", Section: "personal", Task: &personalTask}
	if _, err := (memory.Service{Store: s}).Execute(context.Background(), "owner-a", memory.Request{Operation: "session", Session: &wrongSection}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("section change must be rejected as invalid: %v", err)
	}
	if got := start("owner-a", nil); got.Session.TaskID == nil || *got.Session.TaskID != *first.Session.TaskID {
		t.Fatal("rejected section change altered the session task")
	}
	kind := "task"
	tasks := execute(t, s, "owner-a", memory.Request{Operation: "recent", Recent: &memory.RecentInput{EntryType: &kind, Limit: 10}})
	if len(tasks.Entries) != 1 {
		t.Fatalf("task entries = %d", len(tasks.Entries))
	}
	renamed := "Token audience checks"
	if second := start("owner-a", &renamed); second.Entry == nil || *second.Session.Task != renamed {
		t.Fatal("a new task name must replace the link")
	}
	metaOnly := start("owner-a", nil)
	if metaOnly.Session.Task == nil || *metaOnly.Session.Task != renamed {
		t.Fatal("updating without a task must keep the existing one")
	}
	get := func(owner string) *memory.Session {
		return execute(t, s, owner, memory.Request{Operation: "session_get", SessionGet: &memory.SessionRefInput{SessionRef: ref}}).Session
	}
	if got := get("owner-a"); got == nil || got.Branch == nil || *got.Branch != branch {
		t.Fatalf("session_get = %+v", got)
	}
	if get("owner-b") != nil {
		t.Fatal("cross-owner session read")
	}
	id := memory.IDInput{EntryID: *metaOnly.Session.TaskID}
	execute(t, s, "owner-a", memory.Request{Operation: "delete", ID: &id})
	if got := get("owner-a"); got == nil || got.TaskID != nil || got.Task != nil {
		t.Fatalf("deleting the task entry must unlink it: %+v", got)
	}
}

func TestDatabaseSessionTaskClearAndScope(t *testing.T) {
	s := testStore(t)
	project := "https://github.com/mcp-runtime/cully"
	task := "Oauth validation"
	ref := "claude-fedcba9876543210"
	clear := true
	personal := "personal"
	session := func(owner string, task *string, clear *bool) memory.Result {
		in := memory.SessionInput{SessionRef: ref, Assistant: "claude", Section: "company", ProjectURL: &project, Task: task, ClearTask: clear}
		return execute(t, s, owner, memory.Request{Operation: "session", Session: &in})
	}
	linked := session("owner-a", &task, nil)
	if linked.Session.TaskID == nil {
		t.Fatal("task not linked")
	}
	taskID := *linked.Session.TaskID
	// A linked task keeps its session's section.
	update := memory.UpdateInput{EntryID: taskID, Section: &personal}
	if _, err := (memory.Service{Store: s}).Execute(context.Background(), "owner-a", memory.Request{Operation: "update", Update: &update}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("linked task section change must be rejected as invalid: %v", err)
	}
	// Explicit clear unlinks the session but keeps the task record.
	cleared := session("owner-a", nil, &clear)
	if cleared.Session.TaskID != nil || cleared.Session.Task != nil {
		t.Fatalf("clear_task must unlink the task: %+v", cleared.Session)
	}
	kind := "task"
	kept := execute(t, s, "owner-a", memory.Request{Operation: "recent", Recent: &memory.RecentInput{EntryType: &kind, Limit: 10}})
	if len(kept.Entries) != 1 || kept.Entries[0].ID != taskID {
		t.Fatalf("clear_task must keep the task record: %+v", kept.Entries)
	}
	// Omitted task after a clear stays cleared; setting links again.
	if got := session("owner-a", nil, nil); got.Session.TaskID != nil {
		t.Fatal("omitted task must not restore the cleared link")
	}
	again := "Token audience checks"
	if relinked := session("owner-a", &again, nil); relinked.Session.TaskID == nil || *relinked.Session.Task != again {
		t.Fatal("a new task name must link after a clear")
	}
	// Another owner cannot touch the task through update scoping.
	other := memory.UpdateInput{EntryID: taskID, Summary: &again}
	if got := execute(t, s, "owner-b", memory.Request{Operation: "update", Update: &other}); got.Entry != nil {
		t.Fatal("cross-owner task update")
	}
}

func TestDatabaseSessionTaskRepeatAfterClearRelinks(t *testing.T) {
	s := testStore(t)
	project := "https://github.com/mcp-runtime/cully"
	task := "Oauth validation"
	clear := true
	session := func(task *string, clear *bool) memory.Result {
		in := memory.SessionInput{SessionRef: "claude-0123456789abcdef", Assistant: "claude", Section: "company", ProjectURL: &project, Task: task, ClearTask: clear}
		return execute(t, s, "owner-a", memory.Request{Operation: "session", Session: &in})
	}
	taskID := *session(&task, nil).Session.TaskID
	session(nil, &clear)
	// The same name after a clear relinks the kept record instead of duplicating it.
	relinked := session(&task, nil)
	if relinked.Session.TaskID == nil || *relinked.Session.TaskID != taskID {
		t.Fatalf("repeat after clear must relink the existing task: %+v", relinked.Session)
	}
	kind := "task"
	all := execute(t, s, "owner-a", memory.Request{Operation: "recent", Recent: &memory.RecentInput{EntryType: &kind, Limit: 10}})
	if len(all.Entries) != 1 {
		t.Fatalf("repeat after clear must not insert a duplicate: %+v", all.Entries)
	}
}

func TestDatabaseRecentTimeWindowAndOwner(t *testing.T) {
	s := testStore(t)
	personal, company := "personal", "company"
	for _, row := range []struct{ owner, section, when string }{
		{"window-owner-a", personal, "2026-10-18T09:00:00+05:30"},
		{"window-owner-a", personal, "2026-10-19T09:00:00+05:30"},
		{"window-owner-a", personal, "2026-10-18T08:59:59+05:30"},
		{"window-owner-b", personal, "2026-10-18T09:00:00+05:30"},
		{"window-owner-a", company, "2026-10-18T09:00:00+05:30"},
	} {
		execute(t, s, row.owner, memory.Request{Operation: "log", Log: &memory.LogInput{Summary: "Window boundary evidence", Assistant: "codex", Section: row.section, OccurredAt: row.when}})
	}
	result := execute(t, s, "window-owner-a", memory.Request{Operation: "recent", Recent: &memory.RecentInput{Section: &personal, Since: "2026-10-18T03:30:00Z", Until: "2026-10-19T03:30:00Z", Limit: 3}})
	if result.Since != "2026-10-18T03:30:00Z" || result.Until != "2026-10-19T03:30:00Z" || len(result.Entries) != 1 || result.Entries[0].OccurredAt.Format(time.RFC3339) != "2026-10-18T09:00:00+05:30" {
		t.Fatalf("half-open time window or owner/section scoping failed: %+v", result.Entries)
	}
}
