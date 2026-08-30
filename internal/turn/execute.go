package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Runner runs one claude process. *harness.Runner satisfies it.
//
// It is declared here rather than in internal/harness because this is where it
// is consumed: the harness exports a concrete struct, and the one narrow
// interface a turn needs belongs to the turn. Two methods' worth of surface
// would already be too much — a test double for the whole business path has to
// be cheap to write, or the path stops being tested.
type Runner interface {
	Run(ctx context.Context, req harness.Request) (*harness.RunResult, error)
}

// Options are the dependencies a turn runs with. The daemon builds one value at
// startup and reuses it for every turn; nothing here is per-comment.
type Options struct {
	// DB is the open database. Required.
	DB *store.DB

	// Git is the resolved git binary, from git.Resolve. Resolving it once at
	// startup is what stops a broken git being discovered three hours into a
	// turn.
	Git *git.Binary

	// Claude runs the subprocess. In production this is a *harness.Runner
	// built at startup, so a missing binary fails at boot (AC-3.18).
	Claude Runner

	// Paths is the resolved $LF_HOME layout. Run artefacts are written beneath
	// it.
	Paths config.Paths

	// InstanceID is this daemon's instance id. It is what the per-task lock is
	// held under, and what tells a reclaimed lock from a live one (AC-3.2).
	InstanceID string

	// Author is the identity commits are made under. litefactory commits as the
	// human who registered the repository, never as the bot.
	//
	// Leaving it zero means "ask the clone": user.name and user.email are read
	// with `git config --get`, which is in the S-6 allowlist. That is the right
	// default because it is the same identity the user's own commits carry.
	Author git.Author

	// Remote is the remote to fetch from and push to. Empty means
	// git.DefaultRemote.
	Remote string

	// Logger receives the turn's structured log lines. Required.
	Logger *slog.Logger
}

func (o Options) validate() error {
	switch {
	case o.DB == nil:
		return invalidOptions("a database handle is required")
	case o.Git == nil:
		return invalidOptions("a resolved git binary is required")
	case o.Claude == nil:
		return invalidOptions("a claude runner is required")
	case o.InstanceID == "":
		return invalidOptions("a daemon instance id is required")
	case o.Logger == nil:
		return invalidOptions("a logger is required")
	case !filepath.IsAbs(o.Paths.Home()):
		return invalidOptions("LF_HOME %q must be an absolute path", o.Paths.Home())
	default:
		return nil
	}
}

// remote is the remote this turn works against.
func (o Options) remote() string {
	if o.Remote == "" {
		return git.DefaultRemote
	}
	return o.Remote
}

// Issue is the forge issue a turn works on.
//
// Title and Body are third-party text: the issue author is not necessarily the
// person who triggered the run. They are neutralised and capped by
// harness.BuildPrompt, never here (D-N).
type Issue struct {
	// Number is the issue number. It must match the session's.
	Number int
	// Title is the issue title. It is what the feature slug is derived from on
	// the first run (D-4).
	Title string
	// Body is the issue body.
	Body string
	// Author is the handle that opened the issue, for the prompt's attribution
	// line.
	Author string
	// Labels is every label name on the issue, as GitHub reported it at the
	// head of this tick.
	//
	// It is a lookup key for tool-profile resolution and nothing else: these
	// strings are matched against operator-authored configuration, and the
	// configuration is what supplies any value that travels onwards. No label
	// text may be rendered into a prompt or echoed into a comment — this struct
	// is one hop from [harness.PromptInput], and that hop is the one place the
	// rule has to be kept by hand (S-13).
	Labels []string
}

