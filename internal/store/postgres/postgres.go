// Package postgres implements owner-scoped source records with PostgreSQL.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcp-runtime/cully/internal/mem0"
	"github.com/mcp-runtime/cully/internal/memory"
)

type Store struct {
	Pool        *pgxpool.Pool
	Mem0Enabled bool
	Mem0        *mem0.Client
}

const record = "to_jsonb(e) - 'owner_subject' - 'search_vector'"

func decode(data []byte) (*memory.Entry, error) {
	var e memory.Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	e.NormalizeTimes()
	return &e, nil
}
func one(ctx context.Context, tx pgx.Tx, sql string, args ...any) (*memory.Entry, error) {
	var data []byte
	err := tx.QueryRow(ctx, sql, args...).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decode(data)
}
func (s *Store) Execute(ctx context.Context, owner string, r memory.Request) (memory.Result, error) {
	if r.Operation == "workspace" {
		return s.workspace(ctx, owner, *r.Workspace)
	}
	if r.Operation == "recall" {
		return s.Recall(ctx, owner, *r.Search)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	result, err := s.execute(ctx, tx, owner, r)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			return memory.Result{}, err
		}
		return memory.Result{}, fmt.Errorf("%w: database operation failed", memory.ErrUnavailable)
	}
	if s.Mem0Enabled && (r.Operation == "log" || (r.Operation == "session" && result.Entry != nil) || (r.Operation == "update" && result.Entry != nil) || (r.Operation == "delete" && result.Deleted)) {
		entryID := ""
		if result.Entry != nil {
			entryID = result.Entry.ID
		} else {
			entryID = r.ID.EntryID
		}
		_, err = tx.Exec(ctx, `INSERT INTO cully_mem0_jobs(owner_subject,entry_id) VALUES($1,$2::uuid) ON CONFLICT(owner_subject,entry_id) DO UPDATE SET generation=cully_mem0_jobs.generation+1, attempts=0, next_attempt=now()`, owner, entryID)
		if err != nil {
			return memory.Result{}, memory.ErrUnavailable
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return memory.Result{}, memory.ErrUnavailable
	}
	return result, nil
}
func (s *Store) execute(ctx context.Context, tx pgx.Tx, owner string, r memory.Request) (memory.Result, error) {
	out := memory.Result{}
	switch r.Operation {
	case "log":
		v := r.Log
		when := time.Now()
		if v.OccurredAt != "" {
			when, _ = time.Parse(time.RFC3339, v.OccurredAt)
		}
		e, err := one(ctx, tx, `INSERT INTO cully_entries AS e (id,owner_subject,section,project_url,session_ref,agent_session_id,category,entry_type,summary,approach,outcome,issue,learning,next_steps,assistant,tags,occurred_at) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING `+record, uuid.NewString(), owner, v.Section, v.ProjectURL, v.SessionRef, v.AgentSessionID, v.Category, v.EntryType, v.Summary, v.Approach, v.Outcome, v.Issue, v.Learning, v.NextSteps, v.Assistant, v.Tags, when)
		out.Entry = e
		return out, err
	case "session":
		return s.session(ctx, tx, owner, r.Session)
	case "session_get":
		sess, err := loadSession(ctx, tx, owner, r.SessionGet.SessionRef)
		out.Session = sess
		return out, err
	case "get":
		e, err := one(ctx, tx, "SELECT "+record+" FROM cully_entries e WHERE owner_subject=$1 AND id=$2::uuid", owner, r.ID.EntryID)
		out.Entry = e
		return out, err
	case "delete":
		tag, err := tx.Exec(ctx, "DELETE FROM cully_entries WHERE owner_subject=$1 AND id=$2::uuid", owner, r.ID.EntryID)
		out.Deleted = tag.RowsAffected() > 0
		return out, err
	case "update":
		v := r.Update
		// A task entry linked to a session keeps its session's scope.
		// Sessions reject section changes, so a linked task cannot move
		// to another section through an update either.
		if v.Section != nil {
			// Serialize with session link/unlink, which holds the same lock.
			var taskSession *string
			err := tx.QueryRow(ctx, `SELECT session_ref FROM cully_entries WHERE owner_subject=$1 AND id=$2::uuid AND entry_type='task'`, owner, v.EntryID).Scan(&taskSession)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return out, err
			}
			if taskSession != nil {
				if err := lockSession(ctx, tx, owner, *taskSession); err != nil {
					return out, err
				}
			}
			var linked string
			err = tx.QueryRow(ctx, `SELECT s.section FROM cully_sessions s WHERE s.owner_subject=$1 AND s.task_id=$2::uuid`, owner, v.EntryID).Scan(&linked)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return out, err
			}
			if err == nil && linked != *v.Section {
				return out, fmt.Errorf("%w: linked task section cannot change", memory.ErrInvalid)
			}
		}
		args := []any{owner, v.EntryID}
		assignments := []string{}
		add := func(field string, value any, cast string) {
			args = append(args, value)
			assignments = append(assignments, fmt.Sprintf("%s=$%d%s", field, len(args), cast))
		}
		fields := []struct {
			name  string
			value *string
		}{{"summary", v.Summary}, {"approach", v.Approach}, {"outcome", v.Outcome}, {"issue", v.Issue}, {"learning", v.Learning}, {"next_steps", v.NextSteps}, {"section", v.Section}, {"category", v.Category}}
		for _, f := range fields {
			if f.value != nil {
				if *f.value == "" && f.name != "summary" && f.name != "section" {
					add(f.name, nil, "")
				} else {
					add(f.name, *f.value, "")
				}
			}
		}
		if v.Tags != nil {
			add("tags", *v.Tags, "")
		}
		assignments = append(assignments, "updated_at=now()")
		e, err := one(ctx, tx, "UPDATE cully_entries e SET "+strings.Join(assignments, ",")+" WHERE owner_subject=$1 AND id=$2::uuid RETURNING "+record, args...)
		out.Entry = e
		return out, err
	case "recent":
		v := r.Recent
		where, args := filters(owner, v.ProjectURL, v.SessionRef, v.Section, v.Category, v.EntryType, v.Since)
		if v.Until != "" {
			until, _ := time.Parse(time.RFC3339, v.Until)
			args = append(args, until)
			where += fmt.Sprintf(" AND occurred_at < $%d", len(args))
		}
		args = append(args, v.Limit)
		result, err := rows(ctx, tx, "SELECT "+record+" FROM cully_entries e WHERE "+where+fmt.Sprintf(" ORDER BY occurred_at DESC,id LIMIT $%d", len(args)), args...)
		if err == nil {
			result.Since, result.Until = v.Since, v.Until
		}
		return result, err
	case "search":
		v := r.Search
		where, args := filters(owner, v.ProjectURL, v.SessionRef, v.Section, v.Category, v.EntryType, v.Since)
		args = append(args, v.Query, v.Limit)
		q, count := len(args)-1, len(args)
		sql := fmt.Sprintf(`SELECT %s FROM cully_entries e WHERE %s AND search_vector @@ websearch_to_tsquery('english',$%d) ORDER BY ts_rank_cd(search_vector,websearch_to_tsquery('english',$%d)) DESC,e.occurred_at DESC,e.id LIMIT $%d`, record, where, q, q, count)
		return rows(ctx, tx, sql, args...)
	case "projects":
		v := r.Projects
		rs, err := tx.Query(ctx, `SELECT project_url,section,count(*),max(occurred_at) FROM cully_entries WHERE owner_subject=$1 AND project_url IS NOT NULL AND ($2::text IS NULL OR section=$2) GROUP BY project_url,section ORDER BY max(occurred_at) DESC,project_url LIMIT $3`, owner, v.Section, v.Limit)
		if err != nil {
			return out, err
		}
		defer rs.Close()
		out.Projects = []memory.Project{}
		for rs.Next() {
			var p memory.Project
			if err = rs.Scan(&p.ProjectURL, &p.Section, &p.EntryCount, &p.LastActivity); err != nil {
				return out, err
			}
			p.LastActivity = p.LastActivity.In(memory.IST)
			out.Projects = append(out.Projects, p)
		}
		return out, rs.Err()
	}
	return out, memory.ErrInvalid
}
func filters(owner string, project, session, section, category, kind *string, since string) (string, []any) {
	args := []any{owner}
	parts := []string{"owner_subject=$1"}
	for _, f := range []struct {
		name  string
		value *string
	}{{"project_url", project}, {"session_ref", session}, {"section", section}, {"category", category}, {"entry_type", kind}} {
		if f.value != nil {
			args = append(args, *f.value)
			parts = append(parts, fmt.Sprintf("%s=$%d", f.name, len(args)))
		}
	}
	if since != "" {
		t, _ := time.Parse(time.RFC3339, since)
		args = append(args, t)
		parts = append(parts, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	return strings.Join(parts, " AND "), args
}
func rows(ctx context.Context, tx pgx.Tx, sql string, args ...any) (memory.Result, error) {
	out := memory.Result{Entries: []memory.Entry{}}
	rs, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return out, err
	}
	defer rs.Close()
	for rs.Next() {
		var data []byte
		if err = rs.Scan(&data); err != nil {
			return out, err
		}
		e, err := decode(data)
		if err != nil {
			return out, err
		}
		out.Entries = append(out.Entries, *e)
	}
	return out, rs.Err()
}

const sessionRecord = "to_jsonb(s) - 'owner_subject' || jsonb_build_object('task', t.summary)"

func loadSession(ctx context.Context, tx pgx.Tx, owner, ref string) (*memory.Session, error) {
	var data []byte
	err := tx.QueryRow(ctx, "SELECT "+sessionRecord+" FROM cully_sessions s LEFT JOIN cully_entries t ON t.id=s.task_id AND t.owner_subject=s.owner_subject WHERE s.owner_subject=$1 AND s.session_ref=$2", owner, ref).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sess memory.Session
	if err = json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	sess.StartedAt, sess.LastSeenAt = sess.StartedAt.In(memory.IST), sess.LastSeenAt.In(memory.IST)
	return &sess, nil
}

// lockSession serializes writers of one session and the task entry it links.
func lockSession(ctx context.Context, tx pgx.Tx, owner, sessionRef string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, hashtextextended($2, 0)))`, owner, sessionRef)
	return err
}

// session upserts one session. A new task name creates a task entry and links
// it; the same task name again leaves the existing entry in place. The
// advisory lock serializes concurrent callers for one session so two new task
// names cannot both insert and leave an orphan behind.
func (s *Store) session(ctx context.Context, tx pgx.Tx, owner string, v *memory.SessionInput) (memory.Result, error) {
	out := memory.Result{}
	if err := lockSession(ctx, tx, owner, v.SessionRef); err != nil {
		return out, err
	}
	var existingSection string
	err := tx.QueryRow(ctx, "SELECT section FROM cully_sessions WHERE owner_subject=$1 AND session_ref=$2", owner, v.SessionRef).Scan(&existingSection)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if err == nil && existingSection != v.Section {
		return out, fmt.Errorf("%w: session section cannot change", memory.ErrInvalid)
	}
	var taskID *string
	if v.Task != nil && *v.Task != "" {
		// Compare with the session's latest task record, not the current link,
		// so repeating a name after clear_task relinks it instead of
		// inserting a duplicate.
		var latestID, latest *string
		err = tx.QueryRow(ctx, "SELECT id::text, summary FROM cully_entries WHERE owner_subject=$1 AND session_ref=$2 AND entry_type='task' ORDER BY created_at DESC, id DESC LIMIT 1", owner, v.SessionRef).Scan(&latestID, &latest)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if latest != nil && *latest == *v.Task {
			taskID = latestID
		} else {
			e, err := one(ctx, tx, `INSERT INTO cully_entries AS e (id,owner_subject,section,project_url,session_ref,entry_type,summary,assistant,tags,occurred_at) VALUES ($1::uuid,$2,$3,$4,$5,'task',$6,$7,'{}',now()) RETURNING `+record, uuid.NewString(), owner, v.Section, v.ProjectURL, v.SessionRef, *v.Task, v.Assistant)
			if err != nil {
				return out, err
			}
			out.Entry, taskID = e, &e.ID
		}
	}
	// ClearTask unlinks the session's task while keeping the task record.
	// An omitted task preserves the current link; only an explicit clear
	// nulls it.
	clearTask := v.ClearTask != nil && *v.ClearTask
	_, err = tx.Exec(ctx, `INSERT INTO cully_sessions AS s (owner_subject,session_ref,section,project_url,assistant,branch,task_id) VALUES ($1,$2,$3,$4,$5,$6,$7::uuid)
ON CONFLICT (owner_subject,session_ref) DO UPDATE SET project_url=COALESCE(EXCLUDED.project_url,s.project_url), branch=COALESCE(EXCLUDED.branch,s.branch), task_id=CASE WHEN $8::boolean THEN NULL ELSE COALESCE(EXCLUDED.task_id,s.task_id) END, assistant=EXCLUDED.assistant, last_seen_at=now()`,
		owner, v.SessionRef, v.Section, v.ProjectURL, v.Assistant, v.Branch, taskID, clearTask)
	if err != nil {
		return out, err
	}
	out.Session, err = loadSession(ctx, tx, owner, v.SessionRef)
	return out, err
}
