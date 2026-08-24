package launchd_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cloudvane/litefactory/internal/launchd"
)

// goldenAgent is the agent `daemon start` writes on a machine with a Homebrew
// PATH. testdata/agent.plist is its rendering, byte for byte.
func goldenAgent() launchd.Agent {
	return launchd.Agent{
		Label:            launchd.DefaultLabel,
		ProgramArguments: []string{"/usr/local/bin/litefactory", "daemon", "run"},
		LogsDir:          "/Users/mario/.litefactory/logs",
		Environment: map[string]string{
			"PATH":    "/opt/homebrew/bin:/usr/bin:/bin",
			"LF_HOME": "/Users/mario/.litefactory",
		},
	}
}

func TestRenderMatchesTheGoldenPlist(t *testing.T) {
	t.Parallel()

	got, err := goldenAgent().Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "agent.plist"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Errorf("rendered plist differs from testdata/agent.plist (-want +got):\n%s", diff)
	}
}

// TestRenderIsDeterministic guards the one thing a map in the Agent could
// break: re-running `daemon start` with an unchanged configuration must
// produce an unchanged file, not a file whose environment keys shuffle.
func TestRenderIsDeterministic(t *testing.T) {
	t.Parallel()

	agent := goldenAgent()
	agent.Environment = map[string]string{
		"PATH": "/usr/bin", "ZZZ": "z", "AAA": "a", "LF_HOME": "/home", "MMM": "m",
	}
	first, err := agent.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for i := range 20 {
		again, err := agent.Render()
		if err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("render %d differs from the first render:\n%s", i, cmp.Diff(string(first), string(again)))
		}
	}
}

// TestRenderPolicy pins the four values that are litefactory's decision rather
// than the caller's. Each one is load-bearing and each is one careless edit
// away from being wrong in a way no other test would notice.
func TestRenderPolicy(t *testing.T) {
	t.Parallel()

	plist, err := goldenAgent().Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := string(plist)

	for _, want := range []struct {
		what    string
		snippet string
		why     string
	}{
		{"RunAtLoad", "<key>RunAtLoad</key>\n\t<true/>", "bootstrap alone starts nothing; `daemon start` must actually start the daemon"},
		{"KeepAlive", "<key>KeepAlive</key>\n\t<false/>", "a respawn loop on a fatal misconfiguration is the invisible keychain prompt storm D-D warns about"},
		{"ExitTimeOut", "<key>ExitTimeOut</key>\n\t<integer>20</integer>", "D-B's 3-second exit is a promise made against this grace period"},
		{"WorkingDirectory", "<key>WorkingDirectory</key>\n\t<string>/</string>", "nothing the daemon does may depend on where `daemon start` was run"},
	} {
		if !strings.Contains(got, want.snippet) {
			t.Errorf("%s: plist does not contain %q — %s\n%s", want.what, want.snippet, want.why, got)
		}
	}
}

// TestRenderEscapesHostileValues is the reason this package uses encoding/xml's
// escaper instead of string concatenation. A home directory really can contain
// an ampersand, and a plist that silently loses one is a daemon pointed at the
// wrong path.
func TestRenderEscapesHostileValues(t *testing.T) {
	t.Parallel()

	const (
		hostileDir  = `/Users/a & b/<logs>/"quoted"/'single'`
		hostilePath = `/opt/a&b/bin:/opt/<x>/bin:/usr/bin`
		hostileArg  = `--note=a & b < c > d "e" 'f'`
		hostileHome = `/Users/a & b/.litefactory`
	)

	agent := launchd.Agent{
		Label:            launchd.DefaultLabel,
		ProgramArguments: []string{`/Users/a & b/bin/litefactory`, "daemon", "run", hostileArg},
		LogsDir:          hostileDir,
		Environment:      map[string]string{"PATH": hostilePath, "LF_HOME": hostileHome},
	}
	plist, err := agent.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// A raw < from a value inside character data is exactly the corruption
	// this guards against: it would open an element that never closes.
	for _, corruption := range []string{"<logs>", "<x>", "a & b < c"} {
		if strings.Contains(string(plist), corruption) {
			t.Errorf("plist contains the unescaped value %q:\n%s", corruption, plist)
		}
	}
	if !strings.Contains(string(plist), "&amp;") {
		t.Errorf("plist contains no escaped ampersand, so nothing was escaped:\n%s", plist)
	}

	// The real assertion: a strict XML parser reads back every value exactly
	// as it went in.
	strs := plistStrings(t, plist)
	for _, want := range []string{
		`/Users/a & b/bin/litefactory`,
		hostileArg,
		hostilePath,
		hostileHome,
		hostileDir + "/" + launchd.StdoutLogName,
		hostileDir + "/" + launchd.StderrLogName,
	} {
		if !slices.Contains(strs, want) {
			t.Errorf("value %q did not survive the round trip; decoded strings were %q", want, strs)
		}
	}
}

