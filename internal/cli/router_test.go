package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// newTestRouter builds a router over the real command surface with buffered
// output.
func newTestRouter(t *testing.T) (*Router, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	env := &Env{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Stdin:   strings.NewReader(""),
		Logger:  slog.New(slog.DiscardHandler),
		Version: "1.2.3-test",
	}
	return NewRouter("litefactory", env, Commands()), &stdout, &stderr
}

// TestRouterExitCodes covers AC-B.2 (no arguments prints usage and exits 2;
// --version prints a version and exits 0) and AC-B.3 (an unknown subcommand
// exits 2 with usage).
func TestRouterExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           []string
		wantCode       int
		wantStdoutHas  []string
		wantStderrHas  []string
		wantStdoutOnly bool // output must be on stdout, nothing on stderr
	}{
		{
			name:          "no arguments prints usage and exits 2",
			args:          nil,
			wantCode:      ExitUsage,
			wantStderrHas: []string{"no command given", "Usage:", "litefactory <command>", "repo add <path>"},
		},
		{
			name:           "version exits 0",
			args:           []string{"--version"},
			wantCode:       ExitOK,
			wantStdoutHas:  []string{"litefactory 1.2.3-test"},
			wantStdoutOnly: true,
		},
		{
			name:           "single dash version exits 0",
			args:           []string{"-version"},
			wantCode:       ExitOK,
			wantStdoutHas:  []string{"1.2.3-test"},
			wantStdoutOnly: true,
		},
		{
			name:           "help exits 0 and lists every command",
			args:           []string{"--help"},
			wantCode:       ExitOK,
			wantStdoutHas:  []string{"init", "repo add <path>", "repo list", "daemon run", "daemon start", "daemon stop", "daemon status", "logs [<issue>]", "task clean <issue>"},
			wantStdoutOnly: true,
		},
		{
			name:          "unknown command exits 2 with usage",
			args:          []string{"frobnicate"},
			wantCode:      ExitUsage,
			wantStderrHas: []string{`unknown command "frobnicate"`, "Usage:", "Commands:"},
		},
		{
			name:          "unknown subcommand of a known group exits 2 with the group usage",
			args:          []string{"repo", "frobnicate"},
			wantCode:      ExitUsage,
			wantStderrHas: []string{`unknown subcommand "repo frobnicate"`, "repo add <path>", "repo list"},
		},
		{
			name:          "group without a subcommand exits 2",
			args:          []string{"daemon"},
			wantCode:      ExitUsage,
			wantStderrHas: []string{`"daemon" needs a subcommand`, "daemon run", "daemon status"},
		},
		{
			// logs used to be this case's subject: bare "litefactory logs"
			// was a missing-positional usage error. It no longer is — bare
			// logs now shows litefactory's own log (US-1) — so the arity
			// check is exercised here against another command that still
			// requires its positional, task clean, instead.
			name:          "missing required positional exits 2",
			args:          []string{"task", "clean"},
			wantCode:      ExitUsage,
			wantStderrHas: []string{"missing argument: <issue>", "task clean <issue>"},
		},
		{
			name:          "unknown flag exits 2",
			args:          []string{"repo", "list", "--nope"},
			wantCode:      ExitUsage,
			wantStderrHas: []string{"flag provided but not defined", "repo list"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			router, stdout, stderr := newTestRouter(t)
			code := router.Run(t.Context(), tc.args)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, tc.wantCode, stdout, stderr)
			}
			for _, want := range tc.wantStdoutHas {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q\ngot: %s", want, stdout)
				}
			}
			for _, want := range tc.wantStderrHas {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q\ngot: %s", want, stderr)
				}
			}
			if tc.wantStdoutOnly && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty", stderr)
			}
		})
	}
}

// TestCommandHelpShowsOnlyItsOwnFlags is the second half of AC-B.3.
func TestCommandHelpShowsOnlyItsOwnFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantFlags   []string
		unwantFlags []string
	}{
		{
			name:        "logs",
			args:        []string{"logs", "--help"},
			wantFlags:   []string{"-run", "-raw", "-f", "-repo"},
			unwantFlags: []string{"-force", "repo add", "daemon run", "Commands:"},
		},
		{
			name:        "task clean",
			args:        []string{"task", "clean", "--help"},
			wantFlags:   []string{"-force"},
			unwantFlags: []string{"-run", "-raw", "-repo", "Commands:"},
		},
		{
			name:        "flagless command shows no flag section",
			args:        []string{"repo", "list", "-h"},
			wantFlags:   []string{"litefactory repo list"},
			unwantFlags: []string{"Flags:", "-force", "-run"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			router, stdout, stderr := newTestRouter(t)
			if code := router.Run(t.Context(), tc.args); code != ExitOK {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty: help goes to stdout", stderr)
			}
			out := stdout.String()
			if !strings.Contains(out, "Usage:") {
				t.Errorf("help output has no usage line:\n%s", out)
			}
			for _, want := range tc.wantFlags {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q\ngot:\n%s", want, out)
				}
			}
			for _, unwanted := range tc.unwantFlags {
				if strings.Contains(out, unwanted) {
					t.Errorf("help mentions %q, which belongs to another command\ngot:\n%s", unwanted, out)
				}
			}
		})
	}
}

