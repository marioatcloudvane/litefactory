package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/attach"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Runner is [agent.Runner], aliased here for continuity with the many call
// sites written before that type moved to internal/agent: *harness.Runner
// (Claude) satisfies it today, and internal/codex's runner will once its own
// wire shape is measured.
//
// It is not declared fresh in this package because doing so would be the
// interface-next-to-nothing anti-pattern the M0 layout invariants warn
// against in the other direction: this is a boundary type two independent
// implementation packages have to satisfy without importing internal/turn or
// each other, so it has to live somewhere neither of them is — internal/agent,
// which internal/harness and internal/codex both already import.
type Runner = agent.Runner

// RunnerSet is every coding-agent-tool implementation available to a turn,
// keyed by which [agent.Kind] each one drives. The daemon's factory builds
// exactly one at startup and every turn shares it.
//
// This is what makes [agent.Kind] validity registry-driven rather than a
// hardcoded switch: a Kind is valid exactly when RunnerSet holds an entry for
// it, and a segment naming a Kind with no entry is refused as a reportable
// failure rather than dispatched to a nil Runner. Adding a third
// implementation is registering it here, in internal/daemon's factory —
// nothing in this package or internal/report has to change (AC-5.15).
type RunnerSet map[agent.Kind]Runner

// runner returns the implementation registered for kind, or false when none
// is.
func (s RunnerSet) runner(kind agent.Kind) (Runner, bool) {
	r, ok := s[kind]
	return r, ok
}

