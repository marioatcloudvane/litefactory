package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

const implementNodeInstructions = "\nFactory invocation: private Implement story worker node. Deliver the complete application result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request_id (the shared result helper's preflight, submit and receipt methods are the same route). The accepted submission is this step's result, and the factory derives the step's response from it. Do not print a JSON result: terminal text is never read as a result, so end with a short plain-language note. The result is limited to 1 MiB.\n"

const implementNodeHardRules = "\nResult shapes: when the story is built and its unit tests pass, submit result.outcome completed with no questions and an empty payload.implement.stories; when you cannot finish, submit result.outcome incomplete or blocked with a nonblank result.reason in plain language and no questions. You cannot ask the owner: the factory parks the story with your reason and asks for you. Never commit, push or merge: the factory saves your branch after a completed result. Read the implement-result skill's node-result resource for the worked result of each shape before you submit.\n"

func implementNodeGuidance() string { return implementNodeInstructions + implementNodeHardRules }

// ImplementWorkflowPolicy supplies the Implement graph's response words, result
// decoder and report renderer to the shared graph mechanisms. It carries no flow:
// the order, branches and limits are declared in implement.dot. The factory
// activities and hooks that need a run's context are added when a run is admitted.
func ImplementWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Mode:               agent.ModeImplement,
		Control:            graphexec.ImplementControlPolicy(),
		Specs:              graphexec.ImplementSpecs(),
		Decode:             agentresult.DecodeImplementNode,
		Respond:            implementRespond,
		Guidance:           implementNodeGuidance(),
		Contract:           validateImplementComment,
		Render:             renderImplementComment,
		NextStep:           func(*graphexec.Profile, string, string) string { return "" },
		ValidateCompletion: implementCompletion,
		Handoff:            implementWorkerHandoff,
		ResultPerNode:      true,
	}
}

// ImplementSpecs is the closed registry the packaged Implement graph is
// captured against: the shared actions, the Implement actions and the Implement
// graph checker.
func ImplementSpecs() (graphexec.Registry, error) {
	r, err := WorkflowSpecs(ImplementWorkflowPolicy())
	if err != nil {
		return r, err
	}
	r.Policy = graphexec.ValidateImplementGraph
	return r, nil
}

// implementRespond maps the worker's accepted result to its response: the
// outcome itself. A result that stops short must say why in plain language.
func implementRespond(r agentresult.Result) (string, error) {
	switch r.Outcome {
	case agentresult.ImplementResponseCompleted:
		if len(r.Questions) != 0 {
			return "", errors.New("a completed Implement result carries no questions")
		}
	case agentresult.ImplementResponseIncomplete, agentresult.ImplementResponseBlocked:
		if strings.TrimSpace(r.Reason) == "" || len(r.Questions) != 0 {
			return "", fmt.Errorf("an %s Implement result needs a plain reason and no questions", r.Outcome)
		}
	default:
		return "", fmt.Errorf("outcome %q is not completed, incomplete or blocked", r.Outcome)
	}
	return r.Outcome, nil
}

// implementCompletion adds no check of its own: an Implement result is judged once,
// when it is submitted, against the one story the worker was given.
func implementCompletion(context.Context, store.ResultBinding, agentresult.Result, agentresult.Context) agentresult.Diagnostics {
	return agentresult.Diagnostics{Valid: true, Complete: true}
}

// validateImplementComment judges the one report an Implement graph publishes:
// the batched question about parked stories.
func validateImplementComment(_ store.GraphNodeAttempt, kind string, candidate agentresult.Result, scope agentresult.Context) error {
	if kind != "question" {
		return fmt.Errorf("the Implement graph publishes no %s comment", kind)
	}
	if scope.Mode != string(agent.ModeImplement) {
		return errors.New("an Implement question belongs to an Implement invocation")
	}
	if candidate.Outcome != "incomplete" || len(candidate.Questions) == 0 || strings.TrimSpace(candidate.Reason) == "" || strings.TrimSpace(candidate.Comment) == "" {
		return errors.New("an Implement question is an incomplete result with a comment, a reason and its questions")
	}
	seen := map[string]bool{}
	for _, q := range candidate.Questions {
		if strings.TrimSpace(q.ID) == "" || seen[q.ID] || strings.TrimSpace(q.Text) == "" || !slices.Contains(agentresult.QuestionAudiences, q.Audience) {
			return fmt.Errorf("an Implement question %q needs a unique id, a text and a known audience", q.ID)
		}
		seen[q.ID] = true
	}
	return nil
}

// renderImplementComment lays out the batched question for the owner: what
// happened, one line per parked story, and how to answer.
func renderImplementComment(candidate agentresult.Result, _ string, _ naming.Ref, _ string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(candidate.Comment))
	b.WriteString("\n\n")
	for _, q := range candidate.Questions {
		b.WriteString("- " + strings.TrimSpace(q.Text) + "\n")
	}
	b.WriteString("\n" + strings.TrimSpace(candidate.Reason))
	return b.String()
}
