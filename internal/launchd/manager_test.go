package launchd_test

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cloudvane/litefactory/internal/launchd"
)

// No test in this file bootstraps a real LaunchAgent. Every one of them drives
// [launchd.Manager] through a fake [launchd.Runner] and a temporary home
// directory, because a test that loaded the real agent would leave a daemon
// running on the machine that ran it — and would need the machine to have a GUI
// session to pass at all.

// fakeLaunchctl records the launchctl calls it was asked to make and answers
// them from a script.
type fakeLaunchctl struct {
	// calls is every invocation, in order.
	calls [][]string
	// answer decides what each call returns. Nil means success with no output.
	answer func(args []string) (string, error)
}

func (f *fakeLaunchctl) Run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, slices.Clone(args))
	if f.answer == nil {
		return "", nil
	}
	return f.answer(args)
}

// subcommands returns the first argument of every call, which is the part the
// assertions below care about.
func (f *fakeLaunchctl) subcommands() []string {
	var names []string
	for _, call := range f.calls {
		if len(call) > 0 {
			names = append(names, call[0])
		}
	}
	return names
}

// notLoaded answers as launchctl does for a service that is not in the domain.
// The status differs by subcommand — bootout says 3 and print says 113 — and
// both are real; a bootstrap still succeeds, which is the whole point of
// booting out first.
func notLoaded(args []string) (string, error) {
	switch args[0] {
	case "print":
		return "", &launchd.CommandError{
			Binary:   "/bin/launchctl",
			Args:     args,
			ExitCode: 113,
			Stderr:   `Bad request.; Could not find service "com.github.cloudvane.litefactory" in domain for user gui: 501`,
			Err:      errors.New("exit status 113"),
		}
	case "bootout":
		return "", &launchd.CommandError{
			Binary:   "/bin/launchctl",
			Args:     args,
			ExitCode: 3,
			Stderr:   "Boot-out failed: 3: No such process",
			Err:      errors.New("exit status 3"),
		}
	default:
		return "", nil
	}
}

// newManager builds a manager over a temporary home directory and a fake
// launchctl.
func newManager(t *testing.T, fake *fakeLaunchctl) *launchd.Manager {
	t.Helper()

	return &launchd.Manager{
		UID:    501,
		Home:   t.TempDir(),
		Runner: fake,
		Logger: slog.New(slog.DiscardHandler),
	}
}

func TestTargetsAreAGUIDomain(t *testing.T) {
	t.Parallel()

	manager := newManager(t, &fakeLaunchctl{})
	// SD-8: keychain access, and therefore claude's own authentication, lives
	// in the user's GUI security session. A system/ target here would be a
	// LaunchDaemon, and claude would silently fail to authenticate.
	if got, want := manager.Domain(), "gui/501"; got != want {
		t.Errorf("Domain() = %q, want %q", got, want)
	}
	if got, want := manager.ServiceTarget(), "gui/501/"+launchd.DefaultLabel; got != want {
		t.Errorf("ServiceTarget() = %q, want %q", got, want)
	}

	path, err := manager.PlistPath()
	if err != nil {
		t.Fatalf("plist path: %v", err)
	}
	if want := filepath.Join("Library", "LaunchAgents", launchd.DefaultLabel+".plist"); !strings.HasSuffix(path, want) {
		t.Errorf("PlistPath() = %q, want it to end in %q", path, want)
	}
	if strings.Contains(path, "LaunchDaemons") {
		t.Errorf("PlistPath() = %q: the agent must never be installed as a LaunchDaemon (SD-8)", path)
	}
}

