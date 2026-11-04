package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// BrainstormTasksHarness is the daemon's owner-action service and one round of
// the brainstorm batch processor and Plan launch step, as the serving loop runs them.
type BrainstormTasksHarness struct {
	Actions *owneraction.Service
	Step    func(context.Context) error
}

// EnableBrainstormTasksForTest wires what setup and chat wire in production:
// the owner-action domain, the brainstorm service binding, the connector's
// workflow notice associations and the notice chat with its channel allowlist.
func (d *Daemon) EnableBrainstormTasksForTest(cfg config.RemoteConnector, service *brainstorm.Service, chat *messaging.Service, channels []string, now func() time.Time) BrainstormTasksHarness {
	d.prepareIndependentOwnerActions(&ChatOptions{Now: now}, nil)
	d.ownerActions.brainstormDrafts.bind(service)
	d.remoteConnector = &remoteConnectorRuntime{config: cfg, actions: d.ownerActions}
	if chat != nil {
		d.ownerActions.notices.Store(chat)
		allowed := append([]string(nil), channels...)
		d.brainstormTasks.channels.Store(&allowed)
	}
	d.brainstormTasks.clock = now
	processor := d.brainstormTaskProcessor()
	return BrainstormTasksHarness{Actions: d.ownerActions.actions, Step: func(ctx context.Context) error {
		if processor == nil {
			return errors.New("brainstorm task processor is not wired")
		}
		if err := processor.Step(ctx); err != nil {
			return err
		}
		d.stepBrainstormLaunches(ctx)
		return nil
	}}
}
