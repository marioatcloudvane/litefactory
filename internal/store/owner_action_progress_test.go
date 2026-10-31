package store

import (
	"testing"
	"time"
)

func TestOwnerActionProgressPrioritizesAdmissionAndRotatesRecovery(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	for _, id := range []string{"old-recovery", "new-recovery", "admitted"} {
		next := p
		next.ID, next.RequestKey, next.ExecutionID = id, id, "owner-action:"+id
		next.State = "uncertain"
		if id == "admitted" {
			next.State = "admitted"
		}
		if err := db.CreateOwnerAction(t.Context(), next); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "cancel", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	ids, err := db.ReadyOwnerActions(t.Context(), 1, p.ConfirmNotBefore)
	if err != nil || len(ids) != 1 || ids[0] != "admitted" {
		t.Fatalf("admitted work starved: %v %v", ids, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE owner_action_proposals SET state='succeeded' WHERE id='admitted'`); err != nil {
		t.Fatal(err)
	}
	ids, err = db.ReadyOwnerActions(t.Context(), 1, p.ConfirmNotBefore)
	if err != nil || len(ids) != 1 {
		t.Fatalf("first recovery: %v %v", ids, err)
	}
	first := ids[0]
	ids, err = db.ReadyOwnerActions(t.Context(), 1, p.ConfirmNotBefore.Add(time.Second))
	if err != nil || len(ids) != 1 || ids[0] == first {
		t.Fatalf("recovery did not rotate: %v %v", ids, err)
	}
}

func TestOwnerActionLegacyInvalidationRetention(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	for _, row := range []struct {
		id string
		at time.Time
	}{{"old", p.CreatedAt.Add(-90 * 24 * time.Hour)}, {"recent", p.CreatedAt.Add(-89 * 24 * time.Hour)}} {
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO owner_action_policy_invalidations VALUES('discord','100','200',?,'policy_changed_repropose',?)`, row.id, row.at.Format("2006-01-02T15:04:05.000Z")); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.PurgeOwnerActionHistory(t.Context(), p.CreatedAt); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM owner_action_policy_invalidations WHERE proposal_ref='recent'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recent rollout history lost: %d %v", count, err)
	}
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM owner_action_policy_invalidations WHERE proposal_ref='old'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old rollout history retained: %d %v", count, err)
	}
}
