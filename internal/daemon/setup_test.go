package daemon_test

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// setup builds a Setup over the fixture, with a fake claude on a PATH the test
// controls. Resolving a real Homebrew claude here would make the test depend on
// the machine it runs on, which is the exact fragility AC-3.18 exists to catch.
func (f *fixture) setup(claudeDir string) daemon.Setup {
	f.t.Helper()

	cfg := config.Defaults()
	cfg.GitHub.APIBase = f.forge.server.URL
	cfg.Claude.SettingsPath = f.paths.SettingsPath()

	return daemon.Setup{
		Paths:   f.paths,
		Config:  &cfg,
		DB:      f.db,
		Token:   secrets.NewToken(testToken),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Environ: []string{"PATH=" + claudeDir, "HOME=" + f.paths.Home()},
	}
}

// fakeClaude puts an executable named claude in a directory of its own and
// returns that directory, for use as a PATH.
func fakeClaude(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write the fake claude: %v", err)
	}
	return dir
}

// TestOpenResolvesEverythingAndTakesTheLock walks the start sequence: the
// executables are resolved by absolute path before anything else, and the
// singleton lock is taken last, because it is the first step that leaves a mark.
func TestOpenResolvesEverythingAndTakesTheLock(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)

	loop, startup, err := daemon.Open(t.Context(), f.setup(dir))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if closeErr := loop.Close(); closeErr != nil {
			t.Errorf("close the daemon: %v", closeErr)
		}
	}()

	if startup.Claude.Path != filepath.Join(dir, "claude") {
		t.Errorf("claude resolved to %q, want %q", startup.Claude.Path, filepath.Join(dir, "claude"))
	}
	if !startup.Claude.FromPATH {
		t.Error("a bare claude.binary was not reported as having been found on PATH; " +
			"that is the configuration that breaks under launchd")
	}
	if startup.Git == "" || !filepath.IsAbs(startup.Git) {
		t.Errorf("git resolved to %q, want an absolute path", startup.Git)
	}
	if startup.Identities.OwnerUserID != ownerID || startup.Identities.BotUserID != botID {
		t.Errorf("identities are %+v, want owner %d and bot %d", startup.Identities, ownerID, botID)
	}
	if startup.Repos != 1 {
		t.Errorf("reported %d registered repositories, want 1", startup.Repos)
	}
	if startup.LockPath != f.paths.LockPath() {
		t.Errorf("lock path is %q, want %q", startup.LockPath, f.paths.LockPath())
	}
	if startup.InstanceID == "" {
		t.Error("no instance id was minted")
	}
	if startup.Interval != daemon.DefaultInterval {
		t.Errorf("interval is %s, want %s", startup.Interval, daemon.DefaultInterval)
	}

	// AC-2.6, through the door a real second daemon comes in by.
	second, _, err := daemon.Open(t.Context(), f.setup(dir))
	if err == nil {
		t.Fatalf("a second daemon started; closing it: %v", second.Close())
	}
	if !errors.As(err, new(*daemon.AlreadyRunningError)) {
		t.Errorf("the second daemon failed with %v, want an *AlreadyRunningError", err)
	}
}

// TestOpenNamesThePathItSearchedForClaude is AC-3.18. launchd hands an agent
// PATH=/usr/bin:/bin:/usr/sbin:/sbin, which contains no Homebrew — so a claude
// that resolves in a terminal need not resolve in the daemon, and the error has
// to say where it looked.
func TestOpenNamesThePathItSearchedForClaude(t *testing.T) {
	f := newFixture(t)
	setup := f.setup(t.TempDir()) // an empty directory: no claude in it
	setup.Environ = []string{"PATH=" + launchd.LaunchdPATH}

	loop, _, err := daemon.Open(t.Context(), setup)
	if err == nil {
		t.Fatalf("Open succeeded with no claude on PATH; closing it: %v", loop.Close())
	}
	if !errors.Is(err, config.ErrExecutableNotFound) {
		t.Fatalf("Open failed with %v, want an executable-not-found error", err)
	}
	for _, dir := range strings.Split(launchd.LaunchdPATH, ":") {
		if !strings.Contains(err.Error(), dir) {
			t.Errorf("the error does not name %s, which it searched:\n%v", dir, err)
		}
	}
	if !strings.Contains(err.Error(), "claude.binary") {
		t.Errorf("the error does not name the configuration key:\n%v", err)
	}

	// Nothing was created: the lock is taken last, after everything that can
	// fail without leaving a mark.
	if _, statErr := os.Stat(f.paths.LockPath()); statErr == nil {
		t.Error("a daemon that refused to start still created its lock file")
	}
}

// TestOpenRefusesAnOutOfRangeInterval is AC-2.7 and D-C at the boundary the
// configuration file actually crosses.
func TestOpenRefusesAnOutOfRangeInterval(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)

	for _, seconds := range []int{1, 9, 61, 300} {
		setup := f.setup(dir)
		setup.Config.PollIntervalSeconds = seconds

		loop, _, err := daemon.Open(t.Context(), setup)
		if err == nil {
			t.Errorf("poll_interval_seconds %d started; closing it: %v", seconds, loop.Close())
			continue
		}
		if !errors.Is(err, daemon.ErrInvalidOptions) {
			t.Errorf("poll_interval_seconds %d failed with %v, want an invalid-options error", seconds, err)
		}
		if !strings.Contains(err.Error(), "poll_interval_seconds") {
			t.Errorf("the error does not name the key:\n%v", err)
		}
	}
}

