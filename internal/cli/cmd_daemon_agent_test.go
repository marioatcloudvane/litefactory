package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/store"
)

// Nothing in this file loads a real LaunchAgent. Each test points HOME at a
// temporary directory, so the plist is written under a temporary
// Library/LaunchAgents, and drives launchctl through a fake — a test that
// bootstrapped the real agent would leave a daemon running on the machine that
// ran it.
//
// None of these tests call t.Parallel: they set LF_HOME and HOME with
// t.Setenv, which forbids it.

// testBotToken is the value the token_env tests materialise. It is fabricated
// and never leaves the temporary directory it is written into.
const testBotToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"

// fakeLaunchctl records what it was asked to run and answers from a script.
type fakeLaunchctl struct {
	calls  [][]string
	answer func(args []string) (string, error)
}

func (f *fakeLaunchctl) Run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, slices.Clone(args))
	if f.answer == nil {
		return "", nil
	}
	return f.answer(args)
}

func (f *fakeLaunchctl) subcommands() []string {
	var names []string
	for _, call := range f.calls {
		if len(call) > 0 {
			names = append(names, call[0])
		}
	}
	return names
}

// notLoadedError is launchctl's answer for a service that is not in the
// domain. bootout says 3 and print says 113; both are real.
func notLoadedError(args []string, code int, stderr string) error {
	return &launchd.CommandError{
		Binary:   "/bin/launchctl",
		Args:     args,
		ExitCode: code,
		Stderr:   stderr,
		Err:      errors.New("exit status " + strconv.Itoa(code)),
	}
}

// daemonHome is one machine: an $LF_HOME, a home directory to install the
// plist into, and a fake launchctl.
type daemonHome struct {
	t     *testing.T
	paths config.Paths
	home  string
	fake  *fakeLaunchctl
}

// newDaemonHome creates an $LF_HOME holding the given config.yaml body — it is
// decoded on top of the defaults, so a fragment is enough — and a separate
// home directory for ~/Library/LaunchAgents.
func newDaemonHome(t *testing.T, configBody string) *daemonHome {
	t.Helper()

	lfHome := t.TempDir()
	t.Setenv(config.EnvHome, lfHome)
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	paths := config.NewPaths(lfHome)
	if configBody != "" {
		if err := os.WriteFile(paths.ConfigPath(), []byte(configBody), 0o600); err != nil {
			t.Fatalf("write config.yaml: %v", err)
		}
	}
	return &daemonHome{t: t, paths: paths, home: userHome, fake: &fakeLaunchctl{}}
}

// migrate creates the database `daemon status` reads the poll cursor from.
func (h *daemonHome) migrate() *store.DB {
	h.t.Helper()

	db, err := store.Open(h.t.Context(), h.paths.DBPath())
	if err != nil {
		h.t.Fatalf("open store: %v", err)
	}
	h.t.Cleanup(func() {
		if err := db.Close(); err != nil {
			h.t.Errorf("close store: %v", err)
		}
	})
	if err := db.Migrate(h.t.Context()); err != nil {
		h.t.Fatalf("migrate: %v", err)
	}
	return db
}

// run dispatches one daemon command against the fake launchctl.
func (h *daemonHome) run(args ...string) (code int, stdout, stderr string) {
	h.t.Helper()

	var out, errOut bytes.Buffer
	env := &Env{
		Stdout:  &out,
		Stderr:  &errOut,
		Stdin:   strings.NewReader(""),
		Logger:  slog.New(slog.DiscardHandler),
		Version: "test",
	}
	router := NewRouter("litefactory", env, []*Command{
		daemonStartCommand(h.fake),
		daemonStopCommand(h.fake),
		daemonStatusCommand(h.fake),
	})
	code = router.Run(h.t.Context(), args)
	return code, out.String(), errOut.String()
}

// plistPath is where the agent should have been written.
func (h *daemonHome) plistPath() string {
	return filepath.Join(h.home, "Library", "LaunchAgents", launchd.DefaultLabel+".plist")
}

// plist reads the installed agent.
func (h *daemonHome) plist() string {
	h.t.Helper()

	body, err := os.ReadFile(h.plistPath())
	if err != nil {
		h.t.Fatalf("read the installed plist: %v", err)
	}
	return string(body)
}

