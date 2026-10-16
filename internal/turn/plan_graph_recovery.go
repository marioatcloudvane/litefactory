package turn

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type planPublication struct {
	Intent store.ReportDelivery `json:"intent"`
	Text   string               `json:"text"`
}

func recoverPlanPublications(ctx context.Context, o Options, sessionID int64) error {
	instance, err := o.DB.LatestWorkflowInstance(ctx, sessionID, "plan")
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
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
				delivery, err := o.DB.GetReportDelivery(ctx, intent.Key)
				if err == nil {
					if !delivery.Completed {
						if _, err = o.ResearchReporter.dispatchDelivery(ctx, delivery); err != nil {
							return err
						}
					}
					continue
				}
				if !errors.Is(err, store.ErrNotFound) {
					return err
				}
				var retained planPublication
				if err = json.Unmarshal(intent.Candidate, &retained); err != nil {
					return err
				}
				if retained.Intent.Key != intent.Key || retained.Intent.SessionID != sessionID || retained.Text == "" {
					return errors.New("plan publication retained identity mismatch")
				}
				if _, err = o.ResearchReporter.deliver(ctx, retained.Intent, func() (redact.Body, error) {
					return o.ResearchReporter.renderer.RenderReportDelivery(ctx, retained.Text, "")
				}); err != nil {
					return err
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
