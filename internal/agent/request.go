package agent

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
)

// RunPath says which conversation a run is trying to have. It is the one
// decision every [Runner] implementation has to make from the same three
// values, whatever flag or argument its own tool expresses it with — Claude's
// --session-id versus --resume, for one.
type RunPath string

// The three paths a run can take.
const (
	// PathCold starts a new conversation. It is what the first run of a
	// segment does, and it is the zero value: a caller that knows nothing
	// about resuming gets the behaviour every implementation had before
	// resuming existed.
	PathCold RunPath = "cold"

	// PathResume continues the conversation [SessionRef] already names.
	PathResume RunPath = "resume"

	// PathColdAfterResumeRefused is the rerun of a resume the tool refused —
	// the segment said a conversation existed and the tool could not find
	// it. It is invoked exactly like [PathCold] and differs only in what it
	// says it is, because "this run threw away a conversation we thought we
	// had" is worth being able to grep for.
	PathColdAfterResumeRefused RunPath = "cold_after_resume_refused"
)

// String renders the path for a log attribute, naming the zero value rather
// than logging an empty string for it.
func (p RunPath) String() string {
	if p == "" {
		return string(PathCold)
	}
	return string(p)
}

// Resuming reports whether this path asks the tool to continue an existing
// conversation. Only [PathResume] does: [PathColdAfterResumeRefused] is a
// cold run that remembers where it came from.
func (p RunPath) Resuming() bool { return p == PathResume }

// Request is one coding-agent-tool run: which task, which run number, where,
// and what to say.
type Request struct {
	// Usage optionally receives incremental accounting. Nil preserves legacy callers.
	Usage *UsageCapture

	// Delivery is nil for catalog-free operations and unassigned repositories.
	Delivery *Delivery
	// PlanProposalPath is trusted transient transport, delivered only through the prompt.
	PlanProposalPath   string
	PlanOutcomePath    string
	PlanGeneration     int
	PlanRunID          int64
	PlanAttempt        string
	PlanContextPath    string
	PlanWorkingContext string
	MayAsk             bool
	PreviousMayAsk     bool

	// Ref identifies the task. It fixes the run log path and names the task
	// on every log line the run emits.
	Ref naming.Ref

	// RunOrdinal is the per-task run number, allocated by inserting the runs
	// row before the subprocess starts. It becomes the NNNN file stem.
	RunOrdinal int

	// SegmentOrdinal is the conversation segment, 1 at M0. It is recorded in
	// the metadata document and derives nothing: the identity it produced
	// arrives ready-made on Session below.
	SegmentOrdinal int

	// Session is this segment's conversation identity — see [SessionRef] for
	// why it is two strings rather than one. It is taken from the segments
	// row rather than recomputed: internal/naming derives Ref once, when the
	// row is created, and this package treats it as given. If the stored
	// value and a fresh derivation ever disagree, the stored one wins — it
	// is the one the tool has already seen.
	Session SessionRef

	// Mode is the run mode, "spike" at M0. Recorded in the metadata
	// document.
	Mode string

	// Path says which conversation this run is trying to have. The zero
	// value is a cold run, which is what a first run of a segment is.
	Path RunPath

	// WorktreePath is the run's working directory: the task's git worktree,
	// never the registered clone.
	WorktreePath string

	// Prompt is delivered on the child's stdin, never in argv (AC-3.12).
	Prompt string

	// Permissions is the tool surface the resolved run mode grants this run.
	// The zero value means no mode profile was supplied, and every implementation
	// falls back to its own configured surface — never to granting nothing,
	// which is a materially different thing to at least one implementation.
	// See [Permissions] for why its payload is opaque here.
	Permissions Permissions
}

// Validate rejects a request that would produce an unusable run rather than
// a confusing one. It checks only what every implementation needs checked;
// an implementation MAY check more (internal/harness re-checks Permissions
// against its own configured ceiling, for one).
func (req Request) Validate() error {
	if req.Usage != nil && (req.Usage.AttemptID == "" || req.Usage.Sink == nil) {
		return invalidRequest("usage capture requires attempt and sink")
	}

	if req.Delivery != nil {
		if err := req.Delivery.validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
	}
	if err := req.Ref.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if req.RunOrdinal < 1 {
		return invalidRequest("run ordinal %d is below 1", req.RunOrdinal)
	}
	if req.SegmentOrdinal < 1 {
		return invalidRequest("segment ordinal %d is below 1", req.SegmentOrdinal)
	}
	if req.Session.Ref == "" {
		return invalidRequest("session ref is empty")
	}
	if strings.TrimSpace(req.Mode) == "" {
		return invalidRequest("mode is empty")
	}
	switch req.Path {
	case "", PathCold, PathResume, PathColdAfterResumeRefused:
	default:
		return invalidRequest("run path %q is not one of cold, resume or cold_after_resume_refused", req.Path)
	}
	if req.WorktreePath == "" {
		return invalidRequest("worktree path is empty")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return invalidRequest("prompt is empty")
	}
	return nil
}
