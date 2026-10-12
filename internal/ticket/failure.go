package ticket

import (
	"errors"
	"fmt"
)

// ErrInvalidCursor permits restarting a rejected read continuation from durable coverage.
var ErrInvalidCursor = errors.New("ticket discovery cursor is invalid")

// FailureKind distinguishes dispatch uncertainty from a confirmed refusal.
type FailureKind string

// Failure categories do not imply retry permission; the owning ledger decides.
const (
	Refused     FailureKind = "refused"
	Conflict    FailureKind = "conflict"
	Uncertain   FailureKind = "uncertain"
	Unavailable FailureKind = "unavailable"
	Unsupported FailureKind = "unsupported"
)

// Failure retains the original adapter error for errors.Is/errors.As, including
// retry metadata. Uncertain never authorizes a duplicate dispatch.
type Failure struct {
	Kind      FailureKind
	Operation string
	Cause     error
}

func (f *Failure) Error() string {
	if f.Cause == nil {
		return fmt.Sprintf("ticket: %s: %s", f.Operation, f.Kind)
	}
	return fmt.Sprintf("ticket: %s: %s: %v", f.Operation, f.Kind, f.Cause)
}
func (f *Failure) Unwrap() error { return f.Cause }

// MissingModeMapping identifies a locally refused canonical mode label whose
// connection has no explicit mapping. Other provider refusals retain their kind.
type MissingModeMapping struct{ Mode string }

func (e *MissingModeMapping) Error() string {
	return fmt.Sprintf("explicit mode label mapping is required for %s", e.Mode)
}
