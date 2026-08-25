package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Poster posts one comment to the forge. *github.Client satisfies it.
//
// It is declared here, at the point of consumption, because this is the only
// place in litefactory that posts anything. The body parameter is a
// [redact.Body] rather than a string, so this interface cannot be satisfied by
// something that posts unredacted text either (AC-4.6).
//
// The client behind it carries the bot credential and nothing else: `gh` lives
// in internal/identity and is reached only by `init`, and Mario's SSH key
// pushes git. A report is therefore always visibly from the bot (AC-4.11).
type Poster interface {
	PostComment(ctx context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error)
}

// Renderer renders outbound comment bodies. *report.Reporter satisfies it.
//
// Two methods because there are two shapes: a run report and the run-less reply
// of AC-2.18 and AC-2.20. They are kept apart rather than unified behind one
// "render this" call because the two carry different markers and consume the
// comment through different writes — a reply has no run row to move.
type Renderer interface {
	RenderRun(ctx context.Context, run report.Run) (redact.Body, error)
	RenderReply(ctx context.Context, reply report.Reply) (redact.Body, error)
}

// ReporterOptions are the dependencies of a [Reporter].
type ReporterOptions struct {
	// DB is the open database. Required.
	//
	// It is the concrete store and not a narrow interface, deliberately, and it
	// is the one place in this package where that is the right call. What this
	// type promises is that the result comment id, the watermark advance and the
	// run's move to reported happen in a single transaction (AC-4.10). A fake
	// store would let a test assert that promise against an implementation that
	// does not keep it, which is worse than no test at all — so the tests run
	// against real SQLite in a temp directory.
	DB *store.DB

	// Forge posts the comment. Required.
	Forge Poster

	// Renderer turns a run or a reply into a redacted body. Required.
	Renderer Renderer

	// Logger receives the reporting log lines, including the loud one that
	// explains why a run was never reported. Required.
	Logger *slog.Logger
}

func (o ReporterOptions) validate() error {
	switch {
	case o.DB == nil:
		return invalidOptions("a database handle is required")
	case o.Forge == nil:
		return invalidOptions("a forge poster is required")
	case o.Renderer == nil:
		return invalidOptions("a body renderer is required")
	case o.Logger == nil:
		return invalidOptions("a logger is required")
	default:
		return nil
	}
}

// Reporter posts what a turn did and consumes the comment that caused it.
//
// It is the second half of a turn and the only half that writes to the forge.
// [report.Reporter] renders the body; this one posts it and records that it was
// posted. The two are separate types in separate packages because rendering
// reaches nothing and can be asserted from literals, while this cannot.
//
// # The order is the whole design
//
// Post first, then commit. Every method here renders, posts, and only then
// writes to the database:
//
//   - A render failure — the fail-closed [redact] path of AC-4.9 — posts
//     nothing and writes nothing.
//   - A post failure leaves the watermark and the run status exactly as they
//     were. The comment stays unconsumed, so a later tick sees it again; the
//     run row still exists and is found by [store.DB.GetRunByTrigger], so that
//     tick reports it instead of running claude a second time (AC-3.4,
//     AC-4.10). Retrying a post is free; re-running claude is not.
//   - A database failure after a successful post is the one case that costs
//     something: the comment is on the issue and litefactory does not know it,
//     so a later tick posts a second one. That is the direction the trade has
//     to fall, because the alternative — advancing the watermark before the
//     post — turns a transient forge error into permanent silence, which
//     AC-4.1 forbids.
//
// A Reporter is immutable after construction and safe for concurrent use.
type Reporter struct {
	db       *store.DB
	forge    Poster
	renderer Renderer
	log      *slog.Logger
}

// NewReporter builds a Reporter. Every dependency is required, and a missing one
// is an error here, at wiring time, rather than at the first report — which
// happens when a run has just finished and is the worst moment to discover that
// nothing can be posted.
func NewReporter(opts ReporterOptions) (*Reporter, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return &Reporter{db: opts.DB, forge: opts.Forge, renderer: opts.Renderer, log: opts.Logger}, nil
}