func TestInstallWritesThePlistAndBootstrapsIt(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: notLoaded}
	manager := newManager(t, fake)
	agent := manager.NewAgent(
		[]string{"/usr/local/bin/litefactory", "daemon", "run"},
		"/tmp/lf/logs",
		map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin"},
	)

	result, err := manager.Install(t.Context(), agent)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Replaced {
		t.Error("Replaced is true although nothing was loaded")
	}

	// bootout first, then bootstrap: re-running `daemon start` has to replace
	// and reload rather than fail on an already-loaded agent (AC-2.2).
	if diff := cmp.Diff([]string{"bootout", "bootstrap"}, fake.subcommands()); diff != "" {
		t.Errorf("launchctl calls (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"bootstrap", "gui/501", result.PlistPath}, fake.calls[1]); diff != "" {
		t.Errorf("bootstrap call (-want +got):\n%s", diff)
	}

	rendered, err := agent.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	written, err := os.ReadFile(result.PlistPath)
	if err != nil {
		t.Fatalf("read the installed plist: %v", err)
	}
	if diff := cmp.Diff(string(rendered), string(written)); diff != "" {
		t.Errorf("installed plist (-want +got):\n%s", diff)
	}
}

// TestInstalledPlistIsPrivate is AC-2.3's half that a warning cannot provide.
// The plist may hold a materialised bot token, and it is written 0600 whatever
// the umask of the shell that ran `daemon start` happened to be.
func TestInstalledPlistIsPrivate(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: notLoaded}
	manager := newManager(t, fake)
	agent := manager.NewAgent(
		[]string{"/usr/local/bin/litefactory", "daemon", "run"},
		"/tmp/lf/logs",
		map[string]string{"PATH": "/usr/bin", "LF_BOT_TOKEN": "ghp_averyrealtokenvalue"},
	)

	result, err := manager.Install(t.Context(), agent)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	info, err := os.Stat(result.PlistPath)
	if err != nil {
		t.Fatalf("stat the installed plist: %v", err)
	}
	if got, want := info.Mode().Perm(), launchd.PlistMode.Perm(); got != want {
		t.Errorf("plist mode is %#o, want %#o — it may contain a bot token", got, want)
	}

	body, err := os.ReadFile(result.PlistPath)
	if err != nil {
		t.Fatalf("read the installed plist: %v", err)
	}
	if !strings.Contains(string(body), "ghp_averyrealtokenvalue") {
		t.Error("the materialised token is not in the plist, so the daemon could never see it")
	}
}

func TestInstallReportsAReplacedAgent(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{}
	manager := newManager(t, fake)
	agent := manager.NewAgent([]string{"/usr/local/bin/litefactory", "daemon", "run"},
		"/tmp/lf/logs", map[string]string{"PATH": "/usr/bin"})

	result, err := manager.Install(t.Context(), agent)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !result.Replaced {
		t.Error("Replaced is false although bootout unloaded a running agent")
	}
}

func TestInstallRefusesAnAgentItCannotRender(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{}
	manager := newManager(t, fake)
	agent := manager.NewAgent([]string{"litefactory", "daemon", "run"},
		"/tmp/lf/logs", map[string]string{"PATH": "/usr/bin"})

	if _, err := manager.Install(t.Context(), agent); !errors.Is(err, launchd.ErrInvalidAgent) {
		t.Fatalf("install with a relative program: got %v, want ErrInvalidAgent", err)
	}
	// Nothing was written and nothing was unloaded: a `daemon start` that
	// refuses must leave the already-loaded agent exactly as it was.
	if len(fake.calls) != 0 {
		t.Errorf("launchctl was called %v despite the agent being invalid", fake.calls)
	}
	path, err := manager.PlistPath()
	if err != nil {
		t.Fatalf("plist path: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s: got %v, want the file not to exist", path, err)
	}
}

func TestInstallRefusesALabelThatIsNotTheManagersOwn(t *testing.T) {
	t.Parallel()

	manager := newManager(t, &fakeLaunchctl{})
	agent := manager.NewAgent([]string{"/usr/local/bin/litefactory", "daemon", "run"},
		"/tmp/lf/logs", map[string]string{"PATH": "/usr/bin"})
	agent.Label = "com.example.other"

	// The plist path comes from the manager's label and the service target
	// from the plist's; a mismatch installs an agent that can never be
	// unloaded by name.
	if _, err := manager.Install(t.Context(), agent); !errors.Is(err, launchd.ErrInvalidAgent) {
		t.Fatalf("install with a mismatched label: got %v, want ErrInvalidAgent", err)
	}
}

