package daemon

import (
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (d *Daemon) prepareStageDecisions(reporter *turn.Reporter, sanitizer *redact.Redactor, now func() time.Time) error {
	if d.remoteConnector == nil || d.remoteConnector.actions == nil || d.remoteConnector.actions.actions == nil || reporter == nil || sanitizer == nil {
		return errors.New("stage decision dependencies unavailable")
	}
	if now == nil {
		now = time.Now
	}
	service, err := stagedecision.New(stagedecision.Options{DB: d.db, Now: now, Boundary: stageDecisionBoundary{daemon: d}, Publisher: turn.DiscussionPublisher{Reporter: reporter}, Protect: reporter.ProtectDecisionSummary})
	if err != nil {
		return err
	}
	service.BindActions(d.remoteConnector.actions.actions)
	d.remoteConnector.actions.decisions = service
	d.remoteConnector.server.SetStageDecisions(service)
	return nil
}
