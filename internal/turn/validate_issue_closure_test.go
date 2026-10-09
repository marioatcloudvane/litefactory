package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestValidateOwnerStartAfterIssueClosureCancellation(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	ctx := t.Context()
	if _, handled, err := v.admit(t, v.request(5001, validateOwnerID, "/validate first")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("first admission: handled=%v err=%v", handled, err)
	}
	first := v.entries(t)[0]
	instance, err := v.f.db.GetGraphInstance(ctx, first.InstanceID)
	mustProgress(t, err)
	source, err := v.f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, Limit: 3, StartedAt: instance.StartedAt})
	mustProgress(t, err)
	mustProgress(t, v.f.db.FinishGraphNodeAttempt(ctx, source.ID, store.GraphSucceeded, "done", instance.StartedAt))
	mustProgress(t, v.f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "closure-review", InstanceID: instance.ID, SourceAttemptID: source.ID, Node: instance.CurrentNode, Kind: "review", Candidate: []byte("{}")}))
	// The owner has already received the review; cancellation must preserve
	// that receipt and must not bypass unresolved publication obligations.
	_, err = v.f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "closure-review", SessionID: instance.SessionID, Task: v.f.session.TaskRef, Destination: "acme/repo#540", TriggerNamespace: "issue-comment", Purpose: "validate-review", BotUserID: 99, Body: "Review complete", Marker: "closure-review"})
	mustProgress(t, err)
	_, err = v.f.db.MarkReportDeliverySent(ctx, "closure-review")
	mustProgress(t, err)
	_, err = v.f.db.ConfirmReportDelivery(ctx, "closure-review", 9090)
	mustProgress(t, err)
	_, err = v.f.db.CompleteReportDelivery(ctx, "closure-review")
	mustProgress(t, err)
	mustProgress(t, v.f.db.CancelClosedIssueGraphs(ctx, instance.SessionID, instance.StartedAt))
	cancelled, err := v.f.db.GetGraphInstance(ctx, instance.ID)
	mustProgress(t, err)
	if cancelled.Status != store.GraphCancelled {
		t.Fatalf("predecessor=%+v", cancelled)
	}
	// Cancellation alone gives no authority to restart automatically.
	scheduled, _, err := v.f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: instance.SessionID, SourceCommentID: 1 << 56, OwnerHandle: "owner", Payload: ValidateAutoPayload, DueAt: implementClock})
	mustProgress(t, err)
	out, handled, err := v.admit(t, v.request(scheduled.TriggerID(), 0, ValidateAutoPayload))
	if !handled || err != nil || !strings.Contains(out.Reason, "already been started") {
		t.Fatalf("automatic restart: handled=%v err=%v out=%+v", handled, err, out)
	}
	// With ordinary admission gates satisfied, fresh explicit input on the same
	// head follows the existing restart path, without changing admission policy.
	if _, handled, err := v.admit(t, v.request(5002, validateOwnerID, "/validate again")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("fresh admission: handled=%v err=%v", handled, err)
	}
	entries := v.entries(t)
	if len(entries) != 1 || entries[0].InstanceID == first.InstanceID || entries[0].Commit != first.Commit || v.binds != 2 {
		t.Fatalf("fresh instance on same commit: %+v binds=%d", entries, v.binds)
	}
}
