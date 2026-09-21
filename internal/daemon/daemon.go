package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
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

// The run-concurrency ceiling and its clamp (D2, AC-PAR-6.1). Same rule as the
// interval above: outside the range is a startup error, never a silent
// correction.
//
// The numbers are taken from internal/config rather than restated, because
// they are one decision. The redundancy that matters is where they are
// *checked* — config validation, [Open] and [Options.validate] each refuse
// independently — not how many places the digits are written down.
const (
	// DefaultConcurrentRuns is how many runs may be in flight when none is
	// configured. It is a conservative placeholder rather than a measurement;
	// see [config.Settings.MaxConcurrentRuns].
	DefaultConcurrentRuns = config.DefaultMaxConcurrentRuns
	// MinConcurrentRuns is the fewest runs allowed in flight: one, which is
	// the serial daemon litefactory was before the ceiling existed.
	MinConcurrentRuns = config.MinMaxConcurrentRuns
	// MaxConcurrentRuns is the most allowed. It bounds the *setting*
	// [Options.MaxConcurrentRuns], which is the value actually in force.
	MaxConcurrentRuns = config.MaxMaxConcurrentRuns
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
// Discovery takes two listings rather than one, because the forge indexes
// issues and comments separately and they lag separately: a comment can be
// invisible to the issue listing long enough for the poll cursor to move past
// it (see [github.Client.ListCommentedIssues], which carries the measurement).
// GetIssue fills in the detail for anything only the comment listing found.
type Forge interface {
	ListIssues(ctx context.Context, repo github.Repo, since time.Time) ([]github.Issue, error)
	ListCommentedIssues(ctx context.Context, repo github.Repo, since time.Time) ([]int, error)
	GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error)
	ListCommentsAfter(ctx context.Context, repo github.Repo, issue int, afterID int64) ([]github.Comment, error)
	AddCommentReaction(ctx context.Context, repo github.Repo, commentID int64, content github.ReactionContent) error
}

type labelForge interface {
	AddIssueLabel(ctx context.Context, repo github.Repo, issue int, label string) error
	RemoveIssueLabel(ctx context.Context, repo github.Repo, issue int, label string) error
}

