package ask_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

type diskFactory struct {
	factory
	observation *diskstatus.Observation
	diskErr     error
}

func (f *diskFactory) ReadDiskStatus(context.Context) (*diskstatus.Observation, error) {
	return f.observation, f.diskErr
}

func TestFactsRetainDiskCacheObservations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)
	f := &diskFactory{observation: &diskstatus.Observation{Version: 1, ObservedAt: now.Add(-time.Hour), Main: diskstatus.Cache{Path: "/fixture/main", State: "partial", PartialBytes: 51, PassStartedAt: now.Add(-time.Hour), ProgressAt: now.Add(-time.Hour)}}}
	bundle, err := (ask.Collector{Factory: f}).Collect(t.Context(), store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range bundle.Facts {
		if fact.Handle == "disk_cache" {
			for _, want := range []string{"/fixture/main", "observed partial logical bytes=51", "stale", "complete size unavailable"} {
				if !strings.Contains(fact.Text, want) {
					t.Fatalf("missing %q: %s", want, fact.Text)
				}
			}
			return
		}
	}
	t.Fatal("disk fact missing")
}
