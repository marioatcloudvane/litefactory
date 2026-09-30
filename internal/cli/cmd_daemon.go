package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/logging"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// daemonNoTokenMessage is what `daemon run` says when no bot credential is
// configured. The daemon posts as the bot and has no anonymous mode, so this is
// a refusal rather than a warning (S-5).
const daemonNoTokenMessage = `no bot token is configured, so the daemon does not know which account it posts as.
  Store one in the keychain and reference it in %s:
      security add-generic-password -s litefactory -a github-bot -w
      providers:
        github:
          token_keychain:
            service: "litefactory"
            account: "github-bot"
  Then run ` + "`litefactory init`" + ` to record the bot identity`

// daemonRunOptions holds the parsed flags of "litefactory daemon run".
type daemonRunOptions struct {
	// level overrides the configured log level for this run. It is bound here
	// so --help and an unknown flag are handled correctly, but the value
	// itself is never read from this struct: main resolves --level (via
	// [LogLevelOverride]) before the logger exists, which is before this
	// command's Handler ever runs. See the comment on the handler below and
	// on [LogLevelOverride] for why the flag is declared in two places
	// (DEV-1, US-6).
	level        string
	doctorRepair string
	doctorToken  string
}

// bind registers the daemon run flags on fs.
func (o *daemonRunOptions) bind(fs *flag.FlagSet) {
	fs.StringVar(&o.doctorRepair, "doctor-repair", "", "Factory recovery operation identity")
	fs.StringVar(&o.doctorToken, "doctor-token", "", "Factory recovery handoff token")
	fs.StringVar(&o.level, "level", "", "run at this log `level` instead of the configured one "+
		"(debug, info, warn, error)")
}

// newDaemonRunCommand declares "litefactory daemon run", the foreground poll
// loop that launchd executes.
func newDaemonRunCommand() *Command {
	return &Command{
		Name:  "daemon run",
		Usage: "daemon run [--level level]",
		Short: "Run the poll loop in the foreground until interrupted",
		Long: "Run the poll loop in this process, in the foreground, until SIGINT or\n" +
			"SIGTERM. This is what the launchd agent executes; run it directly to watch\n" +
			"the loop work.\n\n" +
			"Ordinary startup stays in this process and writes no pid file. A second instance refuses to start:\n" +
			"the first one holds an exclusive lock on $LF_HOME/daemon.lock.\n\n" +
			"git and claude are resolved before the first tick, so a binary that is not\n" +
			"where the configuration says fails here rather than three hours into a run.\n\n" +
			"--level overrides log_level in config.yaml for this run only; config.yaml\n" +
			"is never modified.",
		New: func(fs *flag.FlagSet) Handler {
			var opts daemonRunOptions
			opts.bind(fs)
			return func(ctx context.Context, env *Env, _ []string) error {
				// opts.level is intentionally unused: by the time this Handler
				// runs, main has already resolved --level with
				// [LogLevelOverride] and built env.Logger at that level, before
				// the router (and this command) existed. Binding it again here
				// costs nothing and buys correct --help text and correct
				// unknown-flag handling (DEV-1, US-6).
				return runDaemonRunWithRecovery(ctx, env, opts.doctorRepair, opts.doctorToken)
			}
		},
	}
}

// LogLevelOverride resolves a --level flag out of a raw argv slice, before the
// router or the logger exist.
//
// This duplicates the --level binding on "daemon run" (see daemonRunOptions
// above) and, more distantly, on "daemon start" (daemonStartOptions). That
// duplication is deliberate rather than an oversight: main builds env.Logger
// before cli.Router ever parses a command's flags, because the logger needs
// to exist before the very first log line, including "dispatch". By the time
// the router's normal flag parsing would see --level, it is too late to
// affect which level the logger was built with. This is the DEV-1 resolution
// recorded in .aiteam/AC_AMENDMENTS.md for US-6 (Option A): read the flag
// twice, once narrowly here for the level it actually controls, and once on
// the command itself so --help and an unknown flag are still reported
// correctly by the router.
//
// args is scanned by hand for a bare "--level"/"-level" (with a following
// value) or a "--level=value"/"-level=value" form -- not with a
// [flag.FlagSet]: fs.Parse stops at the first non-flag argument it meets,
// which for these commands is the command name itself ("daemon", "run"),
// before it ever reaches --level. A hand-rolled scan is unavoidable here,
// not a second implementation of the parser: the keyword itself is still
// parsed exclusively by [logging.ParseLevel] (D-4). Everything else in args
// -- the command name, other flags -- is irrelevant here and ignored; the
// router parses it properly afterwards. When --level appears more than
// once, the last occurrence wins, matching how repeated flags behave under
// [flag.FlagSet].
//
// It returns (level, true, nil) when --level was present and valid,
// (0, false, nil) when it was absent, and (0, false, err) when it was present
// but did not parse with [logging.ParseLevel]. A non-nil error here is a
// usage error: the caller must report it and exit with [ExitUsage] before
// opening the log sink or building the logger, never fall back to the
// configured level silently.
func LogLevelOverride(args []string) (slog.Level, bool, error) {
	var (
		level string
		found bool
	)
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--level" || arg == "-level":
			if i+1 >= len(args) {
				return 0, false, fmt.Errorf("flag needs an argument: %s", arg)
			}
			level = args[i+1]
			found = true
			i++
		case strings.HasPrefix(arg, "--level="):
			level = strings.TrimPrefix(arg, "--level=")
			found = true
		case strings.HasPrefix(arg, "-level="):
			level = strings.TrimPrefix(arg, "-level=")
			found = true
		}
	}

	if !found {
		return 0, false, nil
	}
	parsed, err := logging.ParseLevel(level)
	if err != nil {
		return 0, false, fmt.Errorf("invalid --level %q: %w", level, err)
	}
	return parsed, true, nil
}

