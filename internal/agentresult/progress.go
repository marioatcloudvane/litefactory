package agentresult

import (
	"crypto/sha256"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Application-v1 advisory limits bound both visible text and dedup bookkeeping.
const (
	MaxProgressMessageBytes      = 2048
	MaxProgressIdentities        = 32
	MaxProgressUpdates           = 1024
	MaxTerminalProgressRetention = 24 * time.Hour
)

// ProgressBinding is current factory authority, never authored progress content.
// The factory revokes Active when the attempt ends or is superseded.
type ProgressBinding struct {
	RunID   int64
	Attempt string
	Active  bool
}

// AdvisoryProgress is agent-authored status with no completion authority.
type AdvisoryProgress struct {
	Sequence int64  `json:"sequence"`
	Message  string `json:"message"`
}

// ProgressDisposition attributes every reducer response independently of result
// receipts. Kinds are accepted, replayed, stale or rejected; Code is bounded.
type ProgressDisposition struct {
	RunID      int64
	Attempt    string
	Sequence   int64
	Kind, Code string
}
type progressIdentity struct {
	sequence int64
	hash     [sha256.Size]byte
}

// ProgressState is process-local and deliberately has no persistence or workflow
// writer. Its private fields prevent callers from constructing unbounded state.
type ProgressState struct {
	runID      int64
	attempt    string
	highest    int64
	accepted   int
	identities []progressIdentity
	latest     *AdvisoryProgress
	terminalAt *time.Time
}

// ProgressView is copied advisory data, not a result or durable receipt.
type ProgressView struct {
	RunID                               int64
	Attempt                             string
	HighestSequence                     int64
	AcceptedUpdates, RetainedIdentities int
	Latest                              *AdvisoryProgress
	TerminalAt                          *time.Time
}

func cloneProgress(s ProgressState) ProgressState {
	s.identities = slices.Clone(s.identities)
	if s.latest != nil {
		value := *s.latest
		s.latest = &value
	}
	if s.terminalAt != nil {
		value := *s.terminalAt
		s.terminalAt = &value
	}
	return s
}

// View exposes at most one visible message, with copied pointers.
func (s ProgressState) View() ProgressView {
	copied := cloneProgress(s)
	return ProgressView{RunID: copied.runID, Attempt: copied.attempt, HighestSequence: copied.highest, AcceptedUpdates: copied.accepted, RetainedIdentities: len(copied.identities), Latest: copied.latest, TerminalAt: copied.terminalAt}
}

// ReduceProgress orders advisory status for a factory-bound attempt. Gaps are
// allowed; unseen older sequences are stale and never displace the latest one.
// Retained exact replays are no-ops even at the total-update limit.
func ReduceProgress(state ProgressState, binding ProgressBinding, now time.Time, update AdvisoryProgress) (ProgressState, ProgressDisposition) {
	disposition := ProgressDisposition{RunID: binding.RunID, Attempt: binding.Attempt, Sequence: update.Sequence, Kind: "rejected"}
	same := state.runID == binding.RunID && state.attempt == binding.Attempt
	terminal := same && state.terminalAt != nil
	next := CleanTerminalProgress(state, now, false)
	if binding.RunID < 1 || strings.TrimSpace(binding.Attempt) == "" || len(binding.Attempt) > 256 || !binding.Active {
		disposition.Code = "authority"
		return next, disposition
	}
	if terminal {
		disposition.Code = "terminal"
		return next, disposition
	}
	if update.Sequence < 1 || strings.TrimSpace(update.Message) == "" || !utf8.ValidString(update.Message) {
		disposition.Code = "invalid_progress"
		return next, disposition
	}
	if len(update.Message) > MaxProgressMessageBytes {
		disposition.Code = "message_limit"
		return next, disposition
	}
	if !same {
		next = ProgressState{runID: binding.RunID, attempt: binding.Attempt}
	}
	hash := sha256.Sum256([]byte(update.Message))
	for _, identity := range next.identities {
		if identity.sequence == update.Sequence {
			if identity.hash != hash {
				disposition.Code = "sequence_conflict"
				return next, disposition
			}
			disposition.Kind = "replayed"
			return next, disposition
		}
	}
	if update.Sequence <= next.highest {
		disposition.Kind = "stale"
		return next, disposition
	}
	if next.accepted >= MaxProgressUpdates {
		disposition.Code = "update_limit"
		return next, disposition
	}
	next.accepted++
	next.highest = update.Sequence
	next.identities = append(next.identities, progressIdentity{sequence: update.Sequence, hash: hash})
	if len(next.identities) > MaxProgressIdentities {
		next.identities = slices.Clone(next.identities[len(next.identities)-MaxProgressIdentities:])
	}
	visible := update
	next.latest = &visible
	disposition.Kind = "accepted"
	return next, disposition
}

// MarkProgressTerminal records factory-supplied termination time without changing
// any workflow state. Repeated marking cannot extend the retention deadline.
func MarkProgressTerminal(state ProgressState, at time.Time) ProgressState {
	next := cloneProgress(state)
	if next.runID == 0 {
		return next
	}
	if next.terminalAt == nil || at.Before(*next.terminalAt) {
		value := at
		next.terminalAt = &value
	}
	return next
}

// CleanTerminalProgress performs explicit cleanup or expiry at 24 hours using
// caller-owned time. Factories invoke it even when no updates arrive; no timer or
// goroutine is created. Restart may discard all process-local advisory state.
func CleanTerminalProgress(state ProgressState, now time.Time, explicit bool) ProgressState {
	if explicit || (state.terminalAt != nil && !now.Before(state.terminalAt.Add(MaxTerminalProgressRetention))) {
		return ProgressState{}
	}
	return cloneProgress(state)
}
