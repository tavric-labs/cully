-- Optimization notes share the owner-scoped source and Mem0 outbox. Only
-- concise evidence is stored; never transcripts, commands or credentials.
ALTER TABLE cully_entries ADD COLUMN optimization jsonb;
ALTER TABLE cully_entries DROP CONSTRAINT cully_entries_entry_type_check;
ALTER TABLE cully_entries ADD CONSTRAINT cully_entries_entry_type_check
  CHECK (entry_type IN ('work','issue','learning','decision','task','optimization')) NOT VALID;
ALTER TABLE cully_entries ADD CONSTRAINT cully_entries_optimization_check
  CHECK ((entry_type = 'optimization') = (optimization IS NOT NULL)) NOT VALID;
CREATE UNIQUE INDEX cully_entries_owner_pattern_idx
  ON cully_entries(owner_subject, (optimization->>'pattern_key'))
  WHERE entry_type = 'optimization';
CREATE TABLE cully_optimization_observations (
  entry_id uuid NOT NULL REFERENCES cully_entries(id) ON DELETE CASCADE,
  session_ref text NOT NULL,
  window_id text NOT NULL,
  agent_session_id text,
  summary text NOT NULL,
  observed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(entry_id, session_ref, window_id)
);