// TestOpenRefusesWithoutABotIdentity is S-5: a daemon that does not know which
// account it posts as cannot run the self-filter, and one that cannot run the
// self-filter reads its own reports as instructions.
func TestOpenRefusesWithoutABotIdentity(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)

	// A machine where `init` ran before the bot credential existed: everything
	// is in place except the identity of the account litefactory posts as.
	home := filepath.Join(t.TempDir(), "half-initialised")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create the second LF_HOME: %v", err)
	}
	paths := config.NewPaths(home)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open the second database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close the second database: %v", closeErr)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate the second database: %v", err)
	}
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{
		Forge: store.ForgeGitHub, Role: store.RoleOwner,
		Handle: ownerLogin, UserID: ownerID, Source: store.SourceGH,
	}); err != nil {
		t.Fatalf("store the owner identity: %v", err)
	}

	setup := f.setup(dir)
	setup.Paths = paths
	setup.DB = db

	loop, _, err := daemon.Open(t.Context(), setup)
	if err == nil {
		t.Fatalf("Open succeeded with no bot identity; closing it: %v", loop.Close())
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Open failed with %v, want a not-found error naming the missing identity", err)
	}
}

// TestOpenRefusesWithoutACredential: the daemon posts as the bot and has no
// anonymous mode.
func TestOpenRefusesWithoutACredential(t *testing.T) {
	f := newFixture(t)
	setup := f.setup(fakeClaude(t))
	setup.Token = secrets.Token{}

	loop, _, err := daemon.Open(t.Context(), setup)
	if err == nil {
		t.Fatalf("Open succeeded with no token; closing it: %v", loop.Close())
	}
	if !errors.Is(err, daemon.ErrInvalidOptions) {
		t.Errorf("Open failed with %v, want an invalid-options error", err)
	}
}

// TestOpenRefusesTwoReposSharingAWorktreeRoot is AC-PAR-4.8 at startup.
//
// The worktree path carries the issue number but not the repository, so two
// repositories under one worktree root map issue 42 to one directory. Two
// turns on two repositories take two different clone gates and hold two
// correctly granted session locks, so nothing else in the design stops them
// entering it at once — which makes this refusal a security control rather
// than tidiness, and makes "most interleavings fail loudly anyway" no defence.
func TestOpenRefusesTwoReposSharingAWorktreeRoot(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)

	second := store.Repo{
		Path:          filepath.Join(f.paths.Home(), "clone-two"),
		Forge:         store.ForgeGitHub,
		Owner:         testOwnerName,
		Name:          "acme-web",
		RemoteURL:     "git@github.com:" + testOwnerName + "/acme-web.git",
		BaseBranch:    "main",
		FeaturePrefix: naming.DefaultFeaturePrefix,
		// Spelled differently from the fixture's root and resolving to the same
		// directory: it is the derived worktree path that collides, not the
		// stored string.
		WorktreeRoot: filepath.Join(f.repo.WorktreeRoot, "..", filepath.Base(f.repo.WorktreeRoot)) + "/",
	}
	if _, err := f.db.UpsertRepo(t.Context(), second); err != nil {
		t.Fatalf("register the second repository: %v", err)
	}

	loop, _, err := daemon.Open(t.Context(), f.setup(dir))
	if err == nil {
		t.Fatalf("a daemon started with two repositories sharing a worktree root; closing it: %v", loop.Close())
	}
	if !errors.Is(err, repos.ErrWorktreeRootCollision) {
		t.Fatalf("Open failed with %v, want a worktree-collision error", err)
	}
	for _, want := range []string{
		f.repo.Path, f.repo.Slug(),
		second.Path, second.Slug(),
		f.repo.WorktreeRoot,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q, which the operator has to act on:\n%v", want, err)
		}
	}

	// The check sits in the group that can fail without leaving a mark, before
	// the singleton lock — so a refusal here does not keep the next attempt out.
	if _, statErr := os.Stat(f.paths.LockPath()); statErr == nil {
		t.Error("a daemon that refused to start still created its lock file")
	}
}

// TestOpenAcceptsDistinctWorktreeRoots is the other half: the sibling default
// gives every clone a directory of its own, and that configuration starts.
func TestOpenAcceptsDistinctWorktreeRoots(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)

	if _, err := f.db.UpsertRepo(t.Context(), store.Repo{
		Path:          filepath.Join(f.paths.Home(), "clone-two"),
		Forge:         store.ForgeGitHub,
		Owner:         testOwnerName,
		Name:          "acme-web",
		RemoteURL:     "git@github.com:" + testOwnerName + "/acme-web.git",
		BaseBranch:    "main",
		FeaturePrefix: naming.DefaultFeaturePrefix,
		WorktreeRoot:  filepath.Join(f.paths.Home(), "worktrees-two"),
	}); err != nil {
		t.Fatalf("register the second repository: %v", err)
	}

	loop, startup, err := daemon.Open(t.Context(), f.setup(dir))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if closeErr := loop.Close(); closeErr != nil {
			t.Errorf("close the daemon: %v", closeErr)
		}
	}()
	if startup.Repos != 2 {
		t.Errorf("reported %d registered repositories, want 2", startup.Repos)
	}
}
