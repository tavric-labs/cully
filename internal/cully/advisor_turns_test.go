package cully

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAdvisorWindowsCountPromptsDeduplicateStopsAndCarryProvenance(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CULLY_ANALYZE_CADENCE", "")
	for _, agent := range []string{"claude", "codex", "cursor"} {
		in := continuityHookInput{SessionID: "native-" + agent}
		for i := 1; i <= 24; i++ {
			in.TurnID = fmt.Sprint(i)
			noteAdvisorPrompt(agent, in)
			noteAdvisorPrompt(agent, in)
			n, window := completeAdvisorWindow(agent, in)
			if n != i {
				t.Fatalf("%s counted duplicate prompt: %d want %d", agent, n, i)
			}
			if (window != "") != (i%12 == 0) {
				t.Fatalf("%s analysis fired at prompt %d: %q", agent, i, window)
			}
			if window != "" && (!strings.Contains(window, "session_ref="+continuitySessionRef(agent, in)) || !strings.Contains(window, "agent_session_id=native-"+agent) || !strings.Contains(window, "cadence_source=user_prompts")) {
				t.Fatal(window)
			}
			if _, again := completeAdvisorWindow(agent, in); again != "" {
				t.Fatal("duplicate stop repeated analysis")
			}
		}
	}
}

func TestAdvisorWindowCountersDoNotPersistHookText(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	in := continuityHookInput{SessionID: "native", TurnID: "opaque-turn", Text: "private prompt must not be saved", LastAssistantMessage: "private response must not be saved"}
	noteAdvisorPrompt("codex", in)
	completeAdvisorWindow("codex", in)
	b, err := os.ReadFile(advisorTurnsFile(continuitySessionRef("codex", in)))
	if err != nil || strings.Contains(string(b), "private") || strings.Contains(string(b), "opaque-turn") {
		t.Fatalf("hook content leaked: %s %v", b, err)
	}
}
