package cli

import "flag"

// taskCleanOptions holds the parsed flags of "litefactory task clean".
type taskCleanOptions struct {
	// force removes the worktree even when it has uncommitted changes.
	force bool
}

// bind registers the task clean flags on fs.
func (o *taskCleanOptions) bind(fs *flag.FlagSet) {
	fs.BoolVar(&o.force, "force", false, "remove the worktree even with uncommitted changes")
}

// newTaskCleanCommand declares "litefactory task clean <issue> [--force]".
//
// Behaviour lands in story LF-3 (T-LF3.7): remove the task's worktree and
// clear its recorded path, refusing on uncommitted changes unless --force.
func newTaskCleanCommand() *Command {
	return &Command{
		Name:  "task clean",
		Usage: "task clean <issue> [--force]",
		Short: "Remove the worktree for an issue",
		Long: "Remove the git worktree created for <issue> and clear its recorded path.\n" +
			"Refuses to touch a worktree with uncommitted changes unless --force is given.",
		Args: ArgSpec{Names: []string{"issue"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			var opts taskCleanOptions
			opts.bind(fs)
			return notImplemented("task clean")
		},
	}
}
