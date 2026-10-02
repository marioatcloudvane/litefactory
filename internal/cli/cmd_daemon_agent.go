package cli

// The launchd half of the daemon commands: `daemon start`, `daemon stop` and
// `daemon status`. `daemon run` — the poll loop itself — lives in cmd_daemon.go
// and is a different concern entirely: these three manage a LaunchAgent and
// never poll anything.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// daemonNeverTicked is what `daemon status` prints for a database whose poll
// cursor has never been written. A daemon that has been installed but has not
// yet completed a tick is a normal state, and printing a zero timestamp
// instead would read as a bug.
const daemonNeverTicked = "never"

// runDaemonStart writes the LaunchAgent and loads it (AC-2.2, AC-2.3).
//
// Everything that can fail without touching the machine fails first: the
// configuration, the credential and this binary's own path are resolved before
// a single byte is written or a single launchctl call is made. A `daemon start`
// that refuses is one that left the previously loaded agent exactly as it was.
func runDaemonStart(ctx context.Context, env *Env, runner launchd.Runner, opts daemonStartOptions) error {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	cfg, err := loadDaemonConfig(paths)
	if err != nil {
		return err
	}
	program, err := daemonProgram(opts.level)
	if err != nil {
		return err
	}

	// PATH is materialised from the installing shell. This is one half of the
	// AC-2.2 fix: launchd would otherwise hand the daemon
	// /usr/bin:/bin:/usr/sbin:/sbin, which contains no Homebrew claude. The
	// other half is `daemon run` resolving its binaries at startup.
	//
	// LF_HOME is pinned to the home this command just read, so the daemon
	// cannot end up using a different one than the configuration it was
	// started from.
	environment := map[string]string{
		launchd.PathEnvVar: launchd.EffectivePATH(os.Getenv("PATH")),
		config.EnvHome:     paths.Home(),
	}
	credential, err := resolveDaemonCredential(cfg, paths, environment)
	if err != nil {
		return err
	}

	// launchd creates the redirect files but not the directory that holds
	// them, and a failed redirect kills the job before the daemon runs.
	//nolint:gosec // 0700 in practice: the process umask is 0o077 (see cmd/litefactory/main.go).
	if mkdirErr := os.MkdirAll(paths.LogsDir(), 0o777); mkdirErr != nil {
		return fmt.Errorf("create %s: %w", paths.LogsDir(), mkdirErr)
	}

	manager := &launchd.Manager{Runner: runner, Logger: env.Logger}
	agent := manager.NewAgent(program, paths.LogsDir(), environment)
	result, err := manager.Install(ctx, agent)
	if err != nil {
		return err
	}

	writeStartReport(env.Stdout, agent, result, credential)
	if credential.EnvName != "" {
		writeMaterialisedTokenWarning(env.Stderr, credential, result.PlistPath)
	}
	if credential.Description == "" {
		writeNoCredentialWarning(env.Stderr, paths)
	}
	return nil
}

// runDaemonStop unloads the agent (AC-2.4).
//
// An agent that was not loaded is reported, not failed: stopping a stopped
// daemon is a success, and a script that runs `daemon stop` before `daemon
// start` must not have to care which state it found.
//
// It deliberately needs neither the configuration nor the database — stopping
// has to work on a machine whose config.yaml is what broke the daemon.
func runDaemonStop(ctx context.Context, env *Env, runner launchd.Runner) error {
	manager := &launchd.Manager{Runner: runner, Logger: env.Logger}
	unloaded, err := manager.Uninstall(ctx)
	if err != nil {
		return err
	}
	path, err := manager.PlistPath()
	if err != nil {
		return err
	}

	if !unloaded {
		fmt.Fprintf(env.Stdout, "%s was not loaded in %s\n", manager.ServiceTarget(), manager.Domain())
	} else {
		fmt.Fprintf(env.Stdout, "unloaded %s\n", manager.ServiceTarget())
	}
	fmt.Fprintf(env.Stdout, "  the agent is still installed at %s; `litefactory daemon start` loads it again\n", path)
	return nil
}

