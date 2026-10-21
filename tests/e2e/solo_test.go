package e2e

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
	cullymcp "github.com/mcp-runtime/cully/internal/transport/mcp"
	"github.com/mcp-runtime/cully/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSolo crosses the live MCP, data API, PostgreSQL and Mem0 services.
// The setup workflow supplies a disposable stack; local unit runs skip it.
func TestSolo(t *testing.T) {
	endpoint := os.Getenv("CULLY_E2E_MCP_URL")
	if endpoint == "" {
		t.Skip("CULLY_E2E_MCP_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "cully-e2e", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) == 0 {
		t.Fatalf("list tools: %v, %+v", err, tools)
	}
	hasWorkspace := false
	for _, tool := range tools.Tools {
		hasWorkspace = hasWorkspace || tool.Name == "cully_workspace_write"
	}
	call := func(name string, input any) memory.Result {
		t.Helper()
		response, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if response.IsError {
			data, _ := json.Marshal(response)
			t.Fatalf("%s returned a tool error: %s", name, data)
		}
		data, err := json.Marshal(response.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var result memory.Result
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	// Solo sessions preserve a task when omitted, unlink it explicitly, and
	// reuse the same owned task record when it is linked again.
	ref, taskName := "solo-e2e-0123456789abcdef", "Verify Solo memory"
	start := call("cully_session", memory.SessionInput{SessionRef: ref, Assistant: "codex", Task: &taskName}).Session
	if start == nil || start.TaskID == nil || start.Task == nil || *start.Task != taskName {
		t.Fatal("Solo session did not link its task")
	}
	taskID := *start.TaskID
	preserved := call("cully_session", memory.SessionInput{SessionRef: ref, Assistant: "codex"}).Session
	if preserved == nil || preserved.TaskID == nil || *preserved.TaskID != taskID {
		t.Fatal("omitting the task lost its link")
	}
	clear := true
	unlinked := call("cully_session", memory.SessionInput{SessionRef: ref, Assistant: "codex", ClearTask: &clear}).Session
	if unlinked == nil || unlinked.TaskID != nil {
		t.Fatal("explicit task clear did not unlink")
	}
	relinked := call("cully_session", memory.SessionInput{SessionRef: ref, Assistant: "codex", Task: &taskName}).Session
	if relinked == nil || relinked.TaskID == nil || *relinked.TaskID != taskID {
		t.Fatal("relinking created a duplicate task")
	}
	if got := call("cully_session_get", memory.SessionRefInput{SessionRef: ref}).Session; got == nil || got.TaskID == nil || *got.TaskID != taskID {
		t.Fatal("session task did not survive a separate read")
	}
	if !call("cully_delete", memory.IDInput{EntryID: taskID}).Deleted {
		t.Fatal("could not delete the Solo task")
	}
	if got := call("cully_session_get", memory.SessionRefInput{SessionRef: ref}).Session; got == nil || got.TaskID != nil {
		t.Fatal("deleted task remained linked")
	}
	// A local single-owner connection cannot create shared Team state.
	// Releases before Team support have no workspace tool; published-CLI mode
	// still exercises their supported Solo lifecycle.
	if hasWorkspace {
		denied, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_workspace_write", Arguments: workspace.Input{Action: "team_create", Name: "Must require OAuth"}})
		if err != nil || !denied.IsError {
			t.Fatal("Solo connection gained Team access")
		}
	}
	loggedSummary := "Disposable container end-to-end check"
	logged := call("cully_log", memory.LogInput{Summary: loggedSummary, Assistant: "other"}).Entry
	if logged == nil || logged.ID == "" {
		t.Fatal("MCP write did not reach PostgreSQL")
	}
	if logged.Summary != "" {
		t.Fatalf("write acknowledgement echoed the record back: %+v", logged)
	}
	id := memory.IDInput{EntryID: logged.ID}
	got := call("cully_get", id).Entry
	if got == nil || got.Summary != loggedSummary {
		t.Fatalf("MCP read did not return the written record: %+v", got)
	}
	found := func(entries []memory.Entry) bool {
		for _, entry := range entries {
			if entry.ID == logged.ID {
				return true
			}
		}
		return false
	}
	if !found(call("cully_search", memory.SearchInput{Query: loggedSummary}).Entries) {
		t.Fatal("PostgreSQL text search did not find the written record")
	}
	if !found(call("cully_recent", memory.RecentInput{Limit: 20}).Entries) {
		t.Fatal("recent records did not include the written record")
	}
	preview, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_context", Arguments: cullymcp.ContextInput{Mode: "recent"}})
	if err != nil || preview.IsError {
		t.Fatalf("context preview failed: %v, %+v", err, preview)
	}
	previewJSON, err := json.Marshal(preview.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var contextResult cullymcp.ContextResult
	if err := json.Unmarshal(previewJSON, &contextResult); err != nil || len(contextResult.Notes) == 0 || contextResult.Notes[0].ID != logged.ID {
		t.Fatalf("context preview did not include the written record: %+v, %v", contextResult, err)
	}
	// Mem0 indexes asynchronously and may briefly report itself unavailable, so
	// poll until the deadline instead of failing on one transient tool error.
	var lastRecallError string
recall:
	for {
		response, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "cully_recall", Arguments: memory.SearchInput{Query: loggedSummary}})
		switch {
		case err != nil:
			lastRecallError = err.Error()
		case response.IsError:
			data, _ := json.Marshal(response.Content)
			lastRecallError = string(data)
		default:
			var recalled memory.Result
			data, _ := json.Marshal(response.StructuredContent)
			if err := json.Unmarshal(data, &recalled); err != nil {
				t.Fatal(err)
			}
			if found(recalled.Entries) {
				break recall
			}
			lastRecallError = ""
		}
		if ctx.Err() != nil {
			t.Fatalf("Mem0 did not index and recall the written record before timeout (last error: %q)", lastRecallError)
		}
		time.Sleep(2 * time.Second)
	}
	updatedSummary := loggedSummary + " updated"
	if updated := call("cully_update", memory.UpdateInput{EntryID: logged.ID, Summary: &updatedSummary}).Entry; updated == nil || updated.ID != logged.ID {
		t.Fatalf("MCP update was not acknowledged: %+v", updated)
	}
	if again := call("cully_get", id).Entry; again == nil || again.Summary != updatedSummary {
		t.Fatalf("MCP update did not change the record: %+v", again)
	}
	if !call("cully_delete", id).Deleted {
		t.Fatal("MCP delete did not delete the record")
	}
	if found(call("cully_search", memory.SearchInput{Query: updatedSummary}).Entries) {
		t.Fatal("deleted record remained visible to text search")
	}
	if found(call("cully_recall", memory.SearchInput{Query: loggedSummary}).Entries) {
		t.Fatal("deleted record remained visible to semantic recall")
	}
}
