// Package cli holds litefactory's command surface: the dispatch table, the
// argument parser and the usage text.
//
// # Adding a command
//
// A command is one [Command] value returned by a constructor in its own
// cmd_<group>.go file, plus one line in [Commands]. Nothing else. There is no
// switch statement to update and no registry to mutate.
//
//	func newRepoListCommand() *Command {
//		return &Command{
//			Name:  "repo list",
//			Usage: "repo list [--json]",
//			Short: "List the registered repositories",
//			New: func(fs *flag.FlagSet) Handler {
//				asJSON := fs.Bool("json", false, "emit JSON instead of a table")
//				return func(ctx context.Context, env *Env, args []string) error {
//					return runRepoList(ctx, env, *asJSON)
//				}
//			},
//		}
//	}
//
// New binds the command's flags to fs and returns the closure that runs it, so
// flag values are captured by the closure instead of being read back out of the
// flag set. Handlers receive only the positional arguments, already checked
// against [Command.Args].
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
)

// Handler runs a command. args holds the positional arguments, validated
// against the command's [ArgSpec]. ctx is the process-wide signal context
// created in main; it is the only cancellation source in the program.
//
// A handler returns nil on success, a [UsageError] when the command line was
// wrong, a [PreconditionError] when the machine is not ready, and a plain
// error otherwise.
type Handler func(ctx context.Context, env *Env, args []string) error

// Env carries the process-wide dependencies a command may use. It is created
// once in main and passed to every handler; commands must not reach for
// globals instead.
//
// Fields may be added as later stories need them (config, store, clock).
// Adding a field is not a breaking change for existing commands.
type Env struct {
	// Stdout is where command *results* go. Printing results with
	// fmt.Fprintf(env.Stdout, ...) is output, not logging, and is the one
	// sanctioned exception to the slog-only rule.
	Stdout io.Writer
	// Stderr is where diagnostics and usage text go.
	Stderr io.Writer
	// Stdin is the process input stream.
	Stdin io.Reader
	// Logger is the structured logger. Never nil.
	Logger *slog.Logger
	// Version is the build version reported by --version.
	Version string
}

// ArgSpec declares a command's positional arguments.
//
// Names are used both for the usage text and for arity checking: at most
// len(Names) positionals are accepted, and at least Min are required. The
// trailing len(Names)-Min arguments are optional.
type ArgSpec struct {
	Names []string
	Min   int
}

// Command is one invocable command.
type Command struct {
	// Name is the space-separated command path, for example "repo add".
	// Matching prefers the longest name, so "repo add" wins over a
	// hypothetical "repo".
	Name string
	// Usage is the one-line invocation form shown in help, without the
	// program name, for example "logs <issue> [--run N] [--raw]".
	Usage string
	// Short is the one-line description listed in the top-level usage.
	Short string
	// Long is an optional paragraph printed by "<command> --help".
	Long string
	// Args declares the positional arguments.
	Args ArgSpec
	// New binds this command's flags to fs and returns its handler. It is
	// called once per invocation, before parsing, so each invocation gets
	// fresh flag storage. A command with no flags ignores fs.
	New func(fs *flag.FlagSet) Handler
}

// words splits the command name into its path segments.
func (c *Command) words() []string { return splitWords(c.Name) }

// notImplemented returns a handler for a command that is declared but not yet
// built. It keeps the dispatch table and usage text honest while the
// behaviour lands in a later story.
func notImplemented(name string) Handler {
	return func(context.Context, *Env, []string) error {
		return fmt.Errorf("%s: %w", name, ErrNotImplemented)
	}
}
