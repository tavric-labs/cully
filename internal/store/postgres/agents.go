package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mcp-runtime/cully/internal/memory"
)

// Named agents live in their own table, private to the owner. They are not
// entries, so they have no Mem0 projection and no outbox job.

func (s *Store) agentOp(ctx context.Context, owner string, r memory.Request) (memory.Result, error) {
	switch r.Operation {
	case "agent_save":
		return s.agentSave(ctx, owner, *r.AgentSave)
	case "agent_list":
		return s.agentList(ctx, owner, *r.AgentQuery)
	default:
		return s.agentDelete(ctx, owner, *r.AgentRef)
	}
}

func (s *Store) agentSave(ctx context.Context, owner string, v memory.AgentInput) (memory.Result, error) {
	var rec memory.AgentRecord
	err := s.Pool.QueryRow(ctx, `INSERT INTO cully_agents(owner_subject,agent_id,name,scope,approved,definition,notes,updated_at)
		VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,now())
		ON CONFLICT (owner_subject,agent_id) DO UPDATE SET name=EXCLUDED.name, scope=EXCLUDED.scope, approved=EXCLUDED.approved, definition=EXCLUDED.definition, notes=EXCLUDED.notes, updated_at=now()
		RETURNING agent_id,name,scope,approved,definition::text,notes,updated_at`,
		owner, v.AgentID, v.Name, v.Scope, v.Approved, v.Definition, v.Notes).
		Scan(&rec.AgentID, &rec.Name, &rec.Scope, &rec.Approved, &rec.Definition, &rec.Notes, &rec.UpdatedAt)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return memory.Result{}, memory.ErrConflict // another agent of this owner already has that name
		}
		return memory.Result{}, memory.ErrUnavailable
	}
	rec.UpdatedAt = rec.UpdatedAt.UTC()
	return memory.Result{Agent: &rec}, nil
}

func (s *Store) agentList(ctx context.Context, owner string, v memory.AgentQueryInput) (memory.Result, error) {
	rows, err := s.Pool.Query(ctx, `SELECT agent_id,name,scope,approved,definition::text,notes,updated_at FROM cully_agents WHERE owner_subject=$1 ORDER BY lower(name) LIMIT $2`, owner, v.Limit)
	if err != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	defer rows.Close()
	out := memory.Result{Agents: []memory.AgentRecord{}}
	for rows.Next() {
		var rec memory.AgentRecord
		var at time.Time
		if err := rows.Scan(&rec.AgentID, &rec.Name, &rec.Scope, &rec.Approved, &rec.Definition, &rec.Notes, &at); err != nil {
			return memory.Result{}, memory.ErrUnavailable
		}
		rec.UpdatedAt = at.UTC()
		out.Agents = append(out.Agents, rec)
	}
	if rows.Err() != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	return out, nil
}

func (s *Store) agentDelete(ctx context.Context, owner string, v memory.AgentRefInput) (memory.Result, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `DELETE FROM cully_agents WHERE owner_subject=$1 AND agent_id=$2 RETURNING agent_id`, owner, v.AgentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.Result{}, nil // already gone
	}
	if err != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	return memory.Result{Deleted: true}, nil
}
