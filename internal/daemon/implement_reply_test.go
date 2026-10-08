package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The turn regressions exercise actual target selection. Here the executor's
// semantic result drives the real daemon, reporter, delivery journal and cursor.
func TestImplementUnusableReplyDeliveryAndAuthentication(t *testing.T) {
	for _, body := range []string{"story-001: change the running story", "No parked stories remain; please retry"} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Implement the feature", "Mode: Implement")
			f.settle()
			f.bootstrapIssues(f.repo, testIssue)
			f.turns.set(func(_ context.Context, req turn.Request) (turn.Result, error) {
				if req.Mode != agent.ModeImplement {
					t.Errorf("mode = %s", req.Mode)
				}
				return turn.Result{}, turn.ErrImplementReplyNotApplied
			})
			f.forge.addComment(testIssue, 90, stranger(), body)
			f.settle()
			if _, ran := turnFor(f, 90); ran {
				t.Fatal("non-owner reached reply execution")
			}
			f.forge.addComment(testIssue, 91, owner(), body)
			lost := strings.HasPrefix(body, "No parked")
			if lost {
				f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "delivery failed"})
					return true
				}
			}
			f.settle()
			if lost {
				if f.watermark() >= 91 {
					t.Fatal("failed refusal delivery consumed input")
				}
				pending, found, err := f.db.FindPendingReportDelivery(t.Context(), f.session().ID, 91)
				if err != nil || !found || !strings.Contains(pending.Body, "This Implement comment did not select any parked stories") {
					t.Fatalf("missing durable refusal: %+v %v %v", pending, found, err)
				}
				f.forge.onPostComment = nil
				// A lost response is reconciled only against a trusted bot receipt.
				f.forge.addComment(testIssue, 900040, bot(), pending.Body)
			}
			if !settleUntilHandled(f, 91, 6) {
				t.Fatalf("refusal never delivered: %s", f.logs)
			}
			for range 2 {
				f.clock.advance(time.Minute)
				f.settle()
			}
			posted, _, _ := f.forge.snapshot()
			count := 0
			for _, text := range posted {
				if strings.Contains(text, "This Implement comment did not select any parked stories") {
					count++
				}
			}
			want := 1
			if lost {
				want = 0 // the restored receipt must not be reposted
			}
			if count != want {
				t.Fatalf("refusals = %d; posts=%v", count, posted)
			}
			f.forge.addComment(testIssue, 900041, owner(), "/unknown-command story-001")
			if !settleUntilHandled(f, 900041, 6) {
				t.Fatal("command handler did not consume its refusal")
			}
			if _, ran := turnFor(f, 900041); ran {
				t.Fatal("command became ordinary guidance")
			}
		})
	}
}

func TestImplementTransientReplyFailureIsNotRefusedOrConsumed(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Implement the feature", "Mode: Implement")
	f.settle()
	f.bootstrapIssues(f.repo, testIssue)
	f.turns.set(func(context.Context, turn.Request) (turn.Result, error) {
		return turn.Result{}, errors.New("transient store failure")
	})
	f.forge.addComment(testIssue, 91, owner(), "story-001: retry")
	f.settle()
	if f.watermark() >= 91 {
		t.Fatal("transient execution failure consumed input")
	}
	posted, _, _ := f.forge.snapshot()
	for _, text := range posted {
		if strings.Contains(text, "did not select any parked stories") {
			t.Fatal("transient error became semantic refusal")
		}
	}
}

func TestImplementClaimedQuestionConflictIsAnsweredAndConsumed(t *testing.T) {
	// Reuse the durable question fixture with an Implement definition and label.
	b := newBetweenQuestions(t)
	f := b.f
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("b", 64), CreatedAt: b.at})
	betweenCheck(t, err)
	b.instance, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: b.session.ID, DefinitionID: definition.ID, CurrentNode: "shape", StartedAt: b.at, NextDueAt: b.at})
	betweenCheck(t, err)
	b.shapeAsks(t)
	f.issueWithLabels("Implement the feature", "Mode: Implement")
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: b.instance.ID, OwnerID: ownerID, AuthorID: ownerID, CommentID: 140, QuestionCommentID: betweenQuestionComment, TaskRef: b.session.TaskRef, SourceRef: b.session.TaskRef, Mode: "implement", Workflow: "implement", DeliveryKey: "shape-question", Body: "story-001: retry", At: b.at})
	betweenCheck(t, err)
	f.forge.addComment(testIssue, 150, owner(), "story-002: retry too")
	if !settleUntilHandled(f, 150, 6) {
		t.Fatalf("claimed wait refusal not consumed: %s", f.logs)
	}
	if _, ran := turnFor(f, 150); ran {
		t.Fatal("conflicting reply executed")
	}
	posted, _, _ := f.forge.snapshot()
	count := 0
	for _, text := range posted {
		if strings.Contains(text, "already handling another reply or question") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("refusal count = %d, posts=%v", count, posted)
	}
}