// Trigger is what started this turn: a comment, or the issue body itself.
type Trigger struct {
	// CommentID is the forge's id for the comment. It is unique per session on
	// the runs row, and that uniqueness is the whole crash-replay guarantee
	// (AC-3.4).
	//
	// Zero is not a comment. It is the issue body, run as the first instruction
	// on an issue assigned to the owner (AC-2.16b), and it is deliberately not
	// the issue's own forge id: reporting a run advances the comment watermark
	// to this value, so any positive id here would push the watermark past
	// every comment the issue will ever receive and swallow them all. Zero
	// makes that advance a MAX(0, 0) no-op (AC-2.23) while the runs table's
	// (session, trigger comment) uniqueness still fires a body exactly once.
	//
	// Negative is neither, and is refused.
	CommentID int64
	// Body is the comment text, verbatim and untrusted.
	Body string
	// Author is the handle that wrote it. It is the owner — the classification
	// that made this an instruction says who may command litefactory, not that
	// the text is safe to obey verbatim.
	Author string
}

// Request is one turn: which task, which comment, and what it asked for.
type Request struct {
	// SessionID is the task, from store.GetOrCreateSession.
	SessionID int64
	// Issue describes the issue.
	Issue Issue
	// Trigger is the comment that caused this turn.
	Trigger Trigger
	// Command is the parsed comment, from [ParseCommand]. It must be a command
	// that spawns a run; a reply-only command is the caller's to handle.
	Command Command

	// Tools and AllowedTools are the tool surface a resolved tool profile
	// grants this turn, and ToolProfile is the name of the profile they came
	// from. All three are empty when no lf:tools= label selected one, and the
	// run then takes the harness's configured surface exactly as it did before
	// profiles existed (AC-2.3).
	//
	// Every string here is operator-authored configuration. The label only ever
	// chose *which* profile; nothing that originated on the forge travels in
	// these fields, and that is what keeps C2 true all the way to argv. The
	// caller has already copied them out of the configuration it holds for the
	// life of the process, because [preparation.request] is mutated in place on
	// the cold-rerun path.
	//
	// The harness checks Tools against its own ceiling before it starts anything
	// (AC-2.6). A list it refuses should be unreachable — config validation
	// proved every profile fits under the ceiling at boot — but if it happens it
	// is reported as a *run* failure and not returned as an error: by then the
	// worktree and the runs row exist, and an error return would leave the
	// comment unconsumed for a next tick that would build the same worktree and
	// be refused in exactly the same way, for ever. It ends up as
	// "claude did not start: …" on the runs row, a failure comment on the issue,
	// and a consumed comment (AC-2.7).
	Tools        []string
	AllowedTools []string
	ToolProfile  string
}

func (r Request) validate() error {
	switch {
	case r.SessionID <= 0:
		return invalidRequest("a session id is required")
	case r.Issue.Number <= 0:
		return invalidRequest("issue number %d must be positive", r.Issue.Number)
	// Not "positive": 0 is the issue body run as the first instruction. See
	// [Trigger.CommentID] for why the body's trigger id is 0 and not the
	// issue's own id.
	case r.Trigger.CommentID < 0:
		return invalidRequest("trigger id %d is neither a comment nor the issue body", r.Trigger.CommentID)
	case !r.Command.Spawns():
		return invalidRequest("%s does not spawn a run", r.Command.Kind)
	case r.Command.Text == "":
		return invalidRequest("%s has no instruction text", r.Command.Kind)
	default:
		return nil
	}
}

// mode is the run mode. For a command it is the command word without its
// slash, so the runs row, the commit trailer and the metadata document all say
// the same thing and none of them has to be kept in step by hand. Free text
// named no command, so it is [InferMode].
func (r Request) mode() string {
	if r.Command.Kind == KindFreeText {
		return harness.InferMode
	}
	return r.Command.Name[len("/"):]
}

