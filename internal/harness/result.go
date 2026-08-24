package harness

import (
	"slices"
	"time"
)

// Outcome is how a run ended. There are five, and the distinctions matter:
// S-9 requires a timeout, an abandonment and a plain non-zero exit to be
// recorded as different things (AC-3.20), and the spike proved the no-result
// stall is reachable in practice rather than theoretical (SD-3).
type Outcome int

const (
	// OutcomeSucceeded means claude exited zero and emitted a terminal result
	// event with is_error false. It is the only outcome that is not a failure.
	OutcomeSucceeded Outcome = iota

	// OutcomeFailed means the process exited non-zero, or exited zero having
	// reported is_error. This is the ordinary failure.
	OutcomeFailed

	// OutcomeTimedOut means claude.timeout_seconds elapsed and the process
	// subtree was killed. Distinct from OutcomeFailed because the run may have
	// been making progress and the operator's response is different: raise the
	// timeout, or narrow the task.
	OutcomeTimedOut

	// OutcomeAbandoned means the caller's context was cancelled — the daemon is
	// shutting down (D-B). Nothing is wrong with the task, and it is not the
	// run's fault, so it is never reported as a timeout.
	OutcomeAbandoned

	// OutcomeNoResult means the process exited cleanly without ever emitting a
	// terminal result event. In the spike this was produced by a stale
	// credential: about ten internal retries, then silence.
	OutcomeNoResult
)

// String renders the outcome for logs, metadata and test failures. The values
// are stable identifiers, not prose: they are written into NNNN.meta.json.
func (o Outcome) String() string {
	switch o {
	case OutcomeSucceeded:
		return "succeeded"
	case OutcomeFailed:
		return "failed"
	case OutcomeTimedOut:
		return "timed_out"
	case OutcomeAbandoned:
		return "abandoned"
	case OutcomeNoResult:
		return "no_result"
	default:
		return "invalid"
	}
}

// SignalExitCode is the exit code recorded when there is no exit status to
// record: the process was killed by the timeout or the shutdown, so the number
// on the runs row is -1 and the reason string carries the meaning (S-9).
const SignalExitCode = -1

// RunResult is everything one claude run produced. It is a plain struct rather
// than an interface on purpose: internal/turn consumes it directly, and where
// turn needs a test double it declares its own small interface at the point of
// consumption.
//
// A RunResult is returned for failed runs too. Only a setup failure — a bad
// request, an unwritable log directory, a process that never started — comes
// back as an error, because every run that actually happened has to be
// reported, logged and have its metadata written (S-9).
type RunResult struct {
	// Outcome is how the run ended.
	Outcome Outcome

	// Reason explains a non-success outcome in one line, suitable for the
	// failure comment and for the runs row. Empty when the run succeeded.
	Reason string

	// ExitCode is claude's exit status, or [SignalExitCode] when it was killed
	// or never produced a status.
	ExitCode int

	// SessionID is the --session-id the run was given.
	SessionID string

	// LogPath is the absolute path of the NNNN.jsonl event stream. It exists
	// even for a run that produced no events, and it is quoted in the failure
	// comment so the operator can read the whole thing locally.
	LogPath string

	// Command is the argv the child was executed with, binary first. The prompt
	// is deliberately not in it — it went on stdin (AC-3.12) — which makes this
	// safe to record and to print.
	Command []string

	// Model and Tools come from the init event (AC-3.17). Tools is the surface
	// claude actually resolved from --tools, which is the real boundary and not
	// necessarily the list that was configured.
	Model string
	Tools []string

	// ResultText is the terminal result event's .result: claude's final message.
	// It is untrusted content — it is written by a model that read third-party
	// text — and must be redacted before it is posted anywhere (S-7).
	ResultText string

	// CostUSD is the run's total_cost_usd. Meaningful only when HasResult.
	CostUSD float64

	// IsError is the terminal result event's .is_error. This, never .subtype,
	// is what says whether claude thinks the run failed (SD-3).
	IsError bool

	// NumTurns is the terminal result event's turn count.
	NumTurns int

	// HasResult reports whether a terminal result event arrived at all. When it
	// is false, CostUSD, IsError, NumTurns and ResultText are all zero and the
	// run's cost is unknown rather than zero — the runs row stores NULL.
	HasResult bool

	// Events counts the JSON events read, and Malformed counts the lines that
	// were not JSON. Both are diagnostics: a run with zero events and a clean
	// exit is the SD-7 "missing --verbose" failure.
	Events    int
	Malformed int

	// LongestLine is the largest line seen, in bytes. It is recorded because
	// the 64 KB assumption that bufio.Scanner would have imposed is the exact
	// thing that silently truncates a run (SD-6), and this is the evidence that
	// it is being exceeded in the field.
	LongestLine int

	// Stderr is the tail of the child's standard error, with a marker when
	// earlier output was dropped. It is quoted in the failure comment (S-9).
	Stderr string

	// StreamError reports that reading one of claude's output pipes or writing
	// NNNN.jsonl failed. The run itself may still have succeeded; what is not
	// guaranteed is that its log and its stderr tail are complete. A descendant
	// that outlived the kill and held a pipe open shows up here too, because
	// the pipe was then closed from litefactory's side.
	StreamError error

	// StartedAt and FinishedAt bracket the subprocess, and Duration is the wall
	// clock between them — not the result event's duration_ms, which excludes
	// process startup and does not exist for a run that never finished.
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
}

// Succeeded reports whether the run finished cleanly.
func (r *RunResult) Succeeded() bool { return r != nil && r.Outcome == OutcomeSucceeded }

// Clone returns a deep copy, so a caller can hand a result to a reporting path
// without that path being able to mutate the slices behind it.
func (r *RunResult) Clone() *RunResult {
	if r == nil {
		return nil
	}
	out := *r
	out.Tools = slices.Clone(r.Tools)
	out.Command = slices.Clone(r.Command)
	return &out
}
