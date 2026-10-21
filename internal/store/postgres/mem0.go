package postgres

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mcp-runtime/cully/internal/mem0"
	"github.com/mcp-runtime/cully/internal/memory"
)

func (s *Store) Recall(ctx context.Context, owner string, input memory.SearchInput) (memory.Result, error) {
	out := memory.Result{Entries: []memory.Entry{}}
	if s.Mem0 == nil {
		return out, memory.ErrUnavailable
	}
	hits, err := s.Mem0.Search(ctx, owner, input)
	if err != nil {
		return out, memory.ErrUnavailable
	}
	seen := map[string]bool{}
	for _, h := range hits {
		// Only return the caller's live source records, never raw Mem0 facts.
		if h.UserID != mem0.UserID(owner) || h.RunID == "" || seen[h.RunID] {
			continue
		}
		seen[h.RunID] = true
		var data []byte
		err = s.Pool.QueryRow(ctx, "SELECT "+record+" FROM cully_entries e WHERE owner_subject=$1 AND id::text=$2", owner, h.RunID).Scan(&data)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, memory.ErrUnavailable
		}
		e, err := decode(data)
		if err != nil {
			return out, memory.ErrUnavailable
		}
		if input.SessionRef != nil && (e.SessionRef == nil || *e.SessionRef != *input.SessionRef) {
			continue
		}
		if input.EntryType != nil && e.EntryType != *input.EntryType {
			continue
		}
		if input.Since != "" {
			since, _ := time.Parse(time.RFC3339, input.Since)
			if e.OccurredAt.Before(since) {
				continue
			}
		}
		stamp, ok := h.Metadata["cully_updated_at"].(string)
		if !ok || stamp != e.UpdatedAt.Format(time.RFC3339Nano) {
			continue
		}
		out.Entries = append(out.Entries, *e)
	}
	return out, nil
}

// RunMem0Worker serializes projections through a durable transactional outbox.
// Failed jobs remain in PostgreSQL without blocking normal log calls.
func (s *Store) RunMem0Worker(ctx context.Context) {
	if s.Mem0 == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.processMem0Job(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("Mem0 projection retry pending")
			}
		}
	}
}
func (s *Store) processMem0Job(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var owner, id string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT owner_subject,entry_id::text,attempts FROM cully_mem0_jobs WHERE next_attempt<=now() ORDER BY next_attempt FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&owner, &id, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	entry, err := one(ctx, tx, "SELECT "+record+" FROM cully_entries e WHERE owner_subject=$1 AND id=$2::uuid", owner, id)
	if err != nil {
		return err
	}
	if err = s.Mem0.Reconcile(ctx, owner, id, entry); err != nil {
		delay := time.Second * time.Duration(1<<min(attempts+1, 12))
		_, err = tx.Exec(ctx, `UPDATE cully_mem0_jobs SET attempts=attempts+1,next_attempt=now()+make_interval(secs => $3) WHERE owner_subject=$1 AND entry_id=$2::uuid`, owner, id, delay.Seconds())
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM cully_mem0_jobs WHERE owner_subject=$1 AND entry_id=$2::uuid`, owner, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) BackfillMem0(ctx context.Context) error {
	if s.Mem0 == nil {
		return memory.ErrUnavailable
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO cully_mem0_jobs(owner_subject,entry_id) SELECT owner_subject,id FROM cully_entries ON CONFLICT(owner_subject,entry_id) DO UPDATE SET generation=cully_mem0_jobs.generation+1,attempts=0,next_attempt=now()`)
	return err
}
