// Package factory owns application operations over the existing factory services and state.
package factory

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/store"
)

// Health reads the existing published factory health through a private storage adapter.
type Health struct{ db *store.DB }

// NewHealth is called by local composition; consumers receive only Read.
func NewHealth(db *store.DB) *Health { return &Health{db: db} }

// Read returns a neutral view. Running means the snapshot is at most five minutes old.
func (f *Health) Read(ctx context.Context, at time.Time) (contracts.Health, error) {
	snap, err := f.db.ReadHealthSnapshot(ctx)
	if err != nil {
		return contracts.Health{}, err
	}
	v := contracts.Health{Running: snap.Found && !snap.Listener.DaemonStartedAt.IsZero() && at.Sub(snap.Listener.WrittenAt) <= 5*time.Minute, DiscoveryPaused: snap.Listener.DiscoveryPaused, RunsInFlight: snap.Listener.RunsInFlight, InFlightSessions: snap.Listener.InFlightSessions, InFlightLabels: snap.Listener.InFlightLabels, SlotsUsed: snap.Listener.SlotsUsed, SlotsTotal: snap.Listener.SlotsTotal, PauseReason: snap.Listener.PauseReason, ExpectedResumeAt: snap.Listener.ExpectedResumeAt, RunsHeld: snap.Listener.RunsHeld, Disk: snap.Listener.Disk}
	if !snap.Listener.LastHeartbeatStartedAt.IsZero() {
		v.HeartbeatKnown = true
		v.HeartbeatAge = max(time.Duration(0), at.Sub(snap.Listener.LastHeartbeatStartedAt)).Round(time.Second)
	}
	owed := 0
	for _, r := range snap.Repos {
		if r.PollOwed {
			owed++
		}
	}
	v.Polling = fmt.Sprintf("Polling: %d repositories, %d awaiting a poll.", len(snap.Repos), owed)
	return v, nil
}
