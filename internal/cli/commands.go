package cli

// Commands returns litefactory's full command surface, in the order it is
// listed in the top-level usage.
//
// This slice is the single registration point: adding a command means writing
// its constructor in a cmd_<group>.go file and adding one line here.
func Commands() []*Command {
	return []*Command{
		newInitCommand(),
		newRepoAddCommand(),
		newRepoListCommand(),
		newDaemonRunCommand(),
		newDaemonStartCommand(),
		newDaemonStopCommand(),
		newDaemonStatusCommand(),
		newLogsCommand(),
		newTaskCleanCommand(),
	}
}
