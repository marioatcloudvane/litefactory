package agent

import (
	"slices"
	"time"
)

// Outcome is how a run ended. There are six, and the distinctions matter: a
// timeout, an abandonment and a plain non-zero exit are recorded as
// different things, and the no-result stall is reachable in practice rather
// than theoretical (the Claude implementation measured it — a stale
// credential produces about ten internal retries and then silence).
type Outcome int

const (
	// OutcomeSucceeded means the process exited zero and reported a terminal
	// result with no error. It is the only outcome that is not a failure.
	OutcomeSucceeded Outcome = iota

	// OutcomeFailed means the process exited non-zero, or exited zero having
	// reported an error. This is the ordinary failure.
	OutcomeFailed

	// OutcomeTimedOut means the run's configured wall-clock limit elapsed
	// and the process subtree was killed. Distinct from OutcomeFailed
	// because the run may have been making progress and the operator's
	// response is different: raise the timeout, or narrow the task.
	OutcomeTimedOut

	// OutcomeAbandoned means the caller's context was cancelled — the
	// daemon is shutting down. Nothing is wrong with the task, and it is not
	// the run's fault, so it is never reported as a timeout.
	OutcomeAbandoned

	// OutcomeNoResult means the process exited cleanly without ever
	// emitting a terminal result. In the Claude implementation this was
	// produced by a stale credential: about ten internal retries, then
	// silence.
	OutcomeNoResult

	// OutcomeInterrupted means verified terminal quota exhaustion.
	OutcomeInterrupted
)

// String renders the outcome for logs, metadata and test failures. The
// values are stable identifiers, not prose: they are written into
// NNNN.meta.json.
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
	case OutcomeInterrupted:
		return "interrupted"
	case OutcomeNoResult:
		return "no_result"
	default:
		return "invalid"
	}
}

// SignalExitCode is the exit code recorded when there is no exit status to
// record: the process was killed by the timeout or the shutdown, so the
// number on the runs row is -1 and the reason string carries the meaning.
const SignalExitCode = -1

// RunResult is everything one run produced. It is a plain struct rather than
// an interface on purpose: internal/turn and internal/report consume it
// directly, and where either needs a test double they declare their own
// small interface at the point of consumption.
//
// A RunResult is returned for failed runs too. Only a setup failure — a bad
// request, an unwritable log directory, a process that never started —
// comes back as a Go error, because every run that actually happened has to
// be reported, logged and have its metadata written.
type RunResult struct {
	// Outcome is how the run ended.
	Outcome Outcome

	// Interruption carries verified quota evidence, including report-only resets.
	Interruption *Interruption

	// Reason explains a non-success outcome in one line, suitable for the
	// failure comment and for the runs row. Empty when the run succeeded.
	Reason string

	// ExitCode is the process's exit status, or [SignalExitCode] when it was
	// killed or never produced a status.
	ExitCode int

	// SessionID is the conversation identity the run was given — Session.Ref
	// or Session.Resume, whichever this implementation invoked the tool
	// with.
	SessionID string
	// ResumeToken is the tool-reported conversation handle discovered during
	// this run. It is empty for tools whose SessionID is chosen in advance.
	ResumeToken string

	// LogPath is the absolute path of the NNNN.jsonl event stream. It
	// exists even for a run that produced no events, and it is quoted in
	// the failure comment so the operator can read the whole thing locally.
	LogPath string

	// Command is the argv the child was executed with, binary first. The
	// prompt is deliberately not in it — it went on stdin — which makes
	// this safe to record and to print.
	Command []string

	// Model and Tools come from the tool's own reported surface, when it
	// reports one. Tools is the surface the tool actually resolved, which is
	// the real boundary and not necessarily the list that was configured; an
	// implementation whose tool has no such notion leaves both zero.
	Model string
	Tools []string

	// ResultText is the run's final message. It is untrusted content — it
	// is written by a model that read third-party text — and must be
	// redacted before it is posted anywhere.
	ResultText string

	// CostUSD is the run's total cost. Meaningful only when HasCost.
	CostUSD float64
	// HasCost distinguishes a reported zero cost from a tool, such as Codex,
	// whose event stream reports token usage but no monetary amount.
	HasCost bool

	// IsError reports whether the tool's own terminal event says the run
	// failed. This, and not any other field of that event, is what decides
	// it — a subtype or status string that reads "success" even on an
	// errored run has been measured on at least one implementation.
	IsError bool

	// NumTurns is the terminal event's turn count, when the tool reports
	// one.
	NumTurns int

	// HasResult reports whether a terminal result arrived at all. When it
	// is false, CostUSD, IsError, NumTurns and ResultText are all zero and
	// the run's cost is unknown rather than zero.
	HasResult bool

	// Events counts the JSON events read, and Malformed counts the lines
	// that were not JSON. Both are diagnostics.
	Events    int
	Malformed int

	// LongestLine is the largest line seen, in bytes. Every implementation
	// reads its event stream with an unbounded line reader — never a
	// bufio.Scanner, which silently truncates a run over its token limit —
	// and this is the evidence of how far that has been exercised in the
	// field.
	LongestLine int

	// Stderr is the tail of the child's standard error, with a marker when
	// earlier output was dropped. It is quoted in the failure comment.
	Stderr string

	// StreamError reports that reading one of the tool's output pipes or
	// writing NNNN.jsonl failed. The run itself may still have succeeded;
	// what is not guaranteed is that its log and its stderr tail are
	// complete.
	StreamError error

	// StartedAt and FinishedAt bracket the subprocess, and Duration is the
	// wall clock between them — not a duration a terminal event may report,
	// which excludes process startup and does not exist for a run that
	// never finished.
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
}

// Succeeded reports whether the run finished cleanly.
func (r *RunResult) Succeeded() bool { return r != nil && r.Outcome == OutcomeSucceeded }

// Clone returns a deep copy, so a caller can hand a result to a reporting
// path without that path being able to mutate the slices behind it.
func (r *RunResult) Clone() *RunResult {
	if r == nil {
		return nil
	}
	out := *r
	if r.Interruption != nil {
		interruption := *r.Interruption
		out.Interruption = &interruption
	}
	out.Tools = slices.Clone(r.Tools)
	out.Command = slices.Clone(r.Command)
	return &out
}
