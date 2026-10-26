package store

import (
	"errors"
	"testing"
)

// A workflow whose graph caps its owner rounds keeps the limit across replies.
func TestGraphLimitSpansRepliesKeepsOwnerRoundsCapped(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	ownerAllowanceRun(t, db, instance.ID, "work", 2, 2)
	ownerAllowanceClaim(t, db, instance, "capped", 100)
	_, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "work", Limit: 2, LimitSpansReplies: true, StartedAt: graphTestTime()})
	if !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("capped owner round passed its limit: %v", err)
	}
}

// ownerAllowanceRun dispatches node n times, finishing each attempt.
func ownerAllowanceRun(t *testing.T, db *DB, instanceID int64, node string, limit, n int) {
	t.Helper()
	for range n {
		a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instanceID, Node: node, Limit: limit, StartedAt: graphTestTime()})
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

func TestGraphOwnerRepliesStartFreshNodeAllowance(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	const limit = 3
	ownerAllowanceRun(t, db, instance.ID, "work", limit, limit)
	// Owner rounds well beyond the limit each resume the node.
	for round := range 3 * limit {
		ownerAllowanceClaim(t, db, instance, "round-"+string(rune('a'+round)), int64(100+round))
		continuation, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "work", Limit: limit, StartedAt: graphTestTime()})
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
	// Without owner input the loop guard still holds: the latest reply bought
	// one fresh allowance, of which its continuation used one dispatch.
	ownerAllowanceRun(t, db, instance.ID, "work", limit, limit-1)
	_, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "work", Limit: limit, StartedAt: graphTestTime()})
	if !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("automatic loop after an owner reply passed its limit: %v", err)
	}
	attempts, err := db.ListGraphNodeAttempts(t.Context(), instance.ID)
	if err != nil || len(attempts) != limit+3*limit+limit-1 {
		t.Fatalf("attempt accounting: %d %v", len(attempts), err)
	}
}