// Execute runs one turn: lock, prepare, run, harvest.
//
// It is the whole business path, and it is a plain function of a context, its
// dependencies and one comment. There is no clock in it and no signal handling
// — those belong to internal/daemon, which decides *when* a turn happens. That
// separation is what makes every path below reachable from a test without
// waiting for anything.
//
// # What is an error and what is a result
//
// A turn that went badly is not an error. A fetch that failed, a worktree
// someone else is standing in, a claude that exited non-zero or timed out: all
// of those come back as a [Result] whose Disposition and Reason say what
// happened, because every one of them owes the issue a comment (AC-4.1) and an
// error return would be the one shape that cannot carry one.
//
// A non-nil error means the turn could not be conducted or recorded at all:
// invalid options, an invalid request, or a database that would not answer. The
// caller logs it and leaves the comment unconsumed; the next tick tries again.
//
// # The lock
//
// The per-task lock is taken before any preparation and released on every exit
// path including a panic (AC-3.1, AC-3.3). A lock already held is not an
// error and not a queue: the turn returns [DispositionLocked], the comment
// stays unconsumed, and a later tick retries it.
func Execute(ctx context.Context, opts Options, req Request) (Result, error) {
	if err := opts.validate(); err != nil {
		return Result{}, err
	}
	if err := req.validate(); err != nil {
		return Result{}, err
	}

	logger := opts.Logger.With(
		slog.Int64("session_id", req.SessionID),
		slog.Int("issue", req.Issue.Number),
		slog.Int64("trigger_comment_id", req.Trigger.CommentID),
		slog.String("instance_id", opts.InstanceID))

	session, err := opts.DB.GetSession(ctx, req.SessionID)
	if err != nil {
		return Result{}, fmt.Errorf("read task %d: %w", req.SessionID, err)
	}
	result := Result{
		SessionID:        req.SessionID,
		TriggerCommentID: req.Trigger.CommentID,
		Session:          session,
		Branch:           session.Branch,
		WorktreePath:     session.WorktreePath,
		Remote:           opts.remote(),
		// Set here, on the first result this function builds, rather than after
		// preparation: a turn that never reaches claude — a failed fetch, an
		// occupied worktree — still owes the issue a report, and the surface it
		// would have run with is exactly what that report has to say (AC-4.3).
		ToolsRequested: append([]string(nil), req.Tools...),
		ToolProfile:    req.ToolProfile,
	}
	if session.IssueNumber != req.Issue.Number {
		return result, invalidRequest("task %s is issue %d, not issue %d",
			session.TaskRef, session.IssueNumber, req.Issue.Number)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return result, fmt.Errorf("task ref %q on session %d: %w", session.TaskRef, session.ID, err)
	}
	result.Ref = ref
	logger = logger.With(slog.String("task", ref.String()))

	// AC-3.1: the lock comes before preparation, so two daemons cannot both
	// fetch, both create the worktree and both start claude in it.
	acquired, err := opts.DB.AcquireSessionLock(ctx, session.ID, opts.InstanceID)
	if err != nil {
		return result, fmt.Errorf("acquire the lock on task %s: %w", ref, err)
	}
	if !acquired {
		result.Disposition = DispositionLocked
		logger.Info("task is locked by another daemon instance; leaving the comment for a later tick")
		return result, nil
	}
	// AC-3.3: every exit path, including a panic and a non-zero claude exit.
	// The context is stripped of cancellation because the commonest reason for
	// unwinding is the daemon shutting down, and a lock that is only released
	// when the daemon is healthy is not a lock, it is a deadlock waiting for a
	// restart.
	defer func() {
		releaseErr := opts.DB.ReleaseSessionLock(context.WithoutCancel(ctx), session.ID, opts.InstanceID)
		if releaseErr != nil {
			logger.Error("release the task lock", slog.Any("error", releaseErr))
		}
	}()

	// AC-3.4: a comment that already produced a run never produces a second
	// one. The unique index on (session, trigger comment) would refuse the
	// insert anyway; looking first is what turns that into a clean no-op.
	existing, replay, err := opts.DB.GetRunByTrigger(ctx, session.ID, req.Trigger.CommentID)
	if err != nil {
		return result, fmt.Errorf("look for an existing run of comment %d: %w", req.Trigger.CommentID, err)
	}
	if replay {
		return replayed(result, existing, logger), nil
	}

	prep, err := prepare(ctx, opts, req, session, ref, logger)
	if err != nil {
		var prepFailure *failure
		if errors.As(err, &prepFailure) {
			return prepareFailed(ctx, opts, result, prepFailure, logger), nil
		}
		return result, err
	}
	result.Session = prep.session
	result.Branch = prep.session.Branch
	result.WorktreePath = prep.worktree.Path()
	result.SHABefore = prep.shaBefore
	result.Run = prep.run
	result.MetaPath = prep.metaPath
	result.LogPath = prep.run.LogPath

	return runAndHarvest(ctx, opts, prep, result, logger)
}

