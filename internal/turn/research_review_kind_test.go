package turn

import (
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// The review gate rode on the question kind when it was first built, and could
// never publish: the guard for a question demands an incomplete result with
// questions, while a review carries exactly what a final does. A live shaping
// stage iterated twice, produced a scope and then failed on the way out. The
// kinds are separate now, and each is checked against the shape it carries.
func TestReviewAndQuestionCarryDifferentResults(t *testing.T) {
	t.Parallel()
	for kind, waits := range map[string]bool{"question": true, "review": true, "final": false, "failure": false} {
		if !store.GraphCommentKind(kind) {
			t.Errorf("%q is not a retained publication kind", kind)
		}
		if store.GraphCommentWaits(kind) != waits {
			t.Errorf("%q waits=%v, want %v", kind, store.GraphCommentWaits(kind), waits)
		}
	}
	if store.GraphCommentKind("draft") {
		t.Error("an unknown kind must not be retained")
	}
}

// The shipped graph has to agree: a stage that says done reaches a review, one
// that asks reaches a question, and both rest on the owner.
func TestShippedGraphRoutesDoneToAReview(t *testing.T) {
	t.Parallel()
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	d, err := teamsourcetest.CaptureResearch(t.Context(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.ValidateResearchControlGraph(d.Profile); err != nil {
		t.Fatalf("shipped graph does not validate: %v", err)
	}
	next, ok := graphexec.Decide(d.Profile, "shape", "success")
	if !ok {
		t.Fatal("the shaping stage has no success edge")
	}
	for response, wantKind := range map[string]string{"ask": "question", "done": "review", "approved": "final"} {
		reached, err := graphexec.ResearchResponseReaches(d.Profile, next, response)
		if err != nil {
			t.Fatalf("%s: %v", response, err)
		}
		node, found := d.Profile.Node(reached)
		if !found || node.Attrs["lf_kind"] != wantKind {
			t.Errorf("response %q reaches %q of kind %q, want %q", response, reached, node.Attrs["lf_kind"], wantKind)
		}
	}
	// The owner is told which moment this is.
	if step := researchNextStep(d.Profile, "review", "review"); !strings.Contains(step, "`done`") || !strings.Contains(step, "`/plan plan the approved scope above into stories`") {
		t.Errorf("the review gate does not offer the owner its moves: %q", step)
	}
}
