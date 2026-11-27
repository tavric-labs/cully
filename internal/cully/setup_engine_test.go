package cully

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupFixture isolates every home the setup code touches and returns a
// manager that knows one project (a Makefile, go.mod and conventional commits).
func setupFixture(t *testing.T) (*uiManager, string) {
	t.Helper()
	repo := uiNewTestRepo(t) // CLAUDE_CONFIG_DIR, CODEX_HOME and CURSOR_HOME in temp folders
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, "cursor"))
	for _, d := range []string{ConfigDir(), CodexConfigDir(), CursorConfigDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saved := uiHarnessVersion
	uiHarnessVersion = func(agent string) string { return "1.0-" + agent }
	t.Cleanup(func() { uiHarnessVersion = saved })
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Makefile", "test:\n\tgo test ./...\nlint:\n\tgo vet ./...\n")
	write("go.mod", "module example.com/x\n")
	write(".env", "NOT_SHOWN=1\n")
	for _, subj := range []string{"feat: one", "fix: two", "docs: three", "feat: four", "fix: five", "chore: six"} {
		if out, err := exec.Command("git", "-C", repo, "commit", "-q", "--allow-empty", "-m", subj).CombinedOutput(); err != nil {
			t.Fatalf("git: %v\n%s", err, out)
		}
	}
	testRepos = []string{repo} // for the wizard tests, whose engine is built inside RunSetupWizard
	m := newUIManager(uiConfig{Repos: []string{repo}, Advisor: func(string, string, bool, string) (string, error) {
		t.Fatal("the advisor must not be called unless the user asks")
		return "", nil
	}})
	return m, repo
}

func strp(s string) *string { return &s }

func questionByID(t *testing.T, iv setupInterview, id string) setupQuestion {
	t.Helper()
	for _, q := range iv.Questions {
		if q.ID == id {
			return q
		}
	}
	t.Fatalf("no question %s", id)
	return setupQuestion{}
}

