package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
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
	RenderReportDelivery(ctx context.Context, body, marker string) (redact.Body, error)
}

// ReporterOptions are the dependencies of a [Reporter].
type ReporterOptions struct {
	// BotUserID and StoryLockDir enable recoverable managed-story acknowledgements.
	// StoryLockDir is config.Paths.StoryProgressLocksDir, shared by every process.
	BotUserID    int64
	StoryLockDir string
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
	case o.BotUserID <= 0:
		return invalidOptions("a numeric bot identity is required for report recovery")
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
// A durable intent precedes dispatch. Once dispatch may have happened, retries
// recover a trustworthy receipt or wait; they never infer non-delivery from an
// empty lookup. Confirmed receipts permit local atomic completion only.
//
// A Reporter is immutable after construction and safe for concurrent use.
type Reporter struct {
	botUserID    int64
	storyLockDir string
	db           *store.DB
	forge        Poster
	renderer     Renderer
	log          *slog.Logger
}

// NewReporter builds a Reporter. Every dependency is required, and a missing one
// is an error here, at wiring time, rather than at the first report — which
// happens when a run has just finished and is the worst moment to discover that
// nothing can be posted.
func NewReporter(opts ReporterOptions) (*Reporter, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if opts.StoryLockDir != "" {
		if err := os.MkdirAll(opts.StoryLockDir, 0o700); err != nil {
			return nil, err
		}
		info, err := os.Lstat(opts.StoryLockDir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.Join(fmt.Errorf("story acknowledgement lock directory must be a real directory"), err)
		}
	}
	return &Reporter{db: opts.DB, forge: opts.Forge, renderer: opts.Renderer, log: opts.Logger, botUserID: opts.BotUserID, storyLockDir: opts.StoryLockDir}, nil
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
	// report. Required, and it is the value the watermark advances to.
	//
	// Zero is the issue body rather than a comment: a run started by assigning
	// an issue to the owner has no triggering comment to consume (AC-2.16b).
	// The advance is then a MAX(0, 0) no-op, which is the whole reason the body
	// carries 0 instead of the issue's forge id — a positive id here would
	// consume every comment the issue will ever receive (AC-2.23).
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
	// Not "positive": 0 is a run triggered by the issue body, which owes the
	// issue a report like any other run. See [RunReport.CommentID].
	case rep.CommentID < 0:
		return invalidRequest("trigger id %d is neither a comment nor the issue body", rep.CommentID)
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
	Creation           *report.CreationReply
	CreationAcceptance *report.CreationAcceptance
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
	//
	// It is not read when Refusal is set: a refusal answers issue metadata and
	// has the same shape whatever the comment asked for.
	Command Command

	// Refusal, when set, makes this a metadata refusal rather than an answer to
	// the command. This includes the retained obsolete lf:tools= refusal.
	//
	// It has to be a field of its own rather than another [CommandKind], because
	// a refusal is drawn by precisely the commands that *do* spawn a run. Both
	// guards on this path — [ReplyRequest.validate] and [ReplyRequest.kind] —
	// ask about the command, and both give the wrong answer for a refusal; this
	// is what they branch on first.
	Refusal report.ReplyKind

	// Agent is the coding-agent-tool name an lf:agent= label asked for. It
	// belongs to [report.ReplyUnknownAgent] alone and is empty everywhere
	// else, for the same S-9 reason [Profile] is: the malformed and ambiguous
	// agent refusals are kept structurally unable to echo label text.
	// internal/report checks it against the agent-name pattern again before
	// rendering it (S-8).
	Agent string

	// OwningAgent and RequestedAgent belong to
	// [report.ReplyAgentSwitchRefused] alone (US-3): the coding-agent tool a
	// session's segment already belongs to, and the one this turn's labels
	// resolved to instead. Both have already passed the agent-name shape
	// check the label ladder applies, and internal/report checks them again
	// before rendering (S-8).
	OwningAgent    string
	RequestedAgent string

	// SyncBehind, SyncBase and SyncMaxBehind belong to
	// [report.ReplyRefusesSyncTooFarBehind] alone: the distance that
	// exceeded the cap, the ref it was measured against, and the cap.
	SyncBehind    int
	SyncBase      string
	SyncDetail    string
	SyncPhase     string
	SyncMaxBehind int

	// ConflictPaths belongs to [report.ReplyRefusesSync] alone (US-4): the
	// paths [git.Worktree.MergePreview] reported as conflicting, carried
	// through exactly as syncBase recorded them on [Result.SyncConflicts].
	// internal/report renders them inside a fenced block (S-2) and does not
	// sanitise or decide which paths are safe to show at all — that is
	// US-5's job.
	ConflictPaths []string

	// ConflictProposal and ConflictSuggestionFailed belong to
	// [report.ReplyRefusesSync]/[report.ReplySuggestsResolution] alone
	// (US-11): the persisted suggestion, when one was produced, and the
	// added one-line "no suggestion could be produced" note, when
	// suggestions were attempted but none resulted. internal/report decides
	// which of the two to render — this package only carries the facts
	// through from [Result.ConflictProposal] and
	// [Result.ConflictSuggestionFailed].
	ConflictProposal         *report.ConflictProposal
	ConflictSuggestionFailed bool
	ConflictSuggestionReason string

	// ScheduleKind and Schedule describe a factory-owned scheduling response.
	// They never carry the scheduled payload, so agent input is not echoed into
	// Litefactory's signed comment.
	ScheduleKind report.ReplyKind
	Schedule     report.ScheduleReply

	// ConfirmKind and Confirm describe a factory-owned /confirm response
	// (US-12), the same shape ScheduleKind/Schedule already give scheduling:
	// internal/daemon's confirmation dispatch is the only caller that sets
	// these, and [report.ReplyKind.IsConfirmationReply] is the closed set
	// [ReplyRequest.kind] and [ReplyRequest.validate] check membership in.
	ConfirmKind report.ReplyKind
	Confirm     report.ConfirmReply
}

// refusing reports whether this request is a tool-permission or
// agent-selection refusal rather than an answer to the comment's command.
func (req ReplyRequest) refusing() bool { return req.Refusal != report.ReplyInvalid }

// kind is the reply internal/report should render for this request.
//
// A refusal names its own kind; everything else is the command's, through the
// mapping at [replyKind].
func (req ReplyRequest) kind() (report.ReplyKind, bool) {
	if req.CreationAcceptance != nil {
		return report.ReplyCreationAccepted, true
	}
	if req.Creation != nil {
		return report.ReplyCreationProposal, true
	}
	if req.ScheduleKind != report.ReplyInvalid {
		return req.ScheduleKind, req.ScheduleKind == report.ReplyScheduleInvalid ||
			req.ScheduleKind == report.ReplyScheduleConfirmed ||
			req.ScheduleKind == report.ReplyScheduleBlocked ||
			req.ScheduleKind == report.ReplyScheduleProposalReady ||
			req.ScheduleKind == report.ReplyScheduleProposalAccepted ||
			req.ScheduleKind == report.ReplyScheduleProposalRejected ||
			req.ScheduleKind == report.ReplyScheduleProposalFailed
	}
	if req.ConfirmKind != report.ReplyInvalid {
		return req.ConfirmKind, req.ConfirmKind.IsConfirmationReply()
	}
	if req.refusing() {
		return req.Refusal, req.Refusal.RefusesTools() || req.Refusal.RefusesAgent() ||
			req.Refusal.RefusesSwitch() || req.Refusal.RefusesMode() || req.Refusal.RefusesSync()
	}
	return replyKind(req.Command.Kind)
}

func (req ReplyRequest) validate() error {
	switch {
	case req.CreationAcceptance != nil && (req.Creation != nil || req.refusing() || req.ScheduleKind != report.ReplyInvalid || req.ConfirmKind != report.ReplyInvalid):
		return invalidRequest("creation acceptance cannot be combined with another reply")
	case req.SessionID <= 0:
		return invalidRequest("a session id is required to reply to a comment")
	// A refusal needs one as much as any other reply does. The issue-body path
	// has no comment to answer and does not come here at all: it refuses the run
	// and logs it, and the next comment on the issue is refused on the thread.
	case req.CommentID < 0 || (req.CommentID == 0 && req.Refusal != report.ReplyBugUnavailable):
		return invalidRequest("a comment id is required to reply to a comment")
	case req.refusing() && !req.Refusal.RefusesTools() && !req.Refusal.RefusesAgent() &&
		!req.Refusal.RefusesSwitch() && !req.Refusal.RefusesMode() && !req.Refusal.RefusesSync():
		return invalidRequest("reply kind %s is not a tool-permission, agent-selection, "+
			"mode-selection, agent-switch or sync refusal", req.Refusal)
	case req.ScheduleKind != report.ReplyInvalid && req.ScheduleKind != report.ReplyScheduleInvalid &&
		req.ScheduleKind != report.ReplyScheduleConfirmed && req.ScheduleKind != report.ReplyScheduleBlocked &&
		req.ScheduleKind != report.ReplyScheduleProposalReady && req.ScheduleKind != report.ReplyScheduleProposalAccepted &&
		req.ScheduleKind != report.ReplyScheduleProposalRejected && req.ScheduleKind != report.ReplyScheduleProposalFailed:
		return invalidRequest("reply kind %s is not a schedule response", req.ScheduleKind)
	case req.ScheduleKind != report.ReplyInvalid && req.refusing():
		return invalidRequest("a reply cannot be both a schedule response and a refusal")
	case req.ConfirmKind != report.ReplyInvalid && !req.ConfirmKind.IsConfirmationReply():
		return invalidRequest("reply kind %s is not a confirmation response", req.ConfirmKind)
	case req.ConfirmKind != report.ReplyInvalid && (req.refusing() || req.ScheduleKind != report.ReplyInvalid):
		return invalidRequest("a reply cannot be both a confirmation response and a refusal or a schedule response")
	// Only when this is not a refusal, schedule response or confirmation
	// response. A refusal is triggered by a command that would have spawned
	// a run, and a command that spawns a run draws no reply — so asking this
	// question of a refusal refuses the refusal.
	case req.Creation == nil && req.CreationAcceptance == nil && !req.refusing() && req.ScheduleKind == report.ReplyInvalid && req.ConfirmKind == report.ReplyInvalid &&
		!req.Command.NeedsReply():
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
	if posted, pending, err := r.ReportPending(ctx, result.SessionID, result.TriggerCommentID); pending || err != nil {
		return posted, err
	}
	if !result.Reportable() {
		r.log.DebugContext(ctx, "turn owes the issue no comment",
			slog.Int64("session_id", result.SessionID),
			slog.Int64("trigger_comment_id", result.TriggerCommentID),
			slog.String("disposition", result.Disposition.String()))
		return Posted{Skipped: true, Run: result.Run}, nil
	}

	if result.Disposition == DispositionAgentSwitchRefused {
		return r.reportAgentSwitchRefusal(ctx, result)
	}
	if result.Disposition == DispositionSyncRefused {
		return r.reportSyncRefusal(ctx, result)
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

// reportAgentSwitchRefusal answers a [DispositionAgentSwitchRefused] turn
// (US-3): an automatic-recovery continuation whose resolved coding-agent tool
// no longer owns the segment it named, because the label changed between
// when the continuation was recorded and when it was dispatched. No
// session-state transition has occurred and no runs row has been allocated;
// the worktree already exists on disk and sessions.worktree_path already
// records it, and both are idempotent and reused by the next turn. Because no
// run was allocated, this takes the reply shape every other label-driven
// refusal uses ([ReplyRequest]/[ReportReply]) rather than the run-report one.
//
// The issue-body trigger is the one path that posts nothing, mirroring
// [Daemon.refuseAgent] and its siblings in internal/daemon exactly: a run
// started from the issue body has no comment id to answer with, and
// [ReplyRequest] requires a real one. The refusal still stands — nothing was
// run — and the next comment on the issue is what reaches the thread with it
// (AC-2.16c's issue-body convention).
func (r *Reporter) reportAgentSwitchRefusal(ctx context.Context, result Result) (Posted, error) {
	log := r.log.With(
		slog.String("task", result.Ref.String()),
		slog.Int64("session_id", result.SessionID),
		slog.Int64("trigger_comment_id", result.TriggerCommentID),
		slog.String("owning_agent", result.AgentOwning),
		slog.String("requested_agent", result.AgentRequested))

	if result.TriggerCommentID == 0 {
		log.InfoContext(ctx, "refusing to run the issue body: its resolved coding-agent tool does "+
			"not own this task's conversation. Nothing was run and no comment was posted, because "+
			"there is no comment to answer; the next comment on this issue is refused on the thread")
		return Posted{Skipped: true}, nil
	}

	return r.ReportReply(ctx, ReplyRequest{
		Ref:            result.Ref,
		SessionID:      result.SessionID,
		CommentID:      result.TriggerCommentID,
		Refusal:        report.ReplyAgentSwitchRefused,
		OwningAgent:    result.AgentOwning,
		RequestedAgent: result.AgentRequested,
	})
}

// reportSyncRefusal answers a [DispositionSyncRefused] turn (US-4): a task
// branch that could not be merged with the base branch cleanly. No
// session-state transition has occurred and no runs row has been allocated,
// and the branch itself was never touched — [git.Worktree.MergePreview] only
// ever inspects whether a merge would be clean. Because no run was
// allocated, this takes the reply shape every other refusal uses
// ([ReplyRequest]/[ReportReply]) rather than the run-report one, the same as
// [Reporter.reportAgentSwitchRefusal].
//
// The issue-body trigger is the one path that posts nothing, for the same
// reason [Reporter.reportAgentSwitchRefusal] does: a run started from the
// issue body has no comment id to answer with, and [ReplyRequest] requires a
// real one. The refusal still stands — nothing was run — and the next
// comment on this issue is what reaches the thread with it (AC-2.16c's
// issue-body convention).
func (r *Reporter) reportSyncRefusal(ctx context.Context, result Result) (Posted, error) {
	log := r.log.With(
		slog.String("task", result.Ref.String()),
		slog.Int64("session_id", result.SessionID),
		slog.Int64("trigger_comment_id", result.TriggerCommentID),
		slog.Int("conflicts", len(result.SyncConflicts)))

	if result.SyncTooFarBehind {
		if result.TriggerCommentID == 0 {
			log.InfoContext(ctx, "refusing to run the issue body: this task's branch is further behind the "+
				"base branch than the cap allows. Nothing was run and no comment was posted, because there "+
				"is no comment to answer")
			return Posted{Skipped: true}, nil
		}
		return r.ReportReply(ctx, ReplyRequest{
			Ref:           result.Ref,
			SessionID:     result.SessionID,
			CommentID:     result.TriggerCommentID,
			Refusal:       report.ReplyRefusesSyncTooFarBehind,
			SyncBehind:    result.SyncBehind,
			SyncBase:      result.SyncBase,
			SyncMaxBehind: result.SyncMaxBehind,
			SyncDetail:    result.SyncDetail,
			SyncPhase:     result.SyncPhase,
		})
	}

	if result.TriggerCommentID == 0 {
		log.InfoContext(ctx, "refusing to run the issue body: the base branch could not be merged into "+
			"this task's branch cleanly. Nothing was run and no comment was posted, because there is no "+
			"comment to answer; the next comment on this issue is refused on the thread")
		return Posted{Skipped: true}, nil
	}

	req := ReplyRequest{
		Ref:           result.Ref,
		SessionID:     result.SessionID,
		CommentID:     result.TriggerCommentID,
		Refusal:       report.ReplyRefusesSync,
		ConflictPaths: result.SyncConflicts,
		SyncDetail:    result.SyncDetail,
		SyncPhase:     result.SyncPhase,
	}
	// US-11: a persisted suggestion draws its own reply kind; a suggestion
	// that was attempted and failed still draws the plain refusal, plus its
	// one added line. Neither is ever set unless
	// [Options.SuggestConflictResolution] was true for this turn (AC-11.3).
	switch {
	case result.ConflictProposal != nil:
		req.Refusal = report.ReplySuggestsResolution
		req.ConflictProposal = conflictProposalReply(*result.ConflictProposal)
	case result.ConflictSuggestionFailed:
		req.ConflictSuggestionFailed = true
		req.ConflictSuggestionReason = result.ConflictSuggestionReason
	}
	return r.ReportReply(ctx, req)
}

// conflictProposalReply maps stored raw metrics and sanitized preview values;
// no statistics are derived from displayed or redacted text.
func conflictProposalReply(proposal store.ConflictProposal) *report.ConflictProposal {
	files := make([]report.ConflictProposalFile, 0, len(proposal.Files))
	for _, f := range proposal.Files {
		resolution := f.Resolution
		if proposal.Format == store.ConflictRegionFormat {
			resolution = ""
			for _, r := range f.Details.Regions {
				resolution += r.Resolution
			}
		}
		files = append(files, report.ConflictProposalFile{Path: f.Path, Resolution: resolution, Diff: f.Details.Diff, Metrics: report.ConflictPreviewMetrics(f.Details.Metrics)})
	}
	return &report.ConflictProposal{ID: proposal.ID, Model: proposal.Model, Files: files, Format: proposal.Format, BranchSHA: proposal.BranchSHA, TargetPhase: proposal.Inputs.TargetPhase, TargetRef: proposal.Inputs.TargetRef, TargetSHA: proposal.BaseSHA, BaseRef: proposal.Inputs.BaseRef, BaseSHA: proposal.Inputs.BaseSHA, TaskRef: proposal.Inputs.TaskRef, TaskSHA: proposal.Inputs.TaskSHA}
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
	if rep.RunID != 0 {
		run, err := r.db.GetRun(ctx, rep.RunID)
		if err != nil {
			return Posted{}, err
		}
		if _, stopped := run.OwnerStop(); stopped {
			if posted, pending, err := r.ReportPending(ctx, rep.SessionID, rep.CommentID); pending || err != nil {
				return posted, err
			}
			var err error
			if rep.CommentID != 0 {
				_, err = r.Consume(ctx, rep.SessionID, rep.CommentID)
			}
			return Posted{Skipped: true, Run: run}, err
		}
	}

	ref := rep.Run.Ref
	destination, refinement, err := r.refinementDestination(ctx, rep.SessionID, rep.CommentID, ref)
	if err != nil {
		return Posted{}, err
	}
	if refinement != nil {
		rep.Run.SourceStoryIssue = refinement.ChildNumber
		rep.Run.SourceCommentID = refinement.CommentID
	}
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

	if rep.RunID != 0 && !existing.Reportable() {
		return Posted{}, fmt.Errorf("run %d is not finished; defer reporting", rep.RunID)
	}

	intent := r.deliveryIntent(ref, destination, rep.SessionID, rep.CommentID, rep.RunID, "run-report")
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		if err := r.attachRecoveryReport(ctx, &rep); err != nil {
			return redact.Body{}, err
		}
		return r.renderer.RenderRun(ctx, rep.Run)
	})
}

// ReportReply posts a run-less reply and consumes the comment it answers.
//
// No run exists on this path, so the transaction shape is different: there is no
// row to move to [store.RunReported] and nothing to record a result comment id
// against. The watermark still advances, because an unsupported command that was
// answered is a comment that has been dealt with (AC-2.18, AC-2.20) — and it
// advances after the post, for the same reason a run report's does: a post that
// failed leaves the comment unconsumed and a later tick answers it again, which
// is the behaviour a tool-permission refusal wants too (AC-3.9).
//
// A [ReplyRequest.Refusal] takes the same path with a different reason: the
// comment asked for a run, and the labels on the issue said the tools it would
// have had cannot be determined. Nothing was prepared, nothing ran, and the
// answer is a comment (AC-3.7).
func (r *Reporter) ReportReply(ctx context.Context, req ReplyRequest) (Posted, error) {
	if err := req.validate(); err != nil {
		return Posted{}, err
	}
	destination := req.Ref
	var sourceStory int
	var sourceComment int64
	// Creation consent always belongs to its original source issue.
	if req.CreationAcceptance == nil {
		target, refinement, err := r.refinementDestination(ctx, req.SessionID, req.CommentID, req.Ref)
		if err != nil {
			return Posted{}, err
		}
		destination = target
		if refinement != nil {
			sourceStory = refinement.ChildNumber
			sourceComment = refinement.CommentID
		}
	}
	kind, ok := req.kind()
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
	if consumed && req.CommentID != 0 {
		log.InfoContext(ctx, "comment has already been consumed; posting no reply")
		return Posted{Skipped: true}, nil
	}

	purpose := fmt.Sprintf("reply:%s:schedule:%d:proposal:%d:confirmation:%d", kind, req.Schedule.ID, req.Schedule.ProposalID, req.Confirm.ProposalID)
	if kind == report.ReplyCreationProposal {
		purpose = "creation-preparation"
	}
	if kind == report.ReplyCreationAccepted {
		purpose = "creation-acceptance"
	}
	intent := r.deliveryIntent(req.Ref, destination, req.SessionID, req.CommentID, 0, purpose)
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		return r.renderer.RenderReply(ctx, report.Reply{
			Ref:                      req.Ref,
			SourceStoryIssue:         sourceStory,
			SourceCommentID:          sourceComment,
			Kind:                     kind,
			Command:                  req.Command.Name,
			Agent:                    req.Agent,
			OwningAgent:              req.OwningAgent,
			RequestedAgent:           req.RequestedAgent,
			SyncBehind:               req.SyncBehind,
			SyncBase:                 req.SyncBase,
			SyncDetail:               req.SyncDetail,
			SyncPhase:                req.SyncPhase,
			SyncMaxBehind:            req.SyncMaxBehind,
			ConflictPaths:            req.ConflictPaths,
			ConflictProposal:         req.ConflictProposal,
			ConflictSuggestionFailed: req.ConflictSuggestionFailed,
			ConflictSuggestionReason: req.ConflictSuggestionReason,
			Schedule:                 req.Schedule,
			Creation:                 req.Creation,
			CreationAcceptance:       req.CreationAcceptance,
			Confirm:                  req.Confirm,
		})
	})
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
	// A factory-minted trigger is not a comment, and it is larger than any id a
	// forge will ever issue. Consuming one moves the watermark past every
	// comment the task will ever receive, so the owner is never heard again.
	// The guard lives here rather than at each call site because a run's
	// trigger is not always a comment any more, and a caller cannot be relied
	// on to remember that — one already did not.
	if store.IsPrivateTrigger(commentID) {
		wm, _, err := r.db.GetWatermark(ctx, sessionID)
		if err != nil {
			return store.Watermark{}, err
		}
		r.log.DebugContext(ctx, "nothing to consume: the trigger is factory-minted, not a comment",
			slog.Int64("session_id", sessionID), slog.Int64("trigger_id", commentID))
		return wm, nil
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
func (r *Reporter) post(ctx context.Context, ref naming.Ref, body redact.Body, _ *slog.Logger) (github.Comment, error) {
	comment, err := r.forge.PostComment(ctx, github.RepoFromRef(ref), ref.Issue, body)
	if err != nil {

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
	if refinement, err := r.db.GetStoryRefinement(ctx, sessionID, commentID); err == nil {
		return refinement.Consumed, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
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
	// Recovery reports reuse recorded observations without re-reading Git or
	// inventing task freshness for historical base-only runs.
	for _, s := range result.Run.SyncObservations {
		value := &report.Sync{Phase: s.Phase, Base: s.Ref, LocalSHA: s.LocalSHA, RemoteSHA: s.RemoteSHA,
			Outcome: report.SyncOutcome(s.Outcome), Behind: s.Behind, Ahead: s.Ahead,
			CommitSHA: s.ResultSHA, Conflicts: append([]string(nil), s.Conflicts...), Reason: s.Reason}
		if s.Phase == "task" && s.Outcome != "absent" && result.TaskSync == nil {
			result.TaskSync = value
		}
		if s.Phase == "base" && result.Sync == nil {
			result.Sync = value
		}
	}

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

	metering, err := r.meterRunSummary(ctx, result.Run.ID)
	if err != nil {
		return report.Run{}, err
	}
	return report.Run{
		Meter:   metering,
		Ref:     result.Ref,
		Ordinal: ordinal,
		// result.Run.Agent is the segment's own value (store.AgentClaude for
		// every run this tree can produce today) rather than anything
		// re-derived here, so the report names the tool that actually ran
		// even on the path where Result itself is nil (AC-5.4).
		Agent:     result.Run.Agent,
		Result:    result.Harness,
		Branch:    result.Branch,
		CommitSHA: result.CommitSHA,
		// Both shas travel, because they answer different questions and the
		// report asks both. CommitSHA names the commit this turn made and is
		// empty when the run committed for itself through its own Bash tool;
		// SHAAfter is where the branch ended up either way, and is what a
		// permalink has to be pinned to or it renders blank on exactly that path
		// (git.CommitResult.Head, AC-3.24).
		SHAAfter:    result.SHAAfter,
		Pushed:      result.Pushed,
		Publication: result.Publication,
		Files:       result.Changes,
		LogPath:     result.LogPath,
		Failure:     turnFailure(result),
		// The requested surface and its provenance travel on the render input
		// itself rather than on the harness result, because the report has to
		// carry them for a run that produced no result at all — the run whose
		// boundary is least obvious afterwards.
		ToolsRequested: append([]string(nil), result.ToolsRequested...),
		ToolProfile:    result.ToolProfile,
		WorktreePath:   result.WorktreePath,
		// US-3. Nil for a branch that was already current (AC-3.1) and for a
		// turn that never reached syncBase; every other outcome renders,
		// including a skip, which has to say why (AC-3.2).
		Sync:     result.Sync,
		TaskSync: result.TaskSync,
		// Found by looking, not by deriving. internal/report does no I/O, and
		// the session directory's date is knowable only from the filesystem —
		// see report.Run.BriefPath for what deriving it used to get wrong.
		BriefPath: briefPath(result, r.log),
		// Best-effort like BriefPath: nil on any path that cannot cheaply and
		// safely answer the question, never propagated as an error — a
		// reporting path must never fail to post for want of a count
		// (S-2, AC-016).
		Stories: storyTally(ctx, result, r.db, r.log),
	}, nil
}

// briefPath is the repository-relative path of this task's RESEARCH_BRIEF.md,
// or "" when the task has none — or when there is no worktree left to look in,
// which is the abandoned-run path, where nothing is linkable anyway.
func briefPath(result Result, log *slog.Logger) string {
	if result.WorktreePath == "" {
		return ""
	}
	dir, ok := findSessionDir(
		result.WorktreePath,
		result.Branch,
		result.Ref.Issue,
		result.Run.StartedAt,
		naming.ResearchBriefName,
		log,
	)
	if !ok {
		return ""
	}
	return path.Join(dir, naming.ResearchBriefName)
}

// storyTally computes the story-completion tally for a finished run's managed
// feature (S-2), for [report.Run.Stories]'s "partial" callout — best-effort,
// like [briefPath]: it returns nil rather than an error on every path that
// cannot answer the question, because a reporting path must never fail to
// post a comment for want of a count (AC-016).
//
// The gate is result.Run.Mode == [agent.ModeImplement], not any inference
// from the request or the harness result: it is the one signal durable
// enough to survive a resumed or recovered run, and it is what makes a
// Plan-mode run — which only ever publishes stories, never implements one —
// structurally unable to render a false partial (AC-013).
func storyTally(ctx context.Context, result Result, db *store.DB, log *slog.Logger) *report.StoryTally {
	if result.Run.Mode != string(agent.ModeImplement) {
		return nil
	}

	parent := result.Ref.String()
	feature, err := db.GetManagedFeature(ctx, parent)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.WarnContext(ctx, "story tally: read the managed feature failed; reporting no tally",
				slog.String("parent", parent), slog.Any("error", err))
		}
		return nil
	}

	stories, err := db.ListManagedStories(ctx, parent, feature.Generation)
	if err != nil {
		log.WarnContext(ctx, "story tally: list the managed stories failed; reporting no tally",
			slog.String("parent", parent), slog.Int("generation", feature.Generation), slog.Any("error", err))
		return nil
	}

	var done int
	for _, s := range stories {
		if storyDone(s) {
			done++
		}
	}
	return &report.StoryTally{Done: done, Total: len(stories)}
}

// storyDone reports whether a managed story counts as finished for the
// partial-run tally (AC-015): EngineeringComplete covers the separated
// workflow's deliverable — recorded as engineering_complete=1, state=frozen —
// and a legacy verified or completed state covers workflow version 1.
// [store.PendingEngineering]'s predicate is this one's near-exact inverse;
// counting only verified/completed here would report every successful
// separated-workflow Implement run as 0-of-N, so the two are unioned rather
// than the legacy pair alone. blocked, or any other state, does not count.
func storyDone(s store.ManagedStory) bool {
	return s.EngineeringComplete || s.State == "verified" || s.State == "completed"
}

// turnFailure is the failure that was not the run's own, as the report should
// word it.
//
// A preparation failure is reported by its one-line reason — "could not fetch
// from origin: …" — rather than by the bare cause it wraps, because the cause on
// its own is a git error with no account of what litefactory was trying to do.
// Everywhere else the error is passed through as it stands, so a *git.PushError
// still reaches the report as itself.
//
// A run row that was started and then failed before any coding agent exited
// (no harness result, no exit code) carries its cause only in the recorded
// Error — the graph Plan save step is one. Its report, usually posted when the
// unconsumed comment is replayed on a later tick, names that recorded error
// instead of claiming the outcome is unknown.
func turnFailure(result Result) error {
	if result.Disposition == DispositionPrepareFailed && result.Reason != "" {
		return errors.New(result.Reason)
	}
	if result.Err == nil && result.Harness == nil && result.Run.Status == store.RunFailed && result.Run.ExitCode == nil {
		if cause := strings.TrimSpace(result.Run.Error); cause != "" {
			return errors.New(cause)
		}
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
	case KindUnknown:
		return report.ReplyUnknownCommand, true
	case KindNone, KindSpike, KindFreeText, KindSchedule:
		return report.ReplyInvalid, false
	default:
		return report.ReplyInvalid, false
	}
}
