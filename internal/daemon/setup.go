package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/attach"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Setup is what building a real daemon needs from the machine.
//
// The credential arrives already resolved. That is D-D: under launchd,
// /usr/bin/security can raise an allow-access dialog nobody can see, so the
// token is read exactly once, by the command, and held for the process
// lifetime. Nothing below re-resolves it, and there is no path here that could.
type Setup struct {
	// DoctorHost installs factory-owned local lifecycle control; nil keeps it unavailable.
	DoctorHost *DoctorHostOptions
	// Paths is the resolved $LF_HOME layout. Required.
	Paths config.Paths
	// Config is the validated config.yaml. Required.
	Config *config.Config
	// DB is the open, migrated database. Required. The daemon does not close
	// it; whoever opened it owns it.
	DB *store.DB
	// Token is the bot credential, resolved once by the caller. Required: the
	// daemon posts as the bot and has no anonymous mode (S-5).
	Token secrets.Token
	// Logger receives every log line the daemon and its dependencies produce.
	// Required.
	Logger *slog.Logger
	// UserAgent identifies this build to GitHub. Empty means the package
	// default.
	UserAgent string
	// Environ is the daemon's own environment, in os.Environ form. Nil means
	// os.Environ(). It is the input to two separate things: the redactor's
	// secret-shaped value table (S-7) and the claude child's env allowlist
	// (SD-4).
	Environ []string
	// Now is the clock, passed through to the daemon. Nil means time.Now.
	Now func() time.Time
	// LogSink rotates daemon.log for housekeeping. Nil skips daemon.log rotation.
	LogSink LogRotator
}

// Startup is what the daemon resolved at boot.
//
// It exists to be printed. A daemon that has silently fallen back to a claude
// found by searching an interactive shell's PATH works today and fails the
// first time launchd starts it, and the only cheap defence is saying out loud
// which file is about to be executed.
type Startup struct {
	// DefaultAgent handles unlabelled runs and mode classification.
	DefaultAgent agent.Kind
	// InstanceID is this process's identity, and what task locks are held
	// under.
	InstanceID string
	// Interval is the tick interval in force.
	Interval time.Duration
	// MaxConcurrentRuns is how many runs may be in flight at once, daemon-wide
	// (D2). It is printed for the same reason the interval is: the number that
	// decides how hard this machine will be worked should be visible at boot,
	// not inferred from how busy it feels.
	MaxConcurrentRuns int
	// Home is $LF_HOME and LockPath the singleton lock file inside it.
	Home     string
	LockPath string
	// APIBase is the GitHub API root being polled.
	APIBase string
	// Git is the resolved git executable and GitVersion what it reported.
	Git        string
	GitVersion string
	// Claude is the resolved claude executable. Claude.FromPATH true means it
	// was found by searching PATH rather than configured absolutely, which is
	// the configuration that breaks under launchd (SD-7).
	Claude config.Executable
	// ClaudeTimeout is the hard wall-clock limit one run is given.
	ClaudeTimeout time.Duration
	// Identities are the two numeric ids every classification decision is made
	// on (D-I).
	Identities turn.Identities
	// Repos is how many repositories are registered, which is the tick's API
	// cost in requests (AC-2.8).
	Repos int
}

