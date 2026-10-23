package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mcp-runtime/cully/internal/memory"
)

func (s *Store) saveOptimization(ctx context.Context, tx pgx.Tx, owner string, v memory.OptimizationSaveInput) (memory.Result, error) {
	if err := lockSession(ctx, tx, owner, "optimization:"+v.PatternKey); err != nil {
		return memory.Result{}, err
	}
	e, err := one(ctx, tx, "SELECT "+record+" FROM cully_entries e WHERE owner_subject=$1 AND entry_type='optimization' AND optimization->>'pattern_key'=$2 FOR UPDATE", owner, v.PatternKey)
	if err != nil {
		return memory.Result{}, err
	}
	if e == nil {
		o := memory.Optimization{PatternKey: v.PatternKey, Status: "observing", FirstSeen: time.Now(), LastSeen: time.Now()}
		data, _ := json.Marshal(o)
		e, err = one(ctx, tx, "INSERT INTO cully_entries AS e (id,owner_subject,entry_type,summary,assistant,session_ref,agent_session_id,occurred_at,optimization) VALUES($1,$2,'optimization',$3,$4,$5,$6,now(),$7) RETURNING "+record, uuid.NewString(), owner, v.Summary, v.Assistant, v.SessionRef, v.AgentSessionID, data)
		if err != nil {
			return memory.Result{}, err
		}
	}
	o := e.Optimization
	// User decisions survive subsequent worker observations. Only an explicit
	// versioned decision may reopen a dismissed/adopted pattern.
	if !memory.OptimizationMatches(*e, "active") {
		return memory.Result{Entry: e}, nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO cully_optimization_observations(entry_id,session_ref,window_id,agent_session_id,summary) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, e.ID, v.SessionRef, v.WindowID, v.AgentSessionID, v.Evidence)
	if err != nil {
		return memory.Result{}, err
	}
	if tag.RowsAffected() == 0 && (v.Proposal == nil || reflect.DeepEqual(v.Proposal, o.Proposal)) {
		return memory.Result{Entry: e}, nil
	}
	if err = tx.QueryRow(ctx, `SELECT count(*),count(DISTINCT session_ref),min(observed_at),max(observed_at) FROM cully_optimization_observations WHERE entry_id=$1`, e.ID).Scan(&o.ObservationCount, &o.SessionCount, &o.FirstSeen, &o.LastSeen); err != nil {
		return memory.Result{}, err
	}
	var evidence []byte
	if err = tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(x)), '[]'::jsonb) FROM (SELECT session_ref,agent_session_id,window_id,summary,observed_at FROM cully_optimization_observations WHERE entry_id=$1 ORDER BY observed_at DESC,session_ref,window_id LIMIT 3) x`, e.ID).Scan(&evidence); err != nil {
		return memory.Result{}, err
	}
	if err = json.Unmarshal(evidence, &o.Evidence); err != nil {
		return memory.Result{}, err
	}
	if v.Proposal != nil {
		o.Proposal, o.Status = v.Proposal, "proposed"
	}
	o.Version++
	data, _ := json.Marshal(o)
	learning := fmt.Sprintf("Observed %d times in %d distinct sessions, from %s through %s. Counts identify recorded observations, not independent proof of inefficiency.", o.ObservationCount, o.SessionCount, o.FirstSeen.Format(time.DateOnly), o.LastSeen.Format(time.DateOnly))
	var approach *string
	if o.Proposal != nil {
		plan := o.Proposal.Text()
		approach = &plan
	}
	e, err = one(ctx, tx, "UPDATE cully_entries e SET summary=$3,issue=$4,learning=$5,approach=$6,optimization=$7,session_ref=$8,agent_session_id=$9,updated_at=clock_timestamp() WHERE owner_subject=$1 AND id=$2 RETURNING "+record, owner, e.ID, v.Summary, v.Evidence, learning, approach, data, v.SessionRef, v.AgentSessionID)
	return memory.Result{Entry: e}, err
}

func (s *Store) decideOptimization(ctx context.Context, tx pgx.Tx, owner string, v memory.OptimizationDecideInput) (memory.Result, error) {
	e, err := one(ctx, tx, "SELECT "+record+" FROM cully_entries e WHERE owner_subject=$1 AND id=$2 AND entry_type='optimization' FOR UPDATE", owner, v.EntryID)
	if err != nil || e == nil {
		return memory.Result{}, err
	}
	o := e.Optimization
	if o.Version != v.Version {
		return memory.Result{}, memory.ErrConflict
	}
	if (v.Status == "accepted" || v.Status == "implemented") && o.Proposal == nil {
		return memory.Result{}, fmt.Errorf("%w: a saved proposal is required", memory.ErrInvalid)
	}
	o.Version++
	o.Status, o.Decision = v.Status, v.Reason
	data, _ := json.Marshal(o)
	e, err = one(ctx, tx, "UPDATE cully_entries e SET optimization=$3,outcome=$4,updated_at=clock_timestamp() WHERE owner_subject=$1 AND id=$2 RETURNING "+record, owner, v.EntryID, data, v.Status+": "+v.Reason)
	return memory.Result{Entry: e}, err
}

func (s *Store) optimizationList(ctx context.Context, owner string, v memory.OptimizationQueryInput) (memory.Result, error) {
	if v.Mode == "semantic" {
		kind := "optimization"
		result, err := s.Recall(ctx, owner, memory.SearchInput{Query: v.Query, EntryType: &kind, Limit: 50})
		if err != nil {
			return memory.Result{}, err
		}
		out := memory.Result{Entries: []memory.Entry{}}
		for _, e := range result.Entries {
			if memory.OptimizationMatches(e, v.Status) && len(out.Entries) < v.Limit {
				out.Entries = append(out.Entries, e)
			}
		}
		return out, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	where := "owner_subject=$1 AND entry_type='optimization'"
	args := []any{owner}
	switch v.Status {
	case "active":
		where += " AND optimization->>'status' IN ('observing','proposed')"
	case "all":
	default:
		args = append(args, v.Status)
		where += fmt.Sprintf(" AND optimization->>'status'=$%d", len(args))
	}
	order := "updated_at DESC,id"
	if v.Mode == "text" {
		args = append(args, v.Query)
		q := fmt.Sprintf("websearch_to_tsquery('english',$%d)", len(args))
		where += " AND search_vector @@ " + q
		order = "ts_rank_cd(search_vector," + q + ") DESC," + order
	}
	args = append(args, v.Limit)
	result, err := rows(ctx, tx, "SELECT "+record+" FROM cully_entries e WHERE "+where+" ORDER BY "+order+fmt.Sprintf(" LIMIT $%d", len(args)), args...)
	if err != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	return result, nil
}