// runDaemonStatus reports whether the agent is loaded, whether it is running,
// under which pid, and when the daemon last completed a tick (AC-2.4).
//
// The pid comes from launchctl. The daemon writes no pid file and never forks
// (D-8, AC-2.5), so there is nothing else to read it from — and a pid file
// would go stale the moment launchd killed the job.
func runDaemonStatus(ctx context.Context, env *Env, runner launchd.Runner) error {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	manager := &launchd.Manager{Runner: runner, Logger: env.Logger}
	status, err := manager.Status(ctx)
	if err != nil {
		return err
	}
	lastCompleted, err := lastTickTime(ctx, paths)
	if err != nil {
		return err
	}
	lastTick := daemonNeverTicked
	if !lastCompleted.IsZero() {
		lastTick = lastCompleted.UTC().Format(time.RFC3339)
	}

	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "label\t%s\n", status.Label)
	fmt.Fprintf(tw, "domain\t%s\n", status.Domain)
	fmt.Fprintf(tw, "plist\t%s (%s)\n", status.PlistPath, daemonPresence(status.Installed))
	fmt.Fprintf(tw, "loaded\t%s\n", daemonYesNo(status.Loaded))
	fmt.Fprintf(tw, "running\t%s\n", daemonRunning(status))
	fmt.Fprintf(tw, "last tick\t%s\n", lastTick)
	if err := tw.Flush(); err != nil {
		return err
	}

	health, err := readHealthSnapshot(ctx, paths)
	if err != nil {
		return err
	}
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	if err := printDaemonHeartbeat(env.Stdout, health, lastCompleted, status.Running, now()); err != nil {
		return err
	}
	return printDaemonHealth(env.Stdout, health, status.Running, now())
}

// daemonRunning renders the running column, including launchd's own word for
// the state when there is no process. "no" alone loses the difference between
// a daemon that exited and one that launchd is holding back.
func daemonRunning(status launchd.Status) string {
	switch {
	case status.Running && status.PID > 0:
		return fmt.Sprintf("yes (pid %d)", status.PID)
	case status.Running:
		return "yes"
	case status.State != "":
		return fmt.Sprintf("no (launchd state: %s)", status.State)
	default:
		return "no"
	}
}

func daemonYesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func daemonPresence(present bool) string {
	if present {
		return "present"
	}
	return "absent"
}

// lastTickTime reads the poll cursor's last completed tick; zero when none ever
// completed.
//
// The database is opened and closed at once: `daemon status` must not hold a
// connection while the daemon it is reporting on is writing.
func lastTickTime(ctx context.Context, paths config.Paths) (_ time.Time, err error) {
	if _, statErr := os.Stat(paths.DBPath()); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return time.Time{}, Preconditionf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())
		}
		return time.Time{}, fmt.Errorf("read %s: %w", paths.DBPath(), statErr)
	}

	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return time.Time{}, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	cursor, err := db.GetPollCursor(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return cursor.LastTickAt, nil
}

// daemonCredential describes where the launchd agent will get the bot token.
//
// It carries no credential material by design. The value of a materialised
// token_env goes straight into the agent's environment map and is held nowhere
// else, so there is no field here that could be printed or logged by accident.
type daemonCredential struct {
	// Description names the source in the terms it was configured in, or is
	// empty when no credential is configured at all.
	Description string
	// EnvName is the environment variable whose *value* was copied into the
	// plist, and is non-empty only on the unsupported token_env path. It is a
	// variable name, never a value.
	EnvName string
}

