package cli

import "flag"

// newInitCommand declares "litefactory init".
//
// Behaviour lands in story LF-0 (T-LF0.3): create $LF_HOME, config.yaml and
// the database, then derive the owner and bot identities.
func newInitCommand() *Command {
	return &Command{
		Name:  "init",
		Usage: "init",
		Short: "Create $LF_HOME, the config file and the database, and derive identities",
		Long: "Bootstrap litefactory on this machine: create $LF_HOME, write a default\n" +
			"config.yaml, create the database, and derive the owner and bot identities\n" +
			"from the git tooling already logged in here.",
		New: func(*flag.FlagSet) Handler {
			return notImplemented("init")
		},
	}
}
