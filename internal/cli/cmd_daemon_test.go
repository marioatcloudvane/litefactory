package cli

import (
	"strings"
	"testing"
)

// TestLogLevelOverride covers LogLevelOverride's hand-rolled argv scan (US-6,
// DEV-1): the various --level/-level spellings it must recognise ahead of the
// router's own flag parsing, and the errors it must surface before the logger
// is built.
func TestLogLevelOverride(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantFound bool
		wantErr   string
	}{
		{
			name:      "no level flag/reports absent",
			args:      []string{"daemon", "run"},
			wantFound: false,
		},
		{
			name:      "double dash separate arg/found",
			args:      []string{"daemon", "run", "--level", "debug"},
			wantFound: true,
		},
		{
			name:      "single dash separate arg/found",
			args:      []string{"daemon", "run", "-level", "debug"},
			wantFound: true,
		},
		{
			name:      "double dash equals form/found",
			args:      []string{"daemon", "run", "--level=debug"},
			wantFound: true,
		},
		{
			name:      "single dash equals form/found",
			args:      []string{"daemon", "run", "-level=debug"},
			wantFound: true,
		},
		{
			name:      "repeated flag/last one wins",
			args:      []string{"daemon", "run", "--level", "debug", "--level", "warn"},
			wantFound: true,
		},
		{
			name:    "trailing flag with no value/errors",
			args:    []string{"daemon", "run", "--level"},
			wantErr: "flag needs an argument",
		},
		{
			name:    "invalid level value/errors naming the flag",
			args:    []string{"daemon", "run", "--level", "loud"},
			wantErr: "invalid --level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, found, err := LogLevelOverride(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("LogLevelOverride(%v): got nil error, want one containing %q", tt.args, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("LogLevelOverride(%v): error = %q, want it to contain %q", tt.args, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LogLevelOverride(%v): unexpected error: %v", tt.args, err)
			}
			if found != tt.wantFound {
				t.Errorf("LogLevelOverride(%v): found = %v, want %v", tt.args, found, tt.wantFound)
			}
		})
	}
}

// TestLogLevelOverride_ResolvesTheParsedLevel checks the returned slog.Level
// actually reflects the requested keyword, not just that something was found.
func TestLogLevelOverride_ResolvesTheParsedLevel(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // via level.String(), to avoid hard-coding slog's numeric constants
	}{
		{name: "debug", args: []string{"--level", "debug"}, want: "DEBUG"},
		{name: "warn", args: []string{"--level=warn"}, want: "WARN"},
		{name: "case insensitive", args: []string{"--level", "ERROR"}, want: "ERROR"},
		{name: "last of repeated wins", args: []string{"--level", "debug", "--level", "error"}, want: "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			level, found, err := LogLevelOverride(tt.args)

			if err != nil {
				t.Fatalf("LogLevelOverride(%v): unexpected error: %v", tt.args, err)
			}
			if !found {
				t.Fatalf("LogLevelOverride(%v): found = false, want true", tt.args)
			}
			if got := level.String(); got != tt.want {
				t.Errorf("LogLevelOverride(%v): level = %s, want %s", tt.args, got, tt.want)
			}
		})
	}
}

// TestDaemonProgram is US-6, AC-6.3's plist-content contract at the unit
// level: the ProgramArguments end with `daemon run` when no level is
// requested, and with `daemon run --level <level>` when one is.
func TestDaemonProgram(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  []string
	}{
		{
			name:  "no level requested/no --level appended",
			level: "",
			want:  []string{"daemon", "run"},
		},
		{
			name:  "level requested/level appended untouched",
			level: "debug",
			want:  []string{"daemon", "run", "--level", "debug"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			program, err := daemonProgram(tt.level)

			if err != nil {
				t.Fatalf("daemonProgram(%q): unexpected error: %v", tt.level, err)
			}
			if len(program) < len(tt.want) {
				t.Fatalf("daemonProgram(%q) = %v, too short to end with %v", tt.level, program, tt.want)
			}
			got := program[len(program)-len(tt.want):]
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("daemonProgram(%q) ends with %v, want %v", tt.level, got, tt.want)
			}
		})
	}
}
