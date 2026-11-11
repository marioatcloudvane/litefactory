package turn

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// SpikeWorkflowPolicy supplies Spike's response words, result decoder and
// comment texts to the shared graph mechanisms. Spike is one conversational
// agent: the factory publishes what it submitted and, when it asked, waits for
// the owner and resumes the same agent. All behaviour lives in the role's
// Markdown; the flow lives in the graph file.
func SpikeWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Mode:                      agent.ModeSpike,
		Control:                   SpikeControlPolicy(),
		Decode:                    agentresult.DecodeSpikeNode,
		Contract:                  validateResearchComment,
		Render:                    renderSpikeComment,
		NextStep:                  spikeNextStep,
		Failure:                   spikeFailureCandidate,
		Questions:                 true,
		AcceptAfterProcessFailure: true,
	}
}

// SpikeControlPolicy is Spike's closed response vocabulary: ask or done.
func SpikeControlPolicy() graphexec.ControlPolicy {
	return graphexec.ControlPolicy{Mode: agent.ModeSpike, Responses: []string{"ask", "done"}, Defaults: []string{"ask", "done"}, Ask: "ask"}
}

// SpikeSpecs is the closed registry for prelaunch built-in asset validation.
func SpikeSpecs() (graphexec.Registry, error) {
	return WorkflowSpecs(SpikeWorkflowPolicy())
}

// spikeFailureCandidate authors the owner-facing account of a failed Spike step.
// It claims no findings: the outcome is blocked and the reason carries the cause.
func spikeFailureCandidate(req Request, cause error) agentresult.Result {
	reason := "Spike failed before it produced a reportable result."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = boundedFailureReason(strings.TrimSpace(cause.Error()))
	}
	request := strings.TrimSpace(req.Trigger.Body)
	// A stage the factory continued has no comment of its own to quote.
	if request == "" || store.IsGraphTrigger(req.Trigger.CommentID) {
		request = "The owner's Spike request is unavailable on this attempt."
	}
	return agentresult.Result{
		Outcome: "blocked",
		Comment: "Spike did not finish. No answer is claimed. The reason is recorded below; comment again to start a fresh attempt.",
		Reason:  reason,
		Payload: agentresult.Payload{Spike: &agentresult.Spike{Request: request}},
	}
}

// renderSpikeComment publishes the agent's own message and questions.
func renderSpikeComment(candidate agentresult.Result, next string, _ naming.Ref, _ string) string {
	return renderResearchComment(candidate, next, nil)
}

func spikeNextStep(profile *graphexec.Profile, node, kind string) string {
	switch kind {
	case "question":
		return "reply on this issue to continue; the same Spike picks up where it left off. Nothing runs until you reply."
	case "final":
		return "this Spike is finished. Reply on this issue to ask more or to have something changed; the next Spike starts from this one. To take it further, comment `/research <what to look into>` or `/plan <what to plan>`."
	}
	return researchNextStep(profile, node, kind)
}
