package cully

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go, so the single binary still builds everywhere
)

// Local mode keeps everything the browser UI remembers in one SQLite database,
// cully.db in the Cully folder, instead of a pile of JSON files: sessions,
// agents and their notes, evals, research, usage, settings. It is the same
// information the Cully MCP keeps in Postgres and Mem0, held on this machine.
// It is simpler to run and needs no server, but it is less efficient than the
// MCP: there is no semantic recall, so an agent loads recent and matching notes
// rather than only the relevant ones, which can cost more tokens. The page says
// so, and says how to set up the MCP.
//
// The store is a small document store: named collections of JSON documents and
// append-only logs. Writes are serialised through one connection, so a few
// goroutines writing at once never fight over the file.

type uiStore struct {
	db   *sql.DB
	path string
	fts  bool // keyword search is available
}

var (
	uiStoresMu sync.Mutex
	uiStores   = map[string]*uiStore{}
)

const uiStoreSchema = `
CREATE TABLE IF NOT EXISTS docs (ns TEXT NOT NULL, id TEXT NOT NULL, body TEXT NOT NULL, updated INTEGER NOT NULL, PRIMARY KEY (ns, id)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS log (seq INTEGER PRIMARY KEY AUTOINCREMENT, ns TEXT NOT NULL, at INTEGER NOT NULL, body TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS log_ns ON log (ns, seq);
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

func uiStorePath() string { return filepath.Join(cullyDir(), "cully.db") }

// uiDB returns the database in the current Cully folder, creating it (and
// importing any JSON files an earlier version left) the first time.
func uiDB() *uiStore {
	path := uiStorePath()
	uiStoresMu.Lock()
	defer uiStoresMu.Unlock()
	if s, ok := uiStores[path]; ok {
		return s
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil { // private from the start
		_ = f.Close()
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return &uiStore{path: path}
	}
	db.SetMaxOpenConns(1)
	s := &uiStore{db: db, path: path}
	if _, err := db.Exec(uiStoreSchema); err != nil {
		_ = db.Close()
		return &uiStore{path: path}
	}
	// Keyword search for local recall. A build without FTS5 simply has no recall.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS recall USING fts5(id UNINDEXED, text, tokenize='porter unicode61')`); err == nil {
		s.fts = true
	}
	uiStores[path] = s
	s.importLegacy()
	return s
}

// uiCloseStores closes every open database. Tests use it so temporary folders can be removed.
func uiCloseStores() {
	uiStoresMu.Lock()
	defer uiStoresMu.Unlock()
	for p, s := range uiStores {
		if s.db != nil {
			_ = s.db.Close()
		}
		delete(uiStores, p)
	}
}

func (s *uiStore) ok() bool { return s != nil && s.db != nil }

func (s *uiStore) put(ns, id string, v any) {
	if !s.ok() {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`INSERT INTO docs(ns,id,body,updated) VALUES(?,?,?,?) ON CONFLICT(ns,id) DO UPDATE SET body=excluded.body, updated=excluded.updated`, ns, id, string(b), time.Now().UnixNano())
}

func (s *uiStore) get(ns, id string, v any) bool {
	if !s.ok() {
		return false
	}
	var body string
	if s.db.QueryRow(`SELECT body FROM docs WHERE ns=? AND id=?`, ns, id).Scan(&body) != nil {
		return false
	}
	return json.Unmarshal([]byte(body), v) == nil
}

func (s *uiStore) del(ns, id string) {
	if s.ok() {
		_, _ = s.db.Exec(`DELETE FROM docs WHERE ns=? AND id=?`, ns, id)
	}
}

// all returns every document of a collection, oldest first.
func (s *uiStore) all(ns string) []json.RawMessage {
	var out []json.RawMessage
	if !s.ok() {
		return out
	}
	rows, err := s.db.Query(`SELECT body FROM docs WHERE ns=? ORDER BY updated, id`, ns)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		if rows.Scan(&b) == nil {
			out = append(out, json.RawMessage(b))
		}
	}
	return out
}

// logAdd appends to a log and keeps only the newest keep rows.
func (s *uiStore) logAdd(ns string, v any, keep int) {
	if !s.ok() {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`INSERT INTO log(ns,at,body) VALUES(?,?,?)`, ns, time.Now().UnixNano(), string(b))
	if keep > 0 {
		_, _ = s.db.Exec(`DELETE FROM log WHERE ns=? AND seq <= (SELECT seq FROM log WHERE ns=? ORDER BY seq DESC LIMIT 1 OFFSET ?)`, ns, ns, keep)
	}
}

// logAll returns a log, oldest first.
func (s *uiStore) logAll(ns string) []json.RawMessage {
	var out []json.RawMessage
	if !s.ok() {
		return out
	}
	rows, err := s.db.Query(`SELECT body FROM log WHERE ns=? ORDER BY seq`, ns)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		if rows.Scan(&b) == nil {
			out = append(out, json.RawMessage(b))
		}
	}
	return out
}

func (s *uiStore) metaGet(k string) string {
	var v string
	if s.ok() {
		_ = s.db.QueryRow(`SELECT v FROM meta WHERE k=?`, k).Scan(&v)
	}
	return v
}

