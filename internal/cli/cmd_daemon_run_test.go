package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// These tests cover what `daemon run` refuses to start over. They stop short of
// the loop itself, which lives in internal/daemon and is tested there against a
// real HTTP forge and a real database — what is being asserted here is the
// mapping from "this machine is not ready" onto exit 3, with a message that
// names the file to fix.
//
// None of them call t.Parallel: they set LF_HOME with t.Setenv.

// runDaemon dispatches `daemon run` against the machine h describes.
func (h *daemonHome) runDaemon(args ...string) (code int, stdout, stderr string) {
	h.t.Helper()

	var out, errOut bytes.Buffer
	env := &Env{
		Stdout:  &out,
		Stderr:  &errOut,
		Stdin:   strings.NewReader(""),
		Logger:  slog.New(slog.DiscardHandler),
		Version: "test",
	}
	router := NewRouter("litefactory", env, []*Command{newDaemonRunCommand()})
	code = router.Run(h.t.Context(), args)
	return code, out.String(), errOut.String()
}

// TestDaemonRunWithoutADatabaseIsAPrecondition: `init` has never run here, and
// saying so is more useful than whatever SQLite would say about a file it was
// about to create.
func TestDaemonRunWithoutADatabaseIsAPrecondition(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)

	code, _, stderr := h.runDaemon("daemon", "run")
	if code != ExitPrecondition {
		t.Fatalf("daemon run: exit %d, want %d\nstderr:\n%s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr, "litefactory init") {
		t.Errorf("the message does not say what to run:\n%s", stderr)
	}
	if !strings.Contains(stderr, h.paths.DBPath()) {
		t.Errorf("the message does not name the database it looked for:\n%s", stderr)
	}
}

// TestDaemonRunWithoutACredentialIsAPrecondition is S-5: the daemon posts as
// the bot and has no anonymous mode, so a missing credential is a refusal
// rather than a warning.
func TestDaemonRunWithoutACredentialIsAPrecondition(t *testing.T) {
	h := newDaemonHome(t, "log_level: info\n")
	h.migrate()

	code, _, stderr := h.runDaemon("daemon", "run")
	if code != ExitPrecondition {
		t.Fatalf("daemon run: exit %d, want %d\nstderr:\n%s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr, h.paths.ConfigPath()) {
		t.Errorf("the message does not name config.yaml:\n%s", stderr)
	}
	if !strings.Contains(stderr, "token_keychain") {
		t.Errorf("the message does not say how to configure a credential:\n%s", stderr)
	}
}

// TestDaemonRunWithAnUnresolvableCredentialNamesTheReference: "the daemon will
// not start" without saying which keychain item or environment variable it
// tried is the message that costs an evening.
func TestDaemonRunWithAnUnresolvableCredentialNamesTheReference(t *testing.T) {
	h := newDaemonHome(t, tokenEnvConfig)
	h.migrate()
	t.Setenv("LF_TEST_BOT_TOKEN", "")

	code, _, stderr := h.runDaemon("daemon", "run")
	if code != ExitPrecondition {
		t.Fatalf("daemon run: exit %d, want %d\nstderr:\n%s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr, "LF_TEST_BOT_TOKEN") {
		t.Errorf("the message does not name the reference it tried:\n%s", stderr)
	}
}

// TestDaemonRunHelpDescribesTheSingleton keeps the two properties an operator
// most needs to know — no pid file, one instance — in the place they will look.
func TestDaemonRunHelpDescribesTheSingleton(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)

	code, stdout, stderr := h.runDaemon("daemon", "run", "--help")
	if code != ExitOK {
		t.Fatalf("daemon run --help: exit %d\nstderr:\n%s", code, stderr)
	}
	for _, want := range []string{"foreground", "pid file", "second instance"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the help text does not mention %q:\n%s", want, stdout)
		}
	}
}
