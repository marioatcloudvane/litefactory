package turn

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// The candidate is authored by the factory, not the agent, so nothing else
// checks it before it reaches the owner. It has to clear the same result
// validation and the same failure-kind guard a published candidate clears.
func TestResearchFailureCandidateIsPublishable(t *testing.T) {
	scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "research", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
	for _, tc := range []struct {
		name  string
		req   Request
		cause error
	}{
		{"diagnostic", Request{}, errors.New("research node result was rejected: $.result.reason blank")},
		{"no cause", Request{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := researchFailureCandidate(tc.req, tc.cause)
			if d := agentresult.ValidateResult(candidate, scope); !d.Valid || !d.Complete {
				t.Fatalf("candidate is not a valid result: %+v", d.Diagnostics)
			}
			// The guard in ReportResearchCandidate for kind "failure".
			if candidate.Outcome == "completed" || candidate.Reason == "" {
				t.Fatalf("candidate fails the failure-kind guard: %+v", candidate)
			}
			if len(candidate.Questions) != 0 {
				t.Error("a failed attempt must not ask the owner anything")
			}
		})
	}
}

// The shipped graph is the one that has to admit. If its failure edge stops
// validating, Research silently goes back to reporting nothing on failure.
func TestShippedResearchGraphDeclaresAFailurePublication(t *testing.T) {
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsource.CaptureResearch(t.Context(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.ValidateResearchControlGraph(definition.Profile); err != nil {
		t.Fatalf("shipped graph does not validate: %v", err)
	}
	target, declared := graphexec.Decide(definition.Profile, "investigate", "failed")
	if !declared {
		t.Fatal("shipped graph declares no failure edge from investigate")
	}
	node, ok := definition.Profile.Node(target)
	if !ok || node.Action != ResearchCommentAction || node.Attrs["lf_kind"] != "failure" {
		t.Fatalf("failure edge targets %q, which is not a failure comment", target)
	}
	next, exists := graphexec.Decide(definition.Profile, target, "published")
	terminal, found := definition.Profile.Node(next)
	if !exists || !found || !terminal.Terminal {
		t.Fatalf("failure publication at %q does not end the graph", target)
	}
}