// TestRenderRejectsControlCharacters covers the values XML cannot carry at all.
// Escaping them is not an option — encoding/xml would substitute a replacement
// rune and hand back a plist that parses and means something else.
func TestRenderRejectsControlCharacters(t *testing.T) {
	t.Parallel()

	agent := goldenAgent()
	agent.LogsDir = "/Users/mario/\x00logs"
	if _, err := agent.Render(); !errors.Is(err, launchd.ErrInvalidAgent) {
		t.Fatalf("render with a NUL in the logs directory: got %v, want ErrInvalidAgent", err)
	}
}

func TestValidateRejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*launchd.Agent)
		want   string
	}{
		{
			name:   "empty label",
			mutate: func(a *launchd.Agent) { a.Label = "" },
			want:   "label",
		},
		{
			name:   "label with a path separator",
			mutate: func(a *launchd.Agent) { a.Label = "../../etc/evil" },
			want:   "label",
		},
		{
			name:   "no program",
			mutate: func(a *launchd.Agent) { a.ProgramArguments = nil },
			want:   "program arguments",
		},
		{
			name:   "relative program",
			mutate: func(a *launchd.Agent) { a.ProgramArguments = []string{"litefactory", "daemon", "run"} },
			want:   "launchd resolves nothing on PATH",
		},
		{
			name:   "relative logs directory",
			mutate: func(a *launchd.Agent) { a.LogsDir = "logs" },
			want:   "logs directory",
		},
		{
			name:   "no PATH",
			mutate: func(a *launchd.Agent) { delete(a.Environment, "PATH") },
			want:   "PATH must be set",
		},
		{
			name:   "empty PATH",
			mutate: func(a *launchd.Agent) { a.Environment["PATH"] = "" },
			want:   "PATH must be set",
		},
		{
			name:   "environment name that is not one",
			mutate: func(a *launchd.Agent) { a.Environment["not a name"] = "x" },
			want:   "is not an environment variable name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			agent := goldenAgent()
			agent.Environment = maps.Clone(agent.Environment)
			tc.mutate(&agent)

			err := agent.Validate()
			if !errors.Is(err, launchd.ErrInvalidAgent) {
				t.Fatalf("validate: got %v, want ErrInvalidAgent", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("validate: %v does not mention %q", err, tc.want)
			}
			if _, renderErr := agent.Render(); renderErr == nil {
				t.Error("render accepted an agent Validate rejected")
			}
		})
	}
}

// TestEffectivePATH covers the AC-2.2 materialisation: whatever the installing
// shell has wins, and launchd's own minimal PATH is the fallback rather than an
// empty value that would leave the daemon unable to find anything at all.
func TestEffectivePATH(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		shell string
		want  string
	}{
		{"a homebrew shell PATH is carried through", "/opt/homebrew/bin:/usr/bin:/bin", "/opt/homebrew/bin:/usr/bin:/bin"},
		{"an empty PATH falls back to launchd's own", "", launchd.LaunchdPATH},
		{"a whitespace PATH falls back too", "   ", launchd.LaunchdPATH},
		{"launchd's own PATH is left alone", launchd.LaunchdPATH, launchd.LaunchdPATH},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := launchd.EffectivePATH(tc.shell); got != tc.want {
				t.Errorf("EffectivePATH(%q) = %q, want %q", tc.shell, got, tc.want)
			}
		})
	}
}

func TestStandardPathsSitBesideEachOtherInTheLogsDirectory(t *testing.T) {
	t.Parallel()

	agent := goldenAgent()
	if got, want := agent.StandardOutPath(), "/Users/mario/.litefactory/logs/"+launchd.StdoutLogName; got != want {
		t.Errorf("StandardOutPath = %q, want %q", got, want)
	}
	if got, want := agent.StandardErrorPath(), "/Users/mario/.litefactory/logs/"+launchd.StderrLogName; got != want {
		t.Errorf("StandardErrorPath = %q, want %q", got, want)
	}
	// daemon.log is a JSONL sink the daemon writes itself; launchd's capture of
	// a panic must not be interleaved into it.
	for _, path := range []string{agent.StandardOutPath(), agent.StandardErrorPath()} {
		if filepath.Base(path) == "daemon.log" {
			t.Errorf("%s: launchd must not redirect into daemon.log", path)
		}
	}
}

// plistStrings decodes every <string> element's text, in document order, with
// a strict XML parser. Round-tripping through a real parser is the only honest
// way to assert that escaping worked.
func plistStrings(t *testing.T, data []byte) []string {
	t.Helper()

	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		values   []string
		inString bool
		current  strings.Builder
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode plist: %v\n%s", err, data)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if tok.Name.Local == "string" {
				inString, current = true, strings.Builder{}
			}
		case xml.CharData:
			if inString {
				current.Write(tok)
			}
		case xml.EndElement:
			if tok.Name.Local == "string" {
				values = append(values, current.String())
				inString = false
			}
		}
	}
	return values
}
