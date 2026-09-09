package agent

import "time"

// ResetPrecision records the precision of the provider's reset evidence.
type ResetPrecision string

const (
	// ResetExact is an absolute reset instant.
	ResetExact ResetPrecision = "exact"
	// ResetMinute is a displayed minute, rounded to its upper boundary.
	ResetMinute ResetPrecision = "minute"
)

// Interruption is verified terminal quota evidence. A zero DueAt means the
// interruption is report-only; Reason explains why its reset cannot be used.
// It contains no provider syntax and no mutable collections.
type Interruption struct {
	ResetBound time.Time
	DueAt      time.Time
	Precision  ResetPrecision
	Reason     string
}
