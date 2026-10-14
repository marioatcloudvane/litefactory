package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type observedPendingReporter struct {
	daemon.Reporter
	inner *turn.Reporter
	calls atomic.Int64
}

func (r *observedPendingReporter) ReportPending(ctx context.Context, sessionID, triggerID int64) (turn.Posted, bool, error) {
	if triggerID == 40 {
		r.calls.Add(1)
	}
	return r.inner.ReportPending(ctx, sessionID, triggerID)
}

func TestReportRecoverySQLiteTickPreservesOrderingAndOtherTaskCapacity(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprintf("confirmed_%t", confirmed), func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
			f.plantCursor()
			f.issue("Blocked report")
			f.forge.addIssue(fakeIssue{Number: 124, Title: "Unrelated work", Body: "No instruction", User: stranger()})
			f.settle()
			f.settle()
			session := f.session()
			f.forge.addComment(testIssue, 40, owner(), "/spike old request")
			f.forge.addComment(testIssue, 41, owner(), "/spike following request")
			marker := "<!-- lf:delivery v=1 key=durable-test -->"
			delivery, err := f.db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: "durable-test", Version: 1, SessionID: session.ID, TriggerID: 40, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: botID, Body: "Retained explanation\n\n" + marker, Marker: marker})
			if err != nil {
				t.Fatal(err)
			}
			if claimed, err := f.db.MarkReportDeliverySent(t.Context(), delivery.Key); err != nil || !claimed {
				t.Fatalf("dispatch boundary: %v %v", claimed, err)
			}
			state := "uncertain"
			if confirmed {
				if _, err := f.db.ConfirmReportDelivery(t.Context(), delivery.Key, 900); err != nil {
					t.Fatal(err)
				}
				state = "confirmed"
			}
			tickAt := f.clock.Now()
			due := tickAt.Add(testInterval)
			key := fmt.Sprintf("source:%d:40", session.ID)
			if err := f.db.PutReportRecovery(t.Context(), store.ReportRecovery{Key: key, SessionID: session.ID, TriggerID: 40, DeliveryKey: delivery.Key, Task: session.TaskRef, NextAt: due, State: state, Reason: "prior delivery incomplete"}); err != nil {
				t.Fatal(err)
			}
			// Reconstruct daemon to prove it reads eligibility before replaying a source.
			observed := &observedPendingReporter{Reporter: f.reporter, inner: f.reporter}
			f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1, Reporter: observed, Now: func() time.Time { return tickAt }})
			_, beforeLists, beforePosts, _ := f.forge.counts()
			for i := 0; i < 3; i++ {
				f.settle()
			}
			_, afterLists, afterPosts, _ := f.forge.counts()
			if observed.calls.Load() != 0 || afterPosts != beforePosts || f.watermark() >= 40 || len(f.turns.calls()) != 0 {
				t.Fatalf("ineligible effects: list=%d/%d post=%d/%d watermark=%d calls=%d", beforeLists, afterLists, beforePosts, afterPosts, f.watermark(), len(f.turns.calls()))
			}
			f.forge.addComment(124, 50, owner(), "/spike unrelated")
			f.settle()
			f.settle()
			if !slices.Equal(f.turns.triggers(), []int64{50}) || f.watermark() >= 40 {
				t.Fatalf("blocked source monopolized capacity: triggers=%v watermark=%d", f.turns.triggers(), f.watermark())
			}
			tickAt = due
			_, beforeLists, beforePosts, _ = f.forge.counts()
			f.settle()
			_, afterLists, afterPosts, _ = f.forge.counts()
			if confirmed {
				// Saved receipt is trusted even when the comment is absent remotely.
				if f.watermark() < 40 || observed.calls.Load() != 1 {
					t.Fatalf("confirmed receipt used lookup or stayed blocked: lists=%d/%d watermark=%d", beforeLists, afterLists, f.watermark())
				}
			} else {
				if afterLists <= beforeLists || afterPosts != beforePosts || f.watermark() >= 40 || slices.Contains(f.turns.triggers(), int64(41)) {
					t.Fatal("empty lookup resent, consumed, or advanced the ordered queue")
				}
				row, err := f.db.GetReportRecovery(t.Context(), key)
				if err != nil || row.Attempts != 1 || !row.NextAt.Equal(due.Add(2*testInterval)) {
					t.Fatalf("eligible recovery did not persist cadence: %+v %v", row, err)
				}
				f.forge.addComment(testIssue, 900, bot(), delivery.Body)
				for i := 0; i < 2; i++ {
					f.settle()
				}
				if f.watermark() >= 40 {
					t.Fatal("trusted evidence bypassed due time")
				}
				tickAt = row.NextAt
				f.settle()
			}
			f.settle()
			f.settle()
			row, err := f.db.GetReportRecovery(t.Context(), key)
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("resolved recovery retained: %+v %v", row, err)
			}
			saved, err := f.db.GetReportDelivery(t.Context(), delivery.Key)
			if err != nil || !saved.Completed || saved.CommentID != 900 {
				t.Fatalf("delivery not completed from receipt: %+v %v", saved, err)
			}
			if slices.Contains(f.turns.triggers(), int64(40)) || !slices.Contains(f.turns.triggers(), int64(41)) {
				t.Fatalf("original relaunched or following source stayed blocked: %v", f.turns.triggers())
			}
		})
	}
}

