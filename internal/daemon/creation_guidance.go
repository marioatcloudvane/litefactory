package daemon

import (
	"context"

	"github.com/cloudvane/litefactory/internal/report"
)

func (d *Daemon) creationPublicationContext(_ context.Context, _ *task) report.CreationPublication {
	e, ok := d.turn.(turnExecutor)
	if !ok || e.opts.MCP == nil || e.opts.MCP.Creation == nil || !e.opts.MCP.CreationEnabled() {
		return report.CreationPublicationUnavailable
	}
	return report.CreationPublicationFactory
}
