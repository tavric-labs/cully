package cully

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uiIdentityInputFor(repo string) uiIdentityInput {
	return uiIdentityInput{Name: "Release watcher", Role: "Checks open pull requests each morning and reports what needs a person.", Instructions: "Never merge. Report only.", Folder: repo, Schedule: "daily", Enabled: true}
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
	if st, err := os.Stat(filepath.Join(cullyDir(), "identities.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", st, err)
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
	for _, want := range []string{"function renderTeam", "function openAgentForm", "What an agent is and how it works", "Forget its notes", "function optimalCard", "What a good session looks like", "coachCard(again, true)"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	idx, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(idx), `href="#/agents"`) || !strings.Contains(string(idx), `id="view-team"`) {
		t.Error("index.html has no Agents page")
	}
}
