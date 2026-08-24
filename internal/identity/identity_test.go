package identity_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/identity"
)

// The fake gh is a shell script on a test-controlled PATH, which is how these
// tests exercise the real exec.LookPath, the real environment construction and
// the real exit-code handling rather than a substituted function.

// ghAuthStatus is the shape `gh auth status` prints for a healthy login: an
// unindented host line, then indented details. The token line is included
// because the real command prints one, masked, and the parser has to ignore it.
const ghAuthStatus = "github.com\n" +
	"  - Active account: true\n" +
	"  - Git operations protocol: ssh\n" +
	"  - Token: gho_************************************\n" +
	"  - Token scopes: 'gist', 'read:org', 'repo'\n"

// ghNotLoggedIn is what gh writes when it holds no credentials at all.
const ghNotLoggedIn = "You are not logged into any GitHub hosts. To log in, run: gh auth login\n"

// ghUserJSON is the answer to `gh api user`.
const ghUserJSON = `{"login":"marioatcloudvane","id":4242,"type":"User"}`

// fakeGH describes how the fake answers each of the two commands the package
// runs.
type fakeGH struct {
	authStdout string
	authStderr string
	authExit   int
	apiStdout  string
	apiStderr  string
	apiExit    int
}

// newFakeGH is a gh that is installed, logged in to github.com and answers with
// a complete identity. Tests break one thing about it at a time.
func newFakeGH() fakeGH {
	return fakeGH{authStdout: ghAuthStatus, apiStdout: ghUserJSON}
}

// ghRecord is what the fake recorded about how it was called.
type ghRecord struct {
	envDump  string
	argsFile string
}

// install writes the fake and puts it first on PATH.
//
// The real PATH is kept behind it: the script needs /usr/bin/env to dump its
// own environment, and keeping it also proves PATH is forwarded to the child at
// all. exec.LookPath still finds the fake, because it is first.
func (f fakeGH) install(t *testing.T) ghRecord {
	t.Helper()

	dir := t.TempDir()
	rec := ghRecord{
		envDump:  filepath.Join(dir, "env.txt"),
		argsFile: filepath.Join(dir, "args.txt"),
	}

	script := strings.Join([]string{
		"#!/bin/sh",
		"env >> " + shellQuote(rec.envDump),
		`echo "$@" >> ` + shellQuote(rec.argsFile),
		`if [ "$1" = "auth" ]; then`,
		"  printf '%s' " + shellQuote(f.authStdout),
		"  printf '%s' " + shellQuote(f.authStderr) + " >&2",
		"  exit " + strconv.Itoa(f.authExit),
		"fi",
		"printf '%s' " + shellQuote(f.apiStdout),
		"printf '%s' " + shellQuote(f.apiStderr) + " >&2",
		"exit " + strconv.Itoa(f.apiExit),
		"",
	}, "\n")

	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return rec
}

// env returns the environment the fake was run with, one NAME=VALUE per line.
func (r ghRecord) env(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(r.envDump)
	if err != nil {
		t.Fatalf("read the environment the fake gh recorded: %v", err)
	}
	return string(data)
}

