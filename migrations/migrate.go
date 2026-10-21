// Package migrations applies each Cully schema migration in its own transaction.
package migrations

import (
	"context"
	_ "embed"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RequiredVersion is the newest schema version that must be present before
// cully-data may serve traffic. Keep this equal to the last Apply step.
const RequiredVersion = 8

//go:embed 002_source_only.sql
var schema string

//go:embed 003_session_ref.sql
var sessionRefSchema string

//go:embed 004_sessions_tasks.sql
var sessionsTasksSchema string

//go:embed 005_validate_task_entry_type.sql
var validateTaskEntryTypeSchema string

//go:embed 006_workspaces.sql
var workspaceSchema string

//go:embed 007_agent_session_id.sql
var agentSessionIDSchema string

//go:embed 008_private_memory.sql
var privateMemorySchema string

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	for _, m := range []struct {
		version int
		sql     string
	}{{2, schema}, {3, sessionRefSchema}, {4, sessionsTasksSchema}, {5, validateTaskEntryTypeSchema}, {6, workspaceSchema}, {7, agentSessionIDSchema}, {RequiredVersion, privateMemorySchema}} {
		if err := applyOne(ctx, pool, m.version, m.sql); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, version int, sql string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(724812093)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS cully_schema_versions(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cully_schema_versions WHERE version=$1)", version).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO cully_schema_versions(version) VALUES($1)", version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
