package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// ReportGraphDeliveryCompletion retries only immutable factory publication.
// Accepted input, pending effects and incomplete integration are never rendered
// as completion; a failed post leaves ordinary report delivery debt.
func (r *Reporter) ReportGraphDeliveryCompletion(ctx context.Context, instanceID int64) (Posted, error) {
	instance, e := r.db.GetGraphInstance(ctx, instanceID)
	if e != nil {
		return Posted{}, e
	}
	if instance.Status != store.GraphSucceeded {
		return Posted{}, errors.New("delivery completion requires completed graph")
	}
	raw, e := r.db.LatestGraphRecord(ctx, instanceID, "delivery_progress")
	if e != nil {
		return Posted{}, e
	}
	var progress graphDeliveryProgress
	if e = json.Unmarshal(raw, &progress); e != nil {
		return Posted{}, e
	}
	if progress.Outcome != "completed" || !progress.Integration || !progress.Closed || progress.PR.Number < 1 || !progress.PR.Merged {
		return Posted{}, errors.New("delivery completion lacks a merged PR and a closed issue")
	}
	session, e := r.db.GetSession(ctx, instance.SessionID)
	if e != nil {
		return Posted{}, e
	}
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		return Posted{}, e
	}
	intent := r.deliveryIntent(ref, ref, session.ID, 0, 0, fmt.Sprintf("bug-delivery-completed:%d", instanceID))
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		display, err := lookupTicketDisplay(ctx, r.forge, ref, progress.Binding.IssueNumber)
		if err != nil {
			return redact.Body{}, err
		}
		return r.renderer.RenderReportDelivery(ctx, fmt.Sprintf("Bug repair delivered: commit `%s` merged into `%s` through PR #%d (merge `%s`), and issue %s is closed.", progress.Commit, progress.Binding.Target, progress.PR.Number, progress.PR.MergeSHA, display.Reference()), "")
	})
}

// ReportGraphDispatchStop records a stable, runless dispatch notice through the
// ordinary delivery journal, including redaction and uncertain-post recovery.
func (r *Reporter) ReportGraphDispatchStop(ctx context.Context, dispatch store.GraphDispatch) (Posted, error) {
	if dispatch.Status != "cancelled" || dispatch.AllocatedRunID != 0 || dispatch.CancellationReason == "" {
		return Posted{}, errors.New("dispatch stop notice requires cancelled unallocated work")
	}
	session, err := r.db.GetSession(ctx, dispatch.SessionID)
	if err != nil {
		return Posted{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return Posted{}, err
	}
	intent := r.deliveryIntent(ref, ref, session.ID, dispatch.TriggerID(), 0, "graph-dispatch-stopped")
	// Reconcile uncertain original delivery before selecting a material update.
	existing, e := r.db.GetReportDelivery(ctx, intent.Key)
	if e == nil && !existing.Completed {
		return r.dispatchDelivery(ctx, existing)
	}
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		return Posted{}, e
	}
	projection, err := ProjectGraphRecovery(ctx, r.db, session.ID)
	if err != nil {
		return Posted{}, err
	}
	if projection == nil {
		projection = &report.StopRecovery{Step: dispatch.Node, Disposition: "cancelled", Reason: dispatch.CancellationReason, Unresolved: "The retained graph and saved launch could not be established.", NextStep: "Ask the factory operator to inspect on the factory host: `" + report.TaskInspectionCommand(session.TaskRef) + "`."}
	}
	return r.deliverRecoveryProjection(ctx, intent, projection, "")
}

// deliverRecoveryProjection links material updates without changing an original
// wait or a previously retained body. Source IDs and state, not prose, define it.
func (r *Reporter) deliverRecoveryProjection(ctx context.Context, intent store.ReportDelivery, projection *report.StopRecovery, initialBody string) (Posted, error) {
	material, _ := json.Marshal(struct {
		Instance, Dispatch, Launch int64
		Step, State, Route         string
		Due                        string
	}{projection.InstanceID, projection.DispatchID, projection.LaunchID, projection.Step, projection.Disposition, projection.Route, projection.DueAt.UTC().Format("2006-01-02T15:04:05Z")})
	digest := sha256.Sum256(material)
	notice, err := r.db.RetainRecoveryNotice(ctx, intent.SessionID, intent.Key, hex.EncodeToString(digest[:]))
	if err != nil {
		return Posted{}, err
	}
	intent.Key = notice.Key
	intent.Marker = "<!-- lf:delivery v=1 key=" + notice.Key + " -->"
	if notice.PreviousKey != "" {
		intent.Purpose = "graph-stop-update:" + notice.Key
		intent.RunID = 0
		intent.TriggerID = 0
		intent.TriggerNamespace = "recovery-notice"
	}
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		body := report.RenderStopRecovery(projection)
		if notice.PreviousKey == "" && initialBody != "" {
			body = initialBody + "\n\n" + body
		}
		if notice.PreviousKey != "" {
			body += "\n\nThis updates the retained stop explanation `" + notice.PreviousKey + "`; prior history remains unchanged."
		}
		return r.renderer.RenderReportDelivery(ctx, body, "")
	})
}