// Posted is what a report left behind.
type Posted struct {
	// Skipped reports that nothing was posted and nothing was consumed: the
	// turn owed no comment, or the run had already been reported.
	Skipped bool

	// CommentID is the forge id of the comment that was posted, 0 when Skipped.
	//
	// It is set as soon as the post returns, so a non-zero id alongside an error
	// means the comment reached the forge but the transaction that records it
	// did not commit.
	CommentID int64

	// Run is the runs row after it moved to [store.RunReported]. It is the zero
	// value for a reply and for a turn that never allocated a run.
	Run store.Run
}

// RunReport is one run's report: the body to render, and what consuming the
// comment means for this particular run.
type RunReport struct {
	// Run is what the report says. Its Ref and Ordinal are the marker's, so the
	// ref must validate and the ordinal must be 1 or more.
	Run report.Run

	// SessionID is the task whose watermark advances. Required.
	SessionID int64

	// CommentID is the comment that triggered the run and is consumed by this
	// report. Required.
	CommentID int64

	// RunID is the runs row that moves to [store.RunReported]. Zero is the
	// preparation-failure path of AC-4.1: no run row was ever allocated, so
	// there is nothing to move and the watermark advances on its own.
	RunID int64
}

func (rep RunReport) validate() error {
	switch {
	case rep.SessionID <= 0:
		return invalidRequest("a session id is required to report a run")
	case rep.CommentID <= 0:
		return invalidRequest("a triggering comment id is required to report a run")
	case rep.RunID < 0:
		return invalidRequest("run id %d is not a run", rep.RunID)
	}
	if err := rep.Run.Ref.Validate(); err != nil {
		return invalidRequest("task ref: %v", err)
	}
	if rep.Run.Ordinal < 1 {
		return invalidRequest("run ordinal %d is not a run number", rep.Run.Ordinal)
	}
	return nil
}

// ReplyRequest is a run-less answer to a comment litefactory will not act on.
type ReplyRequest struct {
	// Ref is the task the comment is on. It appears in the marker, so it must
	// validate.
	Ref naming.Ref

	// SessionID is the task whose watermark advances. Required.
	SessionID int64

	// CommentID is the comment being answered and consumed. Required.
	CommentID int64

	// Command is the parsed comment, from [ParseCommand]. It must be one that
	// needs a reply: an unsupported or unknown command, or a bare /spike
	// (AC-2.18, AC-2.20). Free text draws no reply — it starts a run and is
	// answered by the run report — and an empty comment is consumed with
	// [Reporter.Consume] instead.
	Command Command
}

func (req ReplyRequest) validate() error {
	switch {
	case req.SessionID <= 0:
		return invalidRequest("a session id is required to reply to a comment")
	case req.CommentID <= 0:
		return invalidRequest("a comment id is required to reply to a comment")
	case !req.Command.NeedsReply():
		return invalidRequest("%s draws no reply", req.Command.Kind)
	}
	if err := req.Ref.Validate(); err != nil {
		return invalidRequest("task ref: %v", err)
	}
	return nil
}

// ReportTurn posts the report for a finished turn and consumes its comment.
//
// It is the ordinary path: the daemon runs [Execute] and hands the [Result]
// straight to this. A turn that owes no comment — a contended lock, a replay of
// something already reported — returns a skipped [Posted] and writes nothing,
// which is what keeps the locked comment available for a later tick (AC-3.1).
//
// Everything else is reported, including a turn that never reached claude
// (AC-4.1). See [Reporter] for what happens when a step fails.
func (r *Reporter) ReportTurn(ctx context.Context, result Result) (Posted, error) {
	if !result.Reportable() {
		r.log.DebugContext(ctx, "turn owes the issue no comment",
			slog.Int64("session_id", result.SessionID),
			slog.Int64("trigger_comment_id", result.TriggerCommentID),
			slog.String("disposition", result.Disposition.String()))
		return Posted{Skipped: true, Run: result.Run}, nil
	}

	body, err := r.runInput(ctx, result)
	if err != nil {
		return Posted{}, err
	}
	return r.ReportRun(ctx, RunReport{
		Run:       body,
		SessionID: result.SessionID,
		CommentID: result.TriggerCommentID,
		RunID:     result.Run.ID,
	})
}

