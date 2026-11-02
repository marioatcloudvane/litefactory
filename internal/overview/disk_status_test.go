package overview

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestOverviewRetainsDiskCacheObservations(t *testing.T) {
	now := testNow()
	observed := now.Add(-time.Hour)
	o := &diskstatus.Observation{Version: 1, ObservedAt: observed, Main: diskstatus.Cache{Path: "/fixture/main", State: "partial", PartialBytes: 71, PassStartedAt: observed, ProgressAt: observed}}
	f := &fakeReader{health: store.HealthSnapshot{Found: true, Listener: store.ListenerStatus{WrittenAt: now, Disk: o}}}
	result, err := fixedService(f, now).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Health["disk_cache"])
	if err != nil {
		t.Fatal(err)
	}
	var retained diskstatus.Observation
	if err := json.Unmarshal(encoded, &retained); err != nil {
		t.Fatal(err)
	}
	if !retained.ObservedAt.Equal(observed) || retained.Main.PartialBytes != 71 || retained.Main.CompleteBytes != nil {
		t.Fatalf("observation changed: %+v", retained)
	}
}