// resolveDaemonCredential works out how the agent will authenticate and, on
// the token_env path, copies the value into environment (D-D, AC-2.3).
//
// The two paths are deliberately asymmetric. A keychain reference needs
// nothing in the plist: the daemon resolves it once at its own startup and
// holds it, which is what keeps macOS from raising an allow-access dialog
// nobody can see, over and over. token_env has no such indirection — under
// launchd the daemon inherits no shell environment, so the only way it can see
// the variable is for the value to be written into the plist. That is the
// unsupported path, and the caller warns loudly about it.
func resolveDaemonCredential(cfg *config.Config, paths config.Paths, environment map[string]string) (daemonCredential, error) {
	ref, configured := secrets.ReferenceFrom(
		cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	if !configured {
		return daemonCredential{}, nil
	}
	if ref.Source != secrets.SourceEnv {
		return daemonCredential{Description: ref.String()}, nil
	}

	value, found := os.LookupEnv(ref.EnvVar)
	if !found || strings.TrimSpace(value) == "" {
		// Refusing here is not the same as refusing token_env. The variable is
		// the configured reference and it resolves to nothing, so installing
		// the agent would produce a daemon that cannot authenticate and no
		// terminal to say so in (S-5).
		return daemonCredential{}, Preconditionf(
			"github.token_env names $%s, but that variable is not set in this shell; a launchd agent inherits no shell environment, so `daemon start` has to copy the value into the plist\n"+
				"  export it and re-run, or point %s at github.token_keychain instead",
			ref.EnvVar, paths.ConfigPath())
	}
	environment[ref.EnvVar] = value
	return daemonCredential{Description: ref.String(), EnvName: ref.EnvVar}, nil
}

// writeStartReport prints what was installed and, above all, the PATH the
// daemon will actually search (AC-2.2). Reporting it is the point: a PATH that
// is materialised silently is one nobody checks until claude cannot be found.
func writeStartReport(out io.Writer, agent launchd.Agent, result launchd.InstallResult, credential daemonCredential) {
	verb := "loaded"
	if result.Replaced {
		verb = "reloaded"
	}
	fmt.Fprintf(out, "%s %s\n", verb, agent.Label)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  plist\t%s (mode %#o)\n", result.PlistPath, launchd.PlistMode.Perm())
	fmt.Fprintf(tw, "  domain\t%s\n", result.Domain)
	fmt.Fprintf(tw, "  program\t%s\n", strings.Join(agent.ProgramArguments, " "))
	fmt.Fprintf(tw, "  PATH\t%s\n", agent.Environment[launchd.PathEnvVar])
	fmt.Fprintf(tw, "  %s\t%s\n", config.EnvHome, agent.Environment[config.EnvHome])
	fmt.Fprintf(tw, "  stdout\t%s\n", agent.StandardOutPath())
	fmt.Fprintf(tw, "  stderr\t%s\n", agent.StandardErrorPath())
	fmt.Fprintf(tw, "  bot token\t%s\n", daemonCredentialLabel(credential))
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()

	fmt.Fprintf(out, "\nThe PATH above was copied from this shell. A launchd agent is otherwise given\n"+
		"only %s, which contains no Homebrew install — so the daemon\n"+
		"would not find claude or git there.\n", launchd.LaunchdPATH)
}

func daemonCredentialLabel(credential daemonCredential) string {
	if credential.Description == "" {
		return "none configured"
	}
	return credential.Description
}

// writeMaterialisedTokenWarning is AC-2.3: a loud warning naming the file and
// what it now contains.
//
// It does not refuse. The user asked for this, the daemon will work, and the
// only thing that helps is knowing exactly which file on disk now holds a live
// credential.
func writeMaterialisedTokenWarning(w io.Writer, credential daemonCredential, plistPath string) {
	fmt.Fprintf(w, "\nWARNING: %s now contains your bot token in plain text.\n", plistPath)
	fmt.Fprintf(w, "  github.token_env names $%s, and a launchd agent inherits no shell\n", credential.EnvName)
	fmt.Fprintf(w, "  environment, so `daemon start` had to copy that variable's value into the\n")
	fmt.Fprintf(w, "  plist. The file is mode %#o, but the token is now on disk — and in every\n", launchd.PlistMode.Perm())
	fmt.Fprintf(w, "  backup of your home directory — until you remove it.\n")
	fmt.Fprintf(w, "  The supported source for a launchd daemon is github.token_keychain, which\n")
	fmt.Fprintf(w, "  puts no credential in any file. Switch to it and re-run `litefactory daemon\n")
	fmt.Fprintf(w, "  start`; github.token_env is the development path, for a foreground\n")
	fmt.Fprintf(w, "  `litefactory daemon run`.\n")
}

// writeNoCredentialWarning fires when no bot token is configured at all. The
// agent is still installed — it is a valid thing to want before the credential
// exists — but the daemon will refuse to start until one does, and `daemon
// status` would otherwise just say "not running" with no reason.
func writeNoCredentialWarning(w io.Writer, paths config.Paths) {
	fmt.Fprintf(w, "\nwarning: no bot token is configured, so the daemon will exit at startup.\n")
	fmt.Fprintf(w, "  Set github.token_keychain.service and github.token_keychain.account in %s,\n", paths.ConfigPath())
	fmt.Fprintf(w, "  then re-run `litefactory daemon start`.\n")
}

// loadDaemonConfig reads config.yaml, mapping the two "this machine is not
// ready" failures onto exit 3.
//
// A literal-looking credential in config.yaml is one of the three hard checks
// litefactory performs inline instead of in a `doctor` command, and daemon
// start is where it lands.
func loadDaemonConfig(paths config.Paths) (*config.Config, error) {
	cfg, err := config.Load(paths)
	if err != nil {
		var notInitialized *config.NotInitializedError
		if errors.As(err, &notInitialized) || errors.Is(err, config.ErrLiteralSecret) {
			return nil, Precondition(err)
		}
		return nil, err
	}
	return cfg, nil
}

// daemonProgram is the command the LaunchAgent execs: this binary, followed by
// `daemon run`, followed by `--level <keyword>` when level is set (US-6,
// AC-6.3).
//
// The path is resolved through its symlinks because os.Executable may hand
// back the link the process was started through. A plist pointing at a
// Homebrew shim that is later re-pointed would silently start a different
// binary at the next login, which is not a failure anyone would think to look
// for.
//
// level is appended untouched: Agent.Validate's checkPlistValue already
// scans every ProgramArguments element for non-UTF-8 and control characters
// before it reaches the plist XML, and it was validated against
// logging.ParseLevel by the caller, so no second sanitisation pass belongs
// here (D-5).
func daemonProgram(level string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve this binary's path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolve the symlinks of %s: %w", exe, err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve %s to an absolute path: %w", resolved, err)
	}
	program := []string{abs, "daemon", "run"}
	if level != "" {
		program = append(program, "--level", level)
	}
	return program, nil
}
