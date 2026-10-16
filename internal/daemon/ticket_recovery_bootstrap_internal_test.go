package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestSnapshotTicketRecoveryRetainsErrorsAndExactPosition(t *testing.T) {
	for _, scenario := range []string{"existing", "conflict", "canceled", "missing-version", "interrupted-pin"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDiscoveryHTTPFixture(t)
			ctx := t.Context()
			version, _ := subscriptionCacheFixture(t, f, "bootstrap", "https://api.github.com")
			version.RemoteID = ""
			version, err := f.d.db.RetainConnectionVersion(ctx, version)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := store.ExecutionSnapshot{TicketVersionID: version.ID, TicketRemoteID: "1"}
			if scenario == "missing-version" {
				snapshot.TicketVersionID = version.ID + 100
			}
			if scenario == "canceled" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				if _, err := f.d.snapshotTicketRecovery(canceled, snapshot); !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation replaced: %v", err)
				}
				if _, err := f.d.db.GetTicketRecoveryPosition(ctx, version.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("cancellation initialized position: %v", err)
				}
				return
			}
			if scenario == "missing-version" {
				if _, err := f.d.snapshotTicketRecovery(ctx, snapshot); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("missing version: %v", err)
				}
				return
			}
			position, err := f.d.db.InitTicketRecoveryPosition(ctx, version, version.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "interrupted-pin" {
				container := "1"
				if scenario == "conflict" {
					container = "other"
				}
				position, err = f.d.db.PinTicketRecoveryContainer(ctx, version.ID, position.Revision, container)
				if err != nil {
					t.Fatal(err)
				}
				position.ScanThrough = version.CreatedAt.Add(time.Hour)
				position.TicketCursor = `{"page":2}`
				position, err = f.d.db.CommitTicketRecoveryPage(ctx, position, position.Revision, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				got, err := f.d.snapshotTicketRecovery(ctx, snapshot)
				if scenario == "conflict" {
					if !errors.Is(err, store.ErrRecoveryConflict) {
						t.Fatalf("container conflict accepted: %+v %v", got, err)
					}
					got, err = f.d.db.GetTicketRecoveryPosition(ctx, version.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "interrupted-pin" {
					if got.ContainerID != "1" || !got.EnrollmentAt.Equal(version.CreatedAt) {
						t.Fatalf("interrupted pin not resumed: %+v", got)
					}
				} else if got != position {
					t.Fatalf("exact position changed: %+v want %+v", got, position)
				}
			}
		})
	}
}