func TestInstallSurfacesABootstrapFailure(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: func(args []string) (string, error) {
		if args[0] != "bootstrap" {
			return "", nil
		}
		return "", &launchd.CommandError{
			Binary: "/bin/launchctl", Args: args, ExitCode: 5,
			Stderr: "Bootstrap failed: 5: Input/output error", Err: errors.New("exit status 5"),
		}
	}}
	manager := newManager(t, fake)
	agent := manager.NewAgent([]string{"/usr/local/bin/litefactory", "daemon", "run"},
		"/tmp/lf/logs", map[string]string{"PATH": "/usr/bin"})

	_, err := manager.Install(t.Context(), agent)
	if err == nil {
		t.Fatal("install succeeded although bootstrap failed")
	}
	var cmdErr *launchd.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("install error %v does not carry a *CommandError", err)
	}
	if !strings.Contains(err.Error(), "Input/output error") {
		t.Errorf("install error %v does not quote launchctl's own diagnostics", err)
	}
}

func TestUninstallToleratesAnAgentThatIsNotLoaded(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: notLoaded}
	manager := newManager(t, fake)

	unloaded, err := manager.Uninstall(t.Context())
	if err != nil {
		t.Fatalf("uninstall an agent that is not loaded: %v", err)
	}
	if unloaded {
		t.Error("uninstall reports it unloaded something that was not there")
	}
	if diff := cmp.Diff([]string{"bootout"}, fake.subcommands()); diff != "" {
		t.Errorf("launchctl calls (-want +got):\n%s", diff)
	}
}

func TestUninstallSurfacesARealFailure(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: func(args []string) (string, error) {
		return "", &launchd.CommandError{
			Binary: "/bin/launchctl", Args: args, ExitCode: 1,
			Stderr: "Boot-out failed: 1: Operation not permitted", Err: errors.New("exit status 1"),
		}
	}}
	manager := newManager(t, fake)

	if _, err := manager.Uninstall(t.Context()); err == nil {
		t.Fatal("uninstall swallowed a failure that was not 'not loaded'")
	}
}

// printOutput is a trimmed but faithful `launchctl print` body, including the
// nested "state = active" line that a naive parser would read as the daemon's
// own state.
const printOutput = `gui/501/com.github.cloudvane.litefactory = {
	active count = 1
	path = /Users/mario/Library/LaunchAgents/com.github.cloudvane.litefactory.plist
	type = LaunchAgent
	state = running

	program = /usr/local/bin/litefactory
	arguments = {
		litefactory
		daemon
		run
	}

	exit timeout = 20
	runs = 3
	pid = 4711

	event triggers = {
		com.example.trigger = {
			state = active
			pid = 999999
		}
	}
}`

func TestStatusReportsLoadedRunningAndPID(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: func([]string) (string, error) { return printOutput, nil }}
	manager := newManager(t, fake)

	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Loaded {
		t.Error("Loaded is false although launchctl printed the service")
	}
	if !status.Running {
		t.Error("Running is false although launchd reported a pid")
	}
	// 999999 is the nested trigger's pid; reading it would mean the parser is
	// walking into sub-dictionaries.
	if status.PID != 4711 {
		t.Errorf("PID = %d, want 4711", status.PID)
	}
	if status.State != "running" {
		t.Errorf("State = %q, want %q", status.State, "running")
	}
	if diff := cmp.Diff([]string{"print", "gui/501/" + launchd.DefaultLabel}, fake.calls[0]); diff != "" {
		t.Errorf("print call (-want +got):\n%s", diff)
	}
}

func TestStatusReportsALoadedAgentWithNoProcess(t *testing.T) {
	t.Parallel()

	const stopped = `gui/501/com.github.cloudvane.litefactory = {
	active count = 0
	type = LaunchAgent
	state = not running

	last exit code = 0
}`

	fake := &fakeLaunchctl{answer: func([]string) (string, error) { return stopped, nil }}
	manager := newManager(t, fake)

	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	switch {
	case !status.Loaded:
		t.Error("Loaded is false although the agent is bootstrapped")
	case status.Running:
		t.Error("Running is true although launchd reports no process")
	case status.PID != 0:
		t.Errorf("PID = %d, want 0", status.PID)
	case status.State != "not running":
		t.Errorf("State = %q, want %q", status.State, "not running")
	}
}

