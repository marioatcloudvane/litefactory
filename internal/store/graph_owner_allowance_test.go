package store

import (
	"errors"
	"testing"
)

// budgetRun dispatches node n times under budget, finishing each attempt.
func budgetRun(t *testing.T, db *DB, instanceID int64, node string, budget, n int) {
	t.Helper()
	for range n {
		a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instanceID, Node: node, Budget: budget, StartedAt: graphTestTime()})
		if err != nil {
			t.Fatal(err)
		}
		if err = db.FinishGraphNodeAttempt(t.Context(), a.ID, GraphSucceeded, "success", graphTestTime()); err != nil {
			t.Fatal(err)
		}
	}
}

// ownerAllowanceClaim records an answered owner question resting on the node's
// latest attempt, as a published Research review does once its reply is claimed.
func ownerAllowanceClaim(t *testing.T, db *DB, instance GraphInstance, key string, reply int64) {
	t.Helper()
	latest, err := db.LatestGraphNodeAttempt(t.Context(), instance.ID, instance.CurrentNode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate,created_at) VALUES(?,?,?,?,'question',X'7B7D',?)`, key, instance.ID, latest.ID, latest.Node, FormatTime(graphTestTime())); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state,reply_id,reply_body,reply_author_id,claimed_at,allow_runless) VALUES(?,?,?,'claimed',?,'follow-up',7,?,1)`, key, instance.ID, latest.ID, reply, FormatTime(graphTestTime())); err != nil {
		t.Fatal(err)
	}
}

// The step budget counts every dispatch of the instance, whatever the node, and
// stops the next one once it is spent.
func TestGraphStepBudgetCountsEveryDispatchOfTheInstance(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	const budget = 3
	budgetRun(t, db, instance.ID, "work", budget, budget)
	_, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "work", Budget: budget, StartedAt: graphTestTime()})
	if !errors.Is(err, ErrGraphBudgetExhausted) {
		t.Fatalf("dispatch past the step budget: %v", err)
	}
	// Zero budget is unbounded.
	budgetRun(t, db, instance.ID, "work", 0, 1)
}

// Every accepted owner reply starts a fresh budget, counted from the
// continuation it allocated.
func TestGraphOwnerReplyResetsTheStepBudget(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	const budget = 3
	budgetRun(t, db, instance.ID, "work", budget, budget)
	for round := range 3 * budget {
		ownerAllowanceClaim(t, db, instance, "round-"+string(rune('a'+round)), int64(100+round))
		continuation, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "work", Budget: budget, StartedAt: graphTestTime()})
		if err != nil {
			t.Fatalf("owner round %d: %v", round+1, err)
		}
		wait, err := db.GetGraphOwnerWait(t.Context(), instance.ID)
		if err != nil || wait.State != "allocated" || wait.ContinuationAttemptID != continuation.ID {
			t.Fatalf("round %d reply not allocated: %+v %v", round+1, wait, err)
		}
		if err = db.FinishGraphNodeAttempt(t.Context(), continuation.ID, GraphSucceeded, "success", graphTestTime()); err != nil {
			t.Fatal(err)
		}
	}
	// The latest reply's continuation used one step of its fresh budget.
	budgetRun(t, db, instance.ID, "work", budget, budget-1)
	_, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "work", Budget: budget, StartedAt: graphTestTime()})
	if !errors.Is(err, ErrGraphBudgetExhausted) {
		t.Fatalf("automatic loop after an owner reply passed its budget: %v", err)
	}
	attempts, err := db.ListGraphNodeAttempts(t.Context(), instance.ID)
	if err != nil || len(attempts) != budget+3*budget+budget-1 {
		t.Fatalf("attempt accounting: %d %v", len(attempts), err)
	}
}
