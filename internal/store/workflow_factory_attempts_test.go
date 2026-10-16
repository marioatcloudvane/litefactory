package store

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkflowFactoryAttemptsRecoveryScopeAndPagination(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	var expected []int64
	for n, tc := range []struct {
		mode, state                string
		run, moved, terminal, held bool
	}{
		{mode: "research", state: GraphRunning}, {mode: "plan", state: GraphPending},
		{mode: "research", state: GraphRunning, run: true}, {mode: "research", state: GraphRunning, moved: true},
		{mode: "plan", state: GraphSucceeded}, {mode: "implement", state: GraphRunning},
		{mode: "plan", state: GraphRunning, terminal: true}, {mode: "research", state: GraphRunning, held: true},
	} {
		session := testSession(t, db, 8100+n)
		definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "recovery", Mode: tc.mode, ProfileVersion: 1, Path: tc.mode + ".dot", Digest: fmt.Sprintf("%064x", n+1), CreatedAt: graphTestTime()})
		if err != nil {
			t.Fatal(err)
		}
		instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "judge", StartedAt: graphTestTime()})
		if err != nil {
			t.Fatal(err)
		}
		var runID int64
		if tc.run {
			runID = resultRun(t, db, session, tc.mode, "source", 1).RunID
		}
		attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "judge", RunID: runID, Limit: 1, StartedAt: graphTestTime()})
		if err != nil {
			t.Fatal(err)
		}
		if tc.state == GraphPending {
			if _, err = db.sql.ExecContext(ctx, `UPDATE graph_node_attempts SET status='pending' WHERE id=?`, attempt.ID); err != nil {
				t.Fatal(err)
			}
		}
		if tc.state == GraphSucceeded {
			if err = db.FinishGraphNodeAttempt(ctx, attempt.ID, GraphSucceeded, "success", graphTestTime()); err != nil {
				t.Fatal(err)
			}
		}
		if tc.moved {
			if _, err = db.sql.ExecContext(ctx, `UPDATE graph_instances SET current_node='different' WHERE id=?`, instance.ID); err != nil {
				t.Fatal(err)
			}
		}
		if tc.terminal {
			if err = db.SetGraphInstanceStatus(ctx, instance.ID, GraphFailed, time.Time{}, graphTestTime()); err != nil {
				t.Fatal(err)
			}
		}
		if tc.held {
			_, presentation := retainedPollProposal(t, db, session)
			if _, err = db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "factory-attempt-hold", SourceID: "factory-attempt-approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation}); err != nil {
				t.Fatal(err)
			}
		}
		if n < 2 {
			expected = append(expected, attempt.ID)
		}
	}
	var after int64
	for _, id := range expected {
		page, err := db.ListWorkflowFactoryAttempts(ctx, after, 1)
		if err != nil || len(page) != 1 || page[0].ID != id || page[0].RunID != 0 {
			t.Fatalf("page after %d: %+v %v", after, page, err)
		}
		after = page[0].ID
	}
	page, err := db.ListWorkflowFactoryAttempts(ctx, after, 100)
	if err != nil || len(page) != 0 {
		t.Fatalf("excluded attempts returned: %+v %v", page, err)
	}
	for _, bounds := range [][2]int64{{-1, 1}, {0, 0}, {0, 101}} {
		if _, err = db.ListWorkflowFactoryAttempts(ctx, bounds[0], int(bounds[1])); err == nil {
			t.Fatalf("invalid bounds accepted: %v", bounds)
		}
	}
}
