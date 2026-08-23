package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
)

// tagline is the one-line description printed above the top-level usage.
const tagline = "litefactory watches GitHub issues and runs Claude Code in git worktrees."

// Router dispatches a command line to one of its commands.
type Router struct {
	name string
	env  *Env
	cmds []*Command
}

// NewRouter builds a router for the given commands. name is the program name
// used in usage text. Tests pass their own command slice; main passes
// [Commands].
func NewRouter(name string, env *Env, cmds []*Command) *Router {
	return &Router{name: name, env: env, cmds: cmds}
}

// Run parses args (the command line with the program name already stripped),
// dispatches it and returns the process exit code. All output goes to the
// writers in the router's [Env].
func (r *Router) Run(ctx context.Context, args []string) int {
	err := r.dispatch(ctx, args)
	if err == nil {
		return ExitOK
	}

	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		fmt.Fprintf(r.env.Stderr, "%s: %s\n\n", r.name, usageErr.Error())
		switch {
		case usageErr.Usage != "":
			fmt.Fprint(r.env.Stderr, usageErr.Usage)
		case usageErr.Cmd != nil:
			r.writeCommandUsage(r.env.Stderr, usageErr.Cmd)
		default:
			r.writeGlobalUsage(r.env.Stderr)
		}
		return ExitUsage
	}

	fmt.Fprintf(r.env.Stderr, "%s: %s\n", r.name, err.Error())
	return ExitCode(err)
}

// dispatch resolves and runs a single command line.
func (r *Router) dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return Usagef(nil, "no command given")
	}

	switch {
	case isVersionToken(args[0]):
		fmt.Fprintf(r.env.Stdout, "%s %s\n", r.name, r.env.Version)
		return nil
	case isHelpToken(args[0]):
		r.writeGlobalUsage(r.env.Stdout)
		return nil
	}

	cmd, rest := r.lookup(args)
	if cmd == nil {
		return r.unresolved(args)
	}

	handler, pos, err := parseArgs(cmd, rest)
	if err != nil {
		if errors.Is(err, errHelpRequested) {
			r.writeCommandUsage(r.env.Stdout, cmd)
			return nil
		}
		return err
	}
	return handler(ctx, r.env, pos)
}

// lookup finds the command whose name is the longest prefix of args and
// returns it with the remaining tokens.
func (r *Router) lookup(args []string) (*Command, []string) {
	var best *Command
	bestLen := 0
	for _, cmd := range r.cmds {
		words := cmd.words()
		if len(words) <= bestLen || len(words) > len(args) {
			continue
		}
		if slices.Equal(words, args[:len(words)]) {
			best, bestLen = cmd, len(words)
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, args[bestLen:]
}

// unresolved builds the error for a command line that matched no command. If
// the first token names a known command group ("repo", "daemon") the group's
// subcommands are listed instead of the whole tool's.
func (r *Router) unresolved(args []string) error {
	group := args[0]
	members := r.group(group)
	if len(members) == 0 {
		return Usagef(nil, "unknown command %q", strings.Join(args, " "))
	}
	if len(args) > 1 && isHelpToken(args[1]) {
		r.writeGroupUsage(r.env.Stdout, group, members)
		return nil
	}
	var b strings.Builder
	r.writeGroupUsage(&b, group, members)
	if len(args) == 1 {
		return &UsageError{Usage: b.String(), Err: fmt.Errorf("%q needs a subcommand", group)}
	}
	return &UsageError{Usage: b.String(), Err: fmt.Errorf("unknown subcommand %q", strings.Join(args, " "))}
}

// group returns the commands whose name starts with the given first word and
// has at least two words.
func (r *Router) group(word string) []*Command {
	var members []*Command
	for _, cmd := range r.cmds {
		if words := cmd.words(); len(words) > 1 && words[0] == word {
			members = append(members, cmd)
		}
	}
	return members
}

func (r *Router) writeGlobalUsage(w io.Writer) {
	fmt.Fprintf(w, "%s\n\nUsage:\n  %s <command> [arguments]\n\nCommands:\n", tagline, r.name)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cmd := range r.cmds {
		fmt.Fprintf(tw, "  %s\t%s\n", cmd.Usage, cmd.Short)
	}
	// A failed write of usage text is not actionable.
	_ = tw.Flush()
	fmt.Fprintf(w, "\nFlags:\n  --version\tprint the version and exit\n  --help\tprint this help and exit\n")
	fmt.Fprintf(w, "\nRun %q for a command's own flags.\n", r.name+" <command> --help")
}

func (r *Router) writeGroupUsage(w io.Writer, group string, members []*Command) {
	fmt.Fprintf(w, "Usage:\n  %s %s <subcommand> [arguments]\n\nSubcommands:\n", r.name, group)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cmd := range members {
		fmt.Fprintf(tw, "  %s\t%s\n", cmd.Usage, cmd.Short)
	}
	// A failed write of usage text is not actionable.
	_ = tw.Flush()
}

// writeCommandUsage prints one command's description, invocation form and its
// own flags — and nothing else. It rebuilds the flag set purely to enumerate
// the flags, which is why [Command.New] must have no effect beyond binding
// them.
func (r *Router) writeCommandUsage(w io.Writer, cmd *Command) {
	if cmd.Long != "" {
		fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(cmd.Long))
	} else if cmd.Short != "" {
		fmt.Fprintf(w, "%s\n\n", cmd.Short)
	}
	fmt.Fprintf(w, "Usage:\n  %s %s\n", r.name, cmd.Usage)

	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Usage = func() {}
	_ = cmd.New(fs)

	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	if n == 0 {
		return
	}
	fmt.Fprint(w, "\nFlags:\n")
	fs.PrintDefaults()
}

func isHelpToken(tok string) bool {
	switch tok {
	case "help", "-h", "-help", "--help":
		return true
	default:
		return false
	}
}

func isVersionToken(tok string) bool {
	switch tok {
	case "version", "-version", "--version":
		return true
	default:
		return false
	}
}
