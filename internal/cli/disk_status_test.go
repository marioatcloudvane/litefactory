package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/factory/localadmin"
)

func TestDaemonHeartbeatDiskCacheObservations(t *testing.T) {
	now := time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)
	for _, found := range []bool{false, true} {
		var b bytes.Buffer
		if err := printDaemonHeartbeat(&b, localadmin.HealthSnapshot{Found: found}, time.Time{}, true, now); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "observations unavailable") {
			t.Fatalf("missing snapshot fabricated disk reading: %s", b.String())
		}
	}
	var b bytes.Buffer
	snap := localadmin.HealthSnapshot{Found: true, Listener: localadmin.ListenerHealth{WrittenAt: now, Disk: &diskstatus.Observation{Version: 1, ObservedAt: now.Add(-time.Hour), GuardedPath: "/fixture/factory", Main: diskstatus.Cache{Path: "/fixture/main", State: "partial", PassStartedAt: now.Add(-2 * time.Hour), ProgressAt: now.Add(-time.Hour), PartialBytes: 37, EntriesExamined: 11}, Reviewer: diskstatus.Cache{Path: "/fixture/reviewer", State: "unavailable"}}}}
	if err := printDaemonHeartbeat(&b, snap, time.Time{}, true, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/fixture/main", "/fixture/reviewer", "observed partial logical bytes=37", "complete size unavailable", "stale", "actual_hold=false", "automatic_cleanup=unavailable"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("missing %q: %s", want, b.String())
		}
	}
}