// args returns one line per invocation, holding that invocation's arguments.
func (r ghRecord) args(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(r.argsFile)
	if err != nil {
		t.Fatalf("read the arguments the fake gh recorded: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// shellQuote wraps a value in single quotes so the generated script cannot
// interpret it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// installEmptyPATH points PATH at a directory with no gh in it.
func installEmptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// TestResolveOwnerReturnsHandleAndNumericID is AC-0.2: both the handle and the
// numeric id come back, and they come from the two commands this package is
// allowed to run.
func TestResolveOwnerReturnsHandleAndNumericID(t *testing.T) {
	// The fake is installed with t.Setenv, which forbids t.Parallel.
	rec := newFakeGH().install(t)

	owner, err := identity.ResolveOwner(t.Context(), identity.Options{})
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}
	if owner.Login != "marioatcloudvane" {
		t.Errorf("Login = %q, want %q", owner.Login, "marioatcloudvane")
	}
	if owner.UserID != 4242 {
		t.Errorf("UserID = %d, want 4242", owner.UserID)
	}

	want := []string{"auth status", "api --hostname github.com user"}
	if got := rec.args(t); !slices.Equal(got, want) {
		t.Errorf("gh was called as %q, want %q", got, want)
	}
}

// TestResolveOwnerStripsTheTokenEnvironment is the security assertion this
// package exists for (AC-0.2).
//
// With the bot token exported — the normal state of a shell configured for
// github.token_env — gh must not see it. If it did, `gh api user` would answer
// as the bot, init would store the bot as the owner, and the bot != owner check
// would pass on two rows describing one account.
func TestResolveOwnerStripsTheTokenEnvironment(t *testing.T) {
	const botToken = "ghp_thisisthebottokenandmustnotreachgh"

	for _, name := range identity.StrippedEnvNames() {
		t.Setenv(name, botToken)
	}
	rec := newFakeGH().install(t)

	if _, err := identity.ResolveOwner(t.Context(), identity.Options{}); err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}

	env := rec.env(t)
	for _, name := range identity.StrippedEnvNames() {
		if strings.Contains(env, name+"=") {
			t.Errorf("gh inherited %s; the child environment must never carry a credential:\n%s", name, env)
		}
	}
	if strings.Contains(env, botToken) {
		t.Errorf("the bot token reached gh's environment:\n%s", env)
	}

	// The allowlist has to be a working environment, not just an empty one: gh
	// finds its configuration and its keychain item through these.
	for _, name := range []string{"PATH", "HOME"} {
		if !strings.Contains(env, "\n"+name+"=") && !strings.HasPrefix(env, name+"=") {
			t.Errorf("gh did not inherit %s:\n%s", name, env)
		}
	}
}

// TestAllowedEnvNamesExcludeCredentials states the boundary directly, so a
// later edit to the allowlist that re-admits a token variable fails here rather
// than in production.
func TestAllowedEnvNamesExcludeCredentials(t *testing.T) {
	t.Parallel()

	allowed := identity.AllowedEnvNames()
	for _, name := range identity.StrippedEnvNames() {
		if slices.Contains(allowed, name) {
			t.Errorf("%s is on the gh environment allowlist", name)
		}
	}
	for _, name := range []string{"GH_HOST", "GITHUB_ENTERPRISE_TOKEN", "SSH_AUTH_SOCK"} {
		if slices.Contains(allowed, name) {
			t.Errorf("%s is on the gh environment allowlist and should not be", name)
		}
	}
}

// TestResolveOwnerWithoutGH is the first half of AC-0.4: gh absent is a named
// failure that reports the PATH it searched.
func TestResolveOwnerWithoutGH(t *testing.T) {
	installEmptyPATH(t)

	_, err := identity.ResolveOwner(t.Context(), identity.Options{})
	if !errors.Is(err, identity.ErrNotInstalled) {
		t.Fatalf("error = %v, want it to match ErrNotInstalled", err)
	}
	if !strings.Contains(err.Error(), "PATH=") {
		t.Errorf("error %q does not name the PATH it searched", err)
	}
}

// TestResolveOwnerUnauthenticated is the second half of AC-0.4: a gh that is
// installed but logged out fails with the command and gh's own stderr intact.
func TestResolveOwnerUnauthenticated(t *testing.T) {
	fake := newFakeGH()
	fake.authStderr = ghNotLoggedIn
	fake.authStdout = ""
	fake.authExit = 1
	fake.install(t)

	_, err := identity.ResolveOwner(t.Context(), identity.Options{})
	if !errors.Is(err, identity.ErrNotAuthenticated) {
		t.Fatalf("error = %v, want it to match ErrNotAuthenticated", err)
	}

	var cmdErr *identity.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error %v is not a *CommandError, so init cannot print the command it ran", err)
	}
	if !strings.HasSuffix(cmdErr.Command(), "gh auth status") {
		t.Errorf("Command() = %q, want it to end with `gh auth status`", cmdErr.Command())
	}
	if !strings.Contains(err.Error(), "not logged into any GitHub hosts") {
		t.Errorf("error %q does not carry gh's stderr", err)
	}
}

