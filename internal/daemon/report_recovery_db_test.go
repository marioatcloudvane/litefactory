package daemon

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func recoveryDatabase(t *testing.T) (*store.DB, string, store.Session) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recovery.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "https://github.com/acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 123)
	if err != nil {
		t.Fatal(err)
	}
	return db, path, session
}

func reopenRecoveryDatabase(t *testing.T, db *store.DB, path string) {
	t.Helper()
	reopened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	*db = *reopened // no workers access this fixture during replacement
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestReportRecoverySQLiteRestartKeepsCadenceAndEscalation(t *testing.T) {
	db, path, session := recoveryDatabase(t)
	d, logs, source := recoveryUnitFixture()
	d.db = db
	source.SessionID = session.ID
	source.RunID = 0
	source.Task = session.TaskRef
	source.Key = recoveryKey(session.ID, source.TriggerID)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	calls := 0
	attempt := func() (turn.Posted, error) {
		calls++
		return turn.Posted{}, &turn.DeliveryError{Key: "retained-delivery", State: "uncertain", Cause: errors.New("private-recovery-value missing receipt")}
	}
	for failure := 0; failure < 5; failure++ {
		if _, err := d.reportAttempt(recoveryUnitContext(t, at), source, attempt); !isRecoveryPending(err) {
			t.Fatal(err)
		}
		saved, err := db.GetReportRecovery(t.Context(), source.Key)
		if err != nil || saved.Attempts != failure || saved.Escalated != (failure >= 3) || saved.DeliveryKey != "retained-delivery" || strings.Contains(saved.Reason, "private-recovery-value") {
			t.Fatalf("persisted failure %d: %+v %v", failure, saved, err)
		}
		want := at.Add(time.Minute * time.Duration(1<<failure))
		if !saved.NextAt.Equal(want) {
			t.Fatalf("due=%s want=%s", saved.NextAt, want)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		reopenRecoveryDatabase(t, db, path)
		restarted, _, _ := recoveryUnitFixture()
		restarted.db = db
		restarted.log = d.log
		restarted.refreshRecoveries(t.Context(), at)
		d = restarted
		before := calls
		beforeLogs := logs.Len()
		for tick := 0; tick < 3; tick++ {
			posted, err := d.reportAttempt(recoveryUnitContext(t, want.Add(-time.Second)), source, attempt)
			if !isRecoveryPending(err) || !posted.Skipped || calls != before || logs.Len() != beforeLogs {
				t.Fatal("restart allowed an ineligible attempt or diagnostic")
			}
		}
		got, found, err := db.GetWatermark(t.Context(), session.ID)
		if err != nil || found {
			t.Fatalf("blocked source consumed: %+v %v", got, err)
		}
		at = want
	}
	if got := len(recoveryLogRows(t, logs)); got != 2 {
		t.Fatalf("restart repeated first/third diagnostics: %d", got)
	}
	if _, err := d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) { return turn.Posted{CommentID: 77}, nil }); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetReportRecovery(t.Context(), source.Key)
	if err != nil || row.State != "resolved" || !row.Escalated || row.Attempts != 4 {
		t.Fatalf("resolved history lost: %+v %v", row, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopenRecoveryDatabase(t, db, path)
	restarted, _, _ := recoveryUnitFixture()
	restarted.db = db
	restarted.refreshRecoveries(t.Context(), at)
	allowed, err := restarted.recoveryAllowed(t.Context(), source, at)
	if err != nil || !allowed || restarted.sessionRecoveryHeld(session.ID, at) {
		t.Fatalf("resolved restart blocked source: %v %v", allowed, err)
	}
}

func TestReportRecoverySQLiteOutageRetainsSourceFallbackAndFlushes(t *testing.T) {
	db, path, session := recoveryDatabase(t)
	d, logs, source := recoveryUnitFixture()
	d.db = db
	source.SessionID = session.ID
	source.RunID = 0
	source.Task = session.TaskRef
	source.Key = recoveryKey(session.ID, source.TriggerID)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d.refreshRecoveries(t.Context(), at)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fail := func() (turn.Posted, error) {
		calls++
		return turn.Posted{}, errors.New("private-recovery-value intent unavailable")
	}
	if _, err := d.reportAttempt(recoveryUnitContext(t, at), source, fail); !isRecoveryPending(err) {
		t.Fatal(err)
	}
	first := d.recoveries.items[source.Key]
	if !d.recoveries.dirty[source.Key] || first.DeliveryKey != "" || first.Attempts != 0 || !first.NextAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("source fallback debt=%+v", first)
	}
	for i := 0; i < 3; i++ {
		d.refreshRecoveries(t.Context(), at)
		if _, err := d.reportAttempt(recoveryUnitContext(t, at), source, fail); !isRecoveryPending(err) {
			t.Fatal(err)
		}
	}
	if calls != 1 || len(recoveryLogRows(t, logs)) != 1 {
		t.Fatal("outage ineligible ticks retried or alerted")
	}
	at = first.NextAt
	if _, err := d.reportAttempt(recoveryUnitContext(t, at), source, fail); !isRecoveryPending(err) {
		t.Fatal(err)
	}
	debt := d.recoveries.items[source.Key]
	reopenRecoveryDatabase(t, db, path)
	d.refreshRecoveries(t.Context(), at)
	persisted, err := db.GetReportRecovery(t.Context(), source.Key)
	if err != nil || persisted.Attempts != 1 || !persisted.NextAt.Equal(at.Add(2*time.Minute)) || persisted.State != "pending" || persisted.Reason != debt.Reason || d.recoveries.dirty[source.Key] {
		t.Fatalf("outage debt not flushed: %+v %v", persisted, err)
	}
	deliveries, err := db.ListPendingReportDeliveries(t.Context())
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("fallback invented delivery: %+v %v", deliveries, err)
	}
	if _, err := d.reportAttempt(recoveryUnitContext(t, at), source, fail); !isRecoveryPending(err) || calls != 2 {
		t.Fatal("storage repair skipped durable due")
	}
	if _, err := d.reportAttempt(recoveryUnitContext(t, persisted.NextAt), source, func() (turn.Posted, error) { calls++; return turn.Posted{CommentID: 77}, nil }); err != nil {
		t.Fatal(err)
	}
	persisted, err = db.GetReportRecovery(t.Context(), source.Key)
	if err != nil || persisted.State != "resolved" || calls != 3 {
		t.Fatalf("repair not retained: %+v %v", persisted, err)
	}
}
