package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// daemonNoTokenMessage is what `daemon run` says when no bot credential is
// configured. The daemon posts as the bot and has no anonymous mode, so this is
// a refusal rather than a warning (S-5).
const daemonNoTokenMessage = `no bot token is configured, so the daemon does not know which account it posts as.
  Store one in the keychain and reference it in %s:
      security add-generic-password -s litefactory -a github-bot -w
      github:
        token_keychain:
          service: "litefactory"
          account: "github-bot"
  Then run ` + "`litefactory init`" + ` to record the bot identity`

// newDaemonRunCommand declares "litefactory daemon run", the foreground poll
// loop that launchd executes.
func newDaemonRunCommand() *Command {
	return &Command{
		Name:  "daemon run",
		Usage: "daemon run",
		Short: "Run the poll loop in the foreground until interrupted",
		Long: "Run the poll loop in this process, in the foreground, until SIGINT or\n" +
			"SIGTERM. This is what the launchd agent executes; run it directly to watch\n" +
			"the loop work.\n\n" +
			"It never forks and writes no pid file. A second instance refuses to start:\n" +
			"the first one holds an exclusive lock on $LF_HOME/daemon.lock.\n\n" +
			"git and claude are resolved before the first tick, so a binary that is not\n" +
			"where the configuration says fails here rather than three hours into a run.",
		New: func(*flag.FlagSet) Handler {
			return func(ctx context.Context, env *Env, _ []string) error {
				return runDaemonRun(ctx, env)
			}
		},
	}
}

// runDaemonRun starts the poll loop and blocks until the process is signalled
// (AC-2.1).
//
// Nothing here is the daemon's behaviour: this resolves the machine — home,
// configuration, database, credential — maps the "not ready" failures onto exit
// 3, and hands the result to internal/daemon. The context is the root signal
// context from main and is the only thing that can stop the loop.
func runDaemonRun(ctx context.Context, env *Env) (err error) {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	cfg, err := loadDaemonConfig(paths)
	if err != nil {
		return err
	}

	db, err := openDaemonDB(ctx, paths)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	// D-D: resolved once, here, and held for the process lifetime. Under
	// launchd /usr/bin/security can raise an allow-access dialog that never
	// surfaces, so re-resolving it per tick would be a prompt storm nobody can
	// answer.
	token, err := resolveDaemonToken(ctx, paths, cfg)
	if err != nil {
		return err
	}

	loop, startup, err := daemon.Open(ctx, daemon.Setup{
		Paths:     paths,
		Config:    cfg,
		DB:        db,
		Token:     token,
		Logger:    env.Logger,
		UserAgent: initUserAgent(env),
	})
	if err != nil {
		return daemonRunError(err)
	}
	defer func() { err = errors.Join(err, loop.Close()) }()

	writeDaemonRunReport(env.Stdout, startup)
	return loop.Run(ctx)
}

// openDaemonDB opens the database and brings it up to date.
//
// A missing file means `init` has never run here, which is a precondition
// rather than a failure — and a much more useful message than whatever SQLite
// would say about a file it was about to create.
func openDaemonDB(ctx context.Context, paths config.Paths) (*store.DB, error) {
	if _, err := os.Stat(paths.DBPath()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Preconditionf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())
		}
		return nil, fmt.Errorf("read %s: %w", paths.DBPath(), err)
	}

	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// resolveDaemonToken reads the bot credential the configuration points at.
//
// Both failures are preconditions and both name the reference that was tried:
// "the daemon will not start" with no indication of which keychain item or
// environment variable it looked for is a message that costs an evening.
func resolveDaemonToken(ctx context.Context, paths config.Paths, cfg *config.Config) (secrets.Token, error) {
	ref, configured := secrets.ReferenceFrom(
		cfg.GitHub.TokenKeychain.Service, cfg.GitHub.TokenKeychain.Account, cfg.GitHub.TokenEnv)
	if !configured {
		return secrets.Token{}, Preconditionf(daemonNoTokenMessage, paths.ConfigPath())
	}

	token, err := secrets.Resolver{}.Resolve(ctx, ref)
	if err != nil {
		return secrets.Token{}, Preconditionf("the bot token could not be resolved: %v\n%s",
			err, resolveTokenHint(err, paths))
	}
	return token, nil
}

// resolveTokenHint is the line of advice that follows a failed resolution.
//
// "Fix the reference" is the right answer for most of them and the wrong one
// for exactly one: a keychain item macOS refused to open was *found*, so
// sending the operator to config.yaml sends them to edit a setting that is
// already correct. That failure means the login keychain is locked, and the
// advice has to say so.
func resolveTokenHint(err error, paths config.Paths) string {
	if !errors.Is(err, secrets.ErrInteractionNotAllowed) {
		return fmt.Sprintf("  Fix the reference in %s and try again", paths.ConfigPath())
	}
	return fmt.Sprintf(`  The reference is fine — the item was found. The login keychain is locked, and
  unlocking it needs a password nobody offered. Unlock it and start the daemon again:
      security unlock-keychain
  That prompts on this terminal, so it works over ssh and under a tool as well as at
  the console. The token is read once at startup, so one unlock covers the whole run —
  but the keychain re-locks on sleep, and the next start will need another one.
  Or drop the keychain for a foreground run — replace token_keychain in %s with
      github:
        token_env: "LITEFACTORY_GITHUB_TOKEN"
  and export that variable in the shell you start the daemon from.`, paths.ConfigPath())
}

