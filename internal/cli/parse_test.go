package cli

import (
	"context"
	"errors"
	"flag"
	"slices"
	"strings"
	"testing"
)

func TestSplitPositionals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		max      int
		wantPos  []string
		wantRest []string
	}{
		{
			name:     "positional then flags",
			args:     []string{"123", "--run", "2"},
			max:      1,
			wantPos:  []string{"123"},
			wantRest: []string{"--run", "2"},
		},
		{
			name:     "flags only",
			args:     []string{"--run", "2"},
			max:      1,
			wantPos:  nil,
			wantRest: []string{"--run", "2"},
		},
		{
			name:     "no positionals accepted",
			args:     []string{"--json"},
			max:      0,
			wantPos:  nil,
			wantRest: []string{"--json"},
		},
		{
			name:     "stops at max even with more positionals",
			args:     []string{"123", "456"},
			max:      1,
			wantPos:  []string{"123"},
			wantRest: []string{"456"},
		},
		{
			name:     "end of flags marker is not a positional",
			args:     []string{"--", "-weird-path"},
			max:      1,
			wantPos:  nil,
			wantRest: []string{"--", "-weird-path"},
		},
		{
			name:     "bare dash is a positional",
			args:     []string{"-"},
			max:      1,
			wantPos:  []string{"-"},
			wantRest: []string{},
		},
		{
			name:     "empty",
			args:     nil,
			max:      2,
			wantPos:  nil,
			wantRest: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pos, rest := splitPositionals(tc.args, tc.max)
			if !slices.Equal(pos, tc.wantPos) {
				t.Errorf("positionals = %q, want %q", pos, tc.wantPos)
			}
			if !slices.Equal(rest, tc.wantRest) {
				t.Errorf("rest = %q, want %q", rest, tc.wantRest)
			}
		})
	}
}

// logsFixture mirrors the real logs command's argument shape and records what
// its flags parsed to.
type logsFixture struct {
	run    int
	raw    bool
	follow bool
	repo   string
	args   []string
	ran    bool
}

func (f *logsFixture) command() *Command {
	return &Command{
		Name:  "logs",
		Usage: "logs <issue> [--run N] [--raw] [-f] [--repo owner/name]",
		Short: "test double",
		Args:  ArgSpec{Names: []string{"issue"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			fs.IntVar(&f.run, "run", 0, "run ordinal")
			fs.BoolVar(&f.raw, "raw", false, "raw output")
			fs.BoolVar(&f.follow, "f", false, "follow")
			fs.StringVar(&f.repo, "repo", "", "owner/name")
			return func(_ context.Context, _ *Env, args []string) error {
				f.ran = true
				f.args = args
				return nil
			}
		},
	}
}

// TestParseArgsPositionalBeforeFlags is AC-B.4: Go's flag package stops at the
// first non-flag token, so the positional has to be sliced off before
// fs.Parse. If that ever regresses, --run silently keeps its default.
func TestParseArgsPositionalBeforeFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantIssue  string
		wantRun    int
		wantRaw    bool
		wantFollow bool
		wantRepo   string
	}{
		{
			name:      "positional then long flag with separate value",
			args:      []string{"123", "--run", "2"},
			wantIssue: "123",
			wantRun:   2,
		},
		{
			name:      "positional then long flag with equals",
			args:      []string{"123", "--run=2"},
			wantIssue: "123",
			wantRun:   2,
		},
		{
			name:      "positional then single dash flag",
			args:      []string{"123", "-run", "2"},
			wantIssue: "123",
			wantRun:   2,
		},
		{
			name:      "flags before positional",
			args:      []string{"--run", "2", "123"},
			wantIssue: "123",
			wantRun:   2,
		},
		{
			name:      "positional only",
			args:      []string{"123"},
			wantIssue: "123",
		},
		{
			name:       "all flags after positional",
			args:       []string{"123", "--run", "2", "--raw", "-f", "--repo", "acme/api"},
			wantIssue:  "123",
			wantRun:    2,
			wantRaw:    true,
			wantFollow: true,
			wantRepo:   "acme/api",
		},
		{
			name:      "bool flag then positional",
			args:      []string{"--raw", "123"},
			wantIssue: "123",
			wantRaw:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := &logsFixture{}
			handler, pos, err := parseArgs(fixture.command(), tc.args)
			if err != nil {
				t.Fatalf("parseArgs(%q) returned %v, want no error", tc.args, err)
			}
			if err := handler(t.Context(), &Env{}, pos); err != nil {
				t.Fatalf("handler returned %v", err)
			}
			if !fixture.ran {
				t.Fatal("handler did not run")
			}
			if got := strings.Join(fixture.args, " "); got != tc.wantIssue {
				t.Errorf("positionals = %q, want %q", got, tc.wantIssue)
			}
			if fixture.run != tc.wantRun {
				t.Errorf("--run = %d, want %d", fixture.run, tc.wantRun)
			}
			if fixture.raw != tc.wantRaw {
				t.Errorf("--raw = %t, want %t", fixture.raw, tc.wantRaw)
			}
			if fixture.follow != tc.wantFollow {
				t.Errorf("-f = %t, want %t", fixture.follow, tc.wantFollow)
			}
			if fixture.repo != tc.wantRepo {
				t.Errorf("--repo = %q, want %q", fixture.repo, tc.wantRepo)
			}
		})
	}
}

func TestParseArgsArity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing required positional", args: []string{"--raw"}, wantErr: "missing argument: <issue>"},
		{name: "too many positionals", args: []string{"123", "456"}, wantErr: `unexpected argument: "456"`},
		{name: "unknown flag", args: []string{"123", "--nope"}, wantErr: "flag provided but not defined"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := &logsFixture{}
			_, _, err := parseArgs(fixture.command(), tc.args)
			if err == nil {
				t.Fatalf("parseArgs(%q) succeeded, want a usage error", tc.args)
			}
			if got := ExitCode(err); got != ExitUsage {
				t.Errorf("exit code = %d, want %d", got, ExitUsage)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if fixture.ran {
				t.Error("handler ran despite a usage error")
			}
		})
	}
}

func TestParseArgsHelpRequest(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			fixture := &logsFixture{}
			_, _, err := parseArgs(fixture.command(), []string{arg})
			if !errors.Is(err, errHelpRequested) {
				t.Fatalf("parseArgs(%q) returned %v, want errHelpRequested", arg, err)
			}
			if fixture.ran {
				t.Error("handler ran for a help request")
			}
		})
	}
}
