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
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/standardsexceptions"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
	"github.com/cloudvane/litefactory/internal/webhook"
)

// Setup is what building a real daemon needs from the machine.
//
// The credential arrives already resolved. That is D-D: under launchd,
// /usr/bin/security can raise an allow-access dialog nobody can see, so the
// token is read exactly once, by the command, and held for the process
// lifetime. Saved project versions resolve their own external references once
// when loaded; refresh explicitly verifies the same retained actor.
type Setup struct {
	// BuildVersion identifies the running process; FactorySource is set only when known.
	BuildVersion  string
	FactorySource string
	// ConnectionSecrets resolves project-specific external credential references.
	ConnectionSecrets secrets.Resolver
	// ProjectStartup binds a fallback startup credential to its saved origin.
	ProjectStartup  bool
	BootstrapOrigin ticket.Origin
	// Launchd limits project credentials to the keychain for background operation.
	Launchd bool

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
	// WebhookSecrets resolves per-repository webhook secrets. The zero value
	// uses the real keychain; tests substitute the lookup.
	WebhookSecrets secrets.Resolver
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
	// Interval is the heartbeat interval in force: the fixed internal tick
	// that runs all local work.
	Interval time.Duration
	// DiscoveryInterval is the fallback poll interval in force: how often the
	// forge listing runs between heartbeats. It is reported on its own line.
	DiscoveryInterval time.Duration
	// WebhookAddr is the bound webhook listener address, empty when off.
	WebhookAddr string
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

	// The heartbeat is fixed internal behaviour, not configuration; the
	// operator's interval is the discovery poll, checked again here.
	interval := DefaultInterval
	pollMinutes := s.Config.Settings.PollIntervalMinutes
	if pollMinutes < config.MinPollIntervalMinutes || pollMinutes > config.MaxPollIntervalMinutes {
		return nil, Startup{}, invalidOptions(
			"settings.poll_interval_minutes is %d, which is outside the supported range %d..%d",
			pollMinutes, config.MinPollIntervalMinutes, config.MaxPollIntervalMinutes)
	}
	discoveryInterval := time.Duration(pollMinutes) * time.Minute

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

	bootstrapOrigin := s.Config.Providers.GitHub.APIBase
	legacyToken := s.Token
	if s.ProjectStartup {
		bootstrapOrigin = string(s.BootstrapOrigin)
		legacyToken = secrets.Token{}
	}
	client, err := github.New(github.Options{
		APIBase:   bootstrapOrigin,
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
	identities.TicketOrigin, err = ticket.CanonicalOrigin(s.Config.Providers.GitHub.APIBase)
	if err != nil {
		return nil, Startup{}, err
	}
	ticketIngress, err := github.NewTickets(client)
	if err != nil {
		return nil, Startup{}, err
	}
	sharedRedactor := redact.New(redact.Options{Logger: s.Logger, Token: s.Token, Environ: environ})
	legacyRef, _ := secrets.ReferenceFrom(s.Config.Providers.GitHub.TokenKeychain.Service, s.Config.Providers.GitHub.TokenKeychain.Account, s.Config.Providers.GitHub.TokenEnv)
	connections := projectconnection.NewFactory(projectconnection.Options{DB: s.DB, Resolver: s.ConnectionSecrets, Logger: s.Logger, UserAgent: s.UserAgent, Redactor: sharedRedactor, ExpectedActorID: identities.BotUserID, Launchd: s.Launchd, LegacyReference: legacyRef, LegacyToken: legacyToken})
	if err := projectconnection.Initialize(ctx, s.DB, *s.Config, connections); err != nil {
		return nil, Startup{}, err
	}
	if err := projectconnection.InitializeApprovers(ctx, s.DB, connections); err != nil {
		return nil, Startup{}, err
	}
	selector := func(ctx context.Context, repo github.Repo, number int, sessionID int64) (turn.WorkflowTickets, int64, error) {
		return selectTicketConnection(ctx, s.DB, connections, repo, number, sessionID)
	}
	ticketWorkflow, err := turn.NewRoutedGitHubTicketWorkflow(s.DB, ticketIngress, identities.BotUserID, selector)
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
		Redactor:  sharedRedactor,
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
		Forge:        ticketWorkflow,
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

	// The webhook listener is bound right after the lock: a port that is taken
	// fails start-up here, before any work exists, and gives the lock back. A
	// doctor repair host never reaches this: it is a helper process that does
	// not call Open.
	var webhookOpts *WebhookOptions
	handedOver := false
	if addr := s.Config.Webhook.Listen; addr != "" {
		ln, listenErr := webhook.Listen(ctx, addr)
		if listenErr != nil {
			return nil, Startup{}, errors.Join(
				fmt.Errorf("the webhook listener could not bind %s (webhook.listen in config.yaml; set it to another address or to \"\" to turn webhooks off): %w", addr, listenErr),
				lock.Close())
		}
		defer func() {
			if !handedOver {
				_ = ln.Close()
			}
		}()
		webhookOpts = &WebhookOptions{Listener: ln, Secrets: s.WebhookSecrets, PublicURL: s.Config.Webhook.PublicURL}
	}

	chatOpts, chatErr := prepareChat(ctx, s.Config, s.WebhookSecrets)
	if chatErr != nil {
		return nil, Startup{}, errors.Join(chatErr, lock.Close())
	}
	if chatOpts != nil {
		instructions, instructionErr := instructionpackage.New(ctx, s.Paths)
		if instructionErr != nil {
			return nil, Startup{}, errors.Join(instructionErr, lock.Close())
		}
		chatOpts.Instructions = instructions
		chatOpts.InstructionDiagnostics = func(ctx context.Context, diagnostic prompts.Diagnostic) {
			s.Logger.WarnContext(ctx, "chat instruction fallback", "role", diagnostic.Role, "source", diagnostic.Source, "digest", diagnostic.Digest, "category", diagnostic.Category)
		}
		providers := make(map[string]ask.ChatRunner)
		for kind, runner := range runners {
			if native, ok := runner.(ask.ChatRunner); ok {
				providers[string(kind)] = native
			}
		}
		chatOpts.Answers = &ask.Service{Repositories: s.DB, Collector: ask.Collector{Factory: ask.StoreReader{Reader: s.DB}, Issues: ticketWorkflow}, Model: &ask.NativeModel{Config: s.Config, Paths: s.Paths, Providers: providers, Meter: s.DB}, Redactor: sharedRedactor}
		{
			instructions, instructionErr := instructionpackage.New(ctx, s.Paths)
			if instructionErr != nil {
				return nil, Startup{}, errors.Join(instructionErr, lock.Close())
			}
			options := &messaging.ConversationOptions{Instructions: instructions, Factory: true, StateRoot: s.Paths.ConversationsDir(), Providers: make(map[string]messaging.ConversationProvider), Models: make(map[string]string), Reasoners: make(map[string]messaging.ConversationReasoner)}
			for kind, runner := range runners {
				if native, ok := runner.(messaging.ConversationProvider); ok {
					options.Providers[string(kind)] = native
				}
				if model, ok := s.Config.Tiers.ResolveModel(config.Kind(kind), config.TierBalanced); ok {
					options.Models[string(kind)] = model
				}
				// The consult reasoner runs on the flagship tier, with high effort
				// where that model's effort capability is verified.
				if model, ok := s.Config.Tiers.ResolveModel(config.Kind(kind), config.TierFlagship); ok && model != "" {
					reasoner := messaging.ConversationReasoner{Model: model}
					if config.ValidateThinking(config.Kind(kind), model, "high") == nil {
						reasoner.Effort = "high"
					}
					options.Reasoners[string(kind)] = reasoner
				}
			}
			for _, binding := range chatOpts.Settings.Conversations {
				options.Bindings = append(options.Bindings, messaging.ConversationBinding{Channel: binding.ChannelID, Name: binding.Name, Provider: binding.Provider, Model: options.Models[binding.Provider]})
			}
			chatOpts.Conversations = options
		}
	}

	// Built once and reused for both NewExecutor and NewConfirmApplier
	// (US-12): the ordinary turn and the agentless apply turn share every
	// dependency, and building turn.Options twice would be two chances for
	// the two to drift.
	conflictProposalTTL := time.Duration(s.Config.Settings.ConflictProposalTTLHours) * time.Hour
	codeDelivery := NewProjectCodeDelivery(s.DB, connections, gitBinary)
	storyProgress, err := turn.NewStoryProgress(s.DB, ticketWorkflow, renderer, identities.BotUserID, s.Paths.StoryProgressLocksDir(), codeDelivery)
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
		creationAuthorizer, err := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{DB: s.DB, ProjectApprovers: true, Store: creationStore, Enabled: s.Config.Settings.AllowMCPStoryCreation, OwnerID: identities.OwnerUserID, BotID: identities.BotUserID})
		if err != nil {
			return nil, Startup{}, errors.Join(err, lock.Close())
		}
		mcpRuntime = &turn.MCPRuntime{Authority: authority, Paths: s.Paths, Executable: executable, Now: now, Issues: ticketWorkflow, DisableMCP: !s.Config.Settings.MCPAutoStart, Comments: ticketWorkflow, Creation: ticketWorkflow, CreationAuthorizer: creationAuthorizer, Redactor: sharedRedactor, BotID: identities.BotUserID}
	}
	sharedCloneGate := newCloneGate()
	workflowSource, err := repositoryWorkflowSource(s.Paths, s.DB)
	if err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}
	judgeProviders := make(map[agent.Kind]agent.JudgeRunner)
	for kind, runner := range runners {
		if native, ok := runner.(agent.JudgeRunner); ok {
			judgeProviders[kind] = native
		}
	}
	judgeService := &turn.JudgeService{Logger: s.Logger, DB: s.DB, Config: s.Config, Providers: judgeProviders, Paths: s.Paths, Executable: mcpRuntime.Executable, Git: gitBinary, Redactor: sharedRedactor}
	for _, diagnostic := range s.Config.WorkerTimeoutDiagnostics() {
		s.Logger.Warn(diagnostic)
	}
	turnOpts := turn.Options{
		WorkerTimeoutSeconds: s.Config.Settings.WorkerTimeoutSeconds,
		WorkerTimeoutSource:  s.Config.WorkerTimeoutSource(),
		ProjectApprovers:     true,
		VerifyExecutionConnection: func(ctx context.Context, session store.Session) error {
			snapshot, e := s.DB.GetExecutionSnapshot(ctx, session.ID)
			if e != nil {
				return e
			}
			_, e = codeDelivery.selectClient(sessionContext(ctx, session.ID), github.Repo{Owner: snapshot.CodeTarget.Owner, Name: snapshot.CodeTarget.Name}, false)
			return e
		},
		WorkflowSource:           workflowSource,
		ResearchDriver:           turn.ExecuteResearchWorkflow,
		ResearchReporter:         reporter,
		Judge:                    judgeService,
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
		SubIssueCreator:   ticketWorkflow,
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
		//nolint:staticcheck // SA1019: the deprecated setting is still bound until it is removed.
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
	// Checks wait for disk space rather than fail on a full volume.
	isolation.MinFreeBytes = s.Config.Settings.MinFreeDiskBytes()
	// Spike checks retain the configured operator sandbox.
	mcpRuntime.BugChecksIsolation = isolation
	bugRuntime, err := turn.NewGraphBugRuntime(ctx, turnOpts, turn.BugRuntimeConfig{
		Code: codeDelivery, Tickets: ticketWorkflow, OwnerID: identities.OwnerUserID,
		Permissions: bugRolePermissions(toolProfiles, toolCeiling), Isolation: isolation,
	})
	if err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}
	turnOpts.Bug = bugRuntime
	// The Validate graph's host bindings. Its reviewers take their run slots
	// from the daemon's own pool, which exists only once the daemon is built
	// below, so the slots are bound to it then.
	validateSlots := &runSlots{}
	validateRuntime, err := turn.NewValidateRuntime(ctx, turnOpts, turn.ValidateRuntimeConfig{Code: codeDelivery, Tickets: ticketWorkflow, OwnerID: identities.OwnerUserID, Slots: validateSlots, Permissions: bugRolePermissions(toolProfiles, toolCeiling)})
	if err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}
	turnOpts.Validate = validateRuntime
	// Automatic start is offered only when the Validate graph has its host
	// bindings; the hand-off line must not promise a start that cannot happen.
	turnOpts.ValidateAutoStart = s.Config.ValidateMode.AutoStart && turnOpts.Validate != nil
	status, err := turn.NewImplementStatusService(ctx, turn.ImplementStatusOptions{
		Now: mcpRuntime.Now, Wait: waitImplementStatus,
		Logger: s.Logger,
		Refresh: func(ctx context.Context, parent string, generation int64) (bool, error) {
			return turn.RefreshImplementParentStatus(ctx, turnOpts, parent, generation)
		},
	})
	if err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}
	mcpRuntime.ImplementStatus = status
	analyses, err := newConversationAnalyses(s, connections, sharedRedactor, gitBinary)
	if err != nil {
		status.Close()
		authorityErr := mcpRuntime.Close(context.WithoutCancel(ctx))
		return nil, Startup{}, errors.Join(err, authorityErr, lock.Close())
	}
	if chatOpts != nil && chatOpts.Conversations != nil {
		instructions, captureErr := instructionpackage.New(ctx, s.Paths)
		if captureErr != nil {
			return nil, Startup{}, errors.Join(captureErr, lock.Close())
		}
		providers := make(map[string]response.ChatRunner)
		for kind, runner := range runners {
			if native, ok := runner.(response.ChatRunner); ok {
				providers[string(kind)] = native
			}
		}
		book := &standards.Service{Store: s.DB, OwnerID: identities.OwnerUserID}
		reader := projectread.Reader{DB: s.DB, Git: gitBinary, Redactor: sharedRedactor}
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return nil, Startup{}, errors.Join(executableErr, lock.Close())
		}
		reads := projectread.Session{}
		if s.Config.Settings.MCPAutoStart {
			reads = projectread.Session{Reader: reader, RunsRoot: s.Paths.RunsDir(), Executable: executable, Prepare: projectSecrets(s.DB, connections)}
		}
		author := &standards.Authoring{Store: s.DB, Book: book, Instructions: instructions, Projects: reader, Runner: &response.Authoring{Model: &response.NativeModel{Config: s.Config, Paths: s.Paths, Providers: providers, Meter: s.DB}, Reads: reads}}
		chatOpts.Standards = &ConversationStandards{DB: s.DB, Book: book, Authoring: author, Exceptions: &standardsexceptions.Service{Store: s.DB, OwnerID: identities.OwnerUserID}, Analyses: analyses}
	}
	daemon, err := New(Options{
		Paths:      s.Paths,
		Doctor:     &doctor.Service{BuildVersion: s.BuildVersion, FactorySource: s.FactorySource, Logger: s.Logger, DB: s.DB, Paths: s.Paths, Config: s.Config, Redactor: sharedRedactor},
		Bug:        bugRuntime,
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
		MinFreeDiskBytes:         s.Config.Settings.MinFreeDiskBytes(),
		DiskPath:                 s.Paths.Home(),
		// An automatic start needs the Validate graph's host bindings; without
		// them the turn would take the legacy path, which must never be started
		// by itself.
		ValidateAutoStart: s.Config.ValidateMode.AutoStart && turnOpts.Validate != nil,
		ValidateHead: func(ctx context.Context, session store.Session) (string, error) {
			return turn.IntegrationHead(ctx, turnOpts, session)
		},
		DB:          s.DB,
		Connections: connections,
		Forge:       ticketWorkflow,
		Filter:      filter,
		Tickets:     ticketIngress,
		// Schedule proposals are a separate, tool-free advisory call. They
		// deliberately do not travel through turn.Execute, so no run is
		// allocated and no worktree is created before owner acceptance.
		ProposalGenerator: newProposalGenerator(defaultAgent, runners),
		Turn:              NewExecutor(turnOpts, connections),
		// US-12's agentless apply turn: the daemon's confirmation dispatch
		// reaches turn.ExecuteApplyResolution only through this seam, never
		// directly, so a fake in a dependency-light test can stand in for it
		// without a git clone and a claude binary.
		Confirmer:           NewConfirmApplier(turnOpts, connections),
		ConflictProposalTTL: conflictProposalTTL,
		Reporter:            reporter,
		DiagnosticRedactor:  sharedRedactor,
		// The same two keys the harness was built from above, so the profile a
		// label selects and the ceiling that bounds it are one decision made in
		// one place — internal/config resolved the ceiling before it validated
		// anything against it.
		ToolProfiles: toolProfiles,
		// The same registry turn.Options.Runners was just built from above, so
		// a Coding Agent label is judged against exactly the implementations
		// this run could actually be dispatched to — never a second, looser
		// notion of "known agent" kept in step by hand.
		Agents:            runners,
		Classifiers:       classifiers,
		DefaultAgent:      defaultAgent,
		ToolCeiling:       toolCeiling,
		InstanceID:        instanceID,
		Interval:          interval,
		DiscoveryInterval: discoveryInterval,
		Webhook:           webhookOpts,
		Chat:              chatOpts,
		// Nothing spends the slots yet; carrying the value from here means the
		// pool, when it lands, reads a number that has been clamped three times.
		MaxConcurrentRuns: maxRuns,
		Logger:            s.Logger,
		Lock:              lock,
		Now:               s.Now,
	})
	if err != nil {
		status.Close()
		authorityErr := mcpRuntime.Close(context.WithoutCancel(ctx))
		return nil, Startup{}, errors.Join(err, authorityErr, lock.Close())
	}
	handedOver = true
	validateSlots.bind(daemon.pool)
	if daemon.webhook != nil {
		// Secrets are loaded once here; the heartbeat only retries the missing.
		now := s.Now
		if now == nil {
			now = time.Now
		}
		if daemon.connections != nil {
			daemon.importLegacyWebhookSubscriptions(ctx, now())
			daemon.webhook.refreshSubscriptions(ctx, daemon.db, now())
		} else {
			daemon.webhook.refresh(ctx, registered, now())
		}
	}
	daemon.mcpClose = func() error { return mcpRuntime.Close(context.WithoutCancel(ctx)) }
	daemon.doctorHostOptions = s.DoctorHost
	daemon.judge = judgeService
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
	daemon.doctor.service.Comments = ticketWorkflow
	daemon.doctor.service.RepairRunner = &doctor.RepairAdapters{Providers: repairProviders, Paths: s.Paths, Executable: mcpRuntime.Executable}
	advisors := make(map[string]turn.ConflictAdvisor)
	for kind, runner := range runners {
		if advisor, ok := runner.(turn.ConflictAdvisor); ok {
			advisors[string(kind)] = advisor
		}
	}
	daemon.doctor.service.RepairEffects = &FactoryDoctorRepairEffects{DB: s.DB, Git: gitBinary, CloneGate: sharedCloneGate, Author: turnOpts.Author, Advisors: advisors, Comments: ticketWorkflow, BotID: identities.BotUserID, Redactor: daemon.doctor.service.Redactor}
	daemon.doctor.service.RepairEnvironment = &DoctorRepairEnvironment{DB: s.DB, Paths: s.Paths, Live: daemon}

	if s.DoctorHost != nil {
		daemon.doctor.service.RepairEnvironment = &DoctorRepairEnvironment{DB: s.DB, Paths: s.Paths, Live: daemon, Replacement: s.DoctorHost.Replacement}
		daemon.doctor.service.RestartRoute = &DoctorHostRestart{DB: s.DB, Paths: s.Paths, Live: &DoctorLocalControl{Paths: s.Paths}, Replacement: s.DoctorHost.Replacement}
	}

	startup := Startup{
		DefaultAgent:      defaultAgent,
		InstanceID:        instanceID,
		Interval:          interval,
		DiscoveryInterval: discoveryInterval,
		MaxConcurrentRuns: maxRuns,
		Home:              s.Paths.Home(),
		LockPath:          lock.Path(),
		APIBase:           client.BaseURL(),
		Git:               gitBinary.Path(),
		GitVersion:        gitBinary.Version(),
		Identities:        identities,
		Repos:             len(registered),
	}
	if daemon.webhook != nil {
		startup.WebhookAddr = daemon.webhook.ln.Addr().String()
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

// waitImplementStatus keeps deferred comment timing in the daemon, not the turn.
func waitImplementStatus(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
