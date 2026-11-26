package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/migrations"
)

func agentStore(t *testing.T) (*Store, context.Context) {
	dsn := os.Getenv("CULLY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CULLY_TEST_DATABASE_URL not set; CI uses a disposable PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM cully_agents WHERE owner_subject LIKE 'agent-test-%'"); err != nil {
		t.Fatal(err)
	}
	return &Store{Pool: pool}, ctx
}

func agentReq(id, name string) memory.Request {
	return memory.Request{Operation: "agent_save", AgentSave: &memory.AgentInput{AgentID: id, Name: name, Scope: "project", Approved: true, Definition: `{"role":"x"}`, Notes: "- note"}}
}

func TestAgentsAreSavedListedAndDeletedPerOwner(t *testing.T) {
	s, ctx := agentStore(t)
	owner, other := "agent-test-a", "agent-test-b"
	r, err := s.Execute(ctx, owner, agentReq("aaaa1111", "Release watcher"))
	if err != nil || r.Agent == nil || r.Agent.Name != "Release watcher" || !r.Agent.Approved {
		t.Fatalf("save: %+v %v", r, err)
	}
	// saving again with the same id updates it
	upd := agentReq("aaaa1111", "Release watcher")
	upd.AgentSave.Notes = "- changed"
	if r, err = s.Execute(ctx, owner, upd); err != nil || r.Agent.Notes != "- changed" {
		t.Fatalf("update: %+v %v", r, err)
	}
	list, err := s.Execute(ctx, owner, memory.Request{Operation: "agent_list", AgentQuery: &memory.AgentQueryInput{Limit: 20}})
	if err != nil || len(list.Agents) != 1 || list.Agents[0].Definition == "" {
		t.Fatalf("list: %+v %v", list, err)
	}
	// another owner sees nothing, and may use the same name
	empty, err := s.Execute(ctx, other, memory.Request{Operation: "agent_list", AgentQuery: &memory.AgentQueryInput{Limit: 20}})
	if err != nil || len(empty.Agents) != 0 {
		t.Fatalf("another owner's list: %+v %v", empty, err)
	}
	if _, err := s.Execute(ctx, other, agentReq("bbbb2222", "Release watcher")); err != nil {
		t.Fatalf("the same name for another owner: %v", err)
	}
	// the same owner cannot have two agents with one name, whatever the case
	if _, err := s.Execute(ctx, owner, agentReq("cccc3333", "release WATCHER")); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if d, err := s.Execute(ctx, other, memory.Request{Operation: "agent_delete", AgentRef: &memory.AgentRefInput{AgentID: "aaaa1111"}}); err != nil || d.Deleted {
		t.Fatalf("another owner deleted this owner's agent: %+v %v", d, err)
	}
	if d, err := s.Execute(ctx, owner, memory.Request{Operation: "agent_delete", AgentRef: &memory.AgentRefInput{AgentID: "aaaa1111"}}); err != nil || !d.Deleted {
		t.Fatalf("delete: %+v %v", d, err)
	}
}