// Classifier decides what an inbound comment is and records it, and answers
// which identities it decides with. *turn.Filter satisfies it.
//
// Identities is on the interface because the daemon has no other route to the
// owner's numeric user id, and that id is what "assigned to me" is a question
// about: assignment is matched on the id and never on a handle (D-I,
// AC-2.24), exactly as every other identity decision here is. It is asked of
// the classifier rather than loaded a second time so that one set of ids
// governs both — a daemon that classified with one owner id and gated
// assignment on another would be indefensible.
type Classifier interface {
	Admit(ctx context.Context, sessionID int64, c github.Comment) (turn.Admission, error)
	Identities() turn.Identities
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

// ConfirmApplier applies a validated confirmation (US-12): the daemon's one
// route into [turn.ExecuteApplyResolution], declared here at the point of
// consumption the same way [Executor] is (TEST-5). It is a separate
// interface from [Executor] rather than a third method on it, because the
// two fakes existing daemon tests already hold for [Executor] must keep
// compiling unchanged — a turn executor and a confirmation applier answer
// different questions, and [NewConfirmApplier] happens to wrap the same
// turn.Options [NewExecutor] does, not because the two are one capability.
//
// This interface carries no way to skip [turn.ValidateConfirmation]: the
// only value it accepts as [turn.ApplyRequest.Proposal] is one that call
// already produced, and that remains true however this interface is
// implemented or faked.
type ConfirmApplier interface {
	ApplyConfirmedResolution(ctx context.Context, req turn.ApplyRequest) (turn.ApplyResult, error)
}

// Reporter posts what happened and consumes the comment that caused it.
// *turn.Reporter satisfies it.
type Reporter interface {
	ReportTurn(ctx context.Context, result turn.Result) (turn.Posted, error)
	ReportRun(ctx context.Context, rep turn.RunReport) (turn.Posted, error)
	ReportReply(ctx context.Context, req turn.ReplyRequest) (turn.Posted, error)
	Consume(ctx context.Context, sessionID, commentID int64) (store.Watermark, error)
}

// ProposalGenerator is the bounded, structured boundary used only for
// /schedule --propose. It is intentionally not agent.Runner or Executor: a
// generator can suggest data, but cannot start a workflow, write schedules,
// or decide an owner's approval.
type ProposalGenerator interface {
	ProposeSchedule(ctx context.Context, request ScheduleProposalRequest) (ScheduleProposal, error)
}

// ScheduleProposalRequest is the agent-safe description of advice requested
// by an owner. The factory, not the generator, supplies time and issue scope.
type ScheduleProposalRequest struct {
	Description string
	ReceivedAt  time.Time
}

// ScheduleProposal is structured advisory output from a ProposalGenerator.
// It has no IDs or persistence fields because those remain factory-owned.
type ScheduleProposal struct {
	Items []ScheduleProposalItem
}

// ScheduleProposalItem carries advisory work and timing, without factory-owned identity.
type ScheduleProposalItem struct {
	Payload string
	DueAt   time.Time
}

// Options are the daemon's dependencies. [Open] builds one from the machine;
// a test builds one with its own doubles.
type Options struct {
	// Retirement shares turn coordination; zero options disable automatic retirement.
	Retirement RetirementExecutionOptions

	// AutoResumeOnSessionLimit is the loaded opt-in for automatic quota recovery.
	// False disables automatic recovery without affecting manual schedules.
	AutoResumeOnSessionLimit bool

	DefaultAgent agent.Kind
	Classifiers  map[agent.Kind]agent.Classifier
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
	// DiagnosticRedactor uses the already resolved token/environment hygiene.
	DiagnosticRedactor *redact.Redactor

	// ProposalGenerator may be nil when the configured coding tool has no
	// bounded schedule-advice adapter. --propose then receives an immediate
	// failure reply; it never falls through to a normal agent workflow.
	ProposalGenerator ProposalGenerator

	// Confirmer applies a validated conflict-resolution confirmation
	// (US-12). It may be nil in a dependency-light test that predates this
	// feature's wiring, the same accepted gap [Daemon.run] already has for a
	// nil classifier set — production startup (setup.go) always supplies
	// one. A nil Confirmer is never a way around the gate: it is only
	// consulted after [turn.ValidateConfirmation] has already said
	// [turn.ConfirmationOK], and its absence answers the thread with a
	// failure rather than silently accepting the confirmation.
	Confirmer ConfirmApplier

	// ConflictProposalTTL bounds how long a pending conflict-resolution
	// proposal may be confirmed (US-12). It is read here, at dispatch, as
	// well as inside a turn (see [turn.Options.ConflictProposalTTL]),
	// because a proposal can go stale purely by the clock passing with no
	// further run ever starting on that task — the sweep at proposal-
	// creation time alone would never catch it. Zero disables the sweep,
	// the same as the turn-side field.
	ConflictProposalTTL time.Duration

	// ToolProfiles are the canonical mode permission surfaces.
	//
	// It is read-only for the life of the daemon and never written to. Whatever
	// a resolution takes out of it is copied first — see [Daemon.resolveTools].
	ToolProfiles map[string]config.ToolProfile

	// ToolCeiling is the hard upper bound every profile was checked against at
	// startup, carried here so the daemon can record which boundary was in
	// force when it resolved a profile. Nothing is decided by it: the ceiling
	// is enforced by config validation at boot and again by the harness at the
	// moment argv is built (D-2), and a daemon that made a third, subtly
	// different decision about it would be the one nobody could reason about.
	ToolCeiling []string

	// Agents is the set of coding-agent-tool implementations this daemon has
	// built, keyed by the [agent.Kind] a resolved Coding Agent label names.
	// Optional: nil or empty means no Coding Agent label can resolve, which is
	// the agent-selection counterpart of [Options.ToolProfiles]'s own D-5 —
	// a label asking for an implementation this daemon does not have is
	// refused rather than silently ignored.
	//
	// It is read-only for the life of the daemon and never written to. See
	// [Daemon.resolveAgent].
	Agents turn.RunnerSet

	// InstanceID is this daemon process's identity, and is what the per-task
	// lock is held under. It must be fresh per process — see [NewInstanceID] —
	// because "not the current instance" is how a lock left by a dead daemon is
	// recognised (AC-3.2).
	InstanceID string

	// Interval is the tick interval. Zero means [DefaultInterval]; anything
	// outside [MinInterval]..[MaxInterval] is a startup error (AC-2.7).
	Interval time.Duration

	// MaxConcurrentRuns is how many runs may be in flight at the same instant,
	// daemon-wide across every repository (D2). Zero means
	// [DefaultConcurrentRuns]; anything outside
	// [MinConcurrentRuns]..[MaxConcurrentRuns] is a startup error
	// (AC-PAR-6.1), exactly as Interval is.
	//
	// Zero-means-default is what makes this a usable test seam: a test sets
	// only the field it is asserting on. It is the capacity [New] builds the
	// worker pool with, and the pool is created once, so this is the only
	// moment the number is decided.
	MaxConcurrentRuns int

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
	if o.MaxConcurrentRuns != 0 && (o.MaxConcurrentRuns < MinConcurrentRuns || o.MaxConcurrentRuns > MaxConcurrentRuns) {
		// AC-PAR-6.1: out of range refuses to start. internal/config checks the
		// same bound on max_concurrent_runs; this one catches every other way a
		// value can reach the loop.
		return invalidOptions("max concurrent runs %d is outside the supported range %d..%d",
			o.MaxConcurrentRuns, MinConcurrentRuns, MaxConcurrentRuns)
	}
	return nil
}

// Daemon is the poll loop.
//
// It is built once per process and is not reusable after [Daemon.Close]. Its
// mutable state is the rate-limit backoff, the in-flight set and the pool, all
// mutex- or channel-guarded. [Daemon.Tick] may therefore be called from a test
// on any goroutine — but it is *not* safe to call concurrently with itself, and
// never has been: the poll cursor is a read-modify-write.
type Daemon struct {
	publications publicationRecovery
	mcpClose     func() error
	retirement   RetirementExecutionOptions

	db        *store.DB
	forge     Forge
	filter    Classifier
	turn      Executor
	reporter  Reporter
	proposals ProposalGenerator
	// confirmer and conflictProposalTTL are US-12's confirmation-dispatch
	// dependencies; see [Options.Confirmer] and [Options.ConflictProposalTTL].
	confirmer           ConfirmApplier
	conflictProposalTTL time.Duration
	instanceID          string
	// Loaded once at startup, shared by dispatch and pending recovery cancellation.
	autoResumeOnSessionLimit bool
	// toolProfiles and toolCeiling are the operator's tool configuration as it
	// stood at startup. Read-only after New; see [Options.ToolProfiles].
	toolProfiles map[string]config.ToolProfile
	toolCeiling  []string
	// agents is the set of coding-agent-tool Kinds this daemon has a
	// [agent.Runner] registered for, as it stood at startup. Read-only after
	// New; see [Options.Agents] and [Daemon.resolveAgent].
	agents       turn.RunnerSet
	classifiers  map[agent.Kind]agent.Classifier
	defaultAgent agent.Kind
	interval     time.Duration
	grace        time.Duration
	log          *slog.Logger
	lock         *Lock
	now          func() time.Time
	ticks        <-chan time.Time

	discovery          discoveryBudget
	backoff            backoff
	inflight           inflight
	stoppedDebt        stoppedResults
	recoveries         reportRecoveryCache
	prepFailures       prepareFailures
	diagnosticRedactor *redact.Redactor
	pool               *pool

	// lastRepoID and repoSessionCursor are the two levels of the dispatch
	// rotation (RD-1): the repository the last dispatch came from, and per
	// repository the session id it last dispatched.
	//
	// They live here because they have to survive between ticks — that is the
	// whole of what makes service bounded rather than merely fair within one
	// tick — and being on Daemon alongside the mutex-guarded backoff and the
	// channel-guarded pool invites the assumption that they are shared too. They
	// are not, and they deliberately have no mutex: they are read and written
	// only by [Daemon.dispatch], on the Tick goroutine, which is never
	// concurrent with itself, and no worker can reach them.
	lastRepoID        int64
	repoSessionCursor map[int64]int64
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
	maxRuns := opts.MaxConcurrentRuns
	if maxRuns == 0 {
		maxRuns = DefaultConcurrentRuns
	}
	defaultAgent := opts.DefaultAgent
	if defaultAgent == "" {
		return nil, invalidOptions("a default agent is required")
	}
	if _, ok := opts.Agents[defaultAgent]; !ok {
		return nil, invalidOptions("default agent %q has no registered runner", defaultAgent)
	}
	if _, ok := opts.Classifiers[defaultAgent]; !ok {
		return nil, invalidOptions("default agent %q has no mode classifier", defaultAgent)
	}

	return &Daemon{
		db:                       opts.DB,
		forge:                    opts.Forge,
		filter:                   opts.Filter,
		turn:                     opts.Turn,
		reporter:                 opts.Reporter,
		diagnosticRedactor:       opts.DiagnosticRedactor,
		proposals:                opts.ProposalGenerator,
		confirmer:                opts.Confirmer,
		conflictProposalTTL:      opts.ConflictProposalTTL,
		instanceID:               opts.InstanceID,
		retirement:               opts.Retirement,
		autoResumeOnSessionLimit: opts.AutoResumeOnSessionLimit,
		// Copied, like every other tool list that crosses a boundary in this
		// feature: the caller keeps its own map and slice, and nothing here may
		// reach back into the configuration the process is running on.
		toolProfiles: cloneToolProfiles(opts.ToolProfiles),
		toolCeiling:  append([]string(nil), opts.ToolCeiling...),
		// Copied like every other tool/agent registry that crosses this
		// boundary: the caller keeps its own map, and nothing here may reach
		// back into the registry the process was built with.
		agents:       maps.Clone(opts.Agents),
		classifiers:  maps.Clone(opts.Classifiers),
		defaultAgent: defaultAgent,
		interval:     interval,
		grace:        grace,
		log:          opts.Logger,
		lock:         opts.Lock,
		now:          now,
		ticks:        opts.Ticks,
		pool:         newPool(maxRuns),

		repoSessionCursor: make(map[int64]int64),
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
	var authorityErr error
	if d.mcpClose != nil {
		authorityErr = d.mcpClose()
	}
	return errors.Join(authorityErr, d.lock.Close())
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

	if _, err := d.db.ReconcileResearchGraphs(ctx, reconciliation.At); err != nil {
		return Reconciled{}, err
	}
	if _, err := d.db.ReconcileWorkflowGraphs(ctx, "plan", reconciliation.At); err != nil {
		return Reconciled{}, err
	}
	d.refreshRecoveries(ctx, time.Time{})

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
		graph, err := d.db.HasWorkflowGraphRun(ctx, run.ID, run.Mode)
		if err != nil {
			return result, err
		}
		if graph {
			continue
		} // Only explicit graph comment activities publish.
		if d.reportAbandoned(ctx, run) {
			result.Reported++
		}
	}
	if err := d.db.RetireCompletedModeSelections(ctx); err != nil {
		return result, fmt.Errorf("retire completed mode selections at startup: %w", err)
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

	posted, err := d.reportRun(ctx, turn.RunReport{
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
		if isRecoveryPending(err) {
			return false
		}
		log.ErrorContext(ctx, "could not post the failure comment for an abandoned run; "+
			"it stays unreported and its comment stays unconsumed, so the next tick that sees "+
			"this issue reports it without running claude again",
			slog.Int64("comment_id", posted.CommentID), slog.Any("error", err))
		return false
	}
	if err := d.retireModeSelections(ctx, run.SessionID); err != nil {
		d.markBacklog(ctx, log, run.SessionID)
		log.ErrorContext(ctx, "could not retire the reported run's mode selection",
			slog.Any("error", err))
	}
	if posted.Skipped {
		// Already reported by a previous start. Nothing more to report.
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
// The loop is one of the four goroutines litefactory runs — the other three are
// the harness's two pipe readers and main's second-signal watcher — plus the at
// most max_concurrent_runs turn workers it dispatches, every one of which
// terminates on the one named condition, [Daemon.Quiesce]. This is what the
// loop's own goroutine buys: SIGTERM has to return within three seconds even
// when a turn is in flight, and an in-flight turn can take longer than that to
// unwind — the harness gives claude's process subtree ten seconds to die and
// its pipes ten more. Waiting on the turn would blow the budget; running the
// loop here and returning on cancellation cannot. The goroutine's termination
// condition is the same context: it stops at the end of the turn it is in, and
// if it outlasts the grace the process exits and takes it with it, leaving a
// run row the next start reconciles (D-B).
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

// loop ticks until the context is cancelled or the tick source closes, and
// does not return until every worker its ticks dispatched has finished.
//
// A failed tick is logged and never fatal: the cursor was not persisted, so the
// next tick redoes the work (AC-2.13), and a daemon that exited because GitHub
// returned a 502 would need a human to notice and restart it.
//
// The join is here rather than in [Daemon.Run], and the frame is the mechanism:
// Run's goroutine defers close(done) and then calls loop, so loop's own defer
// runs strictly first and done cannot be closed until the wait has returned.
// That is what makes done mean "the loop returned *and* every worker finished"
// instead of only the first half, and it is why Run's grace timer covers the
// whole shutdown — N runs included — without a line of Run changing.
func (d *Daemon) loop(ctx context.Context, ticks <-chan time.Time) {
	defer d.pool.wait()

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
				if isRecoveryPending(err) {
					continue
				}
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
