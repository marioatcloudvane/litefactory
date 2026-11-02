package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAlertLedgerAtomicRetentionAndRestart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "alerts.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	err = db.UpdateAlerts(ctx, func(s *AlertLedger) error {
		s.Subjects["disk"] = &AlertSubject{Rule: "disk_low", Key: "disk", Active: true, Episode: 1, Episodes: map[int64]*AlertEpisode{1: {Number: 1, OpeningConfirmed: true}}}
		s.Notifications["open"] = &AlertNotification{ID: "open", State: "uncertain", Fence: 2, AttemptedAt: at, Members: []AlertNotificationMember{{Subject: "disk", Episode: 1}}}
		s.Controls.RuleMutes["disk_low"] = at.Add(time.Hour)
		s.Receipts["message"] = AlertCommandReceipt{Source: "message", Reply: "muted", Expiry: at.Add(time.Hour)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	if err = db.UpdateAlerts(ctx, func(s *AlertLedger) error { s.Controls.OffUntil = at; delete(s.Subjects, "disk"); return rollback }); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Revision != 1 || !s.Controls.OffUntil.IsZero() || !s.Subjects["disk"].Active || !s.Subjects["disk"].Episodes[1].OpeningConfirmed || s.Notifications["open"].State != "uncertain" || s.Notifications["open"].Fence != 2 || s.Receipts["message"].Reply != "muted" || !s.Controls.RuleMutes["disk_low"].Equal(at.Add(time.Hour)) {
		t.Fatalf("restored=%+v", s)
	}
	delete(s.Subjects, "disk")
	s, err = db.ReadAlerts(ctx)
	if err != nil || s.Subjects["disk"] == nil {
		t.Fatalf("read snapshot aliases durable state: %v", err)
	}
}

func TestAlertLedgerSerializesConcurrentMutations(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	const writers = 8
	failures := make(chan error, writers)
	var joined sync.WaitGroup
	for i := range writers {
		joined.Go(func() {
			failures <- db.UpdateAlerts(t.Context(), func(l *AlertLedger) error {
				source := fmt.Sprint(i)
				l.Receipts[source] = AlertCommandReceipt{Source: source, Reply: "retained"}
				return nil
			})
		})
	}
	joined.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	l, err := db.ReadAlerts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if l.Revision != writers || len(l.Receipts) != writers {
		t.Fatalf("lost concurrent mutation: revision=%d receipts=%d", l.Revision, len(l.Receipts))
	}
}
