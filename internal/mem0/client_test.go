package mem0

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
)

func TestProjectionLifecycleAndScope(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-secret" {
			t.Error("missing service authentication")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /memories":
			if r.URL.Query().Get("user_id") != UserID("owner-a") || r.URL.Query().Get("run_id") != "entry-a" {
				t.Error("unscoped projection read")
			}
			_ = json.NewEncoder(w).Encode(hits{Results: []Hit{{ID: "memory-a", UserID: UserID("owner-a"), RunID: "entry-a"}}})
		case "DELETE /memories/memory-a":
			_, _ = w.Write([]byte(`{}`))
		case "POST /memories":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["infer"] != false || in["user_id"] != UserID("owner-a") || in["run_id"] != "entry-a" {
				t.Error("incorrect projection identity or inference")
			}
			meta := in["metadata"].(map[string]any)
			if meta["session_ref"] != "codex-0123456789abcdef" || meta["agent_session_id"] != "native-session-123" || meta["assistant"] != "codex" {
				t.Error("factual session provenance missing from projection")
			}
			for _, key := range []string{"project", "project_url", "section", "theme", "category", "tags"} {
				if _, exists := meta[key]; exists {
					t.Errorf("obsolete metadata %s projected", key)
				}
			}
			_, _ = w.Write([]byte(`{}`))
		case "POST /search":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			f := in["filters"].(map[string]any)
			if f["user_id"] != UserID("owner-a") || f["session_ref"] != "codex-0123456789abcdef" {
				t.Error("unscoped search")
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c, err := New(server.URL, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	projectionRef, nativeID := "codex-0123456789abcdef", "native-session-123"
	e := &memory.Entry{ID: "entry-a", Summary: "Fixed OAuth", Assistant: "codex", SessionRef: &projectionRef, AgentSessionID: &nativeID, UpdatedAt: time.Now()}
	if err = c.Reconcile(context.Background(), "owner-a", "entry-a", e); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0] != "GET /memories" || calls[1] != "DELETE /memories/memory-a" || calls[2] != "POST /memories" {
		t.Fatalf("incorrect lifecycle %v", calls)
	}
	calls = nil
	if err = c.Reconcile(context.Background(), "owner-a", "entry-a", nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatal("deletion recreated a projection")
	}
	ref := "codex-0123456789abcdef"
	if _, err = c.Search(context.Background(), "owner-a", memory.SearchInput{Query: "oauth", SessionRef: &ref, Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if UserID("owner-a") == UserID("owner-b") {
		t.Fatal("owners collide")
	}
}
func TestUpstreamFailureAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("private upstream error"))
	}))
	defer server.Close()
	c, _ := New(server.URL, "secret")
	if _, err := c.Search(context.Background(), "owner", memory.SearchInput{Query: "test", Limit: 3}); err != memory.ErrUnavailable {
		t.Fatalf("upstream error escaped: %v", err)
	}
}
