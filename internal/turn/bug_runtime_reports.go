package turn

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func (r *BugRuntime) reconcileReports(ctx context.Context) error {
	reports, e := r.opts.DB.PendingBugLifecycleReports(ctx)
	if e != nil {
		return e
	}
	var failures []error
	for _, report := range reports {
		session, e := r.opts.DB.GetSession(ctx, report.SessionID)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		ref, e := naming.ParseRef(session.TaskRef)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		intent := r.opts.ResearchReporter.deliveryIntent(ref, ref, report.SessionID, 0, 0, "bug-event:"+report.Event)
		if intent.Key != report.DeliveryKey {
			if !strings.HasPrefix(report.Event, "restart-attempt:") {
				failures = append(failures, errors.New("bug report identity differs from retained event"))
				continue
			}
			rendered, re := r.opts.ResearchReporter.renderer.RenderReportDelivery(ctx, report.Body, intent.Marker)
			if re != nil {
				failures = append(failures, re)
				continue
			}
			if rendered.Truncated() {
				failures = append(failures, errors.New("bug restart diagnostic exceeds report bound"))
				continue
			}
			report.Body = rendered.String()
			if re = r.opts.DB.BindBugRestartReport(ctx, report.SessionID, report.Event, intent.Key, report.Body); re != nil {
				failures = append(failures, re)
				continue
			}
		}
		intent.Body = report.Body
		delivery, e := r.opts.DB.EnsureReportDelivery(ctx, intent)
		if e == nil {
			_, e = r.opts.ResearchReporter.dispatchDelivery(ctx, delivery)
		}
		if e == nil {
			e = r.opts.DB.ConfirmBugLifecycleReport(ctx, report.SessionID, report.Event)
			if e == nil && report.AttemptID > 0 {
				source, se := r.opts.DB.GetGraphNodeAttempt(ctx, report.AttemptID)
				if se != nil {
					e = se
				} else if source.Node == "close" && source.Outcome == "confirmed" {
					e = r.opts.DB.CompleteBugLifecycle(ctx, report.SessionID)
				}
			}
		}
		if e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}

// reconcileBugProjection resumes only the exact stored comment transport. The
// owning domain still confirms its review and establishes a wait separately.
func (r *BugRuntime) reconcileBugProjection(ctx context.Context, session int64) error {
	deliveries, e := r.opts.DB.ListPendingReportDeliveries(ctx)
	if e != nil {
		return e
	}
	for _, d := range deliveries {
		if d.SessionID != session {
			continue
		}
		admission, e := r.opts.DB.GetTaskWorkflowAdmission(ctx, session)
		if e != nil {
			return e
		}
		if admission.Workflow != "bug" {
			return store.ErrNotFound
		}
		if _, e = r.opts.ResearchReporter.dispatchDelivery(ctx, d); e != nil {
			return e
		}
	}
	return nil
}