// daemonRunError maps a startup failure onto an exit code.
//
// Everything internal/daemon refuses to start over is the machine not being
// ready rather than the command being wrong: a second instance already holding
// the lock (AC-2.6), a claude or git that is not where the configuration says
// (AC-3.18), identities that `init` never derived. All of them are exit 3, and
// all of them carry the message the domain package wrote — which already names
// the lock file, or the PATH that was searched.
func daemonRunError(err error) error {
	switch {
	case errors.As(err, new(*daemon.AlreadyRunningError)),
		errors.Is(err, config.ErrExecutableNotFound),
		errors.Is(err, config.ErrNotExecutable),
		errors.Is(err, store.ErrNotFound):
		return Precondition(err)
	default:
		return err
	}
}

// writeDaemonRunReport prints what the daemon resolved before its first tick.
//
// The claude line is the one that matters. A binary found by searching an
// interactive shell's PATH works now and fails the first time launchd starts
// the agent, because launchd supplies only /usr/bin:/bin:/usr/sbin:/sbin — so
// the report says when that has happened rather than leaving it to be
// discovered in production.
func writeDaemonRunReport(out io.Writer, startup daemon.Startup) {
	fmt.Fprintf(out, "litefactory daemon running in the foreground; press ctrl-c to stop\n")

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  home\t%s\n", startup.Home)
	fmt.Fprintf(tw, "  lock\t%s\n", startup.LockPath)
	fmt.Fprintf(tw, "  instance\t%s\n", startup.InstanceID)
	fmt.Fprintf(tw, "  interval\t%s\n", startup.Interval)
	fmt.Fprintf(tw, "  api\t%s\n", startup.APIBase)
	fmt.Fprintf(tw, "  repos\t%d registered (one listing per repo per tick)\n", startup.Repos)
	fmt.Fprintf(tw, "  owner\t%s (id %d)\n", startup.Identities.OwnerHandle, startup.Identities.OwnerUserID)
	fmt.Fprintf(tw, "  bot\t%s (id %d)\n", startup.Identities.BotHandle, startup.Identities.BotUserID)
	fmt.Fprintf(tw, "  git\t%s (%s)\n", startup.Git, startup.GitVersion)
	fmt.Fprintf(tw, "  claude\t%s%s\n", startup.Claude.Path, daemonClaudeOrigin(startup.Claude))
	fmt.Fprintf(tw, "  run timeout\t%s\n", startup.ClaudeTimeout)
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()

	if startup.Claude.FromPATH {
		fmt.Fprintf(out, "\nclaude.binary is %q and was found by searching this shell's PATH. A launchd\n"+
			"agent is given only %s, so set claude.binary to the\n"+
			"absolute path above before running `litefactory daemon start`.\n",
			startup.Claude.Name, launchd.LaunchdPATH)
	}
}

func daemonClaudeOrigin(claude config.Executable) string {
	if claude.FromPATH {
		return " (found on PATH — see the warning below)"
	}
	return ""
}

// The three launchd commands each take the [launchd.Runner] they will drive.
// A nil runner means the real launchctl, which is what [Commands] passes; a
// test builds its own router over these constructors with a fake, because
// bootstrapping a real LaunchAgent from a test would leave a daemon running on
// the machine. The handlers themselves live in cmd_daemon_agent.go.

// newDaemonStartCommand declares "litefactory daemon start" (T-LF2.7).
func newDaemonStartCommand() *Command { return daemonStartCommand(nil) }

func daemonStartCommand(runner launchd.Runner) *Command {
	return &Command{
		Name:  "daemon start",
		Usage: "daemon start",
		Short: "Write the launchd agent and load it",
		Long: "Write ~/Library/LaunchAgents/" + launchd.DefaultLabel + ".plist and load it\n" +
			"into your GUI session, so the daemon runs now and again at every login.\n\n" +
			"The agent is a LaunchAgent, not a system daemon: claude authenticates\n" +
			"through the keychain, which only exists inside your GUI session.\n\n" +
			"Your current PATH is copied into the plist and printed back, because a\n" +
			"launchd agent is otherwise given only " + launchd.LaunchdPATH + "\n" +
			"and would never find a Homebrew claude.\n\n" +
			"Running it again replaces and reloads the agent.",
		New: func(*flag.FlagSet) Handler {
			return func(ctx context.Context, env *Env, _ []string) error {
				return runDaemonStart(ctx, env, runner)
			}
		},
	}
}

// newDaemonStopCommand declares "litefactory daemon stop" (T-LF2.7).
func newDaemonStopCommand() *Command { return daemonStopCommand(nil) }

func daemonStopCommand(runner launchd.Runner) *Command {
	return &Command{
		Name:  "daemon stop",
		Usage: "daemon stop",
		Short: "Unload the launchd agent",
		Long: "Unload the launchd agent, stopping the daemon. An agent that was not\n" +
			"loaded is reported rather than treated as a failure.\n\n" +
			"The plist is left in place, so `litefactory daemon start` loads the same\n" +
			"configuration again.",
		New: func(*flag.FlagSet) Handler {
			return func(ctx context.Context, env *Env, _ []string) error {
				return runDaemonStop(ctx, env, runner)
			}
		},
	}
}

// newDaemonStatusCommand declares "litefactory daemon status" (T-LF2.7).
func newDaemonStatusCommand() *Command { return daemonStatusCommand(nil) }

func daemonStatusCommand(runner launchd.Runner) *Command {
	return &Command{
		Name:  "daemon status",
		Usage: "daemon status",
		Short: "Report whether the agent is loaded and running",
		Long: "Report whether the launchd agent is installed and loaded, whether a daemon\n" +
			"process is alive and under which pid, and when the daemon last completed a\n" +
			"tick.\n\n" +
			"The pid comes from launchd. litefactory writes no pid file: the daemon is a\n" +
			"foreground process supervised by launchd and never forks.",
		New: func(*flag.FlagSet) Handler {
			return func(ctx context.Context, env *Env, _ []string) error {
				return runDaemonStatus(ctx, env, runner)
			}
		},
	}
}
