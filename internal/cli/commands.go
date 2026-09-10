package cli

// Commands returns litefactory's full command surface, in the order it is
// listed in the top-level usage.
//
// This slice is the single registration point: adding a command means writing
// its constructor in a cmd_<group>.go file and adding one line here.
func Commands() []*Command {
	return []*Command{
		newInitCommand(),
		newSubmissionCommand("preflight"),
		newSubmissionCommand("submit"),
		newSubmissionCommand("receipt"),
		newRepoAddCommand(),
		newRepoListCommand(),
		newDaemonRunCommand(),
		newDaemonStartCommand(),
		newDaemonStopCommand(),
		newDaemonStatusCommand(),
		newLogsCommand(),
		newMeterCommand(),
		newMeterExportCommand(),
		newMeterPricesCommand(),
		newMeterPricesListCommand(),
		newMeterPricesSetCommand(),
		newTaskCleanCommand(),
		newStoryContractUpgradeCommand(),
		newTeamListCommand(),
		newTeamShowCommand(),
		newTeamAddCommand(),
		newTeamStatusCommand(),
		newTeamUpdateCommand(),
		newTeamManageCommand(),
		newTeamValidateCommand(),
	}
}
