package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// reserveSession keeps legacy work at one slot and resolves retained graph
// work from its pinned current node. It never consumes a dispatch or attempt.
func (d *Daemon) reserveSession(ctx context.Context, sessionID int64) (*stepReservation, string, error) {
	need := turn.SlotNeed{Admission: 1, Full: 1}
	node := "legacy"
	workflow := "legacy"
	if d.db != nil {
		instance, err := d.db.LatestSessionGraphInstance(ctx, sessionID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, "", err
		}
		if err == nil && instance.Status == store.GraphRunning {
			node = instance.CurrentNode
			definition, definitionErr := d.db.GetGraphDefinition(ctx, instance.DefinitionID)
			if definitionErr != nil {
				return nil, "", definitionErr
			}
			workflow = definition.Workflow
			if workflow == "" {
				workflow = definition.Mode
			}
			need, err = turn.GraphDispatchSlotNeed(ctx, d.db, store.GraphDispatch{InstanceID: instance.ID, Node: node, SessionID: sessionID})
			if err != nil {
				return nil, "", err
			}
		}
	}
	cost := need.Admission
	if !d.pool.acquire(cost) {
		used, total := d.pool.capacity()
		return nil, fmt.Sprintf("%s: pool=shared workflow=%s session=%d node=%s requested=%d used=%d available=%d total=%d", need.Deferral(total), workflow, sessionID, node, cost, used, total-used, total), nil
	}
	d.pool.wg.Add(1)
	return &stepReservation{pool: d.pool, cost: cost}, "", nil
}

// abandon discharges both reservation and join when no worker was launched.
func (r *stepReservation) abandon() { r.Release(); r.pool.wg.Done() }

// reserveInput admits observation and input classification. A graph driver
// replaces this unit reservation with its authoritative pinned cost before
// preparation or execution; controls and closure observations need no graph.
func (d *Daemon) reserveInput() (*stepReservation, string) {
	if !d.pool.acquire(1) {
		used, total := d.pool.capacity()
		return nil, fmt.Sprintf("temporary capacity deferral: pool=shared workflow=input node=observe requested=1 used=%d available=%d total=%d", used, total-used, total)
	}
	d.pool.wg.Add(1)
	return &stepReservation{pool: d.pool, cost: 1}, ""
}