// ReportRun posts one run's report, records the result comment id, advances the
// watermark and moves the run to [store.RunReported].
//
// The last three are one transaction ([store.DB.CompleteRunReport]) and they
// happen only after the post has returned a comment id — see [Reporter] for why
// that order is not negotiable.
//
// A run that is already reported is a no-op: it posts nothing and returns a
// skipped [Posted]. That is the crash-replay case, and it is checked here rather
// than left to the caller so that no path through the daemon can produce a
// duplicate comment for the same run.
func (r *Reporter) ReportRun(ctx context.Context, rep RunReport) (Posted, error) {
	if err := rep.validate(); err != nil {
		return Posted{}, err
	}

	ref := rep.Run.Ref
	log := r.log.With(
		slog.String("task", ref.String()),
		slog.Int64("session_id", rep.SessionID),
		slog.Int64("trigger_comment_id", rep.CommentID),
		slog.Int("run_ordinal", rep.Run.Ordinal),
	)

	skip, existing, err := r.reported(ctx, rep)
	if err != nil {
		return Posted{}, err
	}
	if skip {
		log.InfoContext(ctx, "run has already been reported; posting nothing",
			slog.Int64("run_id", rep.RunID),
			slog.Int64("result_comment_id", existing.ResultCommentID))
		return Posted{Skipped: true, Run: existing}, nil
	}

	body, err := r.renderer.RenderRun(ctx, rep.Run)
	if err != nil {
		// AC-4.9: nothing is posted, the run stays un-reported and the watermark
		// does not move. internal/redact has already logged the panic itself;
		// this line is the one that names the consequence.
		log.ErrorContext(ctx, "could not render the run report; nothing was posted, "+
			"the run stays unreported and the comment stays unconsumed",
			slog.Any("error", err))
		return Posted{}, fmt.Errorf("render the report for run %d of task %s: %w", rep.Run.Ordinal, ref, err)
	}

	comment, err := r.post(ctx, ref, body, log)
	if err != nil {
		return Posted{}, err
	}
	posted := Posted{CommentID: comment.ID}

	// The comment exists now. The writes that record it are not allowed to be
	// skipped because the daemon happens to be shutting down: a cancelled
	// context here would cost a duplicate comment on the next start.
	commit := context.WithoutCancel(ctx)
	if rep.RunID == 0 {
		// No run row was ever allocated (a preparation failure), so there is no
		// status to move and the watermark advances on its own.
		if _, err := r.db.AdvanceWatermark(commit, rep.SessionID, rep.CommentID); err != nil {
			log.ErrorContext(ctx, "posted the report but could not consume the comment; "+
				"a later tick will report it again",
				slog.Int64("comment_id", comment.ID), slog.Any("error", err))
			return posted, fmt.Errorf("consume comment %d of task %s: %w", rep.CommentID, ref, err)
		}
	} else {
		run, err := r.db.CompleteRunReport(commit, rep.RunID, comment.ID)
		if err != nil {
			log.ErrorContext(ctx, "posted the report but could not record it; "+
				"the run stays unreported and a later tick will report it again",
				slog.Int64("comment_id", comment.ID), slog.Any("error", err))
			return posted, fmt.Errorf("record the report of run %d of task %s: %w", rep.Run.Ordinal, ref, err)
		}
		posted.Run = run
	}

	log.InfoContext(ctx, "posted the run report",
		slog.Int64("comment_id", comment.ID),
		slog.Int("bytes", body.Len()),
		slog.Bool("truncated", body.Truncated()),
		slog.Any("redactions", body.Counts()))
	return posted, nil
}