// CloneGate serialises the git operations that contend on one registered
// clone. internal/daemon's per-clone gate satisfies it.
//
// It is declared here for the same reason [Runner] is: this is where it is
// consumed. internal/daemon owns when work happens and therefore owns the
// registry; internal/git is a leaf whose [git.Clone] documents itself as
// concurrency-safe, and a package-level map and mutex hidden in there would
// falsify that sentence for every caller and make the serialisation invisible
// at the two places it actually matters.
//
// Which two places is the whole design. A turn takes the gate for its fetch,
// slug freeze and worktree creation, gives it back, runs claude for however
// long that takes, and takes it again to commit and push. The claude run sits
// between the two sections and inside neither, which is why serialising git
// costs almost none of the parallelism it protects (AC-PAR-3.2).
type CloneGate interface {
	// Acquire blocks until the caller holds the gate for the clone at path,
	// and returns the function that gives it back. Every nil-error return is
	// owed exactly one call to release.
	//
	// The context is a parameter, never a field on the implementation: a
	// worker queued behind another worker's 60-second fetch has to be able to
	// abandon the wait when the daemon is shutting down, and an acquisition
	// that cannot be cancelled would hold shutdown past its grace budget. An
	// implementation returns ctx.Err() and no release function in that case.
	Acquire(ctx context.Context, path string) (release func(), err error)
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

	// Runners is every coding-agent-tool implementation this daemon has
	// built, keyed by [agent.Kind]. In production this is built once at
	// startup by internal/daemon's factory — so a Codex binary that cannot
	// be resolved fails boot rather than a later tick (AC-3.18, AC-5.7) —
	// and every turn shares it.
	//
	// Required and refused as the zero value rather than treated as a
	// no-op, the same way [Options.CloneGate] is: a turn that resolved no
	// implementation at all is a turn that cannot run anything, and that has
	// to be a startup error rather than a run that quietly does nothing.
	Runners RunnerSet

	// CloneGate keeps two turns working in one registered clone out of each
	// other's git. Required (AC-PAR-3.1).
	//
	// It is required rather than optional-and-nil-tolerant on purpose. A nil
	// gate degrading to a no-op would be a silent un-serialisation: the
	// measured failures it prevents are 0.75% (worktree add) and 75% (fetch
	// against moved refs), so a build that lost the wiring would look correct
	// for a long time and then post fatal git errors on the issue that read as
	// flakes. There is exactly one production wiring point — NewExecutor in
	// internal/daemon/setup.go — and refusing the zero value here is what makes
	// forgetting it a startup error instead of a Tuesday.
	//
	// The registry is per-process and shared by every turn, which is why it
	// belongs in the value the daemon builds once. Nothing here is
	// per-comment.
	CloneGate CloneGate

	// Paths is the resolved $LF_HOME layout. Run artefacts are written beneath
	// it.
	Paths config.Paths

	// Tiers is the operator-owned specialist model mapping, never remote content.
	Tiers config.Tiers

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

	// SubIssueCreator and SubIssueRenderer are the sub-issue phase's two
	// dependencies (see internal/turn/subissues.go). Nil in either one is a
	// turn that runs without the phase at all: it is skipped rather than
	// refused, the same tolerance [Options.Remote] gets for the same reason —
	// a great many callers of this package, in tests above all, have no
	// occasion to file a plan's sub-issues and should not have to supply a
	// forge client and a renderer to construct a valid Options.
	SubIssueCreator  SubIssueCreator
	SubIssueRenderer SubIssueRenderer

	// AssetFetcher fetches attachment bytes, and AttachmentLimits are the
	// ceilings that ingestion runs under. *github.Client satisfies the
	// first; internal/config supplies the second.
	//
	// A nil fetcher disables ingestion for every turn, the same tolerance
	// SubIssueCreator gets and for the same reason: a great many callers of
	// this package, tests above all, have no occasion to fetch an
	// attachment and should not have to supply a forge client to construct
	// a valid Options. Ingestion is skipped, not refused — a turn that
	// cannot fetch attachments is still a turn that can do its work, which
	// is not true of a missing database or clone gate.
	//
	// The permission gate is a separate thing and lives on the request
	// ([Request.AttachmentsAllowed], US-5). This is capability; that is
	// authority. Both must be present before a single byte is fetched.
	AssetFetcher     attach.Fetcher
	AttachmentLimits attach.Limits

	// AutoSyncBase and AutoSyncMaxBehind are [prepareClone]'s syncBase step's
	// (US-2) configured policy, copied in from [config.Settings.AutoSyncBase]
	// and [config.Settings.AutoSyncMaxBehind] the same way AttachmentLimits is
	// copied in from the attachment ceilings: this package reads a plain bool
	// and a plain int and has no notion of a YAML default of its own.
	//
	// AutoSyncBase false is syncBase's own zero value and disables auto-sync,
	// which is deliberately not [config.Settings.AutoSyncBase]'s default of
	// true — a caller that constructs Options without setting this field (most
	// tests in this package) gets no auto-sync rather than inheriting a
	// default it never asked for. Production always sets both from the loaded
	// configuration (internal/daemon/setup.go).
	//
	// AutoSyncMaxBehind of 0 means no cap, exactly as
	// [config.Settings.AutoSyncMaxBehind] documents; it is never clamped here.
	AutoSyncBase      bool
	AutoSyncMaxBehind int

	// BlockRunOnConflict is [config.Settings.BlockRunOnConflict] (US-4/US-8),
	// copied in the same way AutoSyncBase and AutoSyncMaxBehind are: this
	// package reads a plain bool and has no opinion of its own about whether
	// a conflict should stop the run.
	//
	// True refuses a turn whose task branch would not merge cleanly with the
	// base branch, through [DispositionSyncRefused] and
	// [report.ReplyRefusesSync], before the agent ever starts. False lets the
	// run go ahead against the unmerged branch — the conflict is disclosed,
	// not blocked. A caller that constructs Options without setting this
	// field gets false — the same tolerance AutoSyncBase gets, and for the
	// same reason: production always sets it from the loaded configuration
	// (internal/daemon/setup.go), whose own default is true
	// ([config.Settings.BlockRunOnConflict]).
	BlockRunOnConflict bool

	// SuggestConflictResolution is [config.Settings.SuggestConflictResolution]
	// (US-11), copied in the same way BlockRunOnConflict is: this package
	// reads a plain bool and has no opinion of its own about whether a
	// blocked merge should also collect content for, and ask, a suggested-
	// resolution advisor.
	//
	// True collects and caps the conflicting content inside the same
	// [Options.CloneGate] acquisition [syncBase] already holds for
	// [git.Worktree.MergePreview] (S-10); the advisor call itself always
	// happens later, in [attemptConflictSuggestion], after the gate has
	// been released. False collects nothing and asks nothing — the same
	// tolerance every other Options field defaults to, and this field's own
	// zero value, which is what keeps a caller that constructs Options
	// without setting it on the security-conservative default (AC-11.3).
	SuggestConflictResolution bool

	// ConflictContentMaxBytes is [config.Settings.ConflictContentMaxBytes]
	// (US-11): the total budget [collectConflictContent] caps the
	// conflicting content it collects against, across every conflicting
	// file in one blocked merge (AC-11.5). Zero admits nothing — every file
	// is excluded — rather than being read as "no cap"; production always
	// sets it from the loaded configuration, whose own floor keeps it
	// positive.
	ConflictContentMaxBytes int

	// ConflictProposalTTL bounds how long a pending conflict-resolution
	// proposal survives before a new one for the same session treats it as
	// stale and expires it (US-11), derived from
	// [config.Settings.ConflictProposalTTLHours]. Zero or negative disables
	// expiry for the call [attemptConflictSuggestion] makes.
	ConflictProposalTTL time.Duration

	// Logger receives the turn's structured log lines. Required.
	Logger *slog.Logger
}

