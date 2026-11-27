package cully

import "path/filepath"

// The sources behind what Cully does. Every ready-made agent, job and skill names the ones it rests on, and
// docs/research.md says what each source claims and where it is weak or Cully's own choice. Titles and links
// are kept here so the page can show them beside the thing they support.

type researchSource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Kind  string `json:"kind"` // paper, vendor, or practice: papers are preprints or peer reviewed; vendor and practice posts are not independent evidence
}

var researchSources = map[string]researchSource{
	"anthropic-harness-long":   {"anthropic-harness-long", "Effective harnesses for long-running agents (Anthropic)", "https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents", "vendor"},
	"anthropic-harness-design": {"anthropic-harness-design", "Harness design for long-running apps (Anthropic)", "https://www.anthropic.com/engineering/harness-design-long-running-apps", "vendor"},
	"anthropic-context":        {"anthropic-context", "Effective context engineering for AI agents (Anthropic)", "https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents", "vendor"},
	"anthropic-evals":          {"anthropic-evals", "Demystifying evals for AI agents (Anthropic)", "https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents", "vendor"},
	"anthropic-simple":         {"anthropic-simple", "Building effective agents (Anthropic)", "https://www.anthropic.com/engineering/building-effective-agents", "vendor"},
	"mast":                     {"mast", "Why Do Multi-Agent LLM Systems Fail? (arXiv 2503.13657)", "https://arxiv.org/abs/2503.13657", "paper"},
	"sweagent":                 {"sweagent", "SWE-agent: Agent-Computer Interfaces (arXiv 2405.15793)", "https://arxiv.org/abs/2405.15793", "paper"},
	"swesmith":                 {"swesmith", "SWE-smith (arXiv 2504.21798)", "https://arxiv.org/abs/2504.21798", "paper"},
	"agentsmd":                 {"agentsmd", "Evaluating AGENTS.md (arXiv 2602.11988)", "https://arxiv.org/abs/2602.11988", "paper"},
	"judge":                    {"judge", "Judging LLM-as-a-Judge (arXiv 2306.05685)", "https://arxiv.org/abs/2306.05685", "paper"},
	"ace":                      {"ace", "Agentic Context Engineering (arXiv 2510.04618)", "https://arxiv.org/abs/2510.04618", "paper"},
	"strongdm":                 {"strongdm", "StrongDM Software Factory: holdout scenarios", "https://factory.strongdm.ai/", "practice"},
	"openai-harness":           {"openai-harness", "Harness engineering: Codex in an agent-first world (OpenAI)", "https://openai.com/index/harness-engineering/", "vendor"},
	"spec-driven":              {"spec-driven", "Spec-Driven Development (arXiv 2602.00180)", "https://arxiv.org/abs/2602.00180", "paper"},
	"tokens":                   {"tokens", "How Do AI Agents Spend Your Money? (arXiv 2604.22750)", "https://arxiv.org/abs/2604.22750", "paper"},
}

// researchFor returns the named sources, skipping any it does not know.
func researchFor(ids ...string) []researchSource {
	out := []researchSource{}
	for _, id := range ids {
		if s, ok := researchSources[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

// setupReadyAgents are the agents offered during setup before there is any history to learn from. Each is
// read only, its instructions are short on purpose (extra requirements make agents less successful, per
// agentsmd), and each names the documented failure it exists for. They are proposals: nothing is created until
// the person reviews and approves one.
func setupReadyAgents() []setupAgentSuggestion {
	return []setupAgentSuggestion{
		{ID: "ready-reviewer", Name: "Independent reviewer", Mode: "plan",
			Role: "Looks at a finished change with fresh eyes, runs the project's own checks, and says what is proven and what is only claimed.",
			Instructions: "You review work another agent finished. You are read only: do not edit files.\n" +
				"1. Read the goal and the diff.\n2. Find the project's own checks (README, Makefile, CI) and run them. Quote the command and the result.\n" +
				"3. For each acceptance scenario given, say how you saw it hold, or that you could not.\n4. Report in three parts: proven, claimed but unproven, not checked and why.\n" +
				"Judge from what ran, not from how confident the write-up sounds.",
			Why:     "Agents tend to rate their own work too well, and weak verification is a main failure family in multi-agent traces. A separate reviewer that runs the checks addresses both.",
			Sources: researchFor("anthropic-harness-design", "mast", "judge", "strongdm")},
		{ID: "ready-handoff", Name: "Handoff keeper", Mode: "edit",
			Role: "At a milestone, writes a short progress note and a next step so a fresh session starts from the real state of the work.",
			Instructions: "At a milestone, write PROGRESS.md in ten lines or fewer: goal, what works now, what is half-done or broken (name files), decisions and why, the single next step, the command that shows the state, and what was tried and failed.\n" +
				"Never include a credential, a prompt transcript or file contents. Commit the note with the work.",
			Why:     "Work that spans context windows fails when each session starts blind. A progress file kept with the version history is what carried long-running agent work across sessions.",
			Sources: researchFor("anthropic-harness-long", "anthropic-context", "ace")},
		{ID: "ready-loopbreaker", Name: "Loop breaker", Mode: "plan",
			Role:         "Reads sessions where the same call failed three or more times and writes the one change that would have stopped it.",
			Instructions: "You are read only. Given a session where one call repeated and failed, say in three sentences: what the agent was trying, why the repeat did not help, and the single instruction or tool change that would have stopped it. Do not propose more than one change.",
			Why:          "Repeated identical actions snowball in agent trajectories, and endless repetition is a recurring failure when an agent cannot find the right code.",
			Sources:      researchFor("sweagent", "swesmith")},
	}
}

// setupReadyJobs are command-only background jobs offered during setup. A job runs one fixed command with no
// model, so it costs no tokens. The command comes from what the folder already documents.
func (e *setupEngine) setupReadyJobs() []setupJobSuggestion {
	out := []setupJobSuggestion{}
	for _, r := range e.m.repoList() {
		cmd := setupJobCommandFor(r.Path)
		if cmd == nil {
			continue
		}
		out = append(out, setupJobSuggestion{ID: "ready-checks-" + filepath.Base(r.Path), Name: filepath.Base(r.Path) + " nightly checks", Command: uiShowArgv(cmd), Schedule: "02:30", Folder: r.Path,
			Why:     "Agents often change files and never run the tests afterwards. A nightly run of the project's own test command catches that without using a model.",
			Sources: researchFor("mast", "anthropic-simple")})
		if len(out) >= 3 {
			break
		}
	}
	return out
}