// TestLogsCommandReachesItsHandler is AC-B.4 against the registered command
// rather than a test double: if the positional swallowed the flags, this
// would fail with a usage error instead of reaching the handler.
//
// The assertion is the absence of a UsageError, not the presence of one
// particular error. A sentinel check held only while logs was a stub — that
// was the stub's behaviour, not the parser's contract. What
// the handler goes on to do belongs to LF-5's own tests; that the flag value
// really arrives as 2 is covered end to end by
// TestLogsRunFlagSelectsTheRunAfterThePositional.
//
// LF_HOME is pointed at an empty directory so the reached handler fails on
// the missing database instead of reading the developer's real ~/.litefactory.
func TestLogsCommandReachesItsHandler(t *testing.T) {
	// No t.Parallel: t.Setenv forbids it.
	t.Setenv(config.EnvHome, t.TempDir())

	router, _, _ := newTestRouter(t)
	err := router.dispatch(t.Context(), []string{"logs", "123", "--run", "2"})
	if err == nil {
		t.Fatal("dispatch succeeded against an empty LF_HOME, so it cannot have reached the handler")
	}
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		t.Fatalf("dispatch returned the usage error %q: the positional swallowed --run instead of the handler being reached", err)
	}
}

// TestCommandsAreWellFormed guards the dispatch table itself: every command
// another engineer adds to Commands must be usable.
func TestCommandsAreWellFormed(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, cmd := range Commands() {
		if cmd.Name == "" {
			t.Fatal("a command has an empty Name")
		}
		if seen[cmd.Name] {
			t.Errorf("command %q is registered twice", cmd.Name)
		}
		seen[cmd.Name] = true

		if cmd.New == nil {
			t.Errorf("command %q has no New", cmd.Name)
			continue
		}
		if cmd.Short == "" {
			t.Errorf("command %q has no Short description for the usage list", cmd.Name)
		}
		if !strings.HasPrefix(cmd.Usage, cmd.Name) {
			t.Errorf("command %q has usage %q, which does not start with the command name", cmd.Name, cmd.Usage)
		}
		if cmd.Args.Min > len(cmd.Args.Names) {
			t.Errorf("command %q requires %d positionals but names only %d", cmd.Name, cmd.Args.Min, len(cmd.Args.Names))
		}
		for _, name := range cmd.Args.Names {
			if !strings.Contains(cmd.Usage, "<"+name+">") {
				t.Errorf("command %q has positional %q that its usage %q does not mention", cmd.Name, name, cmd.Usage)
			}
		}
	}
}

// TestRouterReportsAnUnimplementedCommand covers the stub path: a command that
// is declared in the dispatch table but whose behaviour has not landed exits 1
// and says so, rather than exiting 0 having done nothing.
//
// The command is synthetic rather than one of the real ones. It used to point
// at whichever command was still a stub — `repo list`, then `task clean` — and
// moving it every time a story landed made an unrelated test fail in the middle
// of someone else's work.
func TestRouterReportsAnUnimplementedCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	env := &Env{
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
		Logger: slog.New(slog.DiscardHandler),
	}
	cmd := &Command{
		Name:  "frobnicate",
		Usage: "frobnicate",
		Short: "Not built yet",
		New:   func(*flag.FlagSet) Handler { return failingHandler("frobnicate") },
	}

	code := NewRouter("litefactory", env, []*Command{cmd}).Run(t.Context(), []string{"frobnicate"})
	if code != ExitFailure {
		t.Errorf("exit code = %d, want %d\nstderr: %s", code, ExitFailure, &stderr)
	}
	if !strings.Contains(stderr.String(), "frobnicate: not implemented yet") {
		t.Errorf("stderr = %q, want it to name the unimplemented command", &stderr)
	}
}

// TestRouterLookupPrefersTheLongestName makes sure a one-word command could
// never shadow a two-word one registered before it.
func TestRouterLookupPrefersTheLongestName(t *testing.T) {
	t.Parallel()

	stub := func(name string) *Command {
		return &Command{
			Name:  name,
			Usage: name,
			Short: name,
			New:   func(*flag.FlagSet) Handler { return failingHandler(name) },
		}
	}
	env := &Env{Stdout: io.Discard, Stderr: io.Discard, Logger: slog.New(slog.DiscardHandler)}
	router := NewRouter("litefactory", env, []*Command{stub("repo"), stub("repo add")})

	tests := []struct {
		args     []string
		wantName string
		wantRest []string
	}{
		{args: []string{"repo", "add", "/tmp/x"}, wantName: "repo add", wantRest: []string{"/tmp/x"}},
		{args: []string{"repo", "list"}, wantName: "repo", wantRest: []string{"list"}},
	}
	for _, tc := range tests {
		cmd, rest := router.lookup(tc.args)
		if cmd == nil {
			t.Fatalf("lookup(%q) found nothing", tc.args)
		}
		if cmd.Name != tc.wantName {
			t.Errorf("lookup(%q) = %q, want %q", tc.args, cmd.Name, tc.wantName)
		}
		if !slices.Equal(rest, tc.wantRest) {
			t.Errorf("lookup(%q) rest = %q, want %q", tc.args, rest, tc.wantRest)
		}
	}
}

// failingHandler returns a handler that always fails with a plain error naming
// the command, for tests of the router's failure path.
func failingHandler(name string) Handler {
	return func(context.Context, *Env, []string) error {
		return fmt.Errorf("%s: not implemented yet", name)
	}
}
