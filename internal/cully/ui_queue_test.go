package cully

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func uiReadJSON(t *testing.T, r *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedTaskStartsWhenAnAgentFinishes(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	var first []*uiAgent
	for i := 0; i < 2; i++ {
		a, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"})
		if err != nil {
			t.Fatal(err)
		}
		first = append(first, a)
	}
	q, err := m.enqueue(uiStartRequest{Repo: repo, Title: "later", Prompt: "do it later"})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.queueList(); len(got) != 1 || got[0].ID != q.ID || got[0].Title != "later" {
		t.Fatalf("queue = %+v", got)
	}
	first[0].stop()
	uiWaitFor(t, "queued task to start", func() bool { return len(m.queueList()) == 0 })
	found := false
	for _, v := range m.list() {
		if v.Title == "later" {
			found = true
		}
	}
	if !found {
		t.Fatal("queued task never became an agent")
	}
}

func TestEnqueueValidatesAndStartsImmediatelyWhenThereIsRoom(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	if _, err := m.enqueue(uiStartRequest{Repo: repo, Prompt: " "}); err == nil {
		t.Fatal("empty prompt accepted")
	}
	if _, err := m.enqueue(uiStartRequest{Repo: "/nowhere", Prompt: "x"}); err == nil {
		t.Fatal("repo outside the allow-list accepted")
	}
	if _, err := m.enqueue(uiStartRequest{Repo: repo, Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	uiWaitFor(t, "agent to start", func() bool { return len(m.list()) == 1 && len(m.queueList()) == 0 })
}

func TestDequeueAndStartNow(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	for i := 0; i < 2; i++ {
		if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := m.enqueue(uiStartRequest{Repo: repo, Prompt: "a"})
	b, _ := m.enqueue(uiStartRequest{Repo: repo, Prompt: "b"})
	if err := m.dequeue(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.dequeue("nope"); err == nil {
		t.Fatal("removing an unknown task succeeded")
	}
	if _, err := m.startQueued(b.ID); err == nil {
		t.Fatal("start-now ignored the agent limit")
	}
	if len(m.queueList()) != 1 {
		t.Fatalf("queue = %+v", m.queueList())
	}
}

func TestFanOutStartsOptionsAndQueuesTheRest(t *testing.T) {
	m, repo, _ := uiTestManager(t, uiWriteScript(t, fakeClaude), "tok-aaa1")
	if _, err := m.fanout(uiStartRequest{Repo: repo, Title: "fix", Prompt: "fix it"}, 1); err == nil {
		t.Fatal("fan-out of 1 accepted")
	}
	if _, err := m.fanout(uiStartRequest{Repo: repo, Title: "fix", Prompt: "fix it"}, 5); err == nil {
		t.Fatal("fan-out of 5 accepted")
	}
	res, err := m.fanout(uiStartRequest{Repo: repo, Title: "fix", Prompt: "fix it"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Started != 2 || res.Queued != 1 {
		t.Fatalf("fanout = %+v", res)
	}
	vs := m.list()
	if len(vs) != 2 || vs[0].Group == "" || vs[0].Group != vs[1].Group {
		t.Fatalf("agents = %+v", vs)
	}
	opts := map[string]bool{vs[0].Option: true, vs[1].Option: true}
	if !opts["A"] || !opts["B"] {
		t.Fatalf("options = %+v", opts)
	}
	q := m.queueList()
	if len(q) != 1 || q[0].Group != vs[0].Group || q[0].Option != "C" {
		t.Fatalf("queue = %+v", q)
	}
}

func TestQueueEndpoints(t *testing.T) {
	srv, m, repo := uiTestServer(t, true)
	for i := 0; i < 2; i++ {
		if _, err := m.start(uiStartRequest{Repo: repo, Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
	}
	if r := uiDo(t, "GET", srv.URL+"/api/queue", nil, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", r.StatusCode)
	}
	r := uiDo(t, "POST", srv.URL+"/api/queue", map[string]any{"repo": repo, "prompt": "later"}, uiAuthed)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("enqueue = %d", r.StatusCode)
	}
	var list []uiQueuedView
	uiReadJSON(t, uiDo(t, "GET", srv.URL+"/api/queue", nil, uiAuthed), &list)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if r := uiDo(t, "DELETE", srv.URL+"/api/queue/"+list[0].ID, nil, uiAuthed); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", r.StatusCode)
	}
	if r := uiDo(t, "POST", srv.URL+"/api/fanout", map[string]any{"repo": repo, "prompt": "x", "count": 9}, uiAuthed); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad fanout = %d", r.StatusCode)
	}
}

func TestBoardPageOffersQueueFanOutCompareAndLineComments(t *testing.T) {
	b, err := uiStaticFS.ReadFile("ui_static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/api/queue", "Up next", "Compare options", "Keep this one", "Send to task", "Queue for later"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}
