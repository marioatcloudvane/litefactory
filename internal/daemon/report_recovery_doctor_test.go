package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestDoctorGuardedReportOutageRespectsCachedCadence(t *testing.T) {
	db, _, session := recoveryDatabase(t)
	d, logs, source := recoveryUnitFixture()
	d.db = db
	source.SessionID = session.ID
	source.RunID = 0
	source.Task = session.TaskRef
	source.Key = recoveryKey(session.ID, source.TriggerID)
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	d.refreshRecoveries(t.Context(), at)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	effect := func() (turn.Posted, error) { calls++; return turn.Posted{CommentID: 77}, nil }
	for i := 0; i < 4; i++ {
		posted, err := d.guardedReportAttempt(recoveryUnitContext(t, at), source, effect)
		if !isRecoveryPending(err) || !posted.Skipped {
			t.Fatalf("outage permitted effect: %+v %v", posted, err)
		}
	}
	row := d.recoveries.items[source.Key]
	if calls != 0 || row.Attempts != 0 || !row.NextAt.Equal(at.Add(time.Minute)) || len(recoveryLogRows(t, logs)) != 1 {
		t.Fatalf("ineligible guard failure changed debt: calls=%d row=%+v logs=%s", calls, row, logs.String())
	}
	if _, err := d.guardedReportAttempt(recoveryUnitContext(t, row.NextAt), source, effect); !isRecoveryPending(err) {
		t.Fatal(err)
	}
	if calls != 0 || d.recoveries.items[source.Key].Attempts != 1 {
		t.Fatal("due outage bypassed guard or wrong cadence")
	}
}

func TestDoctorGuardedReportHeldTargetPreservesDebt(t *testing.T) {
	d, _, ref, _ := doctorHostFixture(t)
	session, err := d.db.GetSessionByTaskRef(t.Context(), ref.String())
	if err != nil {
		t.Fatal(err)
	}
	source := store.ReportRecovery{Key: recoveryKey(session.ID, 321), SessionID: session.ID, TriggerID: 321, Task: ref.String()}
	calls := 0
	posted, err := d.guardedReportAttempt(recoveryUnitContext(t, time.Now()), source, func() (turn.Posted, error) { calls++; return turn.Posted{CommentID: 77}, nil })
	if calls != 0 || !posted.Skipped || !isRecoveryPending(err) {
		t.Fatalf("repair hold bypassed: %d %+v %v", calls, posted, err)
	}
	if len(d.recoveries.items) != 0 {
		t.Fatal("repair hold fabricated failure or resolved debt")
	}
}

func TestReportRecoveryDoctorHeldResolvedRowCleansAfterRelease(t *testing.T) {
	ctx := t.Context()
	var row store.ReportRecovery
	d, _, ref, _ := doctorHostFixtureBeforeApproval(t, func(db *store.DB, session store.Session) {
		row = store.ReportRecovery{Key: recoveryKey(session.ID, 321), SessionID: session.ID, TriggerID: 321, State: "resolved", NextAt: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
		if err := db.PutReportRecovery(ctx, row); err != nil {
			t.Fatal(err)
		}
	})
	if err := d.db.DeleteReportRecovery(ctx, row.Key); err == nil {
		t.Fatal("delete bypassed doctor hold")
	}
	d.refreshRecoveries(ctx, row.NextAt)
	if saved, err := d.db.GetReportRecovery(ctx, row.Key); err != nil || saved != row || !d.recoveries.dirty[row.Key] {
		t.Fatalf("held history lost: %+v %v", saved, err)
	}
	if err := d.db.StopDoctorRepair(ctx, ref.String(), "fixture release"); err != nil {
		t.Fatal(err)
	}
	d.refreshRecoveries(ctx, row.NextAt)
	if _, err := d.db.GetReportRecovery(ctx, row.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("released history retained: %v", err)
	}
	if len(d.recoveries.items) != 0 || len(d.recoveries.dirty) != 0 {
		t.Fatal("released cache retained")
	}
}