// Open builds a daemon from the machine: it resolves the executables, builds
// the forge client and the reporting chain, and takes the singleton lock.
//
// The order is the point, and it is "everything that can fail without touching
// anything, first":
//
//  1. The tick interval, against the D-C clamp (AC-2.7), and the run
//     concurrency ceiling, against its own (AC-PAR-6.1).
//  2. git and claude, by absolute path, naming the exact PATH searched on
//     failure. This happens before a turn — and therefore before a worktree —
//     can exist (AC-3.18, AC-2.2). launchd hands an agent
//     PATH=/usr/bin:/bin:/usr/sbin:/sbin, which contains no Homebrew, so a
//     claude that resolves in a terminal need not resolve here.
//  3. The forge client, on the credential the caller resolved once (D-D).
//  4. The two identities, by numeric id. A daemon that does not know which
//     account it posts as cannot run the self-filter, and a daemon that cannot
//     run the self-filter reads its own reports as instructions (AC-2.14).
//  5. The redaction and reporting chain, so that nothing can post before S-7
//     is wired up.
//  6. The registered repositories, refusing any two that would create their
//     worktrees in one directory (AC-PAR-4.8). It belongs in this group
//     because it is a security control: the collision is a directory two
//     concurrent, correctly authorised turns can both enter, so it has to be
//     refused before a turn can run rather than discovered by one.
//  7. The singleton lock, last, because it is the first step that leaves a
//     mark (AC-2.6).
//
// The returned daemon owns the lock and releases it on [Daemon.Close].
func Open(ctx context.Context, s Setup) (*Daemon, Startup, error) {
	if err := s.validate(); err != nil {
		return nil, Startup{}, err
	}

	interval := time.Duration(s.Config.Settings.PollIntervalSeconds) * time.Second
	if interval < MinInterval || interval > MaxInterval {
		return nil, Startup{}, invalidOptions(
			"poll_interval_seconds is %d, which is outside the supported range %d..%d",
			s.Config.Settings.PollIntervalSeconds,
			int(MinInterval.Seconds()), int(MaxInterval.Seconds()))
	}

	// Re-clamped here even though config validation already refused anything
	// out of range, for the same reason the interval above is: Open is reached
	// by more than one route into a *config.Config, and a slot count that has
	// not been checked on this path is one nobody can point at.
	maxRuns := s.Config.Settings.MaxConcurrentRuns
	if maxRuns < MinConcurrentRuns || maxRuns > MaxConcurrentRuns {
		return nil, Startup{}, invalidOptions(
			"max_concurrent_runs is %d, which is outside the supported range %d..%d",
			maxRuns, MinConcurrentRuns, MaxConcurrentRuns)
	}

	environ := s.Environ
	if environ == nil {
		environ = os.Environ()
	}

	gitBinary, err := git.Resolve(ctx)
	if err != nil {
		return nil, Startup{}, fmt.Errorf("the daemon needs git: %w", err)
	}
	runners, err := buildRunners(s.Paths, s.Config, environ, s.Logger)
	if err != nil {
		return nil, Startup{}, err
	}
	configuredDefault, ok := s.Config.DefaultAgent()
	if !ok {
		return nil, Startup{}, invalidOptions("configuration has no default agent")
	}
	defaultAgent := agent.Kind(configuredDefault)
	classifiers := make(map[agent.Kind]agent.Classifier, len(runners))
	for kind, runner := range runners {
		classifier, ok := runner.(agent.Classifier)
		if ok {
			classifiers[kind] = classifier
		}
	}
	toolProfiles := s.Config.ToolProfiles.Permissions
	var toolCeiling []string
	if s.Config.Agents.Claude != nil {
		toolCeiling = s.Config.Agents.Claude.ToolCeiling
	}
	// Startup wants to print which claude binary was resolved and its
	// timeout, so the concrete implementation is narrowed back out of the
	// set built above rather than resolved a second time.
	claude, _ := runners[agent.Kind(store.AgentClaude)].(*harness.Runner)

	client, err := github.New(github.Options{
		APIBase:   s.Config.Providers.GitHub.APIBase,
		Token:     s.Token,
		Logger:    s.Logger,
		UserAgent: s.UserAgent,
	})
	if err != nil {
		return nil, Startup{}, err
	}

	identities, err := turn.LoadIdentities(ctx, s.DB, store.ForgeGitHub)
	if err != nil {
		return nil, Startup{}, err
	}
	filter, err := turn.NewFilter(s.DB, identities, s.Logger)
	if err != nil {
		return nil, Startup{}, err
	}

	// Published comments never carry paths on this host: the home directory holds
	// the factory state, instruction bundles and worktrees.
	var hostRoots []string
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		hostRoots = append(hostRoots, home)
	}
	renderer, err := report.New(report.Options{
		Redactor:  redact.New(redact.Options{Logger: s.Logger, Token: s.Token, Environ: environ}),
		Limit:     github.CommentSizeLimit,
		Logger:    s.Logger,
		HostRoots: hostRoots,
	})
	if err != nil {
		return nil, Startup{}, err
	}
	reporter, err := turn.NewReporter(turn.ReporterOptions{
		BotUserID:    identities.BotUserID,
		StoryLockDir: s.Paths.StoryProgressLocksDir(),
		DB:           s.DB,
		Forge:        client,
		Renderer:     renderer,
		Logger:       s.Logger,
	})
	if err != nil {
		return nil, Startup{}, err
	}

	instanceID, err := NewInstanceID()
	if err != nil {
		return nil, Startup{}, err
	}

	registered, err := s.DB.ListRepos(ctx)
	if err != nil {
		return nil, Startup{}, err
	}
	// AC-PAR-4.8. Two repositories under one worktree root map an issue number
	// to one directory, and concurrently that is a directory two correctly
	// authorised turns can both enter. It is checked here, in the group that
	// can fail without leaving a mark, so a machine configured that way refuses
	// to start rather than starting and colliding.
	if err := repos.ValidateWorktreeRoots(registered); err != nil {
		return nil, Startup{}, err
	}

	var lock *Lock
	if s.DoctorHost != nil && s.DoctorHost.Recovery != nil {
		lock, err = WaitDoctorRecoveryLock(ctx, s.DB, s.Paths, *s.DoctorHost.Recovery)
	} else {
		lock, err = AcquireLock(s.Paths.LockPath())
	}
	if err != nil {
		return nil, Startup{}, err
	}

	// Built once and reused for both NewExecutor and NewConfirmApplier
	// (US-12): the ordinary turn and the agentless apply turn share every
	// dependency, and building turn.Options twice would be two chances for
	// the two to drift.
	conflictProposalTTL := time.Duration(s.Config.Settings.ConflictProposalTTLHours) * time.Hour
	storyProgress, err := turn.NewStoryProgress(s.DB, client, renderer, identities.BotUserID, s.Paths.StoryProgressLocksDir())
	if err != nil {
		return nil, Startup{}, err
	}
	var mcpRuntime *turn.MCPRuntime
	{
		executable, resolveErr := os.Executable()
		if resolveErr == nil {
			executable, resolveErr = filepath.EvalSymlinks(executable)
		}
		if resolveErr != nil {
			return nil, Startup{}, errors.Join(resolveErr, lock.Close())
		}
		authority, bindErr := turn.NewAttemptAuthority(ctx, s.DB)
		if bindErr != nil {
			return nil, Startup{}, errors.Join(bindErr, lock.Close())
		}
		now := s.Now
		if now == nil {
			now = time.Now
		}

		creationStore, err := store.NewCreationStore(s.DB, now)
		if err != nil {
			return nil, Startup{}, errors.Join(err, lock.Close())
		}
		creationAuthorizer, err := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: creationStore, Enabled: s.Config.Settings.AllowMCPStoryCreation, OwnerID: identities.OwnerUserID, BotID: identities.BotUserID})
		if err != nil {
			return nil, Startup{}, errors.Join(err, lock.Close())
		}
		mcpRuntime = &turn.MCPRuntime{Authority: authority, Paths: s.Paths, Executable: executable, Now: now, Issues: client, DisableMCP: !s.Config.Settings.MCPAutoStart, Comments: client, Creation: client, CreationAuthorizer: creationAuthorizer, Redactor: redact.New(redact.Options{Logger: s.Logger, Token: s.Token, Environ: environ}), BotID: identities.BotUserID}
	}
	sharedCloneGate := newCloneGate()
	turnOpts := turn.Options{
		ResearchDriver:           turn.ExecuteResearchWorkflow,
		ResearchReporter:         reporter,
		MCP:                      mcpRuntime,
		AutoResumeOnSessionLimit: s.Config.Settings.AutoResumeOnSessionLimit,
		StoryProgress:            storyProgress,
		DB:                       s.DB,
		Git:                      gitBinary,
		Runners:                  runners,
		// One registry for the process, built here because this is the
		// only place a turn's dependencies are bound. turn.Options
		// validates it as required, so this line going missing is a
		// startup error rather than a quietly unserialised daemon
		// (AC-PAR-3.1).
		CloneGate:  sharedCloneGate,
		Paths:      s.Paths,
		Tiers:      copyDeliveryTiers(s.Config.Tiers),
		InstanceID: instanceID,
		// The sub-issue phase's two dependencies (internal/turn/subissues.go,
		// closing CR-B-001): the same forge client and the same renderer
		// already built above for posting the run's own report, reused rather
		// than built twice.
		SubIssueCreator:   client,
		SubIssueRenderer:  renderer,
		ConflictPreviewer: renderer,
		// Attachment ingestion (US-3). The same forge client again: it
		// is the only thing holding the installation token, which is
		// what a private-repo attachment fetch needs, and keeping the
		// fetch there is what lets the SSRF allow-list sit in one place.
		// The ceilings come from the same config the tool profiles above
		// were resolved from — a run's permission to ingest and the
		// limits it ingests under are one decision, made in one file.
		AssetFetcher:     client,
		AttachmentLimits: attachmentLimits(s.Config),
		// US-2's auto-sync policy, read straight off the validated
		// configuration the same way AttachmentLimits is above: this is
		// the one place a turn's dependencies are bound, and
		// config.Settings already enforces AutoSyncMaxBehind's
		// non-negative floor (CFG-6) before this line ever runs.
		AutoSyncBase:      s.Config.Settings.AutoSyncBase,
		AutoSyncMaxBehind: s.Config.Settings.AutoSyncMaxBehind,
		// US-4's hard stop for the conflicts US-2's auto-sync cannot
		// resolve on its own, read from the same validated configuration
		// as the two fields above and bound the same way.
		BlockRunOnConflict: s.Config.Settings.BlockRunOnConflict,
		// US-11's suggested-resolution advisor, read and bound the same
		// way: the flag defaults to false in config.Defaults, so a
		// daemon that has not had it explicitly turned on collects and
		// asks nothing.
		SuggestConflictResolution: s.Config.Settings.SuggestConflictResolution,
		ConflictContentMaxBytes:   s.Config.Settings.ConflictContentMaxBytes,
		ConflictProposalTTL:       conflictProposalTTL,
		Logger:                    s.Logger,
	}

	var isolation bugchecks.Isolation
	if profile := s.Config.BugChecks; profile != nil {
		allowed := make(map[string]bool, len(profile.Environment))
		for _, name := range profile.Environment {
			allowed[name] = true
		}
		values := make(map[string]string, len(allowed))
		for _, entry := range environ {
			name, value, found := strings.Cut(entry, "=")
			if found && allowed[name] {
				values[name] = value
			}
		}
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, Startup{}, errors.Join(fmt.Errorf("resolve operator home for Bug checks: %w", homeErr), lock.Close())
		}
		// Daemon state and credentials are never readable by a check, whatever
		// the operator lists.
		deny := append([]string{filepath.Clean(s.Paths.Home())}, profile.DenyRead...)
		isolation = bugchecks.Isolation{Binary: profile.Binary, WritableRoots: append([]string(nil), profile.WritableRoots...), DenyRead: deny, PATH: profile.PATH, Home: home, Environment: values, Network: profile.Network}
		// Hash the actual operator profile, including declared values, without
		// retaining or exposing those values in check evidence.
		encoded, encodeErr := json.Marshal(isolation)
		if encodeErr != nil {
			return nil, Startup{}, errors.Join(encodeErr, lock.Close())
		}
		digest := sha256.Sum256(encoded)
		isolation.Revision = hex.EncodeToString(digest[:])
	}
	// The bug-fixer's self-check tools run under the same operator sandbox
	// profile as the factory's own final check; a zero value here (no profile
	// configured) simply makes self-checking unavailable, never blocking the
	// fixer.
	mcpRuntime.BugChecksIsolation = isolation
	bugRuntime, err := turn.NewBugRuntime(ctx, turnOpts, turn.BugRuntimeConfig{
		Forge: client, OwnerID: identities.OwnerUserID,
		Permissions: bugRolePermissions(toolProfiles, toolCeiling), Isolation: isolation,
	})
	if err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}
	turnOpts.Bug = bugRuntime
	// A crash mid self-check leaves its run pending with no observed result;
	// restart cancels every such run once, here, before any work is
	// dispatched, and never reruns it (AC-011).
	if err := turn.RecoverBugSelfChecks(ctx, s.DB); err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}
	daemon, err := New(Options{
		Doctor:     &doctor.Service{DB: s.DB, Paths: s.Paths, Config: s.Config, Redactor: redact.New(redact.Options{Logger: s.Logger, Token: s.Token, Environ: environ})},
		Bug:        bugRuntime,
		BugTests:   bugRuntime.Child,
		Retirement: RetirementExecutionOptions{CloneGate: sharedCloneGate, LockDir: s.Paths.StoryProgressLocksDir(), RetainDays: s.Config.Settings.WorktreeRetainDays},
		Archive: ArchiveOptions{Dir: s.Paths.ArchiveDir(), RunsDir: s.Paths.RunsDir(), StartSteps: []ArchiveStep{NewArchiveExpiryStep(ArchiveExpiryOptions{
			Runs: s.DB, RunsDir: s.Paths.RunsDir(), RetainDays: s.Config.Settings.ArchiveRetainDays, Logger: s.Logger,
		})}},
		Housekeeping: HousekeepingOptions{
			DBPath:           s.Paths.DBPath(),
			DeployBackupKeep: s.Config.Settings.DeployBackupKeep,
			LogsDir:          s.Paths.LogsDir(),
			LogRetainDays:    s.Config.Settings.LogRetainDays,
			LogSink:          s.LogSink,
		},
		AutoResumeOnSessionLimit: s.Config.Settings.AutoResumeOnSessionLimit,
		DB:                       s.DB,
		Forge:                    client,
		Filter:                   filter,
		// Schedule proposals are a separate, tool-free advisory call. They
		// deliberately do not travel through turn.Execute, so no run is
		// allocated and no worktree is created before owner acceptance.
		ProposalGenerator: newProposalGenerator(defaultAgent, runners),
		Turn:              NewExecutor(turnOpts),
		// US-12's agentless apply turn: the daemon's confirmation dispatch
		// reaches turn.ExecuteApplyResolution only through this seam, never
		// directly, so a fake in a dependency-light test can stand in for it
		// without a git clone and a claude binary.
		Confirmer:           NewConfirmApplier(turnOpts),
		ConflictProposalTTL: conflictProposalTTL,
		Reporter:            reporter,
		DiagnosticRedactor:  redact.New(redact.Options{Logger: s.Logger, Token: s.Token, Environ: environ}),
		// The same two keys the harness was built from above, so the profile a
		// label selects and the ceiling that bounds it are one decision made in
		// one place — internal/config resolved the ceiling before it validated
		// anything against it.
		ToolProfiles: toolProfiles,
		// The same registry turn.Options.Runners was just built from above, so
		// a Coding Agent label is judged against exactly the implementations
		// this run could actually be dispatched to — never a second, looser
		// notion of "known agent" kept in step by hand.
		Agents:       runners,
		Classifiers:  classifiers,
		DefaultAgent: defaultAgent,
		ToolCeiling:  toolCeiling,
		InstanceID:   instanceID,
		Interval:     interval,
		// Nothing spends the slots yet; carrying the value from here means the
		// pool, when it lands, reads a number that has been clamped three times.
		MaxConcurrentRuns: maxRuns,
		Logger:            s.Logger,
		Lock:              lock,
		Now:               s.Now,
	})
	if err != nil {
		authorityErr := mcpRuntime.Close(context.WithoutCancel(ctx))
		return nil, Startup{}, errors.Join(err, authorityErr, lock.Close())
	}
	daemon.mcpClose = func() error { return mcpRuntime.Close(context.WithoutCancel(ctx)) }
	daemon.doctorHostOptions = s.DoctorHost
	diagnosisProviders := make(map[string]agent.DoctorRunner)
	repairProviders := make(map[string]agent.DoctorRepairRunner)
	for kind, runner := range runners {
		if native, ok := runner.(agent.DoctorRunner); ok {
			diagnosisProviders[string(kind)] = native
		}
		if native, ok := runner.(agent.DoctorRepairRunner); ok {
			repairProviders[string(kind)] = native
		}
	}
	daemon.doctor.service.Runner = &doctor.Adapters{Providers: diagnosisProviders, Paths: s.Paths, Executable: mcpRuntime.Executable}
	daemon.doctor.service.RepairRunner = &doctor.RepairAdapters{Providers: repairProviders, Paths: s.Paths, Executable: mcpRuntime.Executable}
	if s.DoctorHost != nil {
		daemon.doctor.service.RepairEnvironment = &DoctorRepairEnvironment{DB: s.DB, Paths: s.Paths, Live: daemon, Replacement: s.DoctorHost.Replacement}
		daemon.doctor.service.RestartRoute = &DoctorHostRestart{DB: s.DB, Paths: s.Paths, Live: &DoctorLocalControl{Paths: s.Paths}, Replacement: s.DoctorHost.Replacement}
	}

	startup := Startup{
		DefaultAgent:      defaultAgent,
		InstanceID:        instanceID,
		Interval:          interval,
		MaxConcurrentRuns: maxRuns,
		Home:              s.Paths.Home(),
		LockPath:          lock.Path(),
		APIBase:           client.BaseURL(),
		Git:               gitBinary.Path(),
		GitVersion:        gitBinary.Version(),
		Identities:        identities,
		Repos:             len(registered),
	}
	if claude != nil {
		startup.Claude = claude.Binary()
		startup.ClaudeTimeout = claude.Timeout()
	}
	return daemon, startup, nil
}

