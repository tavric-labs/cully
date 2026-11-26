-- Named agents a user keeps in Cully: a role, instructions, tools, a folder and
-- short notes the agent keeps between runs. The definition is stored as the
-- JSON the browser UI writes to disk, so an agent follows the user across
-- machines. Rows are private to the owner, like every other Cully record; no
-- transcripts, commands or credentials are stored here.
CREATE TABLE cully_agents (
  owner_subject text NOT NULL,
  agent_id text NOT NULL CHECK (agent_id ~ '^[a-z0-9]{4,32}$'),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
  scope text NOT NULL CHECK (scope IN ('project', 'global')),
  approved boolean NOT NULL DEFAULT false,
  definition jsonb NOT NULL CHECK (octet_length(definition::text) <= 16000),
  notes text NOT NULL DEFAULT '' CHECK (length(notes) <= 2200),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_subject, agent_id)
);
CREATE UNIQUE INDEX cully_agents_owner_name_idx ON cully_agents(owner_subject, lower(name));