func TestSetupInterviewIsPrefilledFromRepoFactsAndRulesFile(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	iv := e.Interview()
	if n := len(iv.Questions); n < 8 || n > 12 {
		t.Fatalf("%d questions", n)
	}
	q := questionByID(t, iv, "test_cmd")
	if q.Suggested != "make test" || !strings.Contains(q.Evidence, "`make test`") || !strings.Contains(q.Evidence, "1 of your 1 repos") {
		t.Fatalf("test_cmd = %+v", q)
	}
	if l := questionByID(t, iv, "lint_cmd"); l.Suggested != "make lint" {
		t.Fatalf("lint_cmd = %+v", l)
	}
	if c := questionByID(t, iv, "commit_style"); c.Suggested != setupChoiceCommit[0] || c.Evidence == "" || c.Kind != "choice" || len(c.Choices) == 0 {
		t.Fatalf("commit_style = %+v", c)
	}
	if s := questionByID(t, iv, "secrets"); !strings.Contains(s.Evidence, ".env") {
		t.Fatalf("secrets = %+v", s)
	}
	if l := questionByID(t, iv, "languages"); l.Suggested != "Go" {
		t.Fatalf("languages = %+v", l)
	}
	for _, q := range iv.Questions {
		if strings.Contains(q.Suggested+q.Evidence, "NOT_SHOWN") {
			t.Fatalf(".env contents leaked into %s", q.ID)
		}
	}
	// An existing rules file wins over what Cully derives.
	file := setupBuildRules("", map[string]string{"test_cmd": "just check"})
	if err := os.MkdirAll(filepath.Dir(workflowSkillPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowSkillPath(), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	q = questionByID(t, e.Interview(), "test_cmd")
	if q.Suggested != "just check" || q.Evidence != "From your current rules file." {
		t.Fatalf("prefill from file = %+v", q)
	}
}

func TestSetupProposalKeepsUserTextAndSkippedAnswersProduceNothing(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	user := "---\nname: dev-workflow-rules\ndescription: Mine.\n---\n# My notes\nKeep this line.\n"
	if err := os.MkdirAll(filepath.Dir(workflowSkillPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowSkillPath(), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := e.Propose(map[string]*string{"test_cmd": strp("make test"), "never_do": strp("push\ndeploy"), "secrets": nil, "review": nil})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# My notes\nKeep this line.", setupBeginMark, setupEndMark, "- Test command: make test", "- Never do without asking: push; deploy"} {
		if !strings.Contains(p.After, want) {
			t.Fatalf("after lacks %q:\n%s", want, p.After)
		}
	}
	if strings.Contains(p.After, "Secrets") || strings.Contains(p.After, "- Review:") {
		t.Fatalf("skipped questions produced text:\n%s", p.After)
	}
	if b, _ := os.ReadFile(workflowSkillPath()); string(b) != user {
		t.Fatal("proposing wrote the file")
	}
	adds := 0
	for _, d := range p.Diff {
		if d.T == "add" {
			adds++
		}
		if d.T == "del" {
			t.Fatalf("the proposal deleted a line: %+v", d)
		}
	}
	if adds == 0 || p.Summary == "" || p.ID == "" || p.Before != user {
		t.Fatalf("proposal = %+v", p)
	}
	// A later change replaces only the managed block, and an id not listed stays as it was.
	if err := os.WriteFile(workflowSkillPath(), []byte(p.After), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := e.Propose(map[string]*string{"test_cmd": strp("go test ./...")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p2.After, "- Test command: go test ./...") || strings.Contains(p2.After, "- Test command: make test") ||
		!strings.Contains(p2.After, "- Never do without asking: push; deploy") || !strings.Contains(p2.After, "Keep this line.") ||
		strings.Count(p2.After, setupBeginMark) != 1 {
		t.Fatalf("second proposal:\n%s", p2.After)
	}
}

func TestSetupRefusesSecretsInAnswers(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	if _, err := e.Propose(map[string]*string{"secrets": strp("password=hunter2hunter2")}); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.Propose(map[string]*string{"nope": strp("x")}); err == nil {
		t.Fatal("accepted an unknown question")
	}
	if _, err := e.Propose(map[string]*string{"test_cmd": strp(strings.Repeat("x", 500))}); err == nil {
		t.Fatal("accepted a very long answer")
	}
}

func approveWith(t *testing.T, e *setupEngine, answers map[string]*string) setupApproved {
	t.Helper()
	p, err := e.Propose(answers)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Approve(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSetupApproveWritesSourceLinksHarnessesAndRollsBack(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	res := approveWith(t, e, map[string]*string{"test_cmd": strp("make test")})
	if !res.OK || res.Path != workflowSkillPath() || !strings.HasSuffix(res.Path, filepath.Join(".cully", "skills", "dev-workflow-rules", "SKILL.md")) {
		t.Fatalf("res = %+v", res)
	}
	if got := strings.Join(res.Linked, ","); got != "claude,codex,cursor" {
		t.Fatalf("linked = %s", got)
	}
	st, err := os.Stat(res.Path)
	if err != nil || st.Mode().Perm() != 0o644 {
		t.Fatalf("source file: %v %v", st, err)
	}
	first, _ := os.ReadFile(res.Path)
	if !strings.Contains(string(first), "- Test command: make test") {
		t.Fatalf("source = %s", first)
	}
	for _, h := range setupHarnessHomes() {
		fi, err := os.Lstat(setupLinkDir(h))
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink: %v", h.ID, err)
		}
		if b, _ := os.ReadFile(filepath.Join(setupLinkDir(h), "SKILL.md")); string(b) != string(first) {
			t.Fatalf("%s sees %q", h.ID, b)
		}
	}
	state := e.State()
	for _, h := range state.Harnesses {
		if !h.Installed || !h.Linked || h.Version == "" || !strings.HasSuffix(h.SkillPath, "SKILL.md") {
			t.Fatalf("harness = %+v", h)
		}
	}
	if !state.Rules.Exists || state.Rules.Path != res.Path || state.Rules.Lines < 5 || state.Rules.Updated == "" {
		t.Fatalf("rules = %+v", state.Rules)
	}
	// Approving again is idempotent for the links and produces a second ledger entry.
	approveWith(t, e, map[string]*string{"lint_cmd": strp("make lint")})
	second, _ := os.ReadFile(res.Path)
	if !strings.Contains(string(second), "- Lint command: make lint") {
		t.Fatalf("second = %s", second)
	}
	entries := m.ledgerList("rule", 10)
	if len(entries) != 2 || entries[0].Source == "" || !strings.HasPrefix(entries[0].Source, "setup:") || !entries[0].CanRollback {
		t.Fatalf("ledger = %+v", entries)
	}
	if _, err := m.ledgerRollback(entries[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(res.Path); string(b) != string(first) {
		t.Fatalf("rollback did not restore the earlier text:\n%s", b)
	}
	// Rolling the first one back as well removes the file that did not exist before it.
	if _, err := m.ledgerRollback(entries[1].ID, false); err != nil {
		t.Fatal(err)
	}
	if fileExists(res.Path) {
		t.Fatal("the first rollback left the file behind")
	}
}

func TestSetupApproveNeverOverwritesARealFolderWithoutABackup(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	mine := filepath.Join(ConfigDir(), "skills", uiRulesName)
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("MY OWN SKILL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "extra.md"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWith(t, e, map[string]*string{"test_cmd": strp("make test")})
	baks, _ := filepath.Glob(mine + ".bak.*")
	if len(baks) != 1 {
		t.Fatalf("backups = %v", baks)
	}
	if b, _ := os.ReadFile(filepath.Join(baks[0], "SKILL.md")); string(b) != "MY OWN SKILL\n" {
		t.Fatalf("backup = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(baks[0], "extra.md")); string(b) != "keep" {
		t.Fatal("the user's other file was lost")
	}
	if fi, err := os.Lstat(mine); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("not linked after the backup: %v", err)
	}
	// The user's own text survives in the rules because it sits outside the managed block.
	if b, _ := os.ReadFile(workflowSkillPath()); !strings.Contains(string(b), "MY OWN SKILL") {
		t.Fatalf("user text lost:\n%s", b)
	}
}

func TestSetupApproveRefusesAStaleOrEmptyProposal(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	p, err := e.Propose(map[string]*string{"test_cmd": strp("make test")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(workflowSkillPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowSkillPath(), []byte("changed meanwhile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Approve(p.ID); err == nil {
		t.Fatal("approved a proposal made against an older file")
	}
	if _, err := e.Approve("sp-missing"); err == nil {
		t.Fatal("approved a proposal that does not exist")
	}
	if err := e.Discard(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.Discard(p.ID); err == nil {
		t.Fatal("discarded twice")
	}
}

func TestSetupModeValidationAndRegistration(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	for _, bad := range []string{"", "http://mcp.example.com/mcp", "https://user:pw@mcp.example.com/mcp", "ftp://x/y", "https://mcp.example.com/mcp?token=abc", "not a url"} {
		if _, err := e.SetMode("mcp", bad); err == nil {
			t.Fatalf("accepted MCP URL %q", bad)
		}
	}
	if _, err := e.SetMode("other", ""); err == nil {
		t.Fatal("accepted an unknown mode")
	}
	if e.State().Mode != "" {
		t.Fatal("a refused mode was stored")
	}
	st, err := e.SetMode("local", "")
	if err != nil || st.Mode != "local" {
		t.Fatalf("local: %+v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(ConfigDir(), ".claude.json")); strings.Contains(string(b), "mcpServers") {
		t.Fatal("local mode registered an MCP server")
	}
	st, err = e.SetMode("mcp", "https://mcp.example.com/mcp")
	if err != nil || st.Mode != "mcp" {
		t.Fatalf("mcp: %+v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(ConfigDir(), ".claude.json")); !strings.Contains(string(b), "https://mcp.example.com/mcp") {
		t.Fatalf("claude config lacks the URL: %s", b)
	}
	if _, err := e.SetMode("mcp", "https://other.example.com/mcp"); err == nil {
		t.Fatal("replaced an existing Cully server entry without being asked")
	}
	m2, _ := setupFixture(t)
	if _, err := newSetupEngine(m2).SetMode("mcp", "http://localhost:7777/mcp"); err != nil {
		t.Fatalf("localhost http refused: %v", err)
	}
	if st.Modes["local"] != SetupModeLocalText || st.Modes["mcp"] != SetupModeMCPText {
		t.Fatalf("modes = %+v", st.Modes)
	}
	if !strings.Contains(SetupModeMCPText, "Postgres and Mem0") || !strings.Contains(SetupModeLocalText, "One machine") {
		t.Fatal("mode text lost its meaning")
	}
}

func TestSetupDoneNeedsModeAndRulesOrAnExplicitSkip(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	if e.State().Done || SetupDone() {
		t.Fatal("done before anything was chosen")
	}
	if _, err := e.SetMode("local", ""); err != nil {
		t.Fatal(err)
	}
	if e.State().Done {
		t.Fatal("done with a mode but no rules and no skip")
	}
	approveWith(t, e, map[string]*string{"test_cmd": strp("make test")})
	if !e.State().Done || !SetupDone() {
		t.Fatal("not done after a mode and rules")
	}
	m2, _ := setupFixture(t)
	e2 := newSetupEngine(m2)
	if st := e2.Complete(false); !st.Done {
		t.Fatal("complete did not finish setup")
	}
	m3, _ := setupFixture(t)
	e3 := newSetupEngine(m3)
	_, _ = e3.SetMode("local", "")
	uiDB().metaSet(metaSetupSkipped, "1")
	if !e3.State().Done {
		t.Fatal("a deliberate skip did not finish setup")
	}
}

// seedSessions puts n analysed sessions into the index without any model.
func seedSessions(t *testing.T, m *uiManager, cwd string, n int, task string) {
	t.Helper()
	m.idxMu.Lock()
	m.loadIndexLocked()
	for i := 0; i < n; i++ {
		id := "seed-" + uiItoa(i)
		at := time.Now().Add(-time.Duration(i) * 20 * time.Hour)
		e := &uiIndexed{ID: id, Agent: "claude", Cwd: cwd, Name: filepath.Base(cwd), Started: at.Add(-30 * time.Minute), Updated: at, Mod: at.UnixNano(),
			Title: "Fix parser " + uiItoa(i), Task: task, Source: "ai", Completed: "yes", FactsV: uiFactsVersion, Analyzed: at}
		e.Turns, e.ActiveSec, e.Unverified = 4, 1800, i%2 == 0
		m.index.Sessions[id] = e
	}
	m.saveIndexLocked()
	m.idxMu.Unlock()
}

func TestSetupSuggestionsAreStrictAndHonest(t *testing.T) {
	m, repo := setupFixture(t)
	e := newSetupEngine(m)
	seedSessions(t, m, repo, 19, "fix the flaky parser tests")
	sg := e.Suggestions()
	if len(sg.Agents) != 0 || len(sg.Jobs) != 0 || !strings.Contains(sg.Note, "20") || sg.Agents == nil || sg.Jobs == nil {
		t.Fatalf("under the minimum: %+v", sg)
	}
	if sg.Basis.Sessions != 19 || sg.Basis.Hours != 9.5 {
		t.Fatalf("basis = %+v", sg.Basis)
	}
	b, _ := json.Marshal(sg)
	for _, k := range []string{`"basis"`, `"agents":[]`, `"jobs":[]`, `"note"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("json lacks %s: %s", k, b)
		}
	}
	seedSessions(t, m, repo, 24, "fix the flaky parser tests")
	sg = e.Suggestions()
	if len(sg.Agents) != 1 || sg.Note == "" {
		t.Fatalf("with 24 sessions: %+v", sg)
	}
	a := sg.Agents[0]
	if a.Count < 6 || a.EstMinutesPerWeek <= 0 || a.Folder != repo || len(a.Evidence) == 0 || a.Why == "" || a.Name == "" {
		t.Fatalf("agent = %+v", a)
	}
	if a.EstTokensPerRun != 0 {
		t.Fatalf("tokens were invented without a transcript: %+v", a)
	}
	if len(sg.Jobs) != 1 || !strings.Contains(sg.Jobs[0].Command, "make") || sg.Jobs[0].Schedule == "" || sg.Jobs[0].EstMinutesPerWeek <= 0 {
		t.Fatalf("jobs = %+v", sg.Jobs)
	}
}

func TestSetupStateCountsSessionsHoursAndTokens(t *testing.T) {
	m, repo := setupFixture(t)
	e := newSetupEngine(m)
	dir := t.TempDir()
	tr := filepath.Join(dir, "a.jsonl")
	lines := `{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":100,"cache_read_input_tokens":99999}}}
{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":100}}}
{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":1,"output_tokens":2}}}
`
	if err := os.WriteFile(tr, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(dir, "c.jsonl")
	cl := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":7}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"cached_input_tokens":50,"output_tokens":9}}}}
`
	if err := os.WriteFile(codex, []byte(cl), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSessions(t, m, repo, 2, "x")
	m.idxMu.Lock()
	m.index.Sessions["seed-0"].File, m.index.Sessions["seed-0"].Agent = tr, "claude"
	m.index.Sessions["seed-1"].File, m.index.Sessions["seed-1"].Agent = codex, "codex"
	m.idxMu.Unlock()
	s := e.State().Sessions
	if s.Count != 2 || s.Hours != 1 || s.Days != 2 {
		t.Fatalf("sessions = %+v", s)
	}
	if want := int64(10+20+100+3) + int64(200-50+9); s.Tokens != want {
		t.Fatalf("tokens = %d, want %d", s.Tokens, want)
	}
	if u := s.ByHarness["claude"]; u.Count != 1 || u.Hours != 0.5 || u.Tokens != 133 {
		t.Fatalf("claude = %+v", u)
	}
	if _, ok := s.ByHarness["cursor"]; !ok {
		t.Fatal("byHarness lacks cursor")
	}
}

func TestSetupAnalyseRunsOnlyOnRequestAndSendsNoTranscripts(t *testing.T) {
	m, repo := setupFixture(t)
	calls, prompt := 0, ""
	m.cfg.Advisor = func(_ string, _ string, _ bool, p string) (string, error) {
		calls++
		prompt = p
		return `Here: {"test_cmd":"make test && make lint","review":"Open a PR please","never_do":"password=abcdefgh12345"}`, nil
	}
	e := newSetupEngine(m)
	_ = e.Interview()
	if calls != 0 {
		t.Fatal("the advisor ran without a request")
	}
	seedSessions(t, m, repo, 3, "fix tok-aaa1 leaks")
	iv, err := e.Analyse()
	if err != nil || calls != 1 {
		t.Fatalf("analyse: %v calls=%d", err, calls)
	}
	if strings.Contains(prompt, "tok-aaa1") {
		t.Fatalf("a token went to the advisor: %s", prompt)
	}
	if q := questionByID(t, iv, "test_cmd"); q.Suggested != "make test && make lint" || !strings.Contains(q.Evidence, "3 of your sessions") {
		t.Fatalf("test_cmd = %+v", q)
	}
	if q := questionByID(t, iv, "review"); q.Suggested != setupChoiceReview[0] {
		t.Fatalf("a reply outside the choices was accepted: %+v", q)
	}
	if q := questionByID(t, iv, "never_do"); strings.Contains(q.Suggested, "password") {
		t.Fatalf("a secret-looking suggestion was accepted: %+v", q)
	}
}

func TestSetupHTTPContract(t *testing.T) {
	srv, m, _ := uiTestServer(t, false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, "cursor"))
	_ = os.MkdirAll(CodexConfigDir(), 0o755)
	_ = m
	get := func(path string, out any) int {
		res := uiDo(t, "GET", srv.URL+path, nil, uiAuthed)
		if out != nil {
			_ = json.NewDecoder(res.Body).Decode(out)
		}
		return res.StatusCode
	}
	post := func(path string, body, out any) int {
		res := uiDo(t, "POST", srv.URL+path, body, uiAuthed)
		if out != nil {
			_ = json.NewDecoder(res.Body).Decode(out)
		}
		return res.StatusCode
	}
	if res := uiDo(t, "GET", srv.URL+"/api/setup/state", nil, nil); res.StatusCode != 401 {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	if res := uiDo(t, "POST", srv.URL+"/api/setup/mode", map[string]string{"mode": "local"}, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+uiTestToken) }); res.StatusCode != 403 {
		t.Fatalf("no header: %d", res.StatusCode)
	}
	var info map[string]any
	get("/api/info", &info)
	if v, ok := info["setupDone"]; !ok || v != false || info["repos"] == nil {
		t.Fatalf("info = %v", info)
	}
	var st setupState
	if c := get("/api/setup/state", &st); c != 200 || st.Done || len(st.Steps) != 4 || st.Steps[0].ID != "install" || st.Modes["mcp"] == "" || len(st.Harnesses) != 3 {
		t.Fatalf("state %d %+v", c, st)
	}
	if c := post("/api/setup/mode", map[string]string{"mode": "mcp", "mcpUrl": "http://evil.example.com/mcp"}, nil); c != 400 {
		t.Fatalf("bad mode url: %d", c)
	}
	if c := post("/api/setup/mode", map[string]string{"mode": "local"}, &st); c != 200 || st.Mode != "local" {
		t.Fatalf("mode: %d %+v", c, st)
	}
	var iv setupInterview
	if c := get("/api/setup/interview", &iv); c != 200 || len(iv.Questions) < 8 || iv.Answers == nil {
		t.Fatalf("interview %d %+v", c, iv)
	}
	var pr struct {
		Proposal setupProposal `json:"proposal"`
	}
	if c := post("/api/setup/interview", map[string]any{"answers": map[string]any{"test_cmd": "make test", "review": nil}}, &pr); c != 200 || pr.Proposal.ID == "" || !strings.Contains(pr.Proposal.After, "- Test command: make test") {
		t.Fatalf("propose %d %+v", c, pr)
	}
	if _, err := os.Stat(workflowSkillPath()); err == nil {
		t.Fatal("nothing may be written before approval")
	}
	var ap setupApproved
	if c := post("/api/setup/rules/approve", map[string]string{"id": pr.Proposal.ID}, &ap); c != 200 || !ap.OK || len(ap.Linked) == 0 {
		t.Fatalf("approve %d %+v", c, ap)
	}
	if c := post("/api/setup/rules/discard", map[string]string{"id": pr.Proposal.ID}, nil); c != 400 {
		t.Fatalf("discard after approve: %d", c)
	}
	get("/api/setup/state", &st)
	if !st.Done || !st.Rules.Exists {
		t.Fatalf("state after approve: %+v", st)
	}
	var sg setupSuggestions
	if c := get("/api/setup/suggestions", &sg); c != 200 || sg.Note == "" {
		t.Fatalf("suggestions %d %+v", c, sg)
	}
	if c := post("/api/setup/complete", nil, &st); c != 200 || !st.Done {
		t.Fatalf("complete %d", c)
	}
	get("/api/info", &info)
	if info["setupDone"] != true {
		t.Fatalf("info after setup = %v", info["setupDone"])
	}
	// Nothing the API returns carries a credential.
	b, _ := json.Marshal([]any{st, iv, sg, ap})
	if strings.Contains(string(b), "tok-aaa1") || strings.Contains(string(b), uiTestToken) {
		t.Fatal("a credential appeared in setup output")
	}
}

func TestInstallKeepsRulesLinkedAfterApproval(t *testing.T) {
	m, _ := setupFixture(t)
	e := newSetupEngine(m)
	approveWith(t, e, map[string]*string{"test_cmd": strp("make test")})
	if err := installMemorySkill("claude"); err != nil {
		t.Fatal(err)
	}
	if linked, _ := setupLinkState(setupHarnessHomes()[0]); !linked {
		t.Fatal("cully setup removed the approved rules link")
	}
}