func copyDeliveryTiers(source config.Tiers) config.Tiers {
	result := config.Tiers{ModelByTier: make(map[config.Kind]map[config.Tier]string, len(source.ModelByTier))}
	for kind, models := range source.ModelByTier {
		result.ModelByTier[kind] = maps.Clone(models)
	}
	return result
}

func (s Setup) validate() error {
	switch {
	case s.Config == nil:
		return invalidOptions("a configuration is required")
	case s.DB == nil:
		return invalidOptions("a database handle is required")
	case s.Logger == nil:
		return invalidOptions("a logger is required")
	case s.Token.Empty():
		return invalidOptions("a bot token is required; the daemon posts as the bot and has no anonymous mode")
	case s.Paths.Home() == "":
		return invalidOptions("$LF_HOME is not resolved")
	default:
		return nil
	}
}

// instanceRandomBytes is how much randomness goes into an instance id. Eight
// bytes is plenty to make two daemons started in the same second on the same
// host distinguishable, which is all this has to do.
const instanceRandomBytes = 8

// NewInstanceID mints this process's daemon identity.
//
// It must be fresh per process rather than derived from the host or the pid,
// because "the lock owner is not the current instance" is exactly how a lock
// left behind by a daemon that was killed is recognised (AC-3.2). A pid is
// reused by the operating system within hours; a hostname never changes at all.
// The host and pid are still in it, because the first question about a stale
// lock is which machine and which process left it.
func NewInstanceID() (string, error) {
	suffix := make([]byte, instanceRandomBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generate a daemon instance id: %w", err)
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(suffix)), nil
}

// attachmentLimits translates the canonical daemon-wide configuration into
// the attachment package's ingestion limits.
func attachmentLimits(cfg *config.Config) attach.Limits {
	if cfg == nil {
		return attach.Limits{
			MaxImageBytes:    config.DefaultAttachmentMaxImageBytes,
			MaxDocumentBytes: config.DefaultAttachmentMaxDocumentBytes,
			MaxTotalBytes:    config.DefaultAttachmentMaxTotalBytes,
			MaxCount:         config.DefaultAttachmentMaxCount,
		}
	}
	return attach.Limits{
		MaxImageBytes:    cfg.Settings.AttachmentMaxImageBytes,
		MaxDocumentBytes: cfg.Settings.AttachmentMaxDocumentBytes,
		MaxTotalBytes:    cfg.Settings.AttachmentMaxTotalBytes,
		MaxCount:         cfg.Settings.AttachmentMaxCount,
	}
}
