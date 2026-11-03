package factory_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestHealthFreshnessAndHeartbeat(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "health.db"))
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
	service := factory.NewHealth(db)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	empty, err := service.Read(t.Context(), at)
	if err != nil || empty.Running || empty.HeartbeatKnown {
		t.Fatalf("missing snapshot: %+v %v", empty, err)
	}
	if err := db.ReplaceHealthSnapshot(t.Context(), store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at.Add(time.Second), Disk: &diskstatus.Observation{Version: 1, ObservedAt: at.Add(-time.Hour), Main: diskstatus.Cache{State: "partial", PartialBytes: 19}}, InFlightSessions: []int64{42}, InFlightLabels: map[int64]string{42: "due_schedule"}}, nil); err != nil {
		t.Fatal(err)
	}
	view, err := service.Read(t.Context(), at)
	if err != nil || !view.Running || !view.HeartbeatKnown || view.HeartbeatAge != 0 {
		t.Fatalf("future heartbeat: %+v %v", view, err)
	}
	if view.Disk == nil || !view.Disk.ObservedAt.Equal(at.Add(-time.Hour)) || view.Disk.Main.PartialBytes != 19 {
		t.Fatalf("disk observations lost or refreshed: %+v", view.Disk)
	}
	disk, err := factory.NewTaskFacts(db).ReadDiskStatus(t.Context())
	if err != nil || disk == nil || !disk.ObservedAt.Equal(view.Disk.ObservedAt) || disk.Main.PartialBytes != 19 {
		t.Fatalf("task disk observations: %+v %v", disk, err)
	}
	view.InFlightSessions[0] = 99
	view.InFlightLabels[42] = "changed"
	view, err = service.Read(t.Context(), at.Add(5*time.Minute))
	if err != nil || !view.Running || view.InFlightSessions[0] != 42 || view.InFlightLabels[42] != "due_schedule" {
		t.Fatalf("freshness boundary or mutable view: %+v %v", view, err)
	}
	view, err = service.Read(t.Context(), at.Add(5*time.Minute+time.Nanosecond))
	if err != nil || view.Running {
		t.Fatalf("stale snapshot: %+v %v", view, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Read(ctx, at); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
}
