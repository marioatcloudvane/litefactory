package store

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Graph lifecycle values are separate from run and session lifecycle values.
const (
	GraphRunning   = "running"
	GraphSucceeded = "succeeded"
	GraphFailed    = "failed"
	GraphCancelled = "cancelled"
	GraphPending   = "pending"
)

// Graph output kinds form a closed vocabulary. Payloads are opaque bytes; any
// future publisher must redact them at its posting boundary.
const (
	GraphOutputText       = "text"
	GraphOutputJSON       = "json"
	GraphOutputDiagnostic = "diagnostic"
)

// Graph invariant errors support errors.Is without relying on SQLite messages.
var (
	ErrGraphInvariant        = errors.New("store: graph invariant violated")
	ErrGraphAttemptRunning   = errors.New("store: graph node already has a running attempt")
	ErrGraphAttemptTerminal  = errors.New("store: graph attempt already terminal")
	ErrGraphAttemptLimit     = errors.New("store: graph node dispatch limit exhausted")
	ErrGraphInstanceTerminal = errors.New("store: graph instance already terminal")
	ErrGraphDefinitionPath   = errors.New("store: invalid graph definition path")
)

// GraphDefinitionPathError identifies a syntactically unsafe reference. This
// validation never resolves roots, follows symlinks or opens the reference.
type GraphDefinitionPathError struct{ Path string }

func (e *GraphDefinitionPathError) Error() string {
	return fmt.Sprintf("%s: %q", ErrGraphDefinitionPath, e.Path)
}
func (e *GraphDefinitionPathError) Unwrap() error { return ErrGraphDefinitionPath }

// GraphDefinition pins exact source bytes by digest, without storing DOT text.
type GraphDefinition struct {
	ID                               int64
	Team, Mode                       string
	Workflow                         string
	ProfileVersion                   int
	Path, Digest, TeamSnapshotDigest string
	CreatedAt                        time.Time
}

// GraphInstance stores progress against one immutable definition.
type GraphInstance struct {
	ID, SessionID, DefinitionID      int64
	Status, CurrentNode              string
	NextDueAt, StartedAt, FinishedAt time.Time
}

// GraphNodeAttempt counts every node dispatch, including healthy loop re-entry.
// Empty Outcome is SQL NULL; zero RunID and optional times are also SQL NULL.
type GraphNodeAttempt struct {
	ID, InstanceID        int64
	Node                  string
	Attempt               int
	RunID                 int64
	LaunchID              int64
	Status, Outcome       string
	StartedAt, FinishedAt time.Time
}

// GraphOutput retains opaque output and optional owner-reply correlation.
type GraphOutput struct {
	ID, NodeAttemptID int64
	Kind              string
	Payload           []byte
	OwnerCommentID    int64
	CreatedAt         time.Time
}

// GraphTransition is append-only history, ordered by ID within an instance.
type GraphTransition struct {
	ID, InstanceID              int64
	FromNode, ToNode, OnOutcome string
	At                          time.Time
}

// BugSyncPrerequisite is supplied only by the factory after validating the pinned
// Bug graph. It names the single declared sync gate and its fixer destination.
type BugSyncPrerequisite struct {
	SyncNode, FixerNode string
}

// GraphAttemptStart reserves the next attempt. Limit zero is unbounded; a
// positive limit caps total dispatches of this node across the entire instance.
type GraphAttemptStart struct {
	BugSyncPrerequisite *BugSyncPrerequisite
	// ResearchPriorNodes maps captured ancestor stages to this shipped stage.
	ResearchPriorNodes map[int64]string
	// OwnerAllowanceSource binds a successfully allocated mechanical owner
	// continuation to one declared substantive invocation, through its trusted
	// sync prerequisite when present.
	OwnerAllowanceSource int64
	InstanceID           int64
	LaunchID             int64
	Node                 string
	RunID                int64
	Limit                int
	StartedAt            time.Time
}

// GraphCompletion commits attempt, outputs, transition and instance progress
// together. Without a transition CurrentNode stays unchanged. InstanceStatus
// defaults to running; NextDueAt is always caller-owned scheduling policy.
type GraphCompletion struct {
	Status, Outcome string
	FinishedAt      time.Time
	Outputs         []GraphOutput
	Transition      *GraphTransition
	InstanceStatus  string
	NextDueAt       time.Time
}

func validGraphPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00") || (len(path) > 1 && path[1] == ':') {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func graphTerminal(status string) bool {
	return status == GraphSucceeded || status == GraphFailed || status == GraphCancelled
}

func graphInvariant(message string) error { return fmt.Errorf("%w: %s", ErrGraphInvariant, message) }
