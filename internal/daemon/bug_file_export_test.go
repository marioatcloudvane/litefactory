package daemon

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// BugFileHarness is the daemon's owner-action service and one round of the
// bug filing processor and Bug start step, as the serving loop runs them.
type BugFileHarness struct {
	Actions *owneraction.Service
	Step    func(context.Context)
}

// EnableBugFilingForTest wires what setup wires in production: the
// owner-action domain with its bug_file handler, the connector binding the
// owner-action authority checks read, and the wall clock of the owner-action
// steps.
func (d *Daemon) EnableBugFilingForTest(cfg config.RemoteConnector, now func() time.Time) BugFileHarness {
	d.prepareIndependentOwnerActions(&ChatOptions{Now: now}, nil)
	d.remoteConnector = &remoteConnectorRuntime{config: cfg, actions: d.ownerActions}
	d.brainstormTasks.clock = now
	return BugFileHarness{Actions: d.ownerActions.actions, Step: func(ctx context.Context) {
		d.stepBugFilings(ctx)
		d.stepBugFilingStarts(ctx)
	}}
}
