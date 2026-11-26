package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiIdentityInputFor(repo string) uiIdentityInput {
	return uiIdentityInput{Name: "Release watcher", Role: "Checks open pull requests each morning and reports what needs a person.", Instructions: "Never merge. Report only.", Folder: repo, Schedule: "daily", Enabled: true, Approve: true}
}

func TestAgentsAreCreatedValidatedAndKeptOnDisk(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for _, bad := range []uiIdentityInput{
		{Name: "", Role: "x", Folder: repo}, {Name: "ok", Role: "", Folder: repo}, {Name: "bad/name", Role: "x", Folder: repo},
		{Name: "ok", Role: "x", Folder: "/nope"}, {Name: "ok", Role: "x", Folder: repo, Schedule: "weekly"}, {Name: "ok", Role: "x", Folder: repo, Mode: "yolo"},
		{Name: "ok", Role: "x", Folder: repo, Groups: map[string]bool{"nonsense": true}}, {Name: "ok", Role: "x", Folder: repo, Instructions: strings.Repeat("a", 4001)},
	} {
		if _, err := m.identityCreate(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	a, err := m.identityCreate(uiIdentityInputFor(repo))
	if err != nil || !a.Enabled || a.Mode != "edit" || !a.Groups["read"] || a.Groups["web"] {
		t.Fatalf("agent = %+v, err = %v", a, err)
	}
	if _, err := m.identityCreate(uiIdentityInputFor(repo)); err == nil {
		t.Fatal("a second agent with the same name was accepted")
	}
	if n := len(uiDB().all("agent")); n != 1 {
		t.Fatalf("agents in the database = %d", n)
	}
	if l := newUIManager(m.cfg).identityList(); len(l) != 1 || l[0].Name != "Release watcher" {
		t.Fatalf("not kept: %+v", l)
	}
	upd := uiIdentityInputFor(repo)
	upd.Role = "Reports on pull requests."
	upd.Schedule = ""
	b, err := m.identityUpdate(a.ID, upd)
	if err != nil || b.Role != "Reports on pull requests." || b.Enabled {
		t.Fatalf("updated = %+v, err = %v", b, err)
	}
	if err := m.identityDelete(a.ID); err != nil || len(m.identityList()) != 0 {
		t.Fatalf("delete: %v", err)
	}
}

func TestRunningAnAgentUsesItsIdentityToolsAndKeepsItsNotes(t *testing.T) {
	body := uiFakeHead + `  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Checked 3 pull requests; none need you.\nMEMORY: PR 12 waits on CI"}]}}'
` + uiFakeTail
	m, repo, out := uiTestManager(t, uiWriteScript(t, body), "tok-aaa1")
	in := uiIdentityInputFor(repo)
	in.Groups = map[string]bool{"read": true}
	a, _ := m.identityCreate(in)
	run, err := m.identityRun(a.ID)
	if err != nil || run.Status != "running" || run.TaskID == "" {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
	if _, err := m.identityRun(a.ID); err == nil {
		t.Fatal("started twice while running")
	}
	uiWaitFor(t, "the run to finish", func() bool {
		l := m.identityList()
		return len(l) == 1 && len(l[0].Runs) == 1 && l[0].Runs[0].Status == "done"
	})
	got := m.identityList()[0]
	if !strings.Contains(got.Runs[0].Summary, "Checked 3 pull requests") || strings.Contains(got.Runs[0].Summary, "MEMORY:") {
		t.Fatalf("summary = %q", got.Runs[0].Summary)
	}
	if got.Memory != "- PR 12 waits on CI" {
		t.Fatalf("memory = %q", got.Memory)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args.log"))
	line := string(args)
	if !strings.Contains(line, "--append-system-prompt You are Release watcher") || !strings.Contains(line, "--allowedTools Read,Glob,Grep ") {
		t.Fatalf("args = %s", line)
	}
	if _, rest, ok := strings.Cut(line, "--allowedTools "); !ok || strings.Fields(rest)[0] != "Read,Glob,Grep" {
		t.Fatalf("the agent got tools its identity does not allow: %s", line)
	}
	run2, err := m.identityRun(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = run2
	uiWaitFor(t, "the second run", func() bool { l := m.identityList()[0]; return len(l.Runs) == 2 && l.Runs[0].Status == "done" })
	args, _ = os.ReadFile(filepath.Join(out, "args.log"))
	if !strings.Contains(string(args), "<notes>\n- PR 12 waits on CI") {
		t.Fatalf("the notes were not given to the next run: %s", args)
	}
}

func TestAgentNotesAreBoundedAndOnlyTheNewestKept(t *testing.T) {
	old := ""
	for i := 0; i < 80; i++ {
		old = uiMergeMemory(old, "MEMORY: note number "+uiItoa(i)+" "+strings.Repeat("x", 60))
	}
	if len([]rune(old)) > uiIdentityMemory || !strings.Contains(old, "note number 79") || strings.Contains(old, "note number 0 ") {
		t.Fatalf("memory = %d runes: %.80q", len([]rune(old)), old)
	}
	if uiMergeMemory("keep", "no memory line here") != "keep" {
		t.Fatal("notes changed with nothing to add")
	}
}

func TestScheduledAgentsRunWhenDueAndOnlyOnce(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.identityCreate(uiIdentityInputFor(repo))
	m.identitiesDue()
	uiWaitFor(t, "the scheduled run", func() bool { l := m.identityList(); return len(l[0].Runs) == 1 })
	m.identitiesDue()
	if l := m.identityList(); len(l[0].Runs) != 1 {
		t.Fatalf("ran again before it was due: %+v", l[0].Runs)
	}
	uiWaitFor(t, "the run to finish", func() bool { return m.identityList()[0].Runs[0].Status != "running" })
	uiIdentityMu.Lock()
	for _, x := range m.loadIdentitiesLocked() {
		if x.ID == a.ID {
			x.LastRun = time.Now().Add(-25 * time.Hour)
		}
	}
	uiIdentityMu.Unlock()
	m.identitiesDue()
	uiWaitFor(t, "the next day's run", func() bool { return len(m.identityList()[0].Runs) == 2 })
}

func TestAgentEndpoints(t *testing.T) {
	srv, _, repo := uiTestServer(t, true)
	if r := uiDo(t, "GET", srv.URL+"/api/identities", nil, nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	r := uiDo(t, "POST", srv.URL+"/api/identities", uiIdentityInputFor(repo), uiAuthed)
	if r.StatusCode != 201 {
		t.Fatalf("create: %d", r.StatusCode)
	}
	var a uiIdentity
	uiReadJSON(t, r, &a)
	if r := uiDo(t, "POST", srv.URL+"/api/identities", map[string]any{"name": "", "role": "x", "folder": repo}, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("bad create: %d", r.StatusCode)
	}
	if r := uiDo(t, "DELETE", srv.URL+"/api/identities/"+a.ID, nil, uiAuthed); r.StatusCode != 204 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/identities/nope/run", nil, uiAuthed); r.StatusCode != 400 {
		t.Fatalf("run unknown: %d", r.StatusCode)
	}
}

func TestAgentsPageAndGoodSessionCardExist(t *testing.T) {
	b, _ := uiStaticFS.ReadFile("ui_static/app.js")
	for _, want := range []string{"function renderTeam", "function openAgentForm", "What an agent is and how it works", "Forget its notes", "Approve as agent", "function optimalCard", "What a good session looks like", "coachCard(again, true)"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	idx, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(idx), `href="#/agents"`) || !strings.Contains(string(idx), `id="view-team"`) {
		t.Error("index.html has no Agents page")
	}
}

func TestDraftAgentsCannotRunUntilApproved(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	in := uiIdentityInputFor(repo)
	in.Approve = false
	a, err := m.identityCreate(in)
	if err != nil || a.Approved || a.Enabled {
		t.Fatalf("draft = %+v, err = %v", a, err)
	}
	if _, err := m.identityRun(a.ID); err == nil {
		t.Fatal("a draft ran")
	}
	m.identitiesDue()
	if len(m.identityList()[0].Runs) != 0 {
		t.Fatal("a draft was scheduled")
	}
	b, err := m.identityApprove(a.ID)
	if err != nil || !b.Approved {
		t.Fatalf("approve: %+v %v", b, err)
	}
	if _, err := m.identityRun(a.ID); err != nil {
		t.Fatalf("an approved agent did not run: %v", err)
	}
}

func TestAgentsDoNotNeedGitAndGlobalOnesBelongToNoFolder(t *testing.T) {
	m, _, out := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	plain := t.TempDir()
	in := uiIdentityInputFor(plain)
	in.Name = "Plain folder agent"
	a, err := m.identityCreate(in)
	if err != nil {
		t.Fatalf("a folder with no git was refused: %v", err)
	}
	if _, err := m.identityRun(a.ID); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "the run", func() bool { l := m.identityList(); return len(l) == 1 && l[0].Runs[0].Status == "done" })
	g, err := m.identityCreate(uiIdentityInput{Name: "Dev workflow", Role: "Keeps my dev workflow rules.", Scope: "global", Folder: "/ignored", Approve: true})
	if err != nil || g.Scope != "global" || g.Folder != "" {
		t.Fatalf("global = %+v, err = %v", g, err)
	}
	if _, err := m.identityRun(g.ID); err != nil {
		t.Fatalf("global agent did not run: %v", err)
	}
	uiWaitFor(t, "the global run", func() bool {
		for _, x := range m.identityList() {
			if x.ID == g.ID {
				return x.Runs[0].Status == "done"
			}
		}
		return false
	})
	if _, err := os.Stat(filepath.Join(cullyDir(), "agents")); err != nil {
		t.Fatal(err)
	}
	_ = out
	if err := m.identityDelete(g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cullyDir(), "agents", g.ID, "workspace")); err != nil {
		t.Fatalf("deleting an agent removed its workspace: %v", err)
	}
	if uiDB().get("agent", g.ID, &uiIdentity{}) {
		t.Fatal("the deleted agent is still in the database")
	}
}

func TestAgentsAndTheirNotesAreKeptInTheDatabase(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.identityCreate(uiIdentityInputFor(repo))
	uiIdentityMu.Lock()
	m.loadIdentitiesLocked()[0].Memory = "- PR 12 waits on CI"
	m.saveIdentitiesLocked()
	uiIdentityMu.Unlock()
	var stored uiIdentity
	if !uiDB().get("agent", a.ID, &stored) || stored.Memory != "- PR 12 waits on CI" || stored.Name != "Release watcher" || !stored.Approved {
		t.Fatalf("stored = %+v", stored)
	}
	if st, err := os.Stat(uiStorePath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("database file: %v %v", st, err)
	}
	if l := newUIManager(m.cfg).identityList(); len(l) != 1 || l[0].Memory != "- PR 12 waits on CI" {
		t.Fatalf("reloaded = %+v", l)
	}
}

func TestFilesFromEarlierVersionsAreImportedOnceAndSetAside(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Cleanup(uiCloseStores)
	dir := cullyDir()
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("session-index.json", `{"analyze":true,"autoScout":true,"sessions":{"`+uiTestSessA+`":{"id":"`+uiTestSessA+`","agent":"claude","cwd":"/p/x","name":"x","title":"Old title","source":"ai","turns":3}}}`)
	write("research-log.jsonl", `{"id":"r1","kind":"scout","text":"found it","repo":"/p/x"}`+"\n"+`{"id":"r2","kind":"advice","text":"add tests","repo":"/p/x"}`+"\n")
	write("usage-log.jsonl", `{"taskId":"t1","title":"a","repo":"/p/x","fresh":1,"cached":9}`+"\n")
	write("coach.json", `{"based":4,"items":[{"kind":"habit","title":"Run tests","why":"w","how":"h"}]}`)
	write("ui-settings.json", `{"allow":["WebSearch"]}`)
	write("agent-suggestions.json", `[{"id":"s1","name":"A","state":"new"}]`)
	write("identities.json", `[{"id":"abc12345","name":"Old agent","role":"r","folder":"/p/x","groups":{"read":true},"mode":"edit","memory":"- old note","runs":[]}]`)
	write("agents/new-agent-def45678/agent.json", `{"id":"def45678","name":"New agent","role":"r","scope":"global","approved":true}`)
	write("agents/new-agent-def45678/notes.md", "# New agent: notes\n\n- kept note\n")
	write("evals/"+uiTestSessA+".json", `{"id":"`+uiTestSessA+`","task":"Fix it","criteria":["works"]}`)

	db := uiDB()
	var idx uiIndexFile
	if !db.get("index", "meta", &idx) || !idx.Analyze || !idx.AutoScout {
		t.Fatalf("index settings = %+v", idx)
	}
	var sess uiIndexed
	if !db.get("session", uiTestSessA, &sess) || sess.Title != "Old title" || sess.Turns != 3 {
		t.Fatalf("session = %+v", sess)
	}
	if n := len(db.logAll("research")); n != 2 {
		t.Fatalf("research rows = %d", n)
	}
	if n := len(db.logAll("usage")); n != 1 {
		t.Fatalf("usage rows = %d", n)
	}
	var coach uiCoach
	if !db.get("coach", "latest", &coach) || len(coach.Items) != 1 {
		t.Fatalf("coach = %+v", coach)
	}
	var pol uiPolicy
	if !db.get("settings", "policy", &pol) || len(pol.Allow) != 1 {
		t.Fatalf("policy = %+v", pol)
	}
	var old, fresh uiIdentity
	if !db.get("agent", "abc12345", &old) || old.Memory != "- old note" || !old.Approved || old.Scope != "project" {
		t.Fatalf("old agent = %+v", old)
	}
	if !db.get("agent", "def45678", &fresh) || fresh.Memory != "- kept note" || fresh.Scope != "global" {
		t.Fatalf("new agent = %+v", fresh)
	}
	if len(db.all("agentsuggest")) != 1 || len(db.all("eval")) != 1 {
		t.Fatal("suggestions or evals were not imported")
	}
	for _, f := range []string{"session-index.json", "research-log.jsonl", "usage-log.jsonl", "coach.json", "ui-settings.json", "agent-suggestions.json", "identities.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s was not set aside", f)
		}
		if _, err := os.Stat(filepath.Join(dir, f+".imported")); err != nil {
			t.Errorf("%s.imported is missing", f)
		}
	}
	// opening the database again must not import twice
	uiCloseStores()
	again := uiDB()
	if n := len(again.logAll("research")); n != 2 {
		t.Fatalf("research rows after reopening = %d", n)
	}
}