func TestStatusReportsAnAgentThatIsNotLoaded(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: notLoaded}
	manager := newManager(t, fake)

	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatalf("status of an agent that is not loaded: %v", err)
	}
	if status.Loaded || status.Running || status.PID != 0 {
		t.Errorf("status = %+v, want a not-loaded agent", status)
	}
	if status.Installed {
		t.Error("Installed is true although no plist was ever written")
	}
}

func TestStatusReportsAnInstalledButUnloadedAgent(t *testing.T) {
	t.Parallel()

	fake := &fakeLaunchctl{answer: notLoaded}
	manager := newManager(t, fake)
	agent := manager.NewAgent([]string{"/usr/local/bin/litefactory", "daemon", "run"},
		"/tmp/lf/logs", map[string]string{"PATH": "/usr/bin"})
	if _, err := manager.Install(t.Context(), agent); err != nil {
		t.Fatalf("install: %v", err)
	}

	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// This is exactly the state `daemon stop` leaves behind: the plist stays
	// so that `daemon start` can load the same configuration again.
	if !status.Installed {
		t.Error("Installed is false although the plist is on disk")
	}
	if status.Loaded {
		t.Error("Loaded is true although launchctl says the service is not there")
	}
}

// TestExecClassifiesLaunchctlsOwnFailures runs the real subprocess path against
// a fake launchctl, which is the only part of this package a hand-written
// Runner cannot cover: the exit status and the two-line diagnostics really do
// come back the way [launchd.Manager] expects.
func TestExecClassifiesLaunchctlsOwnFailures(t *testing.T) {
	t.Parallel()

	script := fakeLaunchctlBinary(t, `#!/bin/sh
if [ "$1" = "print" ]; then
  echo "Bad request." >&2
  echo "Could not find service \"x\" in domain for user gui: 501" >&2
  exit 113
fi
echo "ok"
`)
	manager := &launchd.Manager{UID: 501, Home: t.TempDir(), Runner: launchd.Exec{Binary: script}}

	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatalf("status through the real exec path: %v", err)
	}
	if status.Loaded {
		t.Error("exit 113 was not read as 'not loaded'")
	}

	unloaded, err := manager.Uninstall(t.Context())
	if err != nil {
		t.Fatalf("uninstall through the real exec path: %v", err)
	}
	if !unloaded {
		t.Error("a successful bootout was not reported as an unload")
	}
}

func TestExecReportsTheCommandItRan(t *testing.T) {
	t.Parallel()

	script := fakeLaunchctlBinary(t, `#!/bin/sh
echo "Boot-out failed: 1: Operation not permitted" >&2
exit 1
`)
	_, err := launchd.Exec{Binary: script}.Run(t.Context(), "bootout", "gui/501/label")

	var cmdErr *launchd.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Run error %v is not a *CommandError", err)
	}
	if cmdErr.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", cmdErr.ExitCode)
	}
	if diff := cmp.Diff([]string{"bootout", "gui/501/label"}, cmdErr.Args); diff != "" {
		t.Errorf("Args (-want +got):\n%s", diff)
	}
	for _, want := range []string{"bootout gui/501/label", "exited 1", "Operation not permitted"} {
		if !strings.Contains(cmdErr.Error(), want) {
			t.Errorf("error %q does not mention %q", cmdErr, want)
		}
	}
}

// fakeLaunchctlBinary writes an executable shell script and returns its path.
func fakeLaunchctlBinary(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "launchctl")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write the fake launchctl: %v", err)
	}
	return path
}

// Compile-time proof that the seam the tests rely on is the same one
// production uses.
var (
	_ launchd.Runner = (*fakeLaunchctl)(nil)
	_ launchd.Runner = launchd.Exec{}
)
