package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// invalidateManagedPlanReadiness withdraws readiness when graph Plan admits a
// replan, creating the managed feature on its first admission.
func invalidateManagedPlanReadiness(ctx context.Context, o Options, ref naming.Ref) error {
	feature, err := o.DB.GetManagedFeature(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		if err := o.DB.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
			return err
		}
		return o.DB.BindFeatureContract(ctx, store.FeatureContract{Parent: ref.String(), Generation: 1, ContentVersion: 2, SubmissionVersion: 2})
	}
	if err != nil {
		return err
	}
	if feature.State != "planning" {
		return fmt.Errorf("user stories are locked after promotion to Implement")
	}
	contract, err := o.DB.GetFeatureContract(ctx, ref.String())
	if err != nil {
		return err
	}
	if err := o.DB.BindFeatureContract(ctx, contract); err != nil {
		return err
	}
	return o.DB.SetManagedFeatureReady(ctx, ref.String(), feature.Generation, false)
}