// releasePendingBugFixture uses the Doctor proposal/approval fixture protocol,
// then the real typed release and settlement against the private test database.
func releasePendingBugFixture(t *testing.T, f *fixture, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	session := f.session()
	conversation, err := f.db.BeginDoctorAttempt(ctx, session.TaskRef, store.DoctorAttempt{ID: "release", SourceID: "release", OwnerText: "fixture", PromptVersion: "test", GuideVersion: "test"})
	check(err)
	evidence, err := json.Marshal(store.DoctorEvidence{TaskRef: session.TaskRef})
	check(err)
	check(f.db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "release", Provider: "claude", ConfiguredModel: "test", Evidence: string(evidence)}))
	binding, err := f.db.BindDoctorResultAttempt(ctx, "release")
	check(err)
	proposal := []byte(fmt.Sprintf(`[{"number":1,"target":"graph:%d","action":{"kind":"release_instance","before":"running","after":"failed"}}]`, instance.ID))
	snapshot := append(append([]byte(`{"proposal":`), proposal...), '}')
	_, err = f.db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "release-result", Payload: snapshot, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), Accepted: true})
	check(err)
	check(f.db.AdoptDoctorResult(ctx, binding, "release-result", proposal))
	id, _, err := f.db.DoctorProposalReference(ctx, conversation.ID)
	check(err)
	check(f.db.PresentDoctorProposal(ctx, session.TaskRef, "release-presentation", id))
	op, err := f.db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "release", SourceID: "release-approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: "release-presentation"})
	check(err)
	check(f.db.AcknowledgeDoctorExclusion(ctx, op.ID, "fixture"))
	step, err := f.db.ApplyDoctorTypedRows(ctx, op.ID, 1, "release_instance")
	check(err)
	if step.State != "verified" {
		t.Fatalf("release: %+v", step)
	}
	check(f.db.SettleDoctorTypedRepair(ctx, op.ID, true, ""))
}

