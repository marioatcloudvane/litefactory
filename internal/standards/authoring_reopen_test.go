package standards_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// authorCompleted drafts and completes one session into its first proposal.
func authorCompleted(t *testing.T, a *standards.Authoring) (standards.AuthoringSession, store.StandardsProposalVersion) {
	t.Helper()
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return authorResponse(false), nil })
	v, e := a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Complete(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	return v, p
}

func TestAuthoringReopenCompletesIntoTheNextProposalVersion(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v, first := authorCompleted(t, a)
	if _, e := a.Answer(t.Context(), v.ID, v.Version, []standards.AuthoringAnswer{{QuestionID: "x", Text: "x"}}); !errors.Is(e, store.ErrStandardsConflict) {
		t.Fatalf("answer on a completed session: %v", e)
	}
	reopened, e := a.Reopen(t.Context(), v.ID, v.Version, "Make the description shorter")
	if e != nil {
		t.Fatal(e)
	}
	if reopened.Status != "draft" || reopened.Version != v.Version+1 {
		t.Fatalf("reopened session: %s v%d", reopened.Status, reopened.Version)
	}
	var prompt string
	a.Runner = authorRunner(func(_ context.Context, c standards.AuthoringCall) ([]byte, error) {
		prompt = c.Prompt
		return []byte(strings.Replace(string(authorResponse(false)), "Use isolated test data.", "Isolate data.", 1)), nil
	})
	next, e := a.Continue(t.Context(), reopened.ID, reopened.Version)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(prompt, "Make the description shorter") {
		t.Fatal("the author did not see the owner's change request")
	}
	second, e := a.Complete(t.Context(), next.ID, next.Version)
	if e != nil {
		t.Fatal(e)
	}
	if second.ProposalID != first.ProposalID || second.Version != 2 || second.Operations[0].Standard.Description != "Isolate data." {
		t.Fatalf("second completion: %+v", second)
	}
	current, e := a.Book.Proposal(t.Context(), first.ProposalID, 0)
	if e != nil || current.Version != 2 || current.ChangeDigest != second.ChangeDigest {
		t.Fatalf("current proposal: %+v %v", current, e)
	}
	shown, e := a.Show(t.Context(), v.ID, 0)
	if e != nil || shown.Status != "completed" || shown.Proposal == nil || shown.Proposal.Version != 2 {
		t.Fatalf("completion link after second completion: %+v %v", shown.Proposal, e)
	}
	if again, e := a.Complete(t.Context(), next.ID, next.Version); e != nil || again.Version != 2 {
		t.Fatalf("second completion retry: %+v %v", again, e)
	}
}

func TestAuthoringReopenRefusedOnceTheProposalIsApprovedOrPublished(t *testing.T) {
	t.Parallel()
	for _, publish := range []bool{false, true} {
		a, _ := authorFixture(t)
		v, p := authorCompleted(t, a)
		approvePublication(t, a.Book, p)
		if publish {
			if _, e := a.Book.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := a.Reopen(t.Context(), v.ID, v.Version, "Make it shorter"); !errors.Is(e, store.ErrStandardsAuthoringSettled) {
			t.Fatalf("reopen after approval (published %v): %v", publish, e)
		}
		shown, e := a.Show(t.Context(), v.ID, 0)
		if e != nil || shown.Status != "completed" || shown.Version != v.Version {
			t.Fatalf("refused reopen changed the session: %s v%d %v", shown.Status, shown.Version, e)
		}
	}
}