// ReportReply posts a run-less reply and consumes the comment it answers.
//
// No run exists on this path, so the transaction shape is different: there is no
// row to move to [store.RunReported] and nothing to record a result comment id
// against. The watermark still advances, because an unsupported command that was
// answered is a comment that has been dealt with (AC-2.18, AC-2.20) — and it
// advances after the post, for the same reason a run report's does.
func (r *Reporter) ReportReply(ctx context.Context, req ReplyRequest) (Posted, error) {
	if err := req.validate(); err != nil {
		return Posted{}, err
	}
	kind, ok := replyKind(req.Command.Kind)
	if !ok {
		return Posted{}, invalidRequest("%s draws no reply", req.Command.Kind)
	}

	log := r.log.With(
		slog.String("task", req.Ref.String()),
		slog.Int64("session_id", req.SessionID),
		slog.Int64("trigger_comment_id", req.CommentID),
		slog.String("reply", kind.String()))

	consumed, err := r.consumed(ctx, req.SessionID, req.CommentID)
	if err != nil {
		return Posted{}, err
	}
	if consumed {
		log.InfoContext(ctx, "comment has already been consumed; posting no reply")
		return Posted{Skipped: true}, nil
	}

	body, err := r.renderer.RenderReply(ctx, report.Reply{
		Ref:     req.Ref,
		Kind:    kind,
		Command: req.Command.Name,
	})
	if err != nil {
		log.ErrorContext(ctx, "could not render the reply; nothing was posted and "+
			"the comment stays unconsumed", slog.Any("error", err))
		return Posted{}, fmt.Errorf("render the %s reply for task %s: %w", kind, req.Ref, err)
	}

	comment, err := r.post(ctx, req.Ref, body, log)
	if err != nil {
		return Posted{}, err
	}
	posted := Posted{CommentID: comment.ID}

	if _, err := r.db.AdvanceWatermark(context.WithoutCancel(ctx), req.SessionID, req.CommentID); err != nil {
		log.ErrorContext(ctx, "posted the reply but could not consume the comment; "+
			"a later tick will reply again",
			slog.Int64("comment_id", comment.ID), slog.Any("error", err))
		return posted, fmt.Errorf("consume comment %d of task %s: %w", req.CommentID, req.Ref, err)
	}

	log.InfoContext(ctx, "posted the reply",
		slog.Int64("comment_id", comment.ID), slog.Int("bytes", body.Len()))
	return posted, nil
}

// Consume advances the watermark past a comment that is answered with silence:
// a body with nothing in it, which is recorded and ignored, and litefactory's
// own or a third party's comment, which the filter already recorded and which
// nothing acts on (AC-2.14, AC-2.15).
//
// Nothing is posted, so there is no post to fail and no ordering to get wrong.
// It exists here, next to the two paths that do post, so that every way a
// comment can be consumed is in one file and none of them is a bare
// [store.DB.AdvanceWatermark] call somewhere in the daemon loop.
func (r *Reporter) Consume(ctx context.Context, sessionID, commentID int64) (store.Watermark, error) {
	switch {
	case sessionID <= 0:
		return store.Watermark{}, invalidRequest("a session id is required to consume a comment")
	case commentID <= 0:
		return store.Watermark{}, invalidRequest("a comment id is required to consume a comment")
	}

	wm, err := r.db.AdvanceWatermark(ctx, sessionID, commentID)
	if err != nil {
		return store.Watermark{}, fmt.Errorf("consume comment %d on session %d: %w", commentID, sessionID, err)
	}
	r.log.DebugContext(ctx, "consumed a comment without replying",
		slog.Int64("session_id", sessionID),
		slog.Int64("comment_id", commentID),
		slog.Int64("watermark", wm.LastCommentID))
	return wm, nil
}

// post is the single call to the forge in this package.
//
// The repository and issue come from the task ref rather than from a separate
// parameter, so a report cannot be posted onto an issue other than the one whose
// marker it carries.
func (r *Reporter) post(ctx context.Context, ref naming.Ref, body redact.Body, log *slog.Logger) (github.Comment, error) {
	comment, err := r.forge.PostComment(ctx, github.RepoFromRef(ref), ref.Issue, body)
	if err != nil {
		// AC-4.10: this is the failure that must change nothing. Everything
		// below this line in the calling method is a database write, and none of
		// them has run.
		log.ErrorContext(ctx, "could not post the comment; the watermark and the run are untouched, "+
			"so a later tick retries the post without running claude again",
			slog.Int("bytes", body.Len()), slog.Any("error", err))
		return github.Comment{}, fmt.Errorf("post the comment on task %s: %w", ref, err)
	}
	if comment.ID == 0 {
		// Defensive: a comment id of zero cannot be recorded as
		// runs.result_comment_id, and the store would refuse it anyway.
		return github.Comment{}, fmt.Errorf("post the comment on task %s: %w: the forge returned no comment id",
			ref, ErrInvalidRequest)
	}
	return comment, nil
}

