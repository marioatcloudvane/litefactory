package daemon

import (
	"context"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (d *Daemon) creationPublicationContext(ctx context.Context, t *task) report.CreationPublication {
	publication := turn.CreationPublicationContext(ctx, d.db, t.session.ID, t.repo.Path)
	if publication != report.CreationPublicationLegacy {
		return publication
	}
	mode, found, err := resolveModeLabel(t.issue.Labels)
	if err != nil || !found || mode != agent.ModeResearch {
		return report.CreationPublicationUnavailable
	}
	choice := d.resolveAgent(t.issue.Labels)
	if choice.refused() {
		return report.CreationPublicationUnavailable
	}
	kind := agent.Kind(choice.Agent)
	if kind == "" {
		kind = d.defaultAgent
	}
	e, ok := d.turn.(turnExecutor)
	if !ok || !e.opts.MCP.CreationPublishingAvailable(e.opts.Runners[kind]) {
		return report.CreationPublicationUnavailable
	}
	return publication
}
