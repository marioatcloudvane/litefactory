package cli

import (
	"context"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/factory/maintenance"
)

type retirementInventory = maintenance.RetirementInventory

// writeRetirementPreview preserves the existing fixture entry point. Production
// callers receive only bounded views from the factory service.
func writeRetirementPreview(ctx context.Context, env *Env, inventory retirementInventory, options daemon.RetirementOptions) error {
	return maintenance.PreviewRetirementInventory(ctx, inventory, options, retirementPresentation(env))
}
