package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
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
}

// Startup is what the daemon resolved at boot.
//
// It exists to be printed. A daemon that has silently fallen back to a claude
// found by searching an interactive shell's PATH works today and fails the
// first time launchd starts it, and the only cheap defence is saying out loud
// which file is about to be executed.
type Startup struct {
	// InstanceID is this process's identity, and what task locks are held
	// under.
	InstanceID string
	// Interval is the tick interval in force.
	Interval time.Duration
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
//  1. The tick interval, against the D-C clamp (AC-2.7).
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
//  6. The singleton lock, last, because it is the first step that leaves a
//     mark (AC-2.6).
//
// The returned daemon owns the lock and releases it on [Daemon.Close].
func Open(ctx context.Context, s Setup) (*Daemon, Startup, error) {
	if err := s.validate(); err != nil {
		return nil, Startup{}, err
	}

	interval := time.Duration(s.Config.PollIntervalSeconds) * time.Second
	if interval < MinInterval || interval > MaxInterval {
		return nil, Startup{}, invalidOptions(
			"poll_interval_seconds is %d, which is outside the supported range %d..%d",
			s.Config.PollIntervalSeconds,
			int(MinInterval.Seconds()), int(MaxInterval.Seconds()))
	}

	environ := s.Environ
	if environ == nil {
		environ = os.Environ()
	}

	gitBinary, err := git.Resolve(ctx)
	if err != nil {
		return nil, Startup{}, fmt.Errorf("the daemon needs git: %w", err)
	}
	claude, err := harness.New(harness.Options{
		Paths:   s.Paths,
		Claude:  s.Config.Claude,
		Environ: environ,
		Logger:  s.Logger,
	})
	if err != nil {
		return nil, Startup{}, fmt.Errorf("the daemon needs claude: %w", err)
	}

	client, err := github.New(github.Options{
		APIBase:   s.Config.GitHub.APIBase,
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

	renderer, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Logger: s.Logger, Token: s.Token, Environ: environ}),
		Limit:    github.CommentSizeLimit,
		Logger:   s.Logger,
	})
	if err != nil {
		return nil, Startup{}, err
	}
	reporter, err := turn.NewReporter(turn.ReporterOptions{
		DB:       s.DB,
		Forge:    client,
		Renderer: renderer,
		Logger:   s.Logger,
	})
	if err != nil {
		return nil, Startup{}, err
	}

	instanceID, err := NewInstanceID()
	if err != nil {
		return nil, Startup{}, err
	}

	repos, err := s.DB.ListRepos(ctx)
	if err != nil {
		return nil, Startup{}, err
	}

	lock, err := AcquireLock(s.Paths.LockPath())
	if err != nil {
		return nil, Startup{}, err
	}

	daemon, err := New(Options{
		DB:     s.DB,
		Forge:  client,
		Filter: filter,
		Turn: NewExecutor(turn.Options{
			DB:         s.DB,
			Git:        gitBinary,
			Claude:     claude,
			Paths:      s.Paths,
			InstanceID: instanceID,
			// The sub-issue phase's two dependencies (internal/turn/subissues.go,
			// closing CR-B-001): the same forge client and the same renderer
			// already built above for posting the run's own report, reused rather
			// than built twice.
			SubIssueCreator:  client,
			SubIssueRenderer: renderer,
			Logger:           s.Logger,
		}),
		Reporter: reporter,
		// The same two keys the harness was built from above, so the profile a
		// label selects and the ceiling that bounds it are one decision made in
		// one place — internal/config resolved the ceiling before it validated
		// anything against it.
		ToolProfiles: s.Config.Claude.ToolProfiles,
		ToolCeiling:  s.Config.Claude.ToolCeiling,
		InstanceID:   instanceID,
		Interval:     interval,
		Logger:       s.Logger,
		Lock:         lock,
		Now:          s.Now,
	})
	if err != nil {
		return nil, Startup{}, errors.Join(err, lock.Close())
	}

	return daemon, Startup{
		InstanceID:    instanceID,
		Interval:      interval,
		Home:          s.Paths.Home(),
		LockPath:      lock.Path(),
		APIBase:       client.BaseURL(),
		Git:           gitBinary.Path(),
		GitVersion:    gitBinary.Version(),
		Claude:        claude.Binary(),
		ClaudeTimeout: claude.Timeout(),
		Identities:    identities,
		Repos:         len(repos),
	}, nil
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