const keychainConfig = `github:
  token_keychain:
    service: litefactory
    account: lf-bot
`

const tokenEnvConfig = `github:
  token_env: LF_TEST_BOT_TOKEN
`

// TestDaemonStartMaterialisesThePATH is AC-2.2. launchd would hand the daemon
// /usr/bin:/bin:/usr/sbin:/sbin, which has no Homebrew claude in it, so the
// installing shell's PATH has to be written into the plist — and reported,
// because a PATH materialised silently is one nobody checks until a run fails.
func TestDaemonStartMaterialisesThePATH(t *testing.T) {
	const shellPATH = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

	h := newDaemonHome(t, keychainConfig)
	t.Setenv("PATH", shellPATH)

	code, stdout, stderr := h.run("daemon", "start")
	if code != ExitOK {
		t.Fatalf("daemon start: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	plist := h.plist()
	if !strings.Contains(plist, "<key>PATH</key>\n\t\t<string>"+shellPATH+"</string>") {
		t.Errorf("the plist does not carry the shell PATH:\n%s", plist)
	}
	if !strings.Contains(stdout, shellPATH) {
		t.Errorf("`daemon start` did not report the PATH it materialised:\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, launchd.LaunchdPATH) {
		t.Errorf("`daemon start` did not say what launchd's own PATH would have been:\nstdout:\n%s", stdout)
	}
}

// TestDaemonStartLoadsTheAgent covers the rest of AC-2.2: the plist invokes
// `litefactory daemon run`, it is bootstrapped into the GUI domain, and
// $LF_HOME is pinned so the daemon cannot end up reading a different one.
func TestDaemonStartLoadsTheAgent(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)

	code, stdout, stderr := h.run("daemon", "start")
	if code != ExitOK {
		t.Fatalf("daemon start: exit %d\nstderr:\n%s", code, stderr)
	}

	plist := h.plist()
	for _, want := range []string{
		"<string>daemon</string>",
		"<string>run</string>",
		"<key>" + config.EnvHome + "</key>\n\t\t<string>" + h.paths.Home() + "</string>",
		"<key>StandardOutPath</key>\n\t<string>" + filepath.Join(h.paths.LogsDir(), launchd.StdoutLogName),
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("the plist does not contain %q:\n%s", want, plist)
		}
	}

	if got := strings.Join(h.fake.subcommands(), ","); got != "bootout,bootstrap" {
		t.Errorf("launchctl calls were %q, want bootout then bootstrap", got)
	}
	// SD-8: a GUI domain, never a system one. Keychain access — and therefore
	// claude's authentication — exists only inside the user's GUI session.
	if got := h.fake.calls[1]; got[1] != "gui/"+strconv.Itoa(os.Getuid()) {
		t.Errorf("bootstrapped into %q, want the current user's GUI domain", got[1])
	}

	// launchd creates the redirect files but not the directory holding them.
	if _, err := os.Stat(h.paths.LogsDir()); err != nil {
		t.Errorf("stat the logs directory: %v — launchd would fail to redirect and the job would never run", err)
	}
	if !strings.Contains(stdout, h.plistPath()) {
		t.Errorf("`daemon start` did not name the plist it wrote:\n%s", stdout)
	}
	if strings.Contains(stderr, "WARNING") {
		t.Errorf("a keychain-configured start warned about a secret:\n%s", stderr)
	}
}

// TestDaemonStartReplacesAndReloads is the "re-running replaces and reloads
// without error" half of AC-2.2.
func TestDaemonStartReplacesAndReloads(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)

	if code, _, stderr := h.run("daemon", "start"); code != ExitOK {
		t.Fatalf("first daemon start: exit %d\nstderr:\n%s", code, stderr)
	}
	first := h.plist()

	code, stdout, stderr := h.run("daemon", "start")
	if code != ExitOK {
		t.Fatalf("second daemon start: exit %d\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "reloaded") {
		t.Errorf("the second start did not report a reload:\n%s", stdout)
	}
	if again := h.plist(); again != first {
		t.Errorf("an unchanged configuration produced a different plist:\n%s", again)
	}
	if got := strings.Join(h.fake.subcommands(), ","); got != "bootout,bootstrap,bootout,bootstrap" {
		t.Errorf("launchctl calls were %q, want each start to boot out before bootstrapping", got)
	}
}

// TestDaemonStartMaterialisesTokenEnvAndWarnsLoudly is AC-2.3. The plist is
// written 0600 and the warning names the file and what it now contains — and
// the command does not refuse, because refusing would leave the user with a
// daemon that cannot start and no explanation.
func TestDaemonStartMaterialisesTokenEnvAndWarnsLoudly(t *testing.T) {
	h := newDaemonHome(t, tokenEnvConfig)
	t.Setenv("LF_TEST_BOT_TOKEN", testBotToken)

	code, stdout, stderr := h.run("daemon", "start")
	if code != ExitOK {
		t.Fatalf("daemon start: exit %d\nstderr:\n%s", code, stderr)
	}

	if !strings.Contains(h.plist(), testBotToken) {
		t.Error("the token was not materialised, so a launchd daemon could never see it")
	}
	info, err := os.Stat(h.plistPath())
	if err != nil {
		t.Fatalf("stat the plist: %v", err)
	}
	if got, want := info.Mode().Perm(), launchd.PlistMode.Perm(); got != want {
		t.Errorf("the plist holds a token and is mode %#o, want %#o", got, want)
	}

	for _, want := range []string{
		"WARNING",
		h.plistPath(),
		"LF_TEST_BOT_TOKEN",
		"plain text",
		"github.token_keychain",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the warning does not mention %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stdout, testBotToken) || strings.Contains(stderr, testBotToken) {
		t.Error("the token itself was printed; the warning must name the file, not repeat the secret")
	}
}

// TestDaemonStartRefusesATokenEnvThatIsNotSet covers the S-5 case that is a
// refusal rather than a warning: the configured reference resolves to nothing,
// so installing the agent would produce a daemon that cannot authenticate and
// no terminal to say so in.
func TestDaemonStartRefusesATokenEnvThatIsNotSet(t *testing.T) {
	h := newDaemonHome(t, tokenEnvConfig)
	// Set but empty is the same failure as unset — the reference resolves to
	// nothing either way — and t.Setenv restores it afterwards.
	t.Setenv("LF_TEST_BOT_TOKEN", "")

	code, _, stderr := h.run("daemon", "start")
	if code != ExitPrecondition {
		t.Fatalf("daemon start: exit %d, want %d\nstderr:\n%s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr, "LF_TEST_BOT_TOKEN") {
		t.Errorf("the refusal does not name the variable:\n%s", stderr)
	}
	if len(h.fake.calls) != 0 {
		t.Errorf("launchctl was called %v although the command refused", h.fake.calls)
	}
	if _, err := os.Stat(h.plistPath()); err == nil {
		t.Error("a plist was written although the command refused")
	}
}

// TestDaemonStartWarnsWithNoCredential keeps the "not running" in `daemon
// status` from being a mystery: with no bot token the daemon exits at startup,
// and the only place that can be explained is here.
func TestDaemonStartWarnsWithNoCredential(t *testing.T) {
	h := newDaemonHome(t, "poll_interval_seconds: 30\n")

	code, stdout, stderr := h.run("daemon", "start")
	if code != ExitOK {
		t.Fatalf("daemon start: exit %d\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "no bot token is configured") {
		t.Errorf("no warning about the missing credential:\n%s", stderr)
	}
	if !strings.Contains(stderr, h.paths.ConfigPath()) {
		t.Errorf("the warning does not name the file to fix:\n%s", stderr)
	}
	if !strings.Contains(stdout, "none configured") {
		t.Errorf("the report does not say the token source is missing:\n%s", stdout)
	}
}

func TestDaemonStartRequiresInit(t *testing.T) {
	h := newDaemonHome(t, "")

	code, _, stderr := h.run("daemon", "start")
	if code != ExitPrecondition {
		t.Fatalf("daemon start with no config.yaml: exit %d, want %d", code, ExitPrecondition)
	}
	if !strings.Contains(stderr, "litefactory init") {
		t.Errorf("the error does not say to run init:\n%s", stderr)
	}
	if len(h.fake.calls) != 0 {
		t.Errorf("launchctl was called %v on a machine that is not initialised", h.fake.calls)
	}
}

// TestDaemonStopUnloadsTheAgent is half of AC-2.4.
func TestDaemonStopUnloadsTheAgent(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)

	code, stdout, stderr := h.run("daemon", "stop")
	if code != ExitOK {
		t.Fatalf("daemon stop: exit %d\nstderr:\n%s", code, stderr)
	}
	if got := strings.Join(h.fake.subcommands(), ","); got != "bootout" {
		t.Errorf("launchctl calls were %q, want a single bootout", got)
	}
	if !strings.Contains(stdout, "unloaded") {
		t.Errorf("`daemon stop` did not report the unload:\n%s", stdout)
	}
}

// TestDaemonStopToleratesAnAgentThatIsNotLoaded: stopping a stopped daemon is
// a success, so that a script can stop before it starts without caring which
// state it found.
func TestDaemonStopToleratesAnAgentThatIsNotLoaded(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	h.fake.answer = func(args []string) (string, error) {
		return "", notLoadedError(args, 3, "Boot-out failed: 3: No such process")
	}

	code, stdout, stderr := h.run("daemon", "stop")
	if code != ExitOK {
		t.Fatalf("daemon stop on a stopped daemon: exit %d\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "was not loaded") {
		t.Errorf("`daemon stop` did not report that nothing was loaded:\n%s", stdout)
	}
}

// TestDaemonStatusReportsLoadedRunningPIDAndLastTick is AC-2.4. last_tick_at is
// why `daemon status` needs the database at all.
func TestDaemonStatusReportsLoadedRunningPIDAndLastTick(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	db := h.migrate()

	lastTick := time.Date(2026, 8, 23, 18, 4, 11, 0, time.UTC)
	if err := db.SetPollCursor(h.t.Context(), store.PollCursor{
		TasksUpdatedSince: lastTick, LastTickAt: lastTick,
	}); err != nil {
		t.Fatalf("set poll cursor: %v", err)
	}

	h.fake.answer = func([]string) (string, error) {
		return "gui/501/" + launchd.DefaultLabel + " = {\n\tstate = running\n\tpid = 4711\n}", nil
	}

	code, stdout, stderr := h.run("daemon", "status")
	if code != ExitOK {
		t.Fatalf("daemon status: exit %d\nstderr:\n%s", code, stderr)
	}
	for _, want := range []string{
		launchd.DefaultLabel,
		"loaded",
		"yes",
		"pid 4711",
		"2026-08-23T18:04:11Z",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("`daemon status` output does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestDaemonStatusReportsAStoppedDaemon(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	h.migrate()
	h.fake.answer = func(args []string) (string, error) {
		return "", notLoadedError(args, 113,
			"Bad request.; Could not find service in domain for user gui: 501")
	}

	code, stdout, stderr := h.run("daemon", "status")
	if code != ExitOK {
		t.Fatalf("daemon status: exit %d\nstderr:\n%s", code, stderr)
	}
	for _, want := range []string{"loaded", "no", "absent", daemonNeverTicked} {
		if !strings.Contains(stdout, want) {
			t.Errorf("`daemon status` output does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestDaemonStatusRequiresADatabase(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)

	code, _, stderr := h.run("daemon", "status")
	if code != ExitPrecondition {
		t.Fatalf("daemon status with no database: exit %d, want %d", code, ExitPrecondition)
	}
	if !strings.Contains(stderr, "litefactory init") {
		t.Errorf("the error does not say to run init:\n%s", stderr)
	}
}

// TestDaemonCommandsWriteNoPIDFile is AC-2.5 as far as these three commands can
// assert it: nothing here creates a pid file, and the pid `daemon status`
// prints came from launchctl.
func TestDaemonCommandsWriteNoPIDFile(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	h.migrate()
	h.fake.answer = func([]string) (string, error) {
		return "gui/501/x = {\n\tstate = running\n\tpid = 4711\n}", nil
	}

	for _, args := range [][]string{{"daemon", "start"}, {"daemon", "status"}, {"daemon", "stop"}} {
		if code, _, stderr := h.run(args...); code != ExitOK {
			t.Fatalf("%v: exit %d\nstderr:\n%s", args, code, stderr)
		}
	}

	entries, err := os.ReadDir(h.paths.Home())
	if err != nil {
		t.Fatalf("read $LF_HOME: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".pid") {
			t.Errorf("%s was created; the daemon is supervised by launchd and writes no pid file (D-8)", entry.Name())
		}
	}
}
