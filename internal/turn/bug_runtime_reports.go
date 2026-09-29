package turn

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// bugRestartDiagnosticReason recovers the plain interruption reason
// ReconcileBugParentGraphs recorded as a graph_outputs diagnostic, so a
// restart-attempt report can be re-rendered without retaining its body.
func bugRestartDiagnosticReason(ctx context.Context, db *store.DB, attemptID int64) (string, error) {
	outputs, e := db.ListGraphOutputsBounded(ctx, attemptID, 1<<20, 16)
	if e != nil {
		return "", e
	}
	for i := len(outputs) - 1; i >= 0; i-- {
		if outputs[i].Kind == "diagnostic" {
			return string(outputs[i].Payload), nil
		}
	}
	return "", errors.New("bug restart diagnostic evidence is missing")
}

// bugNoticeBody regenerates the exact notice/failure comment publishNotice
// renders, from the source attempt's retained node, outcome and outputs. The
// body is never retained: this recomputation is what a post-crash retry uses.
func bugNoticeBody(ctx context.Context, db *store.DB, attemptID int64) (string, error) {
	source, e := db.GetGraphNodeAttempt(ctx, attemptID)
	if e != nil {
		return "", e
	}
	instance, e := db.GetGraphInstance(ctx, source.InstanceID)
	if e != nil {
		return "", e
	}
	values, e := db.ListGraphOutputsBounded(ctx, attemptID, 1<<20, 16)
	if e != nil {
		return "", e
	}
	payloads := make([]string, len(values))
	for n, v := range values {
		payloads[n] = string(v.Payload)
	}
	return bugNoticeComment(source.Node, source.Outcome, payloads, instance.ID, attemptID), nil
}

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
		// The body is never retained; it is regenerated here every time,
		// including a post-crash retry, and report_deliveries' delivery-key
		// dedup keeps a retry from posting a duplicate comment (AC-007).
		var body string
		if strings.HasPrefix(report.Event, "restart-attempt:") {
			reason, re := bugRestartDiagnosticReason(ctx, r.opts.DB, report.AttemptID)
			if re != nil {
				failures = append(failures, re)
				continue
			}
			rendered, re := r.opts.ResearchReporter.renderer.RenderReportDelivery(ctx, reason, intent.Marker)
			if re != nil {
				failures = append(failures, re)
				continue
			}
			if rendered.Truncated() {
				failures = append(failures, errors.New("bug restart diagnostic exceeds report bound"))
				continue
			}
			body = rendered.String()
			if intent.Key != report.DeliveryKey {
				if re = r.opts.DB.BindBugRestartReport(ctx, report.SessionID, report.Event, intent.Key, body); re != nil {
					failures = append(failures, re)
					continue
				}
			}
		} else {
			if intent.Key != report.DeliveryKey {
				failures = append(failures, errors.New("bug report identity differs from retained event"))
				continue
			}
			var re error
			body, re = bugNoticeBody(ctx, r.opts.DB, report.AttemptID)
			if re != nil {
				failures = append(failures, re)
				continue
			}
		}
		intent.Body = body
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
