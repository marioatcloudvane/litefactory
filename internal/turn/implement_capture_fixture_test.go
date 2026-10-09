package turn

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func captureImplement(ctx context.Context) (*teamsource.WorkflowDefinition, error) {
	specs, err := ImplementSpecs()
	if err != nil {
		return nil, err
	}
	definition, err := teamsource.CaptureImplement(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("capture current shipped Implement: %w", err)
	}
	return definition, nil
}
