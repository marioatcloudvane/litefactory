package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// The shipped Spike graph routes a finished result through factory delivery:
// done -> prepare (push) -> review (ship) -> deliver (merge) -> close -> complete.
// An unchanged answer posts the final report; a changed candidate returns to
// the agent for fresh checks.
func TestShippedSpikeGraphRoutesThroughShipReview(t *testing.T) {
	specs, err := SpikeSpecs()
	mustProgress(t, err)
	for _, team := range []string{"go-development", "python-fastapi", "dotnet-web", "swift-ios"} {
		t.Run(team, func(t *testing.T) {
			def, err := teamsourcetest.CaptureTeam(t.Context(), team, agent.WorkflowSpike, specs)
			mustProgress(t, err)
			p := def.Profile
			step := func(from, outcome, want string) graphexec.Node {
				t.Helper()
				next, ok := graphexec.Decide(p, from, outcome)
				if !ok || next != want {
					t.Fatalf("%s/%s routes to %q (%t), want %q", from, outcome, next, ok, want)
				}
				node, found := p.Node(next)
				if !found {
					t.Fatalf("node %q missing", next)
				}
				return node
			}
			delivery := func(n graphexec.Node, operation string) {
				t.Helper()
				if n.Action != graphexec.DeliverAction || n.Attrs["lf_operation"] != operation || n.Attrs["lf_source"] != "spike" {
					t.Fatalf("%s is not lf_deliver %s from spike: %+v", n.Name, operation, n)
				}
			}
			delivery(step("spike", "done", "prepare"), "push")
			review := step("prepare", "ready", "review")
			if review.Action != "lf_ask" || review.Attrs["lf_kind"] != "review" || review.Attrs["lf_approve_reply"] != "ship" {
				t.Fatalf("review is not the ship review: %+v", review)
			}
			if waiting := step("review", "published", "waiting"); !waiting.Terminal {
				t.Fatal("published review must wait at a terminal")
			}
			delivery(step("review", "approved", "deliver"), "merge")
			delivery(step("deliver", "merged", "close"), "close")
			if complete := step("close", "completed", "complete"); !complete.Terminal {
				t.Fatal("confirmed delivery must complete")
			}
			if report := step("prepare", "unchanged", "report"); report.Attrs["lf_kind"] != "final" {
				t.Fatalf("unchanged answer must post the final report: %+v", report)
			}
			for _, from := range []string{"prepare", "deliver", "close"} {
				step(from, "revalidate", "spike")
			}
			step("spike", "ask", "ask")
		})
	}
}
