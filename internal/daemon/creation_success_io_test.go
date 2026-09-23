package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
)

func successfulPreparationControl(t *testing.T, f *publicationIO, id int64, title string) turn.OwnerGrantControl {
	t.Helper()
	raw, err := json.Marshal(submission.CreationEnvelope{Version: 1, Repository: "owner/api", SourceTask: f.session.TaskRef, Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: title, Body: "Original exact content"}}})
	publicationMust(t, err)
	return turn.OwnerGrantControl{Repository: "owner/api", SourceTask: f.session.TaskRef, Mode: "research", Body: "/mcp-create prepare " + string(raw), CommentID: id, ActorID: 10, CreatedAt: f.now}
}

// AC-007: both a lost POST response and the admission-to-report crash gap
// recover the original successful source after restart and mutable controls change.
func TestCreationSuccessIO_PreparationRestart(t *testing.T) {
	for _, gap := range []bool{false, true} {
		name := "lost-response"
		if gap {
			name = "before-report-with-unrelated-legacy-reply"
		}
		t.Run(name, func(t *testing.T) {
			f := newPublicationIO(t)
			task := rejectionTask(t, f)
			c := successfulPreparationControl(t, f, 70, "Original prepared issue")
			if gap {
				_, err := f.runtime.PrepareCreationProposal(t.Context(), c)
				publicationMust(t, err)
				f.bodies = []string{"Old unrelated reply\n\n<!-- lf:reply task=github:owner/api#278 -->"}
			} else {
				f.lost = true
				publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, c.Body)))
			}
			source := store.CreationSource{Repository: c.Repository, SourceTask: c.SourceTask, CommentID: 70, OwnerID: 10}
			before, err := f.db.GetCreationPreparation(t.Context(), source)
			publicationMust(t, err)
			rejectionWatermark(t, f, 0)
			var retainedBody, retainedKey string
			if !gap {
				delivery, found, e := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 70)
				publicationMust(t, e)
				if !found || !delivery.PossiblySent || delivery.Completed {
					t.Fatalf("missing uncertain presentation: %+v", delivery)
				}
				retainedBody, retainedKey = delivery.Body, delivery.Key
			}
			// A newer proposal must remain eligible/current when the old source resumes.
			f.now = f.now.Add(time.Minute)
			newer, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 71, "Newer prepared issue"))
			publicationMust(t, err)
			rejectionRestart(t, f)
			task.tickAt = f.now
			task.issue.Labels = []string{"Mode: Implement"}
			f.runtime.CreationAuthorizer, err = turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: f.cs, Enabled: false, OwnerID: 10, BotID: 20})
			publicationMust(t, err)
			f.lost = false
			publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "edited ordinary instruction")))
			rejectionWatermark(t, f, 70)
			after, err := f.db.GetCreationPreparation(t.Context(), source)
			publicationMust(t, err)
			if after.Hash != before.Hash || string(after.Envelope) != string(before.Envelope) {
				t.Fatal("original proposal changed")
			}
			if f.posts != 1 || f.creationPosts != 0 {
				t.Fatalf("effects posts=%d creations=%d", f.posts, f.creationPosts)
			}
			if !strings.Contains(f.bodies[len(f.bodies)-1], before.Hash) || strings.Contains(f.bodies[len(f.bodies)-1], newer.Hash) {
				t.Fatal("presentation not original")
			}
			if !gap {
				delivery, e := f.db.GetReportDelivery(t.Context(), retainedKey)
				publicationMust(t, e)
				if !delivery.Completed || delivery.CommentID == 0 || delivery.Body != retainedBody {
					t.Fatalf("unstable delivery %+v", delivery)
				}
			}
			publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "another edit")))
			if f.posts != 1 {
				t.Fatal("recovery duplicated presentation")
			}
			if _, _, err = f.cs.EligibleCreationIntent(t.Context(), f.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("preparation created authority: %v", err)
			}
		})
	}
}

type successConsumptionFault struct {
	*turn.Reporter
	fail bool
}

func (r *successConsumptionFault) Consume(ctx context.Context, session, comment int64) (store.Watermark, error) {
	if r.fail {
		return store.Watermark{}, errors.New("consumption unavailable")
	}
	return r.Reporter.Consume(ctx, session, comment)
}

// AC-008: a successful approval remains historical admission across failed
// consumption, expiry, supersession, source edit and disabled creation.
func TestCreationSuccessIO_ApprovalConsumptionRestart(t *testing.T) {
	f := newPublicationIO(t)
	task := rejectionTask(t, f)
	prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Approved issue"))
	publicationMust(t, err)
	realReporter, ok := f.d.reporter.(*turn.Reporter)
	if !ok {
		t.Fatal("fixture does not use the durable reporter")
	}
	reporter := &successConsumptionFault{Reporter: realReporter, fail: true}
	f.d.reporter = reporter
	c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	source := store.CreationSource{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: 70, OwnerID: 10}
	before, err := f.db.GetCreationOwnerIntent(t.Context(), source)
	publicationMust(t, err)
	rejectionWatermark(t, f, 0)
	f.now = f.now.Add(time.Minute)
	_, err = f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 71, "Superseding issue"))
	publicationMust(t, err)
	rejectionRestart(t, f)
	task.tickAt = f.now
	task.issue.Labels = nil
	f.runtime.CreationAuthorizer, err = turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: f.cs, Enabled: false, OwnerID: 10, BotID: 20})
	publicationMust(t, err)
	reporter.fail = false
	c.Body = "edited ordinary instruction"
	c.UpdatedAt = f.now
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	rejectionWatermark(t, f, 70)
	after, err := f.db.GetCreationOwnerIntent(t.Context(), source)
	publicationMust(t, err)
	if after != before || f.posts != 0 || f.creationPosts != 0 {
		t.Fatalf("changed admission/effects: %d -> %d posts=%d creations=%d", before, after, f.posts, f.creationPosts)
	}
	if _, _, err = f.cs.EligibleCreationIntent(t.Context(), f.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("recovery renewed authority: %v", err)
	}
	if _, err = f.db.GetCreationRejection(t.Context(), f.session.ID, 70); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("false rejection: %v", err)
	}
}
