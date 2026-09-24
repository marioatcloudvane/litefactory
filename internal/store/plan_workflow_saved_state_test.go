package store

import (
	"testing"
)

// A Plan state saved before the shared result route was adopted must stay loadable
// and updatable: the route changed how results arrive, not what is saved. The row
// below is written as raw JSON in the earlier shape, with members this build no
// longer knows about and none of the optional ones, so it cannot be produced by
// this build's own SavePlanWorkflowState.
func TestPlanWorkflowStore_LoadsSavedStateFromBeforeTheSharedRoute(t *testing.T) {
	ctx := t.Context()
	db := newTestDB(t)
	session := testSession(t, db, 377)
	const saved = `{"shared_revision":"shared-3","stories":[{"key":"story-001","stage":"technical","candidate":"plan-4-9","draft":{"key":"story-001","name":"Add export"},"approvals":{"story":"plan-4-2"},"retired":false,"private_request":{"operation":"create"},"old_member":"ignored"}],"consumed_replies":[41,42],"legacy_private_field":true}`
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,?,?)`, session.TaskRef, 5, saved); err != nil {
		t.Fatal(err)
	}
	state, revision, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil {
		t.Fatalf("saved state no longer loads: %v", err)
	}
	if revision != 5 || state.SharedRevision != "shared-3" || len(state.Stories) != 1 || state.Stories[0].Key != "story-001" || state.Stories[0].Stage != "technical" || state.Stories[0].Candidate != "plan-4-9" || state.Stories[0].Approvals["story"] != "plan-4-2" || len(state.ConsumedReplies) != 2 || state.Stories[0].Wait != nil {
		t.Fatalf("saved state misread at revision %d: %+v", revision, state)
	}
	// It continues from its own revision: the next save is revision 6 and reads back.
	state.SharedRevision = "shared-4"
	next, err := db.SavePlanWorkflowState(ctx, session.TaskRef, revision, state)
	if err != nil || next != 6 {
		t.Fatalf("saved state cannot be continued: %d %v", next, err)
	}
	again, revision, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil || revision != 6 || again.SharedRevision != "shared-4" || again.Stories[0].Candidate != "plan-4-9" {
		t.Fatalf("continued state lost content: %d %+v %v", revision, again, err)
	}
	// A row that is not a state is a named error, never an empty state that would
	// look like a fresh Plan.
	other := testSession(t, db, 378)
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,?,?)`, other.TaskRef, 1, `{"stories":"not a list"}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.GetPlanWorkflowState(ctx, other.TaskRef); err == nil {
		t.Fatal("a corrupt saved state loaded as if it were valid")
	}
}
