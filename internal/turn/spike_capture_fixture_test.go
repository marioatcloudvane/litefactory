package turn

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// captureSpike captures the shipped Spike under the generic control validator.
func captureSpike(ctx context.Context, policy WorkflowPolicy) (*teamsource.WorkflowDefinition, error) {
	specs, err := WorkflowSpecs(policy)
	if err != nil {
		return nil, err
	}
	definition, err := teamsourcetest.CaptureSpike(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("capture current shipped Spike: %w", err)
	}
	return definition, nil
}
