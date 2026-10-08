package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
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
			creationfixture.NoCreationIntents(t, store.DSN(f.db.Path()), f.session.TaskRef)
		})
	}
}

// AC-008: a successful approval remains historical admission across failed
// consumption, expiry, supersession, source edit and disabled creation.
func TestCreationSuccessIO_ApprovalConsumptionRestart(t *testing.T) {
	f := newPublicationIO(t)
	task := rejectionTask(t, f)
	prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Approved issue"))
	publicationMust(t, err)
	f.lost = true
	c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	source := store.CreationSource{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: 70, OwnerID: 10}
	before, err := f.db.GetCreationAcknowledgment(t.Context(), source)
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
	f.lost = false
	c.Body = "edited ordinary instruction"
	c.UpdatedAt = f.now
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	rejectionWatermark(t, f, 70)
	after, err := f.db.GetCreationAcknowledgment(t.Context(), source)
	publicationMust(t, err)
	// The first approval published (uncertainly: the fixture forge truncates
	// its reply); the disabled, relabelled restart must not publish again.
	if after != before || f.posts != 1 || f.creationPosts != 1 {
		t.Fatalf("changed admission/effects: %+v -> %+v posts=%d creations=%d", before, after, f.posts, f.creationPosts)
	}
	if pub, _, err := f.cs.ClaimOwnerPublication(t.Context(), after.IntentID, true); err != nil || pub.State != "uncertain" {
		t.Fatalf("recovery changed retained publication: %+v %v", pub, err)
	}
	if _, err = f.db.GetCreationRejection(t.Context(), f.session.ID, 70); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("false rejection: %v", err)
	}
}

// AC-005/006/008/009/010/012: an admitted source remains recovery debt before
// journal retention; uncertainty must reconcile the exact historical reply.
func TestCreationAcceptanceIO_AdmissionGapAndReceiptRecovery(t *testing.T) {
	for _, phase := range []string{"admitted-before-retention", "lost-response", "lost-local-confirmation"} {
		t.Run(phase, func(t *testing.T) {
			f := newPublicationIO(t)
			task := rejectionTask(t, f)
			prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Original approval"))
			publicationMust(t, err)
			c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
			source := store.CreationSource{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: 70, OwnerID: 10}
			if phase == "admitted-before-retention" {
				_, err = f.runtime.AdmitCreationApproval(t.Context(), turn.OwnerGrantControl{Repository: source.Repository, SourceTask: source.SourceTask, CommentID: 70, ActorID: 10, Mode: "research", CreatedAt: c.CreatedAt, Body: c.Body})
				publicationMust(t, err)
				f.bodies = []string{"Unrelated legacy reply\n\n<!-- lf:reply task=github:owner/api#278 -->"}
			} else {
				f.lost = phase == "lost-response"
				if phase == "lost-local-confirmation" {
					original := f.server.Config.Handler
					f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						original.ServeHTTP(w, r)
						// Only the reply comment's confirmation is lost; the
						// issue publication before it records normally.
						if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments") {
							if e := f.db.Close(); e != nil {
								t.Error(e)
							}
						}
					})
				}
				publicationMust(t, f.d.handleComment(t.Context(), task, c))
				if phase == "lost-local-confirmation" {
					reopened, e := store.Open(t.Context(), f.path)
					publicationMust(t, e)
					*f.db = *reopened
				}
			}
			before, err := f.db.GetCreationAcknowledgment(t.Context(), source)
			publicationMust(t, err)
			if before.Hash != prepared.Hash || before.Relationship != "linked" || !before.CreatedAt.Equal(c.CreatedAt) || !before.ExpiresAt.Equal(c.CreatedAt.Add(30*time.Minute)) {
				t.Fatalf("incorrect source projection: %+v", before)
			}
			rejectionWatermark(t, f, 0)
			delivery, pending, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 70)
			publicationMust(t, err)
			if phase != "admitted-before-retention" && (!pending || !delivery.PossiblySent || delivery.Completed) {
				t.Fatalf("missing uncertainty: %+v", delivery)
			}
			f.now = f.now.Add(time.Minute)
			newer, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 71, "Replacement proposal"))
			publicationMust(t, err)
			rejectionRestart(t, f)
			task.tickAt = f.now
			task.issue.Labels = []string{"Mode: Implement"}
			f.runtime.CreationAuthorizer, err = turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: f.cs, Enabled: false, OwnerID: 10, BotID: 20})
			publicationMust(t, err)
			f.lost = false
			c.Body = "ordinary edited instruction"
			c.UpdatedAt = f.now
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			rejectionWatermark(t, f, 70)
			after, err := f.db.GetCreationAcknowledgment(t.Context(), source)
			publicationMust(t, err)
			runs, e := f.db.ListRuns(t.Context(), f.session.ID)
			publicationMust(t, e)
			if len(runs) != 0 {
				t.Fatalf("acceptance recovery allocated runs: %+v", runs)
			}
			if after != before {
				t.Fatalf("recovery altered consent: %+v -> %+v", before, after)
			}
			body := f.bodies[len(f.bodies)-1]
			// Admission without publication meets the disabled restart and is
			// refused; a publication begun before the restart is only reported.
			want, creations := "stopped part-way", 1
			if phase == "admitted-before-retention" {
				want, creations = "nothing was published", 0
			}
			for _, want := range []string{before.Hash, want} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q: %s", want, body)
				}
			}
			if strings.Contains(body, newer.Hash) || f.posts != 1 || f.creationPosts != creations {
				t.Fatalf("wrong original reply/effects: posts=%d creations=%d body=%s", f.posts, f.creationPosts, body)
			}
			if pending {
				done, e := f.db.GetReportDelivery(t.Context(), delivery.Key)
				publicationMust(t, e)
				if !done.Completed || done.CommentID == 0 || done.Body != delivery.Body || done.Purpose != "creation-acceptance" {
					t.Fatalf("unstable retained delivery: %+v", done)
				}
			}
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			if f.posts != 1 {
				t.Fatal("replayed old approval produced another reply")
			}
		})
	}
}

