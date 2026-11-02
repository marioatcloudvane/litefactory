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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/localadmin"
	"github.com/cloudvane/litefactory/internal/launchd"
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
	result, err := localadmin.StartAgent(ctx, localadmin.AgentOptions{Level: opts.level, Runner: runner, Logger: env.Logger})
	if err != nil {
		return localAdminError(err)
	}
	writeStartReport(env.Stdout, result)
	if result.Credential.EnvName != "" {
		writeMaterialisedTokenWarning(env.Stderr, result.Credential, result.PlistPath)
	}
	if result.Credential.Description == "" {
		writeNoCredentialWarning(env.Stderr, config.NewPaths(result.Home))
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
	result, err := localadmin.StopAgent(ctx, localadmin.AgentOptions{Runner: runner, Logger: env.Logger})
	if err != nil {
		return err
	}
	if !result.Unloaded {
		fmt.Fprintf(env.Stdout, "%s was not loaded in %s\n", result.Target, result.Domain)
	} else {
		fmt.Fprintf(env.Stdout, "unloaded %s\n", result.Target)
	}
	fmt.Fprintf(env.Stdout, "  the agent is still installed at %s; `litefactory daemon start` loads it again\n", result.PlistPath)
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
	status, err := localadmin.AgentStatus(ctx, localadmin.AgentOptions{Runner: runner, Logger: env.Logger})
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
	if err := printDaemonHeartbeat(env.Stdout, health.HealthSnapshot, lastCompleted, status.Running, now()); err != nil {
		return err
	}
	if err := printDaemonHealth(env.Stdout, health.HealthSnapshot, status.Running, now()); err != nil {
		return err
	}
	for _, gap := range health.missingModeMappings {
		if _, err := fmt.Fprintln(env.Stdout, gap); err != nil {
			return err
		}
	}
	return printDoctorConfiguration(env.Stdout, health.unsupportedDoctor)
}

// daemonRunning renders the running column, including launchd's own word for
// the state when there is no process. "no" alone loses the difference between
// a daemon that exited and one that launchd is holding back.
func daemonRunning(status contracts.AgentStatusView) string {
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
func lastTickTime(ctx context.Context, paths config.Paths) (time.Time, error) {
	at, err := localadmin.LastTickTime(ctx, paths)
	return at, localAdminError(err)
}

type daemonCredential = contracts.AgentCredentialView

// writeStartReport prints what was installed and, above all, the PATH the
// daemon will actually search (AC-2.2). Reporting it is the point: a PATH that
// is materialised silently is one nobody checks until claude cannot be found.
func writeStartReport(out io.Writer, result contracts.AgentStartView) {
	verb := "loaded"
	if result.Replaced {
		verb = "reloaded"
	}
	fmt.Fprintf(out, "%s %s\n", verb, result.Label)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  plist\t%s (mode %#o)\n", result.PlistPath, launchd.PlistMode.Perm())
	fmt.Fprintf(tw, "  domain\t%s\n", result.Domain)
	fmt.Fprintf(tw, "  program\t%s\n", strings.Join(result.ProgramArguments, " "))
	fmt.Fprintf(tw, "  PATH\t%s\n", result.Path)
	fmt.Fprintf(tw, "  %s\t%s\n", config.EnvHome, result.Home)
	fmt.Fprintf(tw, "  stdout\t%s\n", result.StdoutPath)
	fmt.Fprintf(tw, "  stderr\t%s\n", result.StderrPath)
	fmt.Fprintf(tw, "  bot token\t%s\n", daemonCredentialLabel(result.Credential))
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
	cfg, err := localadmin.LoadConfig(paths)
	return cfg, localAdminError(err)
}

// daemonProgram retains the host helper while local administration owns the
// resolved foreground invocation used by installation.
func daemonProgram(level string) ([]string, error) { return localadmin.AgentProgram(level) }

func localAdminError(err error) error {
	var unavailable *contracts.LocalAdminUnavailable
	if errors.As(err, &unavailable) {
		return Precondition(unavailable.Err)
	}
	return err
}
