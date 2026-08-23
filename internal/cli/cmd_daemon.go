package cli

import "flag"

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

// newDaemonStartCommand declares "litefactory daemon start".
//
// Behaviour lands in story LF-2 (T-LF2.7).
func newDaemonStartCommand() *Command {
	return &Command{
		Name:  "daemon start",
		Usage: "daemon start",
		Short: "Write the launchd agent and load it",
		New: func(*flag.FlagSet) Handler {
			return notImplemented("daemon start")
		},
	}
}

// newDaemonStopCommand declares "litefactory daemon stop".
//
// Behaviour lands in story LF-2 (T-LF2.7).
func newDaemonStopCommand() *Command {
	return &Command{
		Name:  "daemon stop",
		Usage: "daemon stop",
		Short: "Unload the launchd agent",
		New: func(*flag.FlagSet) Handler {
			return notImplemented("daemon stop")
		},
	}
}

// newDaemonStatusCommand declares "litefactory daemon status".
//
// Behaviour lands in story LF-2 (T-LF2.7).
func newDaemonStatusCommand() *Command {
	return &Command{
		Name:  "daemon status",
		Usage: "daemon status",
		Short: "Report whether the agent is loaded and running",
		New: func(*flag.FlagSet) Handler {
			return notImplemented("daemon status")
		},
	}
}