// TestResolveOwnerNonZeroExit covers a gh that fails for a reason it did not
// phrase as being logged out. It must not be reclassified — the operator needs
// the exit code and the stderr, not a guess.
func TestResolveOwnerNonZeroExit(t *testing.T) {
	fake := newFakeGH()
	fake.authStderr = "error connecting to github.com: dial tcp: lookup github.com: no such host"
	fake.authStdout = ""
	fake.authExit = 2
	fake.install(t)

	_, err := identity.ResolveOwner(t.Context(), identity.Options{})
	if errors.Is(err, identity.ErrNotAuthenticated) {
		t.Fatalf("error = %v, want it left unclassified", err)
	}

	var cmdErr *identity.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error %v is not a *CommandError", err)
	}
	if cmdErr.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", cmdErr.ExitCode)
	}
	if !strings.Contains(cmdErr.Error(), "no such host") {
		t.Errorf("error %q does not carry gh's stderr", cmdErr)
	}
}

// TestResolveOwnerRejectsAnotherHost is AC-0.8. Only github.com is supported,
// and the message has to name what gh is actually logged in to — otherwise the
// operator has no idea which of their logins is the problem.
func TestResolveOwnerRejectsAnotherHost(t *testing.T) {
	tests := []struct {
		name   string
		onErr  bool // gh has printed auth status to stderr in some versions
		status string
	}{
		{name: "on stdout", status: "ghe.acme.example\n  - Active account: true\n"},
		{name: "on stderr", onErr: true, status: "ghe.acme.example\n  - Active account: true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeGH()
			fake.authStdout, fake.authStderr = tc.status, ""
			if tc.onErr {
				fake.authStdout, fake.authStderr = "", tc.status
			}
			rec := fake.install(t)

			_, err := identity.ResolveOwner(t.Context(), identity.Options{})
			if !errors.Is(err, identity.ErrUnsupportedHost) {
				t.Fatalf("error = %v, want it to match ErrUnsupportedHost", err)
			}
			if !strings.Contains(err.Error(), "ghe.acme.example") {
				t.Errorf("error %q does not name the host gh is logged in to", err)
			}
			if got := rec.args(t); len(got) != 1 {
				t.Errorf("gh was called %d times, want 1: the identity lookup must not run", len(got))
			}
		})
	}
}

// TestResolveOwnerAcceptsGitHubAlongsideAnother covers the operator who has an
// enterprise login as well. github.com is present, so discovery proceeds.
func TestResolveOwnerAcceptsGitHubAlongsideAnother(t *testing.T) {
	fake := newFakeGH()
	fake.authStdout = "ghe.acme.example\n  - Active account: false\n\n" + ghAuthStatus
	fake.install(t)

	owner, err := identity.ResolveOwner(t.Context(), identity.Options{})
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}
	if owner.UserID != 4242 {
		t.Errorf("UserID = %d, want 4242", owner.UserID)
	}
}

// TestResolveOwnerRejectsAnIncompleteIdentity covers the answers that would
// otherwise produce an owner row with a zero id — which the self-filter
// compares, so a zero there would silently match nobody (D-I).
func TestResolveOwnerRejectsAnIncompleteIdentity(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		wantErr error
	}{
		{name: "no id", answer: `{"login":"marioatcloudvane"}`, wantErr: identity.ErrNoIdentity},
		{name: "no login", answer: `{"id":4242}`, wantErr: identity.ErrNoIdentity},
		{name: "empty", answer: "", wantErr: nil},
		{name: "not json", answer: "gh: something went wrong", wantErr: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeGH()
			fake.apiStdout = tc.answer
			fake.install(t)

			_, err := identity.ResolveOwner(t.Context(), identity.Options{})
			if err == nil {
				t.Fatal("ResolveOwner accepted an answer carrying no usable identity")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want it to match %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "api --hostname github.com user") {
				t.Errorf("error %q does not name the command that answered", err)
			}
		})
	}
}

// TestResolveOwnerReportsAFailedUserLookup covers gh failing on the second
// command rather than the first.
func TestResolveOwnerReportsAFailedUserLookup(t *testing.T) {
	fake := newFakeGH()
	fake.apiStdout = ""
	fake.apiStderr = "gh: Bad credentials (HTTP 401)"
	fake.apiExit = 1
	fake.install(t)

	_, err := identity.ResolveOwner(t.Context(), identity.Options{})
	if err == nil {
		t.Fatal("ResolveOwner ignored a failing user lookup")
	}
	if !strings.Contains(err.Error(), "Bad credentials") {
		t.Errorf("error %q does not carry gh's stderr", err)
	}
}