// runDaemonRun starts the poll loop and blocks until the process is signalled
// (AC-2.1).
//
// Nothing here is the daemon's behaviour: this resolves the machine — home,
// configuration, database, credential — maps the "not ready" failures onto exit
// 3, and hands the result to internal/daemon. The context is the root signal
// context from main and is the only thing that can stop the loop.
func runDaemonRunWithRecovery(ctx context.Context, env *Env, repairID, recoveryToken string) (err error) {
	if (repairID == "") != (recoveryToken == "") {
		return errors.New("daemon recovery requires both operation identity and token")
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
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

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	replacement, err := daemon.NewDoctorServiceReplacement(db, executable, paths, os.Environ())
	if err != nil {
		return err
	}
	host := &daemon.DoctorHostOptions{Paths: paths, Replacement: replacement, Stop: stop}
	if repairID == "" {
		host.Recovery, err = daemon.ReadDoctorLaunchdRecovery(ctx, db, paths)
		if err != nil {
			return err
		}
	}
	if repairID != "" {
		host.Recovery = &store.DoctorRestart{RepairID: repairID, Token: recoveryToken}
	}
	loop, startup, err := daemon.Open(ctx, daemon.Setup{
		DoctorHost: host,
		Paths:      paths,
		Config:     cfg,
		DB:         db,
		Token:      token,
		Logger:     env.Logger,
		UserAgent:  initUserAgent(env),
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
		cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
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
      providers:
        github:
          token_env: "LITEFACTORY_GITHUB_TOKEN"
  and export that variable in the shell you start the daemon from.`, paths.ConfigPath())
}

// daemonRunError maps a startup failure onto an exit code.
//
// Everything internal/daemon refuses to start over is the machine not being
// ready rather than the command being wrong: a second instance already holding
// the lock (AC-2.6), a claude or git that is not where the configuration says
// (AC-3.18), identities that `init` never derived, two registered repositories
// that would create their worktrees in one directory (AC-PAR-4.8). All of them
// are exit 3, and all of them carry the message the domain package wrote —
// which already names the lock file, the PATH that was searched, or both
// colliding repositories.
func daemonRunError(err error) error {
	switch {
	case errors.As(err, new(*daemon.AlreadyRunningError)),
		errors.Is(err, config.ErrExecutableNotFound),
		errors.Is(err, config.ErrNotExecutable),
		errors.Is(err, repos.ErrWorktreeRootCollision),
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
	fmt.Fprintf(tw, "  concurrency\t%d run(s) at a time, across all repos\n", startup.MaxConcurrentRuns)
	fmt.Fprintf(tw, "  default agent\t%s (first entry under agents)\n", startup.DefaultAgent)
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

// daemonStartOptions holds the parsed flags of "litefactory daemon start".
type daemonStartOptions struct {
	// level, when set, is appended to the plist's ProgramArguments as
	// "--level <level>" (US-6, AC-6.3), so the installed agent runs at this
	// level on every future login rather than whatever log_level is in
	// config.yaml. config.yaml itself is never modified.
	level string
}

// bind registers the daemon start flags on fs.
func (o *daemonStartOptions) bind(fs *flag.FlagSet) {
	fs.StringVar(&o.level, "level", "", "install the agent to run at this log `level` "+
		"(debug, info, warn, error)")
}

func daemonStartCommand(runner launchd.Runner) *Command {
	cmd := &Command{
		Name:  "daemon start",
		Usage: "daemon start [--level level]",
		Short: "Write the launchd agent and load it",
		Long: "Write ~/Library/LaunchAgents/" + launchd.DefaultLabel + ".plist and load it\n" +
			"into your GUI session, so the daemon runs now and again at every login.\n\n" +
			"The agent is a LaunchAgent, not a system daemon: claude authenticates\n" +
			"through the keychain, which only exists inside your GUI session.\n\n" +
			"Your current PATH is copied into the plist and printed back, because a\n" +
			"launchd agent is otherwise given only " + launchd.LaunchdPATH + "\n" +
			"and would never find a Homebrew claude.\n\n" +
			"--level installs the agent to run at that log level on every future login;\n" +
			"config.yaml is not modified. Running the command again replaces and\n" +
			"reloads the agent.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts daemonStartOptions
		opts.bind(fs)
		return func(ctx context.Context, env *Env, _ []string) error {
			if opts.level != "" {
				// Validated here, not just left to daemonProgram: a typo in
				// --level should fail before a launchd agent is installed,
				// not silently at its first "daemon run" tick.
				if _, err := logging.ParseLevel(opts.level); err != nil {
					return Usagef(cmd, "%s", err)
				}
			}
			return runDaemonStart(ctx, env, runner, opts)
		}
	}
	return cmd
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
