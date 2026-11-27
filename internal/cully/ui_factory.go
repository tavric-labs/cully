package cully

import (
	"errors"
	"strings"
)

// Factory mode is a way of starting a task that follows how software factories run agents: a person states the
// intent and the observable scenarios, the agent specifies, plans, builds and verifies, and a reviewer that holds
// the scenarios decides whether it is done. The pieces already exist in Cully (the keep-going loop, a judge, named
// reviewers, held-back checks); this turns them on together and adds a short brief. It rests on StrongDM's
// holdout scenarios, the spec-driven development flow, and Anthropic's separate evaluator. See docs/research.md.

const uiFactoryBrief = `

Work in factory mode:
1. Write a short spec first: the intent, what is out of scope, and the observable scenarios that would show it is done. If an open question would change the plan, ask it before building.
2. Plan small tasks, each finishable and checkable alone.
3. Build one task at a time and commit each.
4. After your last edit, run the project's own checks and quote the result.
5. Finish by saying what is proven, what is only claimed, and what you did not check.
A reviewer holds acceptance checks you cannot see. Satisfy the real goal, not a guess at the checks.`

const uiFactoryReviewer = "independent reviewer"

// applyFactory turns a task into a factory task. It needs at least one acceptance scenario, because a factory
// without a definition of done has nothing to verify against.
func (m *uiManager) applyFactory(req *uiStartRequest) error {
	if strings.TrimSpace(req.DoneWhen) == "" {
		return errors.New("factory mode needs at least one acceptance scenario in Done when: a thing a person could observe that shows the work is done")
	}
	if req.Mode == "plan" {
		return errors.New("factory mode builds and verifies, so it cannot be plan only")
	}
	req.Mode = "edit"
	req.Loop, req.Context, req.Holdout = true, true, true
	req.Prompt = strings.TrimSpace(req.Prompt) + uiFactoryBrief
	if len(req.Helpers) == 0 { // the independent reviewer, when the person has approved one, takes the finished work
		for _, a := range m.identityList() {
			if a.Approved && strings.EqualFold(strings.TrimSpace(a.Name), uiFactoryReviewer) {
				req.Helpers = []string{a.ID}
				break
			}
		}
	}
	return nil
}