// AC-009: neither counterfeit receipts nor an empty listing authorize another
// POST. Once exact trusted evidence appears, the same delivery can complete.
func TestCreationAcceptanceIO_UntrustedEvidenceRemainsPending(t *testing.T) {
	for _, variant := range []string{"wrong bot", "wrong destination", "changed body", "missing receipt"} {
		t.Run(variant, func(t *testing.T) {
			f := newPublicationIO(t)
			task := rejectionTask(t, f)
			prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Approved"))
			publicationMust(t, err)
			original := f.server.Config.Handler
			trusted := false
			f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
					f.mu.Lock()
					defer f.mu.Unlock()
					body := ""
					if len(f.bodies) > 0 {
						body = f.bodies[0]
					}
					bot := 20
					destination := "https://github.com/owner/api/issues/278#issuecomment-991"
					if !trusted {
						switch variant {
						case "wrong bot":
							bot = 11
						case "wrong destination":
							destination = "https://github.com/other/api/issues/278#issuecomment-991"
						case "changed body":
							body = "altered\n" + body
						}
					}
					rows := []any{}
					if trusted || variant != "missing receipt" {
						rows = append(rows, map[string]any{"id": 991, "body": body, "html_url": destination, "user": map[string]any{"id": bot, "type": "Bot"}})
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(rows)
					return
				}
				original.ServeHTTP(w, r)
			})
			f.lost = true
			c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			rejectionRestart(t, f)
			task.tickAt = f.now
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			rejectionWatermark(t, f, 0)
			retained, pending, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 70)
			publicationMust(t, err)
			if !pending || !retained.PossiblySent || f.posts != 1 {
				t.Fatalf("uncertain source lost or reposted: %+v posts=%d", retained, f.posts)
			}
			f.mu.Lock()
			trusted = true
			f.mu.Unlock()
			rejectionRestart(t, f)
			task.tickAt = f.now
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			rejectionWatermark(t, f, 70)
			if f.posts != 1 {
				t.Fatal("trusted reconciliation reposted")
			}
		})
	}
}

func TestCreationAcceptanceIO_ConsumedApprovalNoBackfillAndFreshRenewal(t *testing.T) {
	f := newPublicationIO(t)
	task := rejectionTask(t, f)
	prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Reusable proposal"))
	publicationMust(t, err)
	c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
	_, err = f.runtime.AdmitCreationApproval(t.Context(), turn.OwnerGrantControl{Repository: "owner/api", SourceTask: f.session.TaskRef, Mode: "research", CommentID: c.ID, ActorID: 10, CreatedAt: c.CreatedAt, Body: c.Body})
	publicationMust(t, err)
	source := store.CreationSource{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: 70, OwnerID: 10}
	old, err := f.db.GetCreationAcknowledgment(t.Context(), source)
	publicationMust(t, err)
	_, err = f.d.reporter.Consume(t.Context(), f.session.ID, 70)
	publicationMust(t, err)
	f.now = f.now.Add(31 * time.Minute)
	task.tickAt = f.now
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	if f.posts != 0 {
		t.Fatal("consumed approval was retrospectively acknowledged")
	}
	c.ID = 71
	c.CreatedAt = f.now
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	source.CommentID = 71
	fresh, err := f.db.GetCreationAcknowledgment(t.Context(), source)
	publicationMust(t, err)
	if fresh.IntentID == old.IntentID || fresh.Hash != old.Hash || !fresh.ExpiresAt.Equal(c.CreatedAt.Add(30*time.Minute)) {
		t.Fatalf("fresh source did not renew correctly: old=%+v fresh=%+v", old, fresh)
	}
	rejectionWatermark(t, f, 71)
	if f.posts != 1 || f.creationPosts != 1 {
		t.Fatalf("unexpected effects: %d/%d", f.posts, f.creationPosts)
	}
	source.CommentID = 70
	again, err := f.db.GetCreationAcknowledgment(t.Context(), source)
	publicationMust(t, err)
	if again != old {
		t.Fatal("renewal changed historical admission")
	}
}
