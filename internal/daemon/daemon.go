package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The tick interval and its clamp (D-C, AC-2.7). A value outside the clamp is a
// startup error and never a silent correction: a daemon that quietly polls
// every 30 seconds when the operator wrote 5 is a daemon whose configuration
// file is a lie.
const (
	// DefaultInterval is the tick interval when none is configured.
	DefaultInterval = time.Duration(config.DefaultPollIntervalSeconds) * time.Second
	// MinInterval is the fastest tick allowed.
	MinInterval = 10 * time.Second
	// MaxInterval is the slowest tick allowed.
	MaxInterval = 60 * time.Second
)

// DefaultShutdownGrace is how long [Daemon.Run] waits for an in-flight turn
// after the context is cancelled before it returns anyway.
//
// AC-2.1 gives the whole shutdown 3 seconds. launchd's ExitTimeOut is 20 and
// then it sends SIGKILL, so the budget is not the constraint — the constraint
// is that a daemon which takes longer than a few seconds to stop is one nobody
// will wait for. A run that outlives the grace is simply left: its row stays
// running, and the next start reconciles it to failed (abandoned), posts the
// failure comment and advances the watermark (D-B, AC-2.21).
const DefaultShutdownGrace = 2 * time.Second

// Forge is the slice of the GitHub API a tick needs. *github.Client satisfies
// it.
//
// It is declared here, at the point of consumption, rather than exported by
// internal/github, which is a concrete client by design. Posting is
// deliberately absent — it belongs to the [Reporter], which takes a redact.Body
// and is therefore the only thing in the tree that can put text on an issue.
//
// Discovery takes two listings rather than one. An issue whose body or state
// changed comes back from ListIssues; an issue that only received a comment
// does not, because GitHub's issue index does not move `updated_at` for a
// comment (see [github.Client.ListCommentedIssues], which carries the
// measurement). GetIssue fills in the detail for anything only the second
// listing found.
type Forge interface {
	ListIssues(ctx context.Context, repo github.Repo, since time.Time) ([]github.Issue, error)
	ListCommentedIssues(ctx context.Context, repo github.Repo, since time.Time) ([]int, error)
	GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error)
	ListCommentsAfter(ctx context.Context, repo github.Repo, issue int, afterID int64) ([]github.Comment, error)
	AddCommentReaction(ctx context.Context, repo github.Repo, commentID int64, content github.ReactionContent) error
}

// Classifier decides what an inbound comment is and records it. *turn.Filter
// satisfies it.
type Classifier interface {
	Admit(ctx context.Context, sessionID int64, c github.Comment) (turn.Admission, error)
}

// Executor runs one turn. [NewExecutor] wraps turn.Execute into one.
//
// The indirection exists so that the loop's own behaviour — the comment queue,
// the backoff, the cursor — is testable without a git clone and a claude
// binary. It is not an abstraction over "how work happens": there is one
// implementation and it is three lines long.
type Executor interface {
	Execute(ctx context.Context, req turn.Request) (turn.Result, error)
}

// Reporter posts what happened and consumes the comment that caused it.
// *turn.Reporter satisfies it.
type Reporter interface {
	ReportTurn(ctx context.Context, result turn.Result) (turn.Posted, error)
	ReportRun(ctx context.Context, rep turn.RunReport) (turn.Posted, error)
	ReportReply(ctx context.Context, req turn.ReplyRequest) (turn.Posted, error)
	Consume(ctx context.Context, sessionID, commentID int64) (store.Watermark, error)
}

