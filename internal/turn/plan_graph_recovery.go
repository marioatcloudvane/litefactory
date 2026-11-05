package turn

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/store"
)

type planPublication struct {
	Intent  store.ReportDelivery        `json:"intent"`
	Text    string                      `json:"text"`
	Display *store.OwnerDecisionDisplay `json:"decision_display,omitempty"`
}

func recoverPlanPublications(ctx context.Context, o Options, sessionID int64) error {
	instance, err := o.DB.LatestWorkflowInstance(ctx, sessionID, "plan")
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	session, err := o.DB.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if o.ResearchReporter == nil {
		return errors.New("plan publication recovery requires durable reporter")
	}
	for instance.ID != 0 {
		after := ""
		for {
			page, err := o.DB.ListGraphComments(ctx, instance.ID, after, 100)
			if err != nil {
				return err
			}
			for _, intent := range page {
				if intent.Kind == "advisory" {
					if _, err := o.ResearchReporter.RecoverAdvisoryComment(ctx, intent.Key); err != nil {
						return err
					}
					continue
				}
				// Listings contain metadata only; both delivery and display recovery
				// need the original immutable publication bytes.
				intent, err = o.DB.GetGraphComment(ctx, intent.Key)
				if err != nil {
					return err
				}
				var retained planPublication
				if err = json.Unmarshal(intent.Candidate, &retained); err != nil {
					return err
				}
				if retained.Intent.Key != intent.Key || retained.Intent.SessionID != sessionID || retained.Intent.Task != session.TaskRef || retained.Text == "" {
					return errors.New("plan publication retained identity mismatch")
				}
				delivery, err := o.DB.GetReportDelivery(ctx, intent.Key)
				if err == nil {
					var dispatchErr error
					if !delivery.Completed {
						_, dispatchErr = o.ResearchReporter.dispatchDelivery(ctx, delivery)
					}
					if err = retainPlanDisplayAfterDelivery(ctx, o, intent); err != nil || dispatchErr != nil {
						return errors.Join(dispatchErr, err)
					}
					continue
				}
				if !errors.Is(err, store.ErrNotFound) {
					return err
				}
				source, err := o.DB.GetGraphNodeAttempt(ctx, intent.SourceAttemptID)
				if err != nil {
					return err
				}
				text, err := o.ResearchReporter.appendGraphResultReview(ctx, source, retained.Text)
				if err != nil {
					return err
				}
				_, deliverErr := o.ResearchReporter.deliverFullText(ctx, retained.Intent, text)
				if err = retainPlanDisplayAfterDelivery(ctx, o, intent); err != nil || deliverErr != nil {
					return errors.Join(deliverErr, err)
				}
			}
			if len(page) < 100 {
				break
			}
			after = page[len(page)-1].Key
		}
		prior, _, err := o.DB.WorkflowPredecessor(ctx, instance.ID)
		if err != nil {
			return err
		}
		instance.ID = prior
	}
	return nil
}

// retainPlanDisplayAfterDelivery runs whether or not delivery returned an
// error: a post confirmed despite caller cancellation is completed without
// cancellation, so its decision display is retained the same way.
func retainPlanDisplayAfterDelivery(ctx context.Context, o Options, intent store.GraphCommentIntent) error {
	ctx = context.WithoutCancel(ctx)
	delivery, err := o.DB.GetReportDelivery(ctx, intent.Key)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return retainRecoveredPlanDisplay(ctx, o, intent, delivery)
}

func retainRecoveredPlanDisplay(ctx context.Context, o Options, intent store.GraphCommentIntent, delivery store.ReportDelivery) error {
	if !delivery.Completed || delivery.CommentID < 1 || len(intent.Candidate) == 0 {
		return nil
	}
	var p planPublication
	if err := json.Unmarshal(intent.Candidate, &p); err != nil {
		return err
	}
	if p.Display == nil {
		return nil
	}
	p.Display.PublicationID = delivery.CommentID
	return o.DB.PutOwnerDecisionDisplay(ctx, *p.Display)
}
