package cli

import "flag"

// logsOptions holds the parsed flags of "litefactory logs".
type logsOptions struct {
	// run selects a single run by ordinal; 0 means list the runs.
	run int
	// raw writes the run's JSONL to stdout byte for byte.
	raw bool
	// follow streams events as they are appended.
	follow bool
	// repo disambiguates an issue number registered in more than one repo.
	repo string
}

// bind registers the logs flags on fs.
func (o *logsOptions) bind(fs *flag.FlagSet) {
	fs.IntVar(&o.run, "run", 0, "show only run `N` (default: list the runs)")
	fs.BoolVar(&o.raw, "raw", false, "write the run's JSONL to stdout byte for byte")
	fs.BoolVar(&o.follow, "f", false, "follow the newest run as events are written")
	fs.StringVar(&o.repo, "repo", "", "disambiguate the issue with `owner/name`")
}

// newLogsCommand declares "litefactory logs <issue> [--run N] [--raw] [-f]
// [--repo owner/name]".
//
// The positional issue number is sliced off before the flag set sees the
// arguments, so "logs 123 --run 2" parses; see splitPositionals.
//
// Behaviour lands in story LF-5 (T-LF5.1 to T-LF5.3).
func newLogsCommand() *Command {
	return &Command{
		Name:  "logs",
		Usage: "logs <issue> [--run N] [--raw] [-f] [--repo owner/name]",
		Short: "Show the local, unredacted log for an issue",
		Long: "Show what the forge comment left out. Reads the database and the run logs\n" +
			"under $LF_HOME directly, so it works with the daemon stopped. Output is\n" +
			"never redacted.",
		Args: ArgSpec{Names: []string{"issue"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			var opts logsOptions
			opts.bind(fs)
			return notImplemented("logs")
		},
	}
}