// Options are the daemon's dependencies. [Open] builds one from the machine;
// a test builds one with its own doubles.
type Options struct {
	// DB is the open, migrated database. Required.
	DB *store.DB

	// Forge is the GitHub client. Required.
	Forge Forge

	// Filter classifies and records inbound comments. Required.
	Filter Classifier

	// Turn executes an instruction comment. Required.
	Turn Executor

	// Reporter posts reports and replies and consumes comments. Required.
	Reporter Reporter

	// InstanceID is this daemon process's identity, and is what the per-task
	// lock is held under. It must be fresh per process — see [NewInstanceID] —
	// because "not the current instance" is how a lock left by a dead daemon is
	// recognised (AC-3.2).
	InstanceID string

	// Interval is the tick interval. Zero means [DefaultInterval]; anything
	// outside [MinInterval]..[MaxInterval] is a startup error (AC-2.7).
	Interval time.Duration

	// Logger receives the daemon's structured log lines. Required.
	Logger *slog.Logger

	// Lock is the acquired singleton lock, held for the daemon's lifetime and
	// released by [Daemon.Close]. Nil is allowed and means the caller is
	// managing the lock itself, which is what a test that is not asserting on
	// the singleton does.
	Lock *Lock

	// Now is the clock. Nil means time.Now. It exists so that backoff and
	// cursor behaviour can be asserted without waiting, which is the only
	// reason there is a clock seam anywhere in litefactory.
	Now func() time.Time

	// Ticks replaces the internal ticker when non-nil. A test drives the loop
	// through it; production leaves it nil and gets a time.Ticker at Interval.
	Ticks <-chan time.Time

	// ShutdownGrace is how long [Daemon.Run] waits for an in-flight turn once
	// the context is cancelled. Zero means [DefaultShutdownGrace].
	ShutdownGrace time.Duration
}

func (o Options) validate() error {
	switch {
	case o.DB == nil:
		return invalidOptions("a database handle is required")
	case o.Forge == nil:
		return invalidOptions("a forge client is required")
	case o.Filter == nil:
		return invalidOptions("a comment classifier is required")
	case o.Turn == nil:
		return invalidOptions("a turn executor is required")
	case o.Reporter == nil:
		return invalidOptions("a reporter is required")
	case o.InstanceID == "":
		return invalidOptions("a daemon instance id is required")
	case o.Logger == nil:
		return invalidOptions("a logger is required")
	}
	if o.Interval != 0 && (o.Interval < MinInterval || o.Interval > MaxInterval) {
		// AC-2.7: out of range refuses to start. internal/config checks the same
		// bound on poll_interval_seconds; this one catches every other way an
		// interval can reach the loop.
		return invalidOptions("tick interval %s is outside the supported range %s..%s",
			o.Interval, MinInterval, MaxInterval)
	}
	return nil
}

// Daemon is the poll loop.
//
// It is built once per process and is not reusable after [Daemon.Close]. Its
// only mutable state is the rate-limit backoff, which is mutex-guarded, so
// [Daemon.Tick] may be called from a test on any goroutine.
type Daemon struct {
	db         *store.DB
	forge      Forge
	filter     Classifier
	turn       Executor
	reporter   Reporter
	instanceID string
	interval   time.Duration
	grace      time.Duration
	log        *slog.Logger
	lock       *Lock
	now        func() time.Time
	ticks      <-chan time.Time

	backoff backoff
}

