package daemon

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// #932 c3: a graph step owed to a Linear session whose ticket container differs
// from the code repository is observed at the retained ticket container, not
// deferred with a ticket identity conflict.
func TestGraphDispatchObservesLinearSessionAtItsTicketContainer(t *testing.T) {
	f, _, v, sub, q := linearObservationFixture(t)
	ctx := t.Context()
	db := f.d.db
	handoffLinearObservation(t, f, v, sub)
	seen := map[int64]*candidate{}
	linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, seen))
	for _, c := range seen {
		if c.discoveryReservation != nil {
			c.discoveryReservation.finish()
		}
	}
	session, err := db.GetSessionByTicket(ctx, ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"})
	linearWorkflowCheck(t, err)
	binding, err := db.GetSessionTicket(ctx, session.ID)
	linearWorkflowCheck(t, err)
	if code := q.repo.Owner + "/" + q.repo.Name; binding.Locator.Container == code {
		t.Fatalf("fixture ticket container %s equals the code repository", code)
	}
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: strings.Repeat("c", 64), CreatedAt: f.now})
	linearWorkflowCheck(t, err)
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "shape", StartedAt: f.now})
	linearWorkflowCheck(t, err)
	dispatch, err := db.ParkGraphInstance(ctx, instance.ID, "shape", f.now, f.now)
	linearWorkflowCheck(t, err)

	// The only run slot is taken, so a dispatch that passes observation stops
	// at the pool and stays pending without starting a worker.
	f.d.pool = newPool(1)
	if !f.d.pool.try() {
		t.Fatal("could not take the run slot")
	}
	defer f.d.pool.done()
	var result TickResult
	linearWorkflowCheck(t, f.d.dispatchGraphSteps(ctx, map[int64]store.Repo{q.repo.ID: q.repo}, f.now, &result))
	if logs := f.logs.String(); strings.Contains(logs, "graph step issue observation failed") {
		t.Fatalf("observation deferred:\n%s", logs)
	}
	if result.Deferred != 1 || result.Dispatched != 0 {
		t.Fatalf("the observed dispatch did not reach the run pool: %+v", result)
	}
	got, err := db.LatestGraphDispatch(ctx, instance.ID)
	linearWorkflowCheck(t, err)
	if got.ID != dispatch.ID || got.Status != "pending" {
		t.Fatalf("dispatch changed: %+v", got)
	}
}
