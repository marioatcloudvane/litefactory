// Package contracts defines bounded factory requests and views without transport or persistence dependencies.
package contracts

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/diskstatus"
)

// Health contains the current read-only factory view.
type Health struct {
	Running, DiscoveryPaused, HeartbeatKnown bool
	HeartbeatAge                             time.Duration
	RunsInFlight                             int
	// InFlightSessions are the sessions behind RunsInFlight; SlotsUsed and
	// SlotsTotal are the worker pool's occupancy, zero total when unknown.
	InFlightSessions []int64
	// InFlightLabels names the dispatch source of an in-flight session's
	// worker, for example "backlog+graph_owner_wait"; absent when unknown.
	InFlightLabels        map[int64]string
	SlotsUsed, SlotsTotal int
	PauseReason, Polling  string
	ExpectedResumeAt      time.Time
	// RunsHeld says why new runs wait, for example on low free disk; empty
	// while nothing holds them.
	RunsHeld string
	Disk     *diskstatus.Observation
}

// HealthReader supplies existing factory health at reply time.
type HealthReader interface {
	Read(context.Context, time.Time) (Health, error)
}