func TestBugPendingRefusalAfterRelease(t *testing.T) {
	for _, state := range []string{"unsent", "failed", "uncertain", "consumed"} {
		t.Run(state, func(t *testing.T) {
			f, instance, _ := stoppedGraphFixture(t, true)
			ctx := t.Context()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			releasePendingBugFixture(t, f, instance)
			attempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
			check(err)
			if len(attempts) != 0 || len(f.turns.calls()) != 0 {
				t.Fatal("release fixture has execution history")
			}
			session := f.session()
			capture, err := f.db.WorkflowCaptureForAdmission(ctx, session.ID, 0, "bug")
			check(err)
			// Keep the executor synthetic while exercising real successor admission.
			f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
				if req.Trigger.CommentID != 41 || req.Workflow != agent.WorkflowBug {
					t.Errorf("unexpected execution: %+v", req)
				}
				admitted, e := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Workflow: "bug", SessionID: session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: instance.DefinitionID, Start: "conversation", ContentDigest: capture.ContentDigest, TopologyDigest: capture.TopologyDigest, Files: capture.Files, Provenance: capture.Provenance, Reply: req.Trigger.Body})
				if e != nil {
					return turn.Result{}, e
				}
				if admitted.PredecessorID != instance.ID {
					t.Error("successor lost released predecessor")
				}
				// A real conversation retains its next work or owner wait. Keep
				// this synthetic successor waiting too, so report-ordering ticks
				// do not correctly discover it as a stranded running instance.
				if e = f.db.ParkGraphNode(ctx, store.GraphStop{Key: "successor-wait", InstanceID: admitted.Instance.ID, Origin: "conversation", Cause: "exhausted", Reason: "synthetic conversation awaits owner", Candidate: []byte(`{}`), At: f.clock.Now(), AllowRunless: true}); e != nil {
					return turn.Result{}, e
				}
				_, e = f.reporter.Consume(ctx, session.ID, req.Trigger.CommentID)
				return turn.Result{ResearchGraph: true, SessionID: session.ID, TriggerCommentID: req.Trigger.CommentID}, e
			})
			f.forge.addComment(testIssue, 40, owner(), "Old owner request")
			f.forge.addComment(testIssue, 41, owner(), "Please start the Bug")
			marker := "<!-- lf:delivery v=1 key=bug-refusal -->"
			delivery, err := f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "bug-refusal", Version: 1, SessionID: session.ID, TriggerID: 40, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "reply:bug_comment_not_applied:schedule:0:proposal:0:confirmation:0", BotUserID: botID, Body: "Retained refusal\n\n" + marker, Marker: marker})
			check(err)
			if state == "uncertain" || state == "consumed" {
				_, err = f.db.MarkReportDeliverySent(ctx, delivery.Key)
				check(err)
			}
			var consumedWatermark store.Watermark
			if state == "consumed" {
				// Advance after drain read its watermark but before the stale list
				// containing A and B returns, as another recovery could do.
				f.forge.onListComments = func(_ http.ResponseWriter, _ int) bool {
					if consumedWatermark.LastCommentID == 0 {
						_, e := f.db.ConfirmReportDelivery(ctx, delivery.Key, 900)
						if e != nil {
							t.Error(e)
							return false
						}
						_, e = f.db.CompleteReportDelivery(ctx, delivery.Key)
						if e != nil {
							t.Error(e)
							return false
						}
						consumedWatermark, _, e = f.db.GetWatermark(ctx, session.ID)
						if e != nil {
							t.Error(e)
						}
					}
					return false
				}
			}
			if state == "failed" {
				f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool { w.WriteHeader(http.StatusServiceUnavailable); return true }
			}
			f.settle()
			if state == "unsent" || state == "consumed" {
				wm, _, e := f.db.GetWatermark(ctx, session.ID)
				check(e)
				if wm.LastCommentID != 40 || len(f.turns.calls()) != 0 {
					t.Fatalf("head was not settled first: %+v calls=%v", wm, f.turns.triggers())
				}
				if state == "consumed" && wm != consumedWatermark {
					t.Fatal("stale replay wrote the watermark again")
				}
			}
			if state == "failed" || state == "uncertain" {
				if f.watermark() >= 40 || len(f.turns.calls()) != 0 {
					t.Fatal("unconfirmed refusal let later input run")
				}
				row, e := f.db.GetReportRecovery(ctx, fmt.Sprintf("source:%d:40", session.ID))
				check(e)
				if row.NextAt.IsZero() {
					t.Fatal("missing retry deadline")
				}
				f.forge.onPostComment = nil
				// Only a trustworthy receipt permits recovery after a possibly sent POST.
				f.forge.addComment(testIssue, 900, bot(), delivery.Body)
				f.clock.advance(4 * testInterval)
			}
			for range 3 {
				f.settle()
			}
			if state == "failed" || state == "uncertain" {
				starts, outcomes := 0, 0
				for _, line := range strings.Split(f.logs.String(), "\n") {
					if !strings.Contains(line, `msg="report recovery attempt`) {
						continue
					}
					for _, field := range []string{"level=INFO", fmt.Sprintf("session_id=%d", session.ID), "task=" + session.TaskRef, fmt.Sprintf("recovery=source:%d:40", session.ID)} {
						if !strings.Contains(line, field) {
							t.Fatalf("missing %s in retry: %s", field, line)
						}
					}
					if strings.Contains(line, `msg="report recovery attempt"`) {
						starts++
					} else if strings.Contains(line, "outcome=resolved") {
						outcomes++
					}
				}
				if starts != 1 || outcomes != 1 {
					t.Fatalf("runless retry diagnostics starts=%d resolved=%d logs=%s", starts, outcomes, f.logs)
				}
			}
			saved, err := f.db.GetReportDelivery(ctx, delivery.Key)
			check(err)
			if !saved.Completed || saved.Body != delivery.Body || f.watermark() < 41 {
				t.Fatalf("refusal/queue unfinished: %+v watermark=%d logs=%s", saved, f.watermark(), f.logs)
			}
			if !slices.Equal(f.turns.triggers(), []int64{41}) {
				t.Fatalf("execution order: %v logs=%s", f.turns.triggers(), f.logs)
			}
			old, err := f.db.GetGraphInstance(ctx, instance.ID)
			check(err)
			latest, err := f.db.LatestStandaloneBugInstance(ctx, session.ID)
			check(err)
			if old.Status != store.GraphFailed || latest.ID == old.ID {
				t.Fatalf("released graph reopened or successor missing: %+v %+v", old, latest)
			}
			posts, _, _ := f.forge.snapshot()
			count := 0
			for _, body := range posts {
				if body == delivery.Body {
					count++
				}
			}
			wantPosts := 0
			if state == "unsent" {
				wantPosts = 1
			}
			if count != wantPosts {
				t.Fatalf("saved refusal posts=%d want=%d", count, wantPosts)
			}
		})
	}
}
