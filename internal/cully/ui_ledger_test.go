package cully

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ledgerKinds(m *uiManager, kind string) []uiLedgerView { return m.ledgerList(kind, 0) }

func TestLedgerRecordsAgentChangesWithoutNotesAndRollsThemBack(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	in := uiIdentityInputFor(repo)
	in.Proposal = "p1"
	a, err := m.identityCreate(in)
	if err != nil {
		t.Fatal(err)
	}
	uiIdentityMu.Lock()
	m.loadIdentitiesLocked()[0].Memory = "secret notes"
	uiIdentityMu.Unlock()
	upd := uiIdentityInputFor(repo)
	upd.Role = "Reports on pull requests."
	if _, err := m.identityUpdate(a.ID, upd); err != nil {
		t.Fatal(err)
	}
	l := ledgerKinds(m, "agent")
	if len(l) != 2 || l[1].Action != "create" || l[1].Source != "proposal:p1" || l[0].Action != "update" || l[0].Source != "user" {
		t.Fatalf("ledger = %+v", l)
	}
	if len(l[0].Changes) != 1 || l[0].Changes[0].Field != "What it does" || l[0].Changes[0].New != "Reports on pull requests." {
		t.Fatalf("diff = %+v", l[0].Changes)
	}
	for _, e := range uiLedgerAll() {
		if strings.Contains(string(e.Before)+string(e.After), "secret notes") || strings.Contains(string(e.After), "runs") {
			t.Fatalf("notes or runs in the snapshot: %s", e.After)
		}
	}
	// roll back the update
	if _, err := m.ledgerRollback(l[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if got := m.identityList()[0]; got.Role != in.Role || got.Memory != "secret notes" {
		t.Fatalf("after rollback = %+v", got)
	}
	if _, err := m.ledgerRollback(l[0].ID, false); err == nil {
		t.Fatal("rolled the same entry back twice")
	}
	// delete, then bring it back
	if err := m.identityDelete(a.ID); err != nil {
		t.Fatal(err)
	}
	del := ledgerKinds(m, "agent")[0]
	if del.Action != "delete" {
		t.Fatalf("entry = %+v", del)
	}
	if _, err := m.ledgerRollback(del.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := m.identityList(); len(got) != 1 || got[0].ID != a.ID || got[0].Name != "Release watcher" || len(got[0].Runs) != 0 {
		t.Fatalf("restored = %+v", got)
	}
	if n := len(uiLedgerAll()); n != 5 { // create, update, rollback, delete, rollback
		t.Fatalf("entries = %d", n)
	}
}

func TestLedgerRollbackRefusesWhenChangedSinceUnlessForced(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.identityCreate(uiIdentityInputFor(repo))
	u1 := uiIdentityInputFor(repo)
	u1.Role = "First change."
	m.identityUpdate(a.ID, u1)
	u2 := uiIdentityInputFor(repo)
	u2.Role = "Second change."
	m.identityUpdate(a.ID, u2)
	first := ledgerKinds(m, "agent")[1] // the First change entry
	_, err := m.ledgerRollback(first.ID, false)
	if !errors.Is(err, errLedgerChange) {
		t.Fatalf("err = %v", err)
	}
	if m.identityList()[0].Role != "Second change." {
		t.Fatal("a refused rollback changed the agent")
	}
	if _, err := m.ledgerRollback(first.ID, true); err != nil {
		t.Fatal(err)
	}
	if m.identityList()[0].Role == "Second change." {
		t.Fatal("force did not roll back")
	}
	// a created agent is removed on rollback
	c := ledgerKinds(m, "agent")
	if _, err := m.ledgerRollback(c[len(c)-1].ID, true); err != nil || len(m.identityList()) != 0 {
		t.Fatalf("remove created: %v %+v", err, m.identityList())
	}
}

func TestLedgerRollbackChecksTheRestoredAgentLikeANewOne(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.identityCreate(uiIdentityInputFor(repo))
	u := uiIdentityInputFor(repo)
	u.Folder = t.TempDir()
	if _, err := m.identityUpdate(a.ID, u); err != nil {
		t.Fatal(err)
	}
	upd := ledgerKinds(m, "agent")[0]
	_ = os.RemoveAll(repo)
	if _, err := m.ledgerRollback(upd.ID, false); err == nil {
		t.Fatal("restored an agent whose folder is gone")
	}
}

func TestLedgerRecordsAndRollsBackJobs(t *testing.T) {
	m, repo, _ := uiIndexManager(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.MkdirAll(filepath.Join(home, ".cully"), 0o700)
	s := uiJobSuggestion{Evidence: uiEv(), ID: "j1", Name: "nightly-tests", Command: []string{"make", "-C", repo, "test"}, After: "02:00", Folder: repo, Why: "w", State: "new"}
	uiDB().put("jobsuggest", s.ID, s)
	if _, err := m.jobSuggestionAccept("j1", "03:15"); err != nil {
		t.Fatal(err)
	}
	l := ledgerKinds(m, "job")
	if len(l) != 1 || l[0].Action != "create" || l[0].Source != "proposal:j1" {
		t.Fatalf("ledger = %+v", l)
	}
	// a user pause is recorded, and rolling back the creation is refused until forced
	before := uiJobSnapshot("nightly-tests")
	if err := setJobPaused("nightly-tests", true); err != nil {
		t.Fatal(err)
	}
	m.ledgerJob(before, uiJobSnapshot("nightly-tests"), "user")
	l = ledgerKinds(m, "job")
	if l[0].Action != "pause" {
		t.Fatalf("pause entry = %+v", l[0])
	}
	if _, err := m.ledgerRollback(l[1].ID, false); !errors.Is(err, errLedgerChange) {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.ledgerRollback(l[0].ID, false); err != nil || uiJobSnapshot("nightly-tests").Paused {
		t.Fatalf("resume by rollback: %v", err)
	}
	// remove the job, then restore it
	snap := uiJobSnapshot("nightly-tests")
	_ = uiEditJobsFile(func(_ []scheduledJob, _ []json.RawMessage) ([]json.RawMessage, error) { return nil, nil })
	m.ledgerJob(snap, nil, "user")
	del := ledgerKinds(m, "job")[0]
	if _, err := m.ledgerRollback(del.ID, false); err != nil {
		t.Fatal(err)
	}
	if j := uiJobSnapshot("nightly-tests"); j == nil || j.After != "03:15" {
		t.Fatalf("restored job = %+v", j)
	}
	// a tampered snapshot with a command outside the allowlist is refused
	bad := uiLedgerEntry{Kind: "job", Subject: "evil", Action: "delete", Before: uiSnapJSON(&uiJobSnap{Name: "evil", Command: []string{"rm", "-rf", "/"}, After: "01:00"})}
	m.ledgerAdd(bad)
	id := ledgerKinds(m, "job")[0].ID
	if _, err := m.ledgerRollback(id, true); err == nil {
		t.Fatal("restored a command outside the allowlist")
	}
}

func TestLedgerIsCappedAndRedactsTokens(t *testing.T) {
	m, _, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for i := 0; i < uiLedgerMax+5; i++ {
		m.ledgerRecord("policy", "Tool policy", "policy", "update", "user", []byte(`{"n":`+uiItoa(i)+`}`), []byte(`{"n":`+uiItoa(i+1)+`}`))
	}
	if n := len(uiLedgerAll()); n != uiLedgerMax {
		t.Fatalf("entries = %d", n)
	}
	m.ledgerRecord("agent", "x", "x1", "update", "user", []byte(`{"role":"a"}`), []byte(`{"role":"uses tok-aaa1 here"}`))
	e := uiLedgerAll()[uiLedgerMax-1]
	if strings.Contains(string(e.After), "tok-aaa1") || !e.Lossy {
		t.Fatalf("entry = %+v", e)
	}
	if ok, _ := e.rollbackable(); ok {
		t.Fatal("a lossy entry may roll back")
	}
}

func TestLedgerRulesAndPolicyRollBack(t *testing.T) {
	m, _ := uiRulesFixture(t)
	if _, err := m.rulesPropose(); err != nil {
		t.Fatal(err)
	}
	p := m.rulesView().Proposal
	if p == nil {
		t.Fatal("no proposal")
	}
	pv := m.rulesPreview(p, m.rulesView().Current)
	if pv.Action != "update" || len(pv.Changes) != 1 {
		t.Fatalf("preview = %+v", pv)
	}
	if _, err := m.rulesApprove(p.ID); err != nil {
		t.Fatal(err)
	}
	l := ledgerKinds(m, "rule")
	if len(l) != 1 || l[0].Source != "proposal:"+p.ID {
		t.Fatalf("ledger = %+v", l)
	}
	if _, err := m.ledgerRollback(l[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if uiRulesCurrent() != "" {
		t.Fatal("the created rules file was not removed")
	}
	before := m.policySnap()
	if err := m.setToolPolicy(uiPolicyInput{Presets: map[string]bool{"web": true}}); err != nil {
		t.Fatal(err)
	}
	m.ledgerPolicy(before, "user")
	pl := ledgerKinds(m, "policy")[0]
	if _, err := m.ledgerRollback(pl.ID, false); err != nil || m.policy.Presets["web"] {
		t.Fatalf("policy rollback: %v %+v", err, m.policy)
	}
}

func TestProposalPreviewsShowWhatWillChange(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	p := m.agentPreview(uiAgentSuggestion{Name: "New one", Role: "Does a thing.", Folder: repo, Groups: map[string]bool{"read": true}, Schedule: "daily"})
	if p.Action != "create" || len(p.Changes) == 0 {
		t.Fatalf("create preview = %+v", p)
	}
	a, _ := m.identityCreate(uiIdentityInputFor(repo))
	p = m.agentPreview(uiAgentSuggestion{Name: a.Name, Role: "A sharper role.", Instructions: a.Instructions, Folder: repo, Groups: map[string]bool{"read": true}, Schedule: "daily", Replaces: a.ID})
	found := false
	for _, c := range p.Changes {
		if c.Field == "What it does" && c.New == "A sharper role." && c.Old == a.Role {
			found = true
		}
	}
	if p.Action != "update" || !found {
		t.Fatalf("update preview = %+v", p)
	}
	same := m.agentPreview(uiAgentSuggestion{Name: a.Name, Role: a.Role, Instructions: a.Instructions, Folder: a.Folder, Groups: a.Groups, Schedule: a.Schedule, Replaces: a.ID})
	if same.Action != "none" {
		t.Fatalf("unchanged preview = %+v", same)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	jp := m.jobPreview(uiJobSuggestion{Name: "j", Command: []string{"make", "-C", repo, "test"}, After: "02:00"})
	if jp.Action != "create" {
		t.Fatalf("job preview = %+v", jp)
	}
}

func TestLifecycleStaleArchivedAndPinned(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	a, _ := m.identityCreate(uiIdentityInputFor(repo))
	now := time.Now()
	if a.lifecycle(now) != "active" || a.lifecycle(now.Add(31*24*time.Hour)) != "stale" {
		t.Fatalf("lifecycle = %s / %s", a.lifecycle(now), a.lifecycle(now.Add(31*24*time.Hour)))
	}
	if p, _ := m.identitySetLifecycle(a.ID, "pin", true); p.lifecycle(now.Add(90*24*time.Hour)) != "active" {
		t.Fatal("a pinned agent went stale")
	}
	r, _ := m.identitySetLifecycle(a.ID, "archive", true)
	if r.Lifecycle != "archived" || r.Pinned {
		t.Fatalf("archived = %+v", r)
	}
	if len(uiIdentitiesByLifecycle(m.identityList(), false)) != 0 || len(uiIdentitiesByLifecycle(m.identityList(), true)) != 1 {
		t.Fatal("archived agents are not hidden by default")
	}
	if len(uiDB().all("agent")) != 1 {
		t.Fatal("archiving deleted the agent")
	}
	// proposals
	old := time.Now().Add(-15 * 24 * time.Hour)
	uiDB().put("agentsuggest", "s1", uiAgentSuggestion{ID: "s1", Name: "Old idea", Created: old, State: "new", Evidence: uiEv()})
	uiDB().put("agentsuggest", "s2", uiAgentSuggestion{ID: "s2", Name: "Pinned idea", Created: old, State: "new", Pinned: true, Evidence: uiEv()})
	uiDB().put("jobsuggest", "j1", uiJobSuggestion{ID: "j1", Name: "old-job", Command: []string{"git", "-C", repo, "status"}, Created: old, State: "new", Evidence: uiEv()})
	if n := m.proposalsCurate(); n != 2 {
		t.Fatalf("archived %d", n)
	}
	arch := m.archivedSuggestions()
	if len(arch) != 2 {
		t.Fatalf("archived = %+v", arch)
	}
	if err := m.proposalSetLifecycle("s1", "archive", false); err != nil {
		t.Fatal(err)
	}
	if m.proposalsCurate() != 0 {
		t.Fatal("a restored proposal was archived again at once")
	}
	if len(uiReadAgentSuggestions()) != 2 || len(uiReadJobSuggestions()) != 1 {
		t.Fatal("a proposal was deleted")
	}
}
