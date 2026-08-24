package cli

import (
	"context"
	"flag"

	"github.com/cloudvane/litefactory/internal/launchd"
)

// newDaemonRunCommand declares "litefactory daemon run", the foreground poll
// loop that launchd executes.
//
// Behaviour lands in story LF-2 (T-LF2.6).
func newDaemonRunCommand() *Command {
	return &Command{
		Name:  "daemon run",
		Usage: "daemon run",
		Short: "Run the poll loop in the foreground until interrupted",
		Long: "Run the poll loop in this process, in the foreground, until SIGINT or\n" +
			"SIGTERM. This is what the launchd agent executes; run it directly to watch\n" +
			"the loop work.",
		New: func(*flag.FlagSet) Handler {
			return notImplemented("daemon run")
		},
	}
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
