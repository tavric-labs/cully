package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
	"github.com/mcp-runtime/cully/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestTeam crosses signed OAuth, public MCP, the private API and PostgreSQL,
// including a Claude-to-Codex handoff and an independent maintainer review.
func TestTeam(t *testing.T) {
	s := startTeamStack(t)
	t.Run("authentication", func(t *testing.T) {
		for _, tc := range []struct{ name, token string }{
			{"missing token", ""},
			{"wrong issuer", s.token(t, "dev", "tools:read tools:write", "https://wrong.test", s.endpoint, time.Now().Add(time.Hour))},
			{"wrong resource", s.token(t, "dev", "tools:read tools:write", s.issuer, "https://wrong.test/mcp", time.Now().Add(time.Hour))},
			{"expired", s.token(t, "dev", "tools:read tools:write", s.issuer, s.endpoint, time.Now().Add(-time.Hour))},
		} {
			t.Run(tc.name, func(t *testing.T) {
				req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("invalid identity accepted: status=%d", resp.StatusCode)
				}
			})
		}
	})
	lead, dev, next, outsider := s.connect(t, "lead", "tools:read tools:write"), s.connect(t, "dev", "tools:read tools:write"), s.connect(t, "next", "tools:read tools:write"), s.connect(t, "outsider", "tools:read tools:write")
	team := callWorkspace(t, s, lead, workspace.Input{Action: "team_create", Name: "E2E engineering"}).TeamID
	devID := callWorkspace(t, s, dev, workspace.Input{Action: "whoami"}).Principal
	nextID := callWorkspace(t, s, next, workspace.Input{Action: "whoami"}).Principal
	if devID != workspace.Principal(s.issuer, "dev") {
		t.Fatal("verified identity lost its issuer scope")
	}
	for _, id := range []string{devID, nextID} {
		callWorkspace(t, s, lead, workspace.Input{Action: "team_member", TeamID: team, Principal: id, Role: "member"})
	}
	project := callWorkspace(t, s, lead, workspace.Input{Action: "project_create", TeamID: team, Name: "Cully", Repository: "https://github.com/mcp-runtime/cully"}).Project.ID
	scoped := func(v workspace.Input) workspace.Input { v.TeamID, v.ProjectID = team, project; return v }
	for _, id := range []string{devID, nextID} {
		callWorkspace(t, s, lead, scoped(workspace.Input{Action: "project_member", Principal: id, Role: "maintainer"}))
	}
	rejectWorkspace(t, s, outsider, scoped(workspace.Input{Action: "board"}), "outsider project read")
	reader := s.connect(t, "dev", "tools:read")
	rejectWorkspace(t, s, reader, scoped(workspace.Input{Action: "task_create", Name: "Denied", Criteria: []string{"No writes"}}), "read-only token write")
	wrongTool, err := lead.CallTool(s.ctx, &sdk.CallToolParams{Name: "cully_workspace_read", Arguments: scoped(workspace.Input{Action: "task_create", Name: "Denied", Criteria: []string{"No writes"}})})
	if err != nil || !wrongTool.IsError {
		t.Fatal("read tool dispatched a mutation")
	}

	t.Log("Claude saves a portable checkpoint and releases the task to Codex")
	task := callWorkspace(t, s, dev, scoped(workspace.Input{Action: "task_create", Name: "Fix sign-in", Criteria: []string{"Correct issuer accepted", "Wrong issuer rejected"}})).Task
	task = callWorkspace(t, s, dev, scoped(workspace.Input{Action: "task_claim", TaskID: task.ID, Version: task.Version, Agent: "claude", SessionRef: "claude-0123456789abcdef"})).Task
	rejectWorkspace(t, s, next, scoped(workspace.Input{Action: "task_claim", TaskID: task.ID, Version: task.Version, Agent: "codex"}), "duplicate live claim")
	task = callWorkspace(t, s, dev, scoped(workspace.Input{Action: "task_checkpoint", TaskID: task.ID, Version: task.Version, Checkpoint: "Issuer patch is on fix/auth", NextStep: "Run both acceptance checks", Branch: "fix/auth", SessionRef: "claude-0123456789abcdef"})).Task
	task = callWorkspace(t, s, dev, scoped(workspace.Input{Action: "task_release", TaskID: task.ID, Version: task.Version})).Task
	packet := callWorkspace(t, s, next, scoped(workspace.Input{Action: "task_get", TaskID: task.ID})).Task
	if packet.Owner != "" || packet.State != "ready" || packet.Checkpoint != "Issuer patch is on fix/auth" || packet.NextStep != "Run both acceptance checks" || packet.Branch != "fix/auth" {
		t.Fatal("handoff lost the portable work state")
	}
	task = callWorkspace(t, s, next, scoped(workspace.Input{Action: "task_claim", TaskID: task.ID, Version: packet.Version, Agent: "codex", SessionRef: "codex-abcdef0123456789"})).Task
	rejectWorkspace(t, s, dev, scoped(workspace.Input{Action: "task_checkpoint", TaskID: task.ID, Version: task.Version, Checkpoint: "Old attempt", NextStep: "Overwrite"}), "previous owner write")

	t.Log("Incomplete evidence stays visible; only an independent reviewer can accept complete evidence")
	task = callWorkspace(t, s, next, scoped(workspace.Input{Action: "task_submit", TaskID: task.ID, Version: task.Version, Revision: "revision-1", Artifact: "https://github.com/mcp-runtime/cully/pull/19"})).Task
	approve := scoped(workspace.Input{Action: "task_approve", TaskID: task.ID, Version: task.Version, Revision: task.Revision})
	rejectWorkspace(t, s, lead, approve, "missing evidence")
	packet = callWorkspace(t, s, lead, scoped(workspace.Input{Action: "task_get", TaskID: task.ID})).Task
	if packet.Version != task.Version || packet.State != "review" || len(packet.Evidence) != 0 {
		t.Fatal("failed approval changed the review packet")
	}
	inbox := callWorkspace(t, s, lead, scoped(workspace.Input{Action: "inbox"})).Tasks
	if len(inbox) != 1 || inbox[0].ID != task.ID {
		t.Fatal("review packet missing from maintainer inbox")
	}
	evidence := []workspace.Evidence{{Criterion: 0, Check: "valid issuer", Status: "pass", Revision: "revision-1", ObservedAt: time.Now().Add(-time.Second), Source: "verified"}, {Criterion: 1, Check: "wrong issuer", Status: "pass", Revision: "revision-1", ObservedAt: time.Now().Add(-time.Second)}}
	task = callWorkspace(t, s, next, scoped(workspace.Input{Action: "task_submit", TaskID: task.ID, Version: task.Version, Revision: "revision-1", Artifact: "https://github.com/mcp-runtime/cully/pull/19", Evidence: evidence})).Task
	if task.Evidence[0].Source != "reported" {
		t.Fatal("client falsely attested independent verification")
	}
	approve.Version = task.Version
	rejectWorkspace(t, s, next, approve, "current owner self approval")
	rejectWorkspace(t, s, dev, approve, "previous contributor approval")
	stale := approve
	stale.Version--
	rejectWorkspace(t, s, lead, stale, "stale task version")
	stale = approve
	stale.Revision = "revision-2"
	rejectWorkspace(t, s, lead, stale, "different revision")
	task = callWorkspace(t, s, lead, approve).Task
	if task.State != "done" || task.ApprovedVersion != task.Version || len(task.Attempts) != 2 || task.Attempts[0].Agent != "claude" || task.Attempts[1].Agent != "codex" {
		t.Fatal("review or cross-agent history lost")
	}

	t.Log("Lessons remain private until publication, and withdrawing a source removes adopted guidance")
	lesson := callWorkspace(t, s, next, scoped(workspace.Input{Action: "learning_draft", TaskID: task.ID, Lesson: "Validate issuer before using identity", AppliesWhen: "OAuth sign-in", Limitations: "Single provider"})).Learning
	if len(callWorkspace(t, s, lead, scoped(workspace.Input{Action: "lessons"})).Learnings) != 0 {
		t.Fatal("private draft leaked")
	}
	lesson = callWorkspace(t, s, next, scoped(workspace.Input{Action: "learning_publish", LearningID: lesson.ID, Version: lesson.Version})).Learning
	callWorkspace(t, s, lead, scoped(workspace.Input{Action: "playbook_adopt", LearningID: lesson.ID, Version: lesson.Version, Steps: []string{"Run wrong issuer test"}}))
	shared := callWorkspace(t, s, dev, scoped(workspace.Input{Action: "lessons"}))
	if len(shared.Learnings) != 1 || len(shared.Playbooks) != 1 {
		t.Fatal("published learning or playbook missing")
	}
	callWorkspace(t, s, next, scoped(workspace.Input{Action: "learning_unshare", LearningID: lesson.ID, Version: lesson.Version}))
	shared = callWorkspace(t, s, dev, scoped(workspace.Input{Action: "lessons"}))
	if len(shared.Learnings) != 0 || len(shared.Playbooks) != 0 {
		t.Fatal("withdrawn source remained shared")
	}

	t.Log("The CLI reads the same persisted workspace")
	input, err := json.Marshal(scoped(workspace.Input{Action: "board"}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(s.ctx, s.cli, "workspace", "--mcp-url", s.endpoint)
	token := s.token(t, "lead", "tools:read", s.issuer, s.endpoint, time.Now().Add(time.Hour))
	cmd.Env = append(append([]string{}, s.env...), "CULLY_WORKSPACE_TOKEN="+token)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workspace CLI: %v", err)
	}
	if strings.Contains(string(output), token) {
		t.Fatal("CLI exposed its access token")
	}
	var cliResult memory.Result
	if err = json.Unmarshal(output, &cliResult); err != nil || cliResult.Workspace == nil || len(cliResult.Workspace.Tasks) != 1 || cliResult.Workspace.Tasks[0].State != "done" {
		t.Fatalf("CLI did not return persisted board: %v", err)
	}

	t.Log("Concurrent claims through MCP have one winner and one conflict")
	raceTask := callWorkspace(t, s, lead, scoped(workspace.Input{Action: "task_create", Name: "Claim race", Criteria: []string{"Only one owner"}})).Task
	var wg sync.WaitGroup
	results := make(chan *sdk.CallToolResult, 2)
	errors := make(chan error, 2)
	start := make(chan struct{})
	for _, client := range []*sdk.ClientSession{dev, next} {
		wg.Go(func() {
			<-start
			result, err := client.CallTool(s.ctx, &sdk.CallToolParams{Name: "cully_workspace_write", Arguments: scoped(workspace.Input{Action: "task_claim", TaskID: raceTask.ID, Version: raceTask.Version, Agent: "codex"})})
			results <- result
			errors <- err
		})
	}
	close(start)
	wg.Wait()
	wins, rejected := 0, 0
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result == nil {
			t.Fatal("missing claim response")
		}
		if result.IsError {
			content, _ := json.Marshal(result.Content)
			if !strings.Contains(string(content), memory.ErrConflict.Error()) {
				t.Fatalf("claim failed without a version conflict: %s", content)
			}
			rejected++
		} else {
			wins++
		}
	}
	if wins != 1 || rejected != 1 {
		t.Fatalf("claim race: wins=%d rejected=%d", wins, rejected)
	}
	claimed := callWorkspace(t, s, lead, scoped(workspace.Input{Action: "task_get", TaskID: raceTask.ID})).Task
	if claimed.Version != 2 || len(claimed.Attempts) != 1 {
		t.Fatal("duplicate claim persisted")
	}

	otherProject := callWorkspace(t, s, lead, workspace.Input{Action: "project_create", TeamID: team, Name: "Private project", Repository: "https://github.com/mcp-runtime/cully"}).Project.ID
	rejectWorkspace(t, s, dev, workspace.Input{Action: "task_get", TeamID: team, ProjectID: otherProject, TaskID: task.ID}, "direct ID crossing project boundary")
	callWorkspace(t, s, lead, workspace.Input{Action: "team_member", TeamID: team, Principal: devID, Role: "remove"})
	rejectWorkspace(t, s, dev, scoped(workspace.Input{Action: "board"}), "removed member read")
	rejectWorkspace(t, s, dev, scoped(workspace.Input{Action: "task_create", Name: "Denied", Criteria: []string{"No writes"}}), "removed member write")
	var count int
	if err := s.pool.QueryRow(s.ctx, "SELECT count(*) FROM cully_entries").Scan(&count); err != nil || count != 0 {
		t.Fatalf("Team workflow wrote private memory: count=%d err=%v", count, err)
	}
	if err := s.pool.QueryRow(s.ctx, "SELECT count(*) FROM cully_workspace_events WHERE team_id=$1::uuid AND action='task_claim'", team).Scan(&count); err != nil || count != 3 {
		t.Fatalf("failed claim affected audit: count=%d err=%v", count, err)
	}
	// Team membership never grants access to a person's Solo memory.
	entry := callTool(t, s.ctx, next, "cully_log", memory.LogInput{Summary: "Private E2E memory", Assistant: "codex"}).Entry
	if entry == nil {
		t.Fatal("private memory write failed")
	}
	if got := callTool(t, s.ctx, lead, "cully_get", memory.IDInput{EntryID: entry.ID}).Entry; got != nil {
		t.Fatal("team maintainer read private memory")
	}
	if entries := callTool(t, s.ctx, lead, "cully_search", memory.SearchInput{Query: "Private E2E memory"}).Entries; len(entries) != 0 {
		t.Fatal("team membership leaked private search results")
	}
	if !callTool(t, s.ctx, next, "cully_delete", memory.IDInput{EntryID: entry.ID}).Deleted {
		t.Fatal("private memory cleanup failed")
	}
	t.Log(fmt.Sprintf("Team handoff, review, sharing, revocation and atomic claims verified for %s", team))
}
