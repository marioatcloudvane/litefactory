package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
)

func TestDiskObservationRoundTripAndLegacySnapshot(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	// NULL is also the state of an old row after the additive migration.
	listener := ListenerStatus{WrittenAt: at, DaemonStartedAt: at}
	if err := db.ReplaceHealthSnapshot(ctx, listener, nil); err != nil {
		t.Fatal(err)
	}
	legacy, err := db.ReadHealthSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Listener.Disk != nil {
		t.Fatalf("old snapshot fabricated measurement: %+v", legacy.Listener.Disk)
	}
	zero := uint64(0)
	complete := int64(99)
	listener.Disk = &diskstatus.Observation{Version: 1, ObservedAt: at.Add(-time.Minute), GuardEnabled: true, StopBytes: 2 << 30, FreeBytes: &zero, Held: true, Main: diskstatus.Cache{Path: "/main", Filesystem: "other", State: "incomplete", PartialBytes: 7, EntriesExamined: 3, PassStartedAt: at.Add(-time.Hour), ProgressAt: at.Add(-time.Second), CompleteBytes: &complete, CompleteAt: at.Add(-time.Hour), Error: "unreadable entry"}, Reviewer: diskstatus.Cache{Path: "/reviewer", Filesystem: "unknown", State: "unavailable"}, AutomaticCleanup: "unavailable"}
	if err := db.ReplaceHealthSnapshot(ctx, listener, nil); err != nil {
		t.Fatal(err)
	}
	got, err := db.ReadHealthSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := db.OverviewHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Listener.Disk, listener.Disk) || !reflect.DeepEqual(overview.Listener.Disk, listener.Disk) {
		t.Fatalf("round trip: %+v %+v", got.Listener.Disk, overview.Listener.Disk)
	}
	// Publication time changes independently of original measurement times.
	listener.WrittenAt = at.Add(time.Hour)
	if err := db.ReplaceHealthSnapshot(ctx, listener, nil); err != nil {
		t.Fatal(err)
	}
	got, err = db.ReadHealthSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Listener.Disk.ObservedAt.Equal(at.Add(-time.Minute)) {
		t.Fatal("sample time overwritten")
	}
}

func TestDiskObservationReadBudgetCountsBytes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	if err := db.ReplaceHealthSnapshot(ctx, ListenerStatus{}, nil); err != nil {
		t.Fatal(err)
	}
	oversized := `{"version":1,"probe_error":"` + strings.Repeat("é", 40000) + `"}`
	// SQLite TEXT length counts characters; the read budget must count bytes,
	// including values written by another version or a damaged local row.
	if _, err := db.sql.ExecContext(ctx, `UPDATE daemon_listener_status SET disk_status=? WHERE id=1`, oversized); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (HealthSnapshot, error){func() (HealthSnapshot, error) { return db.ReadHealthSnapshot(ctx) }, func() (HealthSnapshot, error) { return db.OverviewHealth(ctx) }} {
		got, err := read()
		if err != nil || !got.Found || got.Listener.Disk != nil {
			t.Fatalf("oversized observation: %+v %v", got, err)
		}
	}
}
