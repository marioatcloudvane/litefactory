package cli

import (
	"errors"
	"flag"
	"io"
	"strings"
)

// splitWords splits a space-separated command name into its segments.
func splitWords(name string) []string {
	return strings.Fields(name)
}

// isFlagToken reports whether tok looks like a flag rather than a positional
// argument. A bare "-" is a positional (it conventionally means stdin) and a
// bare "--" is handled separately as the end-of-flags marker.
func isFlagToken(tok string) bool {
	return len(tok) > 1 && tok[0] == '-' && tok != "--"
}

// splitPositionals peels up to limit leading positional arguments off args and
// returns them together with the remaining tokens.
//
// This is the reason litefactory does not call fs.Parse(args) directly: Go's
// flag package stops parsing at the first non-flag token, so parsing
// "logs 123 --run 2" wholesale would leave --run unparsed and sitting in
// fs.Args(). Slicing the positionals off the front first makes the natural
// "verb subject --options" order work.
func splitPositionals(args []string, limit int) (pos, rest []string) {
	i := 0
	for i < len(args) && len(pos) < limit {
		tok := args[i]
		if tok == "--" || isFlagToken(tok) {
			break
		}
		pos = append(pos, tok)
		i++
	}
	return pos, args[i:]
}

// errHelpRequested is returned by parseArgs when the user asked for a
// command's help with -h or --help.
var errHelpRequested = errors.New("help requested")

// parseArgs builds the command's flag set, binds its flags, parses args and
// validates the positional arity.
//
// Positionals may appear before the flags (the common form) or after them;
// both "logs 123 --run 2" and "logs --run 2 123" parse identically.
func parseArgs(cmd *Command, args []string) (Handler, []string, error) {
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	// Take full control of error and help reporting: the flag package would
	// otherwise write its own message to stderr and print the whole usage.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	handler := cmd.New(fs)

	pos, rest := splitPositionals(args, len(cmd.Args.Names))
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil, errHelpRequested
		}
		return nil, nil, &UsageError{Cmd: cmd, Err: err}
	}
	pos = append(pos, fs.Args()...)

	if len(pos) < cmd.Args.Min {
		missing := cmd.Args.Names[len(pos):cmd.Args.Min]
		return nil, nil, Usagef(cmd, "missing argument: <%s>", strings.Join(missing, "> <"))
	}
	if len(pos) > len(cmd.Args.Names) {
		return nil, nil, Usagef(cmd, "unexpected argument: %q", pos[len(cmd.Args.Names)])
	}
	return handler, pos, nil
}