func (s *uiStore) metaSet(k, v string) {
	if s.ok() {
		_, _ = s.db.Exec(`INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	}
}

// importLegacy moves what earlier versions kept in JSON files into the database,
// once, then sets each file aside with an .imported suffix. A file that cannot be
// read is left alone.
func (s *uiStore) importLegacy() {
	if s.metaGet("imported") != "" {
		return
	}
	dir := filepath.Dir(s.path)
	setAside := func(p string) { _ = os.Rename(p, p+".imported") }
	readJSON := func(name string, v any) (string, bool) {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil || json.Unmarshal(b, v) != nil {
			return p, false
		}
		return p, true
	}
	if p, ok := readJSON("session-index.json", &struct {
		Meta     map[string]any             `json:"-"`
		Sessions map[string]json.RawMessage `json:"sessions"`
	}{}); ok {
		var raw map[string]json.RawMessage
		b, _ := os.ReadFile(p)
		_ = json.Unmarshal(b, &raw)
		var sessions map[string]json.RawMessage
		_ = json.Unmarshal(raw["sessions"], &sessions)
		for id, body := range sessions {
			s.put("session", id, json.RawMessage(body))
		}
		delete(raw, "sessions")
		s.put("index", "meta", raw)
		setAside(p)
	}
	for file, ns := range map[string][2]string{"coach.json": {"coach", "latest"}, "rules-proposal.json": {"rules", "proposal"}, "ui-settings.json": {"settings", "policy"}, "harness-init.json": {"cache", "init"}, "mcp-catalog.json": {"cache", "catalog"}} {
		var v json.RawMessage
		if p, ok := readJSON(file, &v); ok {
			s.put(ns[0], ns[1], v)
			setAside(p)
		}
	}
	var sugg []json.RawMessage
	if p, ok := readJSON("agent-suggestions.json", &sugg); ok {
		for i, x := range sugg {
			var h struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(x, &h)
			if h.ID == "" {
				h.ID = uiItoa(i)
			}
			s.put("agentsuggest", h.ID, x)
		}
		setAside(p)
	}
	for file, ns := range map[string]string{"research-log.jsonl": "research", "usage-log.jsonl": "usage"} {
		p := filepath.Join(dir, file)
		if b, err := os.ReadFile(p); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.TrimSpace(line) != "" && json.Valid([]byte(line)) {
					s.logAdd(ns, json.RawMessage(line), 0)
				}
			}
			setAside(p)
		}
	}
	// agents: one folder each in the last layout, one file in the first
	if dirs, err := os.ReadDir(filepath.Join(dir, "agents")); err == nil {
		for _, d := range dirs {
			base := filepath.Join(dir, "agents", d.Name())
			b, err := os.ReadFile(filepath.Join(base, "agent.json"))
			if err != nil {
				continue
			}
			var m map[string]any
			if json.Unmarshal(b, &m) != nil {
				continue
			}
			if nb, err := os.ReadFile(filepath.Join(base, "notes.md")); err == nil {
				m["memory"] = uiNotesBody(string(nb))
			}
			if rb, err := os.ReadFile(filepath.Join(base, "runs.json")); err == nil {
				var runs any
				if json.Unmarshal(rb, &runs) == nil {
					m["runs"] = runs
				}
			}
			id, _ := m["id"].(string)
			if id == "" {
				continue
			}
			s.put("agent", id, m)
			if eb, err := os.ReadFile(filepath.Join(base, "evals.json")); err == nil && json.Valid(eb) {
				s.put("agenteval", id, json.RawMessage(eb))
			}
			for _, f := range []string{"agent.json", "notes.md", "runs.json", "evals.json"} {
				setAside(filepath.Join(base, f))
			}
		}
	}
	var legacy []json.RawMessage
	if p, ok := readJSON("identities.json", &legacy); ok {
		for _, x := range legacy {
			var h struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(x, &h) == nil && h.ID != "" {
				var m map[string]any
				_ = json.Unmarshal(x, &m)
				m["approved"], m["scope"] = true, "project"
				s.put("agent", h.ID, m)
			}
		}
		setAside(p)
	}
	if files, err := os.ReadDir(filepath.Join(dir, "evals")); err == nil {
		for _, f := range files {
			p := filepath.Join(dir, "evals", f.Name())
			b, err := os.ReadFile(p)
			if err != nil || !json.Valid(b) {
				continue
			}
			if f.Name() == "runs.json" {
				var runs []json.RawMessage
				_ = json.Unmarshal(b, &runs)
				for _, r := range runs {
					var h struct {
						ID string `json:"id"`
					}
					_ = json.Unmarshal(r, &h)
					s.put("evalrun", h.ID, r)
				}
			} else if strings.HasSuffix(f.Name(), ".json") {
				s.put("eval", strings.TrimSuffix(f.Name(), ".json"), json.RawMessage(b))
			}
			setAside(p)
		}
	}
	s.metaSet("imported", time.Now().UTC().Format(time.RFC3339))
}
