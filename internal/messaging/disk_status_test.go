package messaging_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestStatusRetainsDiskCacheObservations(t *testing.T) {
	t.Parallel()
	db := database(t)
	now := time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)
	observed := now.Add(-time.Hour)
	o := &diskstatus.Observation{Version: 1, ObservedAt: observed, GuardEnabled: true, Held: true, Main: diskstatus.Cache{Path: "/fixture/main", State: "partial", PartialBytes: 19, PassStartedAt: observed, ProgressAt: observed}, Reviewer: diskstatus.Cache{Path: "/fixture/reviewer"}}
	if err := db.ReplaceHealthSnapshot(t.Context(), store.ListenerStatus{WrittenAt: now, DaemonStartedAt: now, Disk: o}, nil); err != nil {
		t.Fatal(err)
	}
	mapped, err := (messaging.FactoryStatus{DB: db}).Read(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Disk == nil || !mapped.Disk.ObservedAt.Equal(observed) {
		t.Fatalf("sample timestamp refreshed: %+v", mapped.Disk)
	}
	s := messaging.New(db, "test", "owner", []string{"channel"}, nil, messaging.FactoryStatus{DB: db})
	text, status, err := s.RunCommand(t.Context(), messaging.Message{Transport: "test", ID: "disk", Channel: "channel", Sender: "owner", Command: "status"}, now)
	if err != nil || status != "success" {
		t.Fatalf("status: %s %v", status, err)
	}
	for _, want := range []string{"/fixture/main", "/fixture/reviewer", "observed partial logical bytes=19", "complete size unavailable", "actual_hold=true", "stale", "automatic_cleanup=unavailable"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
