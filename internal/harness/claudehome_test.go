package harness

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// These tests are about the seam that made the daemon's runs hang for 9 to 31
// minutes each: the child reading the operator's ~/.claude.json, whose project
// registry reaches into TCC-protected folders under an attribution that has no
// grant. See [EnsureClaudeConfigDir].

// seedOperatorHome returns an OS home containing a .claude/.credentials.json,
// and the path of that file.
func seedOperatorHome(t *testing.T) (home, credentials string) {
	t.Helper()
	home = t.TempDir()
	dir := filepath.Join(home, userClaudeDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	credentials = filepath.Join(dir, credentialsFileName)
	if err := os.WriteFile(credentials, []byte(`{"claudeAiOauth":{}}`), 0o600); err != nil {
		t.Fatalf("write %s: %v", credentials, err)
	}
	return home, credentials
}

func TestEnsureClaudeConfigDirCreatesAndLinks(t *testing.T) {
	home, credentials := seedOperatorHome(t)
	dir := filepath.Join(t.TempDir(), "claude-home")

	created, err := EnsureClaudeConfigDir(dir, home)
	if err != nil {
		t.Fatalf("EnsureClaudeConfigDir: %v", err)
	}
	if !created {
		t.Error("first call did not report creating the directory")
	}

	link := filepath.Join(dir, credentialsFileName)
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %s: %v", link, err)
	}
	if got != credentials {
		t.Errorf("credentials link points at %q, want %q", got, credentials)
	}
}

// The credentials are linked, never copied: the token refreshes, and a copy
// would be stale from the moment it was made.
func TestEnsureClaudeConfigDirLinksRatherThanCopies(t *testing.T) {
	home, credentials := seedOperatorHome(t)
	dir := filepath.Join(t.TempDir(), "claude-home")
	if _, err := EnsureClaudeConfigDir(dir, home); err != nil {
		t.Fatalf("EnsureClaudeConfigDir: %v", err)
	}

	if err := os.WriteFile(credentials, []byte(`{"refreshed":true}`), 0o600); err != nil {
		t.Fatalf("refresh credentials: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, credentialsFileName))
	if err != nil {
		t.Fatalf("read through link: %v", err)
	}
	if string(body) != `{"refreshed":true}` {
		t.Errorf("reading through the link gave %q; a copy was made instead of a link", body)
	}
}

func TestEnsureClaudeConfigDirIsIdempotent(t *testing.T) {
	home, _ := seedOperatorHome(t)
	dir := filepath.Join(t.TempDir(), "claude-home")

	if _, err := EnsureClaudeConfigDir(dir, home); err != nil {
		t.Fatalf("first call: %v", err)
	}
	created, err := EnsureClaudeConfigDir(dir, home)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created {
		t.Error("second call reported creating an existing directory")
	}
}

// A link left behind by a moved home directory is repaired rather than
// inherited: a wrong credential file surfaces as an authentication error, a
// long way from its cause.
func TestEnsureClaudeConfigDirRepairsAStaleLink(t *testing.T) {
	home, credentials := seedOperatorHome(t)
	dir := filepath.Join(t.TempDir(), "claude-home")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	link := filepath.Join(dir, credentialsFileName)
	if err := os.Symlink("/Users/someone-else/.claude/.credentials.json", link); err != nil {
		t.Fatalf("seed stale link: %v", err)
	}

	if _, err := EnsureClaudeConfigDir(dir, home); err != nil {
		t.Fatalf("EnsureClaudeConfigDir: %v", err)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if got != credentials {
		t.Errorf("stale link survived: points at %q, want %q", got, credentials)
	}
}

// litefactory cannot log in for the operator, so a home with no credentials is
// not this function's failure to report.
func TestEnsureClaudeConfigDirToleratesMissingCredentials(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude-home")

	if _, err := EnsureClaudeConfigDir(dir, t.TempDir()); err != nil {
		t.Fatalf("EnsureClaudeConfigDir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory was not created: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, credentialsFileName)); err == nil {
		t.Error("a credentials link was created with nothing to link to")
	}
}

func TestEnsureClaudeConfigDirRejectsARelativeDir(t *testing.T) {
	if _, err := EnsureClaudeConfigDir("claude-home", t.TempDir()); err == nil {
		t.Error("a relative config dir was accepted")
	}
}

// CLAUDE_CONFIG_DIR is set by litefactory, never taken from the daemon's own
// environment — the operator's value is precisely the one that must not win.
func TestBuildEnvSetsTheConfigDirAndIgnoresTheInheritedOne(t *testing.T) {
	env := buildEnv(daemonEnviron(), false, "/Users/tester/.litefactory/claude-home")

	want := ClaudeConfigDirEnv + "=/Users/tester/.litefactory/claude-home"
	if !slices.Contains(env, want) {
		t.Errorf("missing %q in:\n%v", want, env)
	}
	if slices.Contains(env, ClaudeConfigDirEnv+"=/Users/tester/.claude") {
		t.Error("the daemon's own CLAUDE_CONFIG_DIR was forwarded to the child")
	}
}

// An embedder that assembled a Runner by hand gets claude's default rather than
// a directory litefactory invented for it.
func TestBuildEnvOmitsTheConfigDirWhenUnset(t *testing.T) {
	for _, name := range envNames(buildEnv(daemonEnviron(), false, "")) {
		if name == ClaudeConfigDirEnv {
			t.Error("CLAUDE_CONFIG_DIR was set from an empty config dir")
		}
	}
}