// replayed fills in a result for a comment that has already produced a run.
func replayed(result Result, existing store.Run, logger *slog.Logger) Result {
	result.Run = existing
	result.Disposition = DispositionAlreadyRan
	if existing.Status == store.RunReported {
		result.Disposition = DispositionAlreadyReported
	}
	result.LogPath = existing.LogPath
	if existing.Status == store.RunFailed {
		result.Outcome = harness.OutcomeFailed
		result.Reason = existing.Error
	}
	result.CommitSHA = existing.CommitSHA
	logger.Info("comment has already been run",
		slog.Int("run_ordinal", existing.Ordinal),
		slog.String("run_status", string(existing.Status)),
		slog.String("disposition", result.Disposition.String()))
	return result
}

// prepareFailed records a turn that never reached the subprocess.
//
// The session is marked failed and the reason is carried out for the report:
// AC-3.5 wants a failure comment for a fetch that did not run, and AC-4.1 wants
// one for every other way a turn can end early. No run row was allocated, so
// there is no ordinal to burn and the next comment on this task starts clean.
func prepareFailed(ctx context.Context, opts Options, result Result, prepFailure *failure, logger *slog.Logger) Result {
	result.Disposition = DispositionPrepareFailed
	result.Outcome = harness.OutcomeFailed
	result.Reason = prepFailure.Error()
	result.Err = prepFailure.Unwrap()

	// The context may already be cancelled — a shutdown during a fetch looks
	// exactly like this — and the session state still has to be honest.
	if err := opts.DB.SetSessionState(context.WithoutCancel(ctx), result.SessionID, store.SessionFailed); err != nil {
		logger.Error("mark the task failed", slog.Any("error", err))
	}
	logger.Error("turn could not be prepared",
		slog.String("reason", prepFailure.reason),
		slog.Any("error", prepFailure.Unwrap()))
	return result
}

// Reconcile clears what a killed daemon left behind, and is called once at
// daemon start before any turn runs (D-B).
//
// Runs still marked running are failed as abandoned and locks held by an
// instance id other than this one are released — AC-3.2 requires that reclaim
// to be logged, which is the half this function adds over the store call it
// wraps. The abandoned runs come back so the daemon can post the ordinary
// failure comment for each and advance their watermarks; they are never re-run.
func Reconcile(ctx context.Context, opts Options) (store.Reconciliation, error) {
	if err := opts.validate(); err != nil {
		return store.Reconciliation{}, err
	}

	reconciliation, err := opts.DB.ReconcileOrphans(ctx, opts.InstanceID)
	if err != nil {
		return store.Reconciliation{}, fmt.Errorf("reconcile orphaned runs and locks: %w", err)
	}
	for _, lock := range reconciliation.ReclaimedLocks {
		opts.Logger.Warn("reclaimed a task lock from a previous daemon instance",
			slog.Int64("session_id", lock.SessionID),
			slog.String("task", lock.TaskRef),
			slog.String("previous_owner", lock.PreviousOwner),
			slog.String("instance_id", opts.InstanceID),
			slog.Time("acquired_at", lock.AcquiredAt))
	}
	for _, run := range reconciliation.AbandonedRuns {
		opts.Logger.Warn("run was abandoned by a previous daemon instance",
			slog.Int64("session_id", run.SessionID),
			slog.Int64("run_id", run.ID),
			slog.Int("run_ordinal", run.Ordinal),
			slog.String("log_path", run.LogPath))
	}
	return reconciliation, nil
}