// reported reports whether this run has already been reported, and returns the
// row so the caller can say which comment carries it.
func (r *Reporter) reported(ctx context.Context, rep RunReport) (bool, store.Run, error) {
	if rep.RunID == 0 {
		// No run row, so the watermark is the only record that this comment was
		// dealt with.
		consumed, err := r.consumed(ctx, rep.SessionID, rep.CommentID)
		return consumed, store.Run{}, err
	}

	existing, err := r.db.GetRun(ctx, rep.RunID)
	if err != nil {
		return false, store.Run{}, fmt.Errorf("read run %d: %w", rep.RunID, err)
	}
	return existing.Status == store.RunReported, existing, nil
}

// consumed reports whether the watermark has already moved past this comment.
//
// It is the run-less half of the replay guard. A missing watermark is not
// consumed: a task with no watermark row has had nothing consumed at all.
func (r *Reporter) consumed(ctx context.Context, sessionID, commentID int64) (bool, error) {
	wm, exists, err := r.db.GetWatermark(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("read the watermark of session %d: %w", sessionID, err)
	}
	return exists && wm.LastCommentID >= commentID, nil
}

// runInput turns a finished turn into the render input for its report.
//
// It is the whole translation between what a turn produced and what a comment
// says, and it lives here rather than in internal/report because that package
// deliberately takes none of these types wholesale.
func (r *Reporter) runInput(ctx context.Context, result Result) (report.Run, error) {
	ordinal := result.Run.Ordinal
	if ordinal < 1 {
		// A preparation failure never allocated a run row, and so never got an
		// ordinal — but the marker needs one and AC-4.1 still wants the comment.
		// The number the run would have had is the honest answer.
		next, err := r.nextOrdinal(ctx, result.SessionID)
		if err != nil {
			return report.Run{}, err
		}
		ordinal = next
	}

	return report.Run{
		Ref:          result.Ref,
		Ordinal:      ordinal,
		Result:       result.Harness,
		Branch:       result.Branch,
		CommitSHA:    result.CommitSHA,
		Pushed:       result.Pushed,
		Files:        result.Changes,
		LogPath:      result.LogPath,
		Failure:      turnFailure(result),
		WorktreePath: result.WorktreePath,
	}, nil
}

// turnFailure is the failure that was not the run's own, as the report should
// word it.
//
// A preparation failure is reported by its one-line reason — "could not fetch
// from origin: …" — rather than by the bare cause it wraps, because the cause on
// its own is a git error with no account of what litefactory was trying to do.
// Everywhere else the error is passed through as it stands, so a *git.PushError
// still reaches the report as itself.
func turnFailure(result Result) error {
	if result.Disposition == DispositionPrepareFailed && result.Reason != "" {
		return errors.New(result.Reason)
	}
	return result.Err
}

// nextOrdinal is the number the next run of this task would be allocated.
func (r *Reporter) nextOrdinal(ctx context.Context, sessionID int64) (int, error) {
	runs, err := r.db.ListRuns(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("read the runs of session %d: %w", sessionID, err)
	}
	if len(runs) == 0 {
		return 1, nil
	}
	// ListRuns is ordered by ordinal, so the last row holds the highest.
	return runs[len(runs)-1].Ordinal + 1, nil
}

// replyKind maps a parsed command onto the reply internal/report renders for it.
//
// The two enums are separate on purpose — internal/report cannot import this
// package without a cycle — and this is the three-line mapping its documentation
// says belongs at the call site.
func replyKind(kind CommandKind) (report.ReplyKind, bool) {
	switch kind {
	case KindSpikeEmpty:
		return report.ReplyEmptySpike, true
	case KindUnsupported:
		return report.ReplyUnsupportedCommand, true
	case KindUnknown:
		return report.ReplyUnknownCommand, true
	case KindNone, KindSpike, KindFreeText:
		return report.ReplyInvalid, false
	default:
		return report.ReplyInvalid, false
	}
}