// New builds a daemon from its dependencies.
//
// Everything that can be wrong about the wiring is wrong here, at startup,
// rather than at the first tick — which happens thirty seconds after the
// operator has stopped watching the terminal.
func New(opts Options) (*Daemon, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	interval := opts.Interval
	if interval == 0 {
		interval = DefaultInterval
	}
	grace := opts.ShutdownGrace
	if grace == 0 {
		grace = DefaultShutdownGrace
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Daemon{
		db:         opts.DB,
		forge:      opts.Forge,
		filter:     opts.Filter,
		turn:       opts.Turn,
		reporter:   opts.Reporter,
		instanceID: opts.InstanceID,
		interval:   interval,
		grace:      grace,
		log:        opts.Logger,
		lock:       opts.Lock,
		now:        now,
		ticks:      opts.Ticks,
	}, nil
}

// InstanceID returns this daemon's instance id, which is what a task lock is
// held under.
func (d *Daemon) InstanceID() string { return d.instanceID }

// Interval returns the tick interval in force.
func (d *Daemon) Interval() time.Duration { return d.interval }

// Close releases the singleton lock, if this daemon was given one. It does not
// close the database: whoever opened it owns it.
func (d *Daemon) Close() error {
	if d == nil {
		return nil
	}
	return d.lock.Close()
}

// Reconciled is what the startup reconciliation repaired (D-B, AC-2.21).
type Reconciled struct {
	// AbandonedRuns is how many runs were still marked running and have been
	// failed as abandoned.
	AbandonedRuns int
	// Reported is how many of those had their failure comment posted and their
	// triggering comment consumed. It is below AbandonedRuns when the forge
	// refused a post; see [Daemon.Start].
	Reported int
	// ReclaimedLocks is how many per-task locks were released from a previous
	// instance (AC-3.2).
	ReclaimedLocks int
}

// Start performs the startup reconciliation and returns what it repaired.
//
// It is a step in starting the daemon, not a separate code path, and it runs
// before the first tick. Any run still marked running belonged to a daemon that
// was killed mid-run: it is failed as abandoned, its stale task lock is
// released, and the ordinary reporting path posts the failure comment and
// advances the watermark past its triggering comment. Nothing is re-run.
//
// Without this the daemon would violate its own (session, triggering comment)
// uniqueness the first time it was rebooted mid-run, and the task would stay
// locked by a process that no longer exists.
//
// A post that fails here is logged loudly and does not stop the daemon. The run
// stays un-reported and its comment stays unconsumed, so the ordinary path
// picks it up — [turn.Execute] finds the existing run row and reports it
// instead of running claude again (AC-3.4) — the next time that issue is
// listed. Refusing to start because GitHub is having a bad minute would be the
// worse failure.
func (d *Daemon) Start(ctx context.Context) (Reconciled, error) {
	reconciliation, err := d.db.ReconcileOrphans(ctx, d.instanceID)
	if err != nil {
		return Reconciled{}, fmt.Errorf("reconcile what a previous daemon left behind: %w", err)
	}

	for _, lock := range reconciliation.ReclaimedLocks {
		// AC-3.2: the reclaim is logged. A lock silently taken from another
		// instance is how two daemons end up in the same worktree.
		d.log.WarnContext(ctx, "reclaimed a task lock from a previous daemon instance",
			slog.Int64("session_id", lock.SessionID),
			slog.String("task", lock.TaskRef),
			slog.String("previous_owner", lock.PreviousOwner),
			slog.String("instance_id", d.instanceID),
			slog.Time("acquired_at", lock.AcquiredAt))
	}

	result := Reconciled{
		AbandonedRuns:  len(reconciliation.AbandonedRuns),
		ReclaimedLocks: len(reconciliation.ReclaimedLocks),
	}
	for _, run := range reconciliation.AbandonedRuns {
		if d.reportAbandoned(ctx, run) {
			result.Reported++
		}
	}
	if result.AbandonedRuns > 0 || result.ReclaimedLocks > 0 {
		d.log.InfoContext(ctx, "reconciled the state a previous daemon left behind",
			slog.Int("abandoned_runs", result.AbandonedRuns),
			slog.Int("failure_comments_posted", result.Reported),
			slog.Int("reclaimed_locks", result.ReclaimedLocks))
	}
	return result, nil
}

// reportAbandoned posts the failure comment for one abandoned run and consumes
// its triggering comment, reporting whether it got that far.
//
// The run is reported as what it was — a run that started and was killed by a
// shutdown — rather than as a turn that never began, which is why a synthetic
// [harness.RunResult] is assembled here from the row. The alternative, passing
// a nil result, renders "litefactory did not record how this ended", and
// litefactory does know: it recorded exactly this.
func (d *Daemon) reportAbandoned(ctx context.Context, run store.Run) bool {
	log := d.log.With(
		slog.Int64("session_id", run.SessionID),
		slog.Int64("run_id", run.ID),
		slog.Int("run_ordinal", run.Ordinal),
		slog.Int64("trigger_comment_id", run.TriggerCommentID),
		slog.String("log_path", run.LogPath))

	session, err := d.db.GetSession(ctx, run.SessionID)
	if err != nil {
		log.ErrorContext(ctx, "could not read the task of an abandoned run; it stays unreported",
			slog.Any("error", err))
		return false
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		log.ErrorContext(ctx, "the task ref of an abandoned run does not parse; it stays unreported",
			slog.String("task_ref", session.TaskRef), slog.Any("error", err))
		return false
	}

	posted, err := d.reporter.ReportRun(ctx, turn.RunReport{
		Run: report.Run{
			Ref:     ref,
			Ordinal: run.Ordinal,
			Result:  abandonedResult(run),
			Branch:  session.Branch,
			LogPath: run.LogPath,
		},
		SessionID: run.SessionID,
		CommentID: run.TriggerCommentID,
		RunID:     run.ID,
	})
	if err != nil {
		log.ErrorContext(ctx, "could not post the failure comment for an abandoned run; "+
			"it stays unreported and its comment stays unconsumed, so the next tick that sees "+
			"this issue reports it without running claude again",
			slog.Int64("comment_id", posted.CommentID), slog.Any("error", err))
		return false
	}
	if posted.Skipped {
		// Already reported by a previous start. Nothing to say and nothing to do.
		return true
	}

	log.InfoContext(ctx, "reported a run abandoned by a previous daemon instance; it is not re-run",
		slog.Int64("comment_id", posted.CommentID))
	return true
}

// abandonedResult renders an abandoned runs row as the harness result it never
// got to produce.
func abandonedResult(run store.Run) *harness.RunResult {
	result := &harness.RunResult{
		Outcome:    harness.OutcomeAbandoned,
		Reason:     run.Error,
		ExitCode:   harness.SignalExitCode,
		LogPath:    run.LogPath,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	}
	if result.Reason == "" {
		result.Reason = store.ReasonAbandoned
	}
	if run.ExitCode != nil {
		result.ExitCode = int(*run.ExitCode)
	}
	if duration, ok := run.Duration(); ok {
		result.Duration = duration
	}
	return result
}

// Run reconciles, then ticks until the context is cancelled.
//
// It is the foreground loop launchd supervises. It never forks, never
// self-daemonizes and writes no pid file (AC-2.5, D-8): the process *is* the
// daemon, and the root signal context created in main is the only thing that
// can stop it.
//
// A cancelled context returns nil, not [context.Canceled]. Being asked to stop
// is not a failure, and an exit code that says otherwise would make launchd
// treat every ordinary `daemon stop` as a crash.
//
// # Why the loop is on its own goroutine
//
// It is the one goroutine this package is allotted, and this is what it buys:
// SIGTERM has to return within three seconds even when a turn is in flight, and
// an in-flight turn can take longer than that to unwind — the harness gives
// claude's process subtree ten seconds to die and its pipes ten more. Waiting
// on the turn would blow the budget; running the loop here and returning on
// cancellation cannot. The goroutine's termination condition is the same
// context: it stops at the end of the turn it is in, and if it outlasts the
// grace the process exits and takes it with it, leaving a run row the next
// start reconciles (D-B).
func (d *Daemon) Run(ctx context.Context) error {
	if _, err := d.Start(ctx); err != nil {
		return err
	}

	ticks, stop := d.tickSource()
	defer stop()

	d.log.InfoContext(ctx, "daemon started",
		slog.String("instance_id", d.instanceID),
		slog.String("interval", d.interval.String()),
		slog.String("lock", d.lock.Path()))

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.loop(ctx, ticks)
	}()

	select {
	case <-done:
		// The tick source was closed. Only a test does that; treat it as a
		// clean stop rather than inventing an error for it.
		return nil
	case <-ctx.Done():
	}

	d.log.InfoContext(ctx, "daemon stopping", slog.String("grace", d.grace.String()))
	timer := time.NewTimer(d.grace)
	defer timer.Stop()
	select {
	case <-done:
		d.log.Info("daemon stopped")
	case <-timer.C:
		// AC-2.1 and D-B: the run is abandoned, not waited for. The row stays
		// running and the next start fails it, posts the comment and advances
		// the watermark.
		d.log.Warn("a turn was still in flight at shutdown; abandoning it. "+
			"The next start reports it as failed (abandoned) and does not re-run it",
			slog.String("grace", d.grace.String()))
	}
	return nil
}

// loop ticks until the context is cancelled or the tick source closes.
//
// A failed tick is logged and never fatal: the cursor was not persisted, so the
// next tick redoes the work (AC-2.13), and a daemon that exited because GitHub
// returned a 502 would need a human to notice and restart it.
func (d *Daemon) loop(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				// The signal arrived while this tick was queued. Starting a turn
				// now would only produce a run to abandon.
				return
			}
			if _, err := d.Tick(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				d.log.ErrorContext(ctx, "tick failed; the poll cursor is unchanged and the next tick redoes it",
					slog.Any("error", err))
			}
		}
	}
}

// tickSource returns the channel ticks arrive on and the function that retires
// it. An injected source is used as it stands and is never closed here: the
// test that created it owns it.
func (d *Daemon) tickSource() (<-chan time.Time, func()) {
	if d.ticks != nil {
		return d.ticks, func() {}
	}
	ticker := time.NewTicker(d.interval)
	return ticker.C, ticker.Stop
}