func (o Options) validate() error {
	switch {
	case o.DB == nil:
		return invalidOptions("a database handle is required")
	case o.Git == nil:
		return invalidOptions("a resolved git binary is required")
	case len(o.Runners) == 0:
		return invalidOptions("a runner set is required")
	case o.CloneGate == nil:
		return invalidOptions("a clone gate is required")
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
	// Mode is the concrete workflow mode selected before this turn starts.
	Mode agent.Mode
	// SessionID is the task, from store.GetOrCreateSession.
	SessionID int64
	// Issue describes the issue.
	Issue Issue
	// Trigger is the comment that caused this turn.
	Trigger Trigger
	// Command is the parsed comment, from [ParseCommand]. It must be a command
	// that spawns a run; a reply-only command is the caller's to handle.
	Command Command

	// Tools and AllowedTools are the tool surface the resolved mode profile
	// grants this turn, and ToolProfile is its canonical mode-profile name.
	//
	// Every string here is operator-authored configuration. Nothing that
	// originated on the forge travels in these fields. The
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
	CodexSandbox string

	// Agent is the coding-agent tool this turn resolved to, from the daemon's
	// stage-1 label ladder ([Daemon.resolveAgent] in internal/daemon). Empty
	// means no Coding Agent label was on the issue, which resolves to the
	// configured default agent — the first agents entry — the same way an
	// absent lf:tools= label resolves to the harness's default surface
	// (AC-2.1). [prepare] is what applies that default; nothing here does.
	//
	// It is what makes the cold-vs-resume decision and the mid-session
	// switch refusal tool-aware (US-3): a session's segment is fixed to the
	// agent it was created with, and a later run whose label resolves to a
	// different one is refused rather than resumed or silently restarted.
	Agent agent.Kind

	// AttachmentsAllowed is the US-5 permission gate: true only when a
	// selected mode profile is present AND that profile's
	// attachments key is true. It is computed by the daemon's per-agent
	// narrowing stage (tick.go), the same stage that resolves ToolProfile
	// above — deliberately not by [resolveTools]'s pure label ladder, which
	// has no side effects and stays that way.
	//
	// This is a permission, not a capability: true grants no new tool (no
	// WebFetch/WebSearch implied). It only decides whether internal/attach's
	// pre-step runs before the subprocess is spawned. False — the default,
	// and the outcome whenever no label resolved a profile at all — means
	// this turn performs zero fetch attempts and produces prompt/argv bytes
	// identical to a build without this feature (AC-5.1, AC-5.2).
	AttachmentsAllowed bool

	// SyncOverride is the per-issue "Sync:" label's resolved decision from
	// the daemon's stage-1 label ladder (resolveSync in internal/daemon), or
	// nil when no such label was present or the one that was could not be
	// resolved — either way the configured automatic-sync default stays in
	// force. A non-nil value beats that default: true is "on", false is
	// "off" (AC-7.1). US-2's syncBase step is what acts on this; nothing
	// here does.
	SyncOverride *bool
}

func (r Request) validate() error {
	switch {
	case r.SessionID <= 0:
		return invalidRequest("a session id is required")
	case r.Issue.Number <= 0:
		return invalidRequest("issue number %d must be positive", r.Issue.Number)
	case r.Trigger.CommentID < 0:
		return invalidRequest("trigger id %d is neither a comment nor the issue body", r.Trigger.CommentID)
	// Not "positive": 0 is the issue body run as the first instruction. See
	// [Trigger.CommentID] for why the body's trigger id is 0 and not the
	// issue's own id.
	case !r.Command.Spawns():
		return invalidRequest("%s does not spawn a run", r.Command.Kind)
	case r.Command.Text == "":
		return invalidRequest("%s has no instruction text", r.Command.Kind)
	case r.Agent == "":
		return invalidRequest("no coding agent was selected")
	case r.Mode != "" && !r.Mode.Valid():
		return invalidRequest("mode %q is not research, plan, implement, validate or spike", r.Mode)
	default:
		return nil
	}
}

// mode is the run mode. For a command it is the command word without its
// slash, so the runs row, the commit trailer and the metadata document all say
// the same thing and none of them has to be kept in step by hand. Free text
// named no command. The fallback is retained for old direct callers and tests;
// production daemon requests always carry [Request.Mode].
func (r Request) mode() string {
	if r.Mode.Valid() {
		return string(r.Mode)
	}
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
	//
	// # What this lock is now for, and what it is not (RD-3)
	//
	// It is the *cross-process* mechanism, and only that. It is durable, it
	// survives a crash, and it is what stops a second litefactory process
	// against the same $LF_HOME from touching this worktree and this runs row.
	// That role is unchanged.
	//
	// What it is no longer doing is serialising the turns of one daemon. The
	// daemon keeps an in-memory set of the sessions it has a turn in flight for
	// (internal/daemon/inflight.go), enters a session before dispatching it and
	// leaves it only once the report has posted — which is wider than this lock,
	// because the watermark advances after the post, and this lock is released
	// when Execute returns. That set is checked and inserted on the daemon's one
	// Tick goroutine, so two sibling workers of the same daemon can never both
	// arrive here for the same session.
	//
	// Within a process, then, this has become defence in depth against a
	// dispatcher bug rather than the primary mechanism, and DispositionLocked
	// below is reachable only from a second process. Say it here, because the
	// obvious reading of the code is the wrong one: nothing about "the database
	// holds a lock" suggests that it is not what the concurrency rests on.
	acquired, err := opts.DB.AcquireSessionLock(ctx, session.ID, opts.InstanceID)
	if err != nil {
		return result, fmt.Errorf("acquire the lock on task %s: %w", ref, err)
	}
	if !acquired {
		result.Disposition = DispositionLocked
		// "process", and no longer "instance". The word is the whole message.
		// Within one process this branch is unreachable: a session is entered
		// in the daemon's in-flight set before it is dispatched and left only
		// once its report has posted, so a second worker for this session
		// cannot exist to lose this race. Reaching here therefore means one of
		// exactly two things — a genuinely second daemon process against this
		// $LF_HOME, or a bug in the dispatcher — and an operator will not reach
		// the second reading on their own unless the line rules out the naive
		// one. The store lock is defence in depth here, not the intra-process
		// mechanism; see the comment on AcquireSessionLock above.
		logger.Info("task is locked by another daemon process; leaving the comment for a later tick")
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
		// Checked before the generic *failure: an agent-switch refusal is a
		// *failure in spirit — a turn that could not be carried out because
		// of the state of the world — but it is answered with
		// [report.ReplyAgentSwitchRefused] rather than a run-failure comment,
		// which needs its own [Disposition] and its own branch here (US-3).
		var switchRefusal *agentSwitchRefusal
		if errors.As(err, &switchRefusal) {
			return agentSwitchRefused(result, switchRefusal, logger), nil
		}
		// Checked before the generic *failure for the same reason the
		// agent-switch refusal is: a blocked base-branch merge is a *failure
		// in spirit — a turn that could not be carried out because of the
		// state of the world — but it is answered with
		// [report.ReplyRefusesSync] rather than a run-failure comment, which
		// needs its own [Disposition] and its own branch here (US-4).
		var sr *syncRefusal
		if errors.As(err, &sr) {
			result = syncRefused(result, sr, logger)
			// US-11: the model call, if there is to be one, happens here —
			// after [prepare]'s call into [prepareClone] has already
			// returned and its deferred release of [Options.CloneGate] has
			// already run (AC-11.7). Nothing here can turn this refusal
			// into anything but a refusal; see [attemptConflictSuggestion].
			return attemptConflictSuggestion(ctx, opts, req, result, sr, logger), nil
		}
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
	// US-3: what syncBase did to this branch travels to the report, which is
	// the only place a person sees it. Without this the outcome is written to
	// the runs row (US-9) and never spoken aloud.
	result.Sync = reportSync(prep.sync)

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
// one for every other way a turn can end early. For a failure raised before
// allocateRun, no run row was allocated, so there is no ordinal to burn and
// the next comment on this task starts clean.
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

// agentSwitchRefused records a turn whose resolved agent does not own this
// session's segment (US-3).
//
// Unlike [prepareFailed] it allocates nothing and never transitions session
// state: no session-state transition has occurred and no runs row has been
// allocated; the worktree already exists on disk and sessions.worktree_path
// already records it, and both are idempotent and reused by the next turn
// (AC-3.1). The two agent names travel on [Result] rather than being
// formatted here, because it is [Reporter.ReportTurn] that decides whether
// there is even a comment to answer — the issue-body trigger has none — and
// formatting them twice would be two places to keep in step.
func agentSwitchRefused(result Result, refusal *agentSwitchRefusal, logger *slog.Logger) Result {
	result.Disposition = DispositionAgentSwitchRefused
	result.AgentOwning = refusal.owning
	result.AgentRequested = refusal.requested

	logger.Info("refusing a run: the resolved coding-agent tool does not own this task's conversation",
		slog.String("owning_agent", refusal.owning),
		slog.String("requested_agent", refusal.requested))
	return result
}

// syncRefused records a turn whose task branch could not be merged with the
// base branch cleanly (US-4).
//
// Like [agentSwitchRefused] it allocates nothing and never transitions
// session state: no session-state transition has occurred and no runs row
// has been allocated. The branch itself was never touched either —
// [git.Worktree.MergePreview] only ever inspects whether a merge would be
// clean, it never performs one (US-2) — so the task branch is exactly where
// it was before this turn started.
func syncRefused(result Result, refusal *syncRefusal, logger *slog.Logger) Result {
	result.Disposition = DispositionSyncRefused
	result.SyncConflicts = append([]string(nil), refusal.conflicts...)

	if refusal.tooFarBehind {
		result.SyncTooFarBehind = true
		result.SyncBehind, result.SyncMaxBehind = refusal.behind, refusal.maxBehind
		result.SyncBase = refusal.base
		logger.Info("refusing a run: this task's branch is further behind the base branch than the cap allows",
			slog.Int("behind", refusal.behind), slog.Int("max_behind", refusal.maxBehind))
		return result
	}
	logger.Info("refusing a run: the base branch could not be merged into this task's branch cleanly",
		slog.Int("conflicts", len(refusal.conflicts)))
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
