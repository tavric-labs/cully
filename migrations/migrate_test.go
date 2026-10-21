package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyUpgradesSourceOnlySchema(t *testing.T) {
	dsn := os.Getenv("CULLY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CULLY_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	name := "cully_migrate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + name + `"`
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE") })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, "CREATE TABLE cully_schema_versions(version integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, schema); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO cully_schema_versions(version) VALUES(2)"); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err = pool.Exec(ctx, "INSERT INTO cully_entries(id,owner_subject,section,project_url,entry_type,summary,assistant,occurred_at) VALUES($1::uuid,'owner-a','company','https://github.com/example/old-project','work','old note','codex',now())", id); err != nil {
		t.Fatal(err)
	}
	// Exercise an existing v7 session/task link as well as a v2 note. The
	// metadata cleanup must not move either record to a different owner.
	for _, step := range []struct {
		version int
		sql     string
	}{{3, sessionRefSchema}, {4, sessionsTasksSchema}, {5, validateTaskEntryTypeSchema}, {6, workspaceSchema}, {7, agentSessionIDSchema}} {
		if err := applyOne(ctx, pool, step.version, step.sql); err != nil {
			t.Fatal(err)
		}
	}
	refValue := "codex-0123456789abcdef"
	if _, err := pool.Exec(ctx, `INSERT INTO cully_entries(id,owner_subject,section,entry_type,summary,assistant,session_ref,occurred_at) VALUES('00000000-0000-0000-0000-000000000001','owner-a','company','task','Keep task link','codex',$1,now())`, refValue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO cully_sessions(owner_subject,session_ref,section,assistant,task_id) VALUES('owner-a',$1,'company','codex','00000000-0000-0000-0000-000000000001')`, refValue); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, pool); err != nil {
		t.Fatal("migration is not idempotent:", err)
	}
	var summary string
	var sessionOwner, taskID string
	var sessionAgentID *string
	if err := pool.QueryRow(ctx, `SELECT owner_subject,task_id::text,agent_session_id FROM cully_sessions WHERE session_ref=$1`, refValue).Scan(&sessionOwner, &taskID, &sessionAgentID); err != nil || sessionOwner != "owner-a" || taskID != "00000000-0000-0000-0000-000000000001" || sessionAgentID != nil {
		t.Fatalf("session/task provenance lost: owner=%s task=%s native=%v err=%v", sessionOwner, taskID, sessionAgentID, err)
	}
	var obsolete int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name IN ('cully_entries','cully_sessions') AND column_name IN ('project_url','project','section','category','theme','tags')`).Scan(&obsolete); err != nil || obsolete != 0 {
		t.Fatalf("ambiguous columns remain: %d %v", obsolete, err)
	}
	var ref, agentID *string
	if err = pool.QueryRow(ctx, "SELECT summary,session_ref,agent_session_id FROM cully_entries WHERE id=$1::uuid", id).Scan(&summary, &ref, &agentID); err != nil || summary != "old note" || ref != nil || agentID != nil {
		t.Fatalf("old record lost: summary=%q ref=%v agentID=%v err=%v", summary, ref, agentID, err)
	}
	var queued bool
	if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cully_mem0_jobs WHERE owner_subject='owner-a' AND entry_id=$1::uuid)", id).Scan(&queued); err != nil || !queued {
		t.Fatalf("semantic metadata rebuild was not queued: %v", err)
	}
	var version5, versionRequired, validated bool
	if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cully_schema_versions WHERE version=5)").Scan(&version5); err != nil || !version5 {
		t.Fatalf("version 5 missing: %v", err)
	}
	if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cully_schema_versions WHERE version=$1)", RequiredVersion).Scan(&versionRequired); err != nil || !versionRequired {
		t.Fatalf("required version %d missing: %v", RequiredVersion, err)
	}
	if err = pool.QueryRow(ctx, "SELECT convalidated FROM pg_constraint WHERE conrelid='cully_entries'::regclass AND conname='cully_entries_entry_type_check'").Scan(&validated); err != nil || !validated {
		t.Fatalf("task entry constraint not validated: %v", err)
	}
}
