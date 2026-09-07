package report

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/redact"
)

// Options configures [New].
type Options struct {
	// Redactor is the S-7 pass every body goes through. Required: this package
	// has no unredacted output path, so there is nothing sensible to do with a
	// nil one.
	Redactor *redact.Redactor

	// Limit is the forge's comment size limit in bytes — github.CommentSizeLimit
	// for GitHub. Required, and passed in rather than imported, so that this
	// package depends on no forge client and stays a pure renderer.
	Limit int

	// Logger records what was cut from a body and why. Nil discards.
	Logger *slog.Logger
}

// Reporter renders outbound comment bodies.
//
// It is immutable after construction and safe for concurrent use; every render
// works on per-call state.
type Reporter struct {
	redactor *redact.Redactor
	limit    int
	log      *slog.Logger

	// budget is the size the body is assembled to, below limit by
	// [redact.Margin]. Redaction runs after assembly and can grow a body — a
	// match shorter than "[redacted]" adds bytes — so assembling to the limit
	// itself would leave Apply's blunt hard-truncate to fix it, and that cuts
	// from the bottom without knowing which parts matter.
	budget int
}

// New builds a Reporter.
//
// A nil redactor or a non-positive limit is an error here, at wiring time,
// rather than at the first render. The first render happens when a run has just
// finished, which is the worst moment to discover that the reporter cannot
// produce a body: the run is done, the work exists, and the only thing missing
// would be the comment that says so.
func New(opts Options) (*Reporter, error) {
	if opts.Redactor == nil {
		return nil, fmt.Errorf("report: %w: no redactor", ErrInvalidArgument)
	}
	if opts.Limit <= 0 {
		return nil, fmt.Errorf("report: %w: comment size limit %d is not positive", ErrInvalidArgument, opts.Limit)
	}

	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Reporter{
		redactor: opts.Redactor,
		limit:    opts.Limit,
		log:      log,
		budget:   redact.Budget(opts.Limit),
	}, nil
}

// Limit returns the comment size limit this Reporter assembles against.
func (r *Reporter) Limit() int { return r.limit }

// RenderRun renders the report for one completed run: the success form, or the
// failure form appropriate to how it ended.
//
// It renders from whatever the run recorded. A missing duration, a missing exit
// code, an absent result event or a nil [Run.Result] altogether all produce a
// usable comment rather than an error, because AC-4.1 makes silence the one
// outcome that is not allowed — a run that crashed on its way to reporting is
// exactly the run whose comment matters most. Only a malformed task ref or a
// run ordinal below 1 fails, because both make the marker unrenderable.
//
// The returned [redact.Body] has been through the S-7 pass and is within the
// configured limit. An error means no comment may be posted: the caller leaves
// the run un-reported and the watermark where it is (AC-4.9, AC-4.10).
func (r *Reporter) RenderRun(ctx context.Context, run Run) (redact.Body, error) {
	if err := run.Ref.Validate(); err != nil {
		return redact.Body{}, fmt.Errorf("report: %w: %w", ErrInvalidArgument, err)
	}
	if run.Ordinal < 1 {
		return redact.Body{}, fmt.Errorf("report: %w: run ordinal %d is not a run number", ErrInvalidArgument, run.Ordinal)
	}

	body, cuts := r.runBody(run)
	if cuts.any() {
		// Info rather than debug: a truncated report is a comment that does not
		// say everything, and the line that explains why is worth having when
		// someone asks where the rest of the output went.
		r.log.LogAttrs(ctx, slog.LevelInfo, "trimmed run report to fit the comment size limit",
			slog.String("task", run.Ref.String()),
			slog.Int("run", run.Ordinal),
			slog.Bool("output_truncated", cuts.output),
			slog.Bool("output_dropped", cuts.outputDropped),
			slog.Bool("result_truncated", cuts.result),
			slog.Bool("files_dropped", cuts.filesDropped),
			slog.Int("bytes", len(body)),
			slog.Int("budget", r.budget),
		)
	}
	return r.apply(ctx, body)
}

// RenderReply renders a run-less reply — the answer to a command litefactory
// will not act on (AC-2.18, AC-2.20). It carries the lf:reply marker, which has
// no run attribute because no run happened.
func (r *Reporter) RenderReply(ctx context.Context, reply Reply) (redact.Body, error) {
	if err := reply.Ref.Validate(); err != nil {
		return redact.Body{}, fmt.Errorf("report: %w: %w", ErrInvalidArgument, err)
	}
	if !reply.Kind.valid() {
		return redact.Body{}, fmt.Errorf("report: %w: reply kind %q", ErrInvalidArgument, reply.Kind)
	}
	if reply.Kind == ReplyScheduleConfirmed && (reply.Schedule.ID <= 0 || reply.Schedule.Outstanding < 1 ||
		(reply.Schedule.DueAt.IsZero() && len(reply.Schedule.After) == 0)) {
		return redact.Body{}, fmt.Errorf("report: %w: incomplete schedule confirmation", ErrInvalidArgument)
	}
	if reply.Kind == ReplyScheduleBlocked && reply.Schedule.ID <= 0 {
		return redact.Body{}, fmt.Errorf("report: %w: incomplete schedule blocked reply", ErrInvalidArgument)
	}
	if reply.Kind == ReplyRefusesSync && len(reply.ConflictPaths) == 0 {
		return redact.Body{}, fmt.Errorf("report: %w: sync refusal with no conflicting paths", ErrInvalidArgument)
	}
	return r.apply(ctx, reply.body())
}

// apply is the single exit from this package: every body leaves through the
// S-7 pass, and none of them leaves as a string.
//
// The whole truncate → redact → assert → hard-truncate sequence lives in
// [redact.Redactor.Apply]; this package's own trimming aims at
// [redact.Budget] so that Apply's hard-truncate stays the last resort it was
// meant to be rather than the usual path.
func (r *Reporter) apply(ctx context.Context, body string) (redact.Body, error) {
	redacted, err := r.redactor.Apply(ctx, body, r.limit)
	if err != nil {
		return redact.Body{}, fmt.Errorf("report: redact comment body: %w", err)
	}
	return redacted, nil
}
