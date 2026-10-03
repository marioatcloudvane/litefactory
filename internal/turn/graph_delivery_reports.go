package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
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
	if progress.Outcome != "completed" || !progress.Integration || !progress.Closed || progress.Binding.InstanceID != instanceID || progress.PR.ID < 1 || !progress.PR.Merged {
		return Posted{}, errors.New("delivery completion lacks independent integration and closure observations")
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
		return r.renderer.RenderReportDelivery(ctx, fmt.Sprintf("Bug repair delivered. Confirmed integration of tested commit `%s` through PR #%d (numeric ID %d), merge `%s`, into `%s`. Independently confirmed closure of issue #%d (numeric ID %d).\n\nValidation generation: `%s`.", progress.Commit, progress.PR.Number, progress.PR.ID, progress.PR.MergeSHA, progress.Binding.Target, progress.Binding.IssueNumber, progress.Binding.IssueID, progress.Binding.Eligibility.Repair.Validation.Generation), "")
	})
}
