package cli

import "flag"

// newRepoAddCommand declares "litefactory repo add <path>".
//
// Behaviour lands in story LF-1 (T-LF1.4): register the clone at path,
// idempotently upserting its mutable fields.
func newRepoAddCommand() *Command {
	return &Command{
		Name:  "repo add",
		Usage: "repo add <path>",
		Short: "Register a local clone with litefactory",
		Long: "Register the git clone at <path>. The owner, name and default branch are\n" +
			"read from the clone itself; nothing is fetched over the network. Running it\n" +
			"again on the same path updates the registration instead of duplicating it.",
		Args: ArgSpec{Names: []string{"path"}, Min: 1},
		New: func(*flag.FlagSet) Handler {
			return notImplemented("repo add")
		},
	}
}

// newRepoListCommand declares "litefactory repo list".
//
// Behaviour lands in story LF-1 (T-LF1.4).
func newRepoListCommand() *Command {
	return &Command{
		Name:  "repo list",
		Usage: "repo list",
		Short: "List the registered repositories",
		New: func(*flag.FlagSet) Handler {
			return notImplemented("repo list")
		},
	}
}
