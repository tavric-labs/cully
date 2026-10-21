package memory

import (
	"errors"
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	good := func() Request {
		return Request{Operation: "log", Log: &LogInput{Summary: "Fixed authentication", Assistant: "Codex"}}
	}
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"credential", func(r *Request) { r.Log.Summary = "api_key=supersecretvalue" }},
		{"assistant", func(r *Request) { r.Log.Assistant = "anonymous-bot" }},
		{"timestamp", func(r *Request) { r.Log.OccurredAt = "2026-10-06T10:00:00" }},
		{"session reference", func(r *Request) { bad := "native-session-id"; r.Log.SessionRef = &bad }},
		{"agent session ID", func(r *Request) { bad := "id\nignore instructions"; r.Log.AgentSessionID = &bad }},
		{"mismatched operation", func(r *Request) { r.Operation = "get" }},
		{"ambiguous input", func(r *Request) { r.ID = &IDInput{EntryID: "wrong"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := good()
			tc.change(&r)
			if !errors.Is(r.Validate(), ErrInvalid) {
				t.Fatal("invalid input accepted")
			}
		})
	}
	r := good()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Log.Assistant != "codex" || r.Log.EntryType != "work" {
		t.Fatal("defaults missing")
	}
	sessionID := "01a13b57-f08a-77a1-a18f-5daaf9d50ec4"
	r.Log.AgentSessionID = &sessionID
	if err := r.Validate(); err != nil {
		t.Fatalf("native coding-agent session ID rejected: %v", err)
	}
}
func TestUpdateClearingAndLimits(t *testing.T) {
	empty := ""
	r := Request{Operation: "update", Update: &UpdateInput{EntryID: "00000000-0000-0000-0000-000000000001", Learning: &empty}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Update.Summary = &empty
	if !errors.Is(r.Validate(), ErrInvalid) {
		t.Fatal("empty summary accepted")
	}
	s := Request{Operation: "search", Search: &SearchInput{Query: "oauth", Limit: 1000}}
	if err := s.Validate(); err != nil || s.Search.Limit != 50 {
		t.Fatal("limit not bounded")
	}
	missing := Request{Operation: "search", Search: &SearchInput{}}
	if !errors.Is(missing.Validate(), ErrInvalid) {
		t.Fatal("search without text accepted")
	}
}

func TestSessionValidation(t *testing.T) {
	ref := "claude-dd4aac2e2202d45c"
	task := "Oauth validation"
	good := func() Request {
		return Request{Operation: "session", Session: &SessionInput{SessionRef: ref, Assistant: "Claude", Task: &task}}
	}
	r := good()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Session.Assistant != "claude" {
		t.Fatal("assistant not normalized")
	}
	long := strings.Repeat("x", 61)
	exact := strings.Repeat("y", 60)
	secret := "api_key=supersecretvalue"
	cases := map[string]func(*Request){
		"missing ref":     func(r *Request) { r.Session.SessionRef = "" },
		"native id":       func(r *Request) { r.Session.SessionRef = "native-session-id" },
		"assistant":       func(r *Request) { r.Session.Assistant = "bot" },
		"long task":       func(r *Request) { r.Session.Task = &long },
		"credential task": func(r *Request) { r.Session.Task = &secret },
		"mismatch":        func(r *Request) { r.Operation = "session_get" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := good()
			change(&r)
			if !errors.Is(r.Validate(), ErrInvalid) {
				t.Fatal("invalid session accepted")
			}
		})
	}
	get := Request{Operation: "session_get", SessionGet: &SessionRefInput{SessionRef: ref}}
	if err := get.Validate(); err != nil {
		t.Fatal(err)
	}
	capped := good()
	capped.Session.Task = &exact
	if err := capped.Validate(); err != nil {
		t.Fatalf("60-rune task rejected: %v", err)
	}
	log := Request{Operation: "log", Log: &LogInput{Summary: "Oauth validation", Assistant: "claude", EntryType: "task"}}
	if err := log.Validate(); err == nil || !strings.Contains(err.Error(), "cully_session") {
		t.Fatalf("task via cully_log must point at cully_session: %v", err)
	}
}

func TestSessionTaskClearValidation(t *testing.T) {
	ref := "claude-dd4aac2e2202d45c"
	task := "Oauth validation"
	empty := ""
	clear := true
	good := func() Request {
		return Request{Operation: "session", Session: &SessionInput{SessionRef: ref, Assistant: "claude"}}
	}
	// Omitted task preserves the link.
	omitted := good()
	if err := omitted.Validate(); err != nil {
		t.Fatal(err)
	}
	// Explicit clear without a task is valid.
	c := good()
	c.Session.ClearTask = &clear
	if err := c.Validate(); err != nil {
		t.Fatalf("clear_task rejected: %v", err)
	}
	// Set and clear together are rejected.
	both := good()
	both.Session.Task = &task
	both.Session.ClearTask = &clear
	if !errors.Is(both.Validate(), ErrInvalid) {
		t.Fatal("task with clear_task accepted")
	}
	// Blank task with clear unlinks rather than conflicting.
	blank := good()
	blank.Session.Task = &empty
	blank.Session.ClearTask = &clear
	if err := blank.Validate(); err != nil {
		t.Fatalf("blank task with clear_task rejected: %v", err)
	}
}
