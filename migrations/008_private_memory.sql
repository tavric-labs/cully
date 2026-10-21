-- Keep factual owner/session/task provenance and useful note content only.
-- Dropping metadata also drops its dependent classification indexes.
ALTER TABLE cully_entries
  DROP COLUMN project_url,
  DROP COLUMN section,
  DROP COLUMN category,
  DROP COLUMN theme,
  DROP COLUMN tags;
ALTER TABLE cully_sessions DROP COLUMN project_url, DROP COLUMN section;
ALTER TABLE cully_sessions ADD COLUMN agent_session_id text;
CREATE INDEX cully_sessions_owner_seen_idx ON cully_sessions(owner_subject, last_seen_at DESC);

-- Replace semantic metadata using the same authenticated owner namespace.
INSERT INTO cully_mem0_jobs(owner_subject, entry_id)
SELECT owner_subject, id FROM cully_entries
ON CONFLICT(owner_subject, entry_id) DO UPDATE
SET generation=cully_mem0_jobs.generation+1, attempts=0, next_attempt=now();
