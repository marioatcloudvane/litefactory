package repos_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/repos"
)

// testRemoteURL is the origin every fixture is repointed at once it has
// finished pushing to its local bare repository.
//
// It names a repository that does not exist, on purpose. Every test below
// resolves a base branch through it, so if registration ever reached for the
// network — ls-remote, or the API — these tests would fail (or hang) rather
// than quietly pass on a machine that happens to be online. The review
// invariant "`repo add` makes no network call beyond the local
// `git remote get-url`" is what that is protecting.
const testRemoteURL = "git@github.com:acme/does-not-exist.git"

// newFixture builds a registrable clone: one commit, an origin pointing at
// GitHub, a cached origin/HEAD, and an aiteam marker.
func newFixture(t *testing.T) *gittest.Repo {
	t.Helper()
	repo := gittest.New(t)
	repo.WriteFile("CLAUDE.md", "# acme-api\n\n<!-- aiteam:team=go-backend -->\n")
	repo.Commit("declare the aiteam team")
	repo.SetOriginURL(testRemoteURL)
	return repo
}

// gitBinary resolves the real git once per test.
func gitBinary(t *testing.T) *git.Binary {
	t.Helper()
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	return binary
}

// mustResolve registers the fixture and fails the test if it will not.
func mustResolve(t *testing.T, repo *gittest.Repo, opts repos.Options, def repos.Defaults) *repos.Registration {
	t.Helper()
	registration, err := repos.Resolve(t.Context(), gitBinary(t), repo.Dir, opts, def)
	if err != nil {
		t.Fatalf("Resolve(%s) error = %v, want nil", repo.Dir, err)
	}
	return registration
}

// TestResolveReadsTheCloneItself is the happy path: everything `repo add`
// stores comes out of the clone, with no flags and no configuration.
func TestResolveReadsTheCloneItself(t *testing.T) {
	t.Parallel()

	repo := newFixture(t)
	got := mustResolve(t, repo, repos.Options{}, repos.Defaults{})

	if got.Path != repo.Dir {
		t.Errorf("Path = %q, want %q (absolute and symlink-resolved)", got.Path, repo.Dir)
	}
	if want := (git.RepoRef{Forge: git.ForgeGitHub, Owner: "acme", Name: "does-not-exist"}); got.Ref != want {
		t.Errorf("Ref = %+v, want %+v", got.Ref, want)
	}
	if got.RemoteURL != testRemoteURL {
		t.Errorf("RemoteURL = %q, want %q", got.RemoteURL, testRemoteURL)
	}
	if got.BaseBranch != gittest.DefaultBranch || got.BaseBranchSource != repos.SourceRemoteHEAD {
		t.Errorf("base branch = %q from %q, want %q from %q",
			got.BaseBranch, got.BaseBranchSource, gittest.DefaultBranch, repos.SourceRemoteHEAD)
	}
	if got.FeaturePrefix != naming.DefaultFeaturePrefix {
		t.Errorf("FeaturePrefix = %q, want %q", got.FeaturePrefix, naming.DefaultFeaturePrefix)
	}
	if got.Aiteam.Team != "go-backend" {
		t.Errorf("Aiteam.Team = %q, want %q", got.Aiteam.Team, "go-backend")
	}
}

// TestResolveSubdirectoryFindsTheRepositoryRoot covers the other half of
// AC-1.1's path resolution: a path inside the clone registers the clone.
func TestResolveSubdirectoryFindsTheRepositoryRoot(t *testing.T) {
	t.Parallel()

	repo := newFixture(t)
	sub := filepath.Join(repo.Dir, "internal", "deep")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("create %s: %v", sub, err)
	}

	got, err := repos.Resolve(t.Context(), gitBinary(t), sub, repos.Options{}, repos.Defaults{})
	if err != nil {
		t.Fatalf("Resolve(%s) error = %v, want nil", sub, err)
	}
	if got.Path != repo.Dir {
		t.Errorf("Path = %q, want the repository root %q", got.Path, repo.Dir)
	}
}

// TestResolveBaseBranchOrder covers AC-1.3 exhaustively: --base, then
// default_base_branch, then the remote's HEAD, then failure.
//
// The remote-HEAD row is the one that matters most. It resolves from
// refs/remotes/origin/HEAD — a file in .git — against an origin URL that would
// 404 if it were ever contacted, which is what makes it a test of the offline
// guarantee and not just of precedence.
func TestResolveBaseBranchOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		dropRemoteHEAD   bool
		opts             repos.Options
		defaults         repos.Defaults
		wantBranch       string
		wantSource       repos.Source
		wantErr          error
		wantMessageNames string
	}{
		{
			name:       "--base beats config and the remote HEAD",
			opts:       repos.Options{BaseBranch: "release"},
			defaults:   repos.Defaults{BaseBranch: "develop"},
			wantBranch: "release",
			wantSource: repos.SourceFlag,
		},
		{
			name:       "config beats the remote HEAD",
			defaults:   repos.Defaults{BaseBranch: "develop"},
			wantBranch: "develop",
			wantSource: repos.SourceConfig,
		},
		{
			name:       "the remote HEAD is the last resort",
			wantBranch: gittest.DefaultBranch,
			wantSource: repos.SourceRemoteHEAD,
		},
		{
			name:           "--base still answers without a remote HEAD",
			dropRemoteHEAD: true,
			opts:           repos.Options{BaseBranch: "release"},
			wantBranch:     "release",
			wantSource:     repos.SourceFlag,
		},
		{
			name:           "config still answers without a remote HEAD",
			dropRemoteHEAD: true,
			defaults:       repos.Defaults{BaseBranch: "develop"},
			wantBranch:     "develop",
			wantSource:     repos.SourceConfig,
		},
		{
			name:             "nothing resolves and the message asks for --base",
			dropRemoteHEAD:   true,
			wantErr:          repos.ErrBaseBranchUnknown,
			wantMessageNames: "--base",
		},
		{
			name:             "a --base git would not accept is rejected",
			opts:             repos.Options{BaseBranch: "--upload-pack=touch"},
			wantErr:          repos.ErrInvalidBaseBranch,
			wantMessageNames: "--upload-pack=touch",
		},
		{
			name:             "a --base containing .. is rejected",
			opts:             repos.Options{BaseBranch: "main/../evil"},
			wantErr:          repos.ErrInvalidBaseBranch,
			wantMessageNames: "main/../evil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			repo.WriteFile("CLAUDE.md", "managed by aiteam\n")
			repo.Commit("add CLAUDE.md")
			if tt.dropRemoteHEAD {
				// A clone made with --single-branch, and some mirrors, never
				// write an origin/HEAD. Deleting the ref is a local operation.
				repo.Git("remote", "set-head", "origin", "--delete")
			}
			repo.SetOriginURL(testRemoteURL)

			got, err := repos.Resolve(t.Context(), gitBinary(t), repo.Dir, tt.opts, tt.defaults)
			if tt.wantErr != nil {
				assertCheckFailure(t, err, tt.wantErr, repos.CheckBaseBranch, tt.wantMessageNames)
				return
			}
			if err != nil {
				t.Fatalf("Resolve error = %v, want nil", err)
			}
			if got.BaseBranch != tt.wantBranch {
				t.Errorf("BaseBranch = %q, want %q", got.BaseBranch, tt.wantBranch)
			}
			if got.BaseBranchSource != tt.wantSource {
				t.Errorf("BaseBranchSource = %q, want %q", got.BaseBranchSource, tt.wantSource)
			}
		})
	}
}

// TestResolveWorktreeRootOrder covers AC-1.6 and S-6: --worktrees, then the
// global worktree_root, then the sibling default — always stored absolute.
func TestResolveWorktreeRootOrder(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	tests := []struct {
		name       string
		opts       repos.Options
		defaults   repos.Defaults
		want       func(clone string) string
		wantSource repos.Source
	}{
		{
			name:       "--worktrees beats config and the sibling default",
			opts:       repos.Options{WorktreeRoot: "/srv/worktrees"},
			defaults:   repos.Defaults{WorktreeRoot: "/var/worktrees"},
			want:       func(string) string { return "/srv/worktrees" },
			wantSource: repos.SourceFlag,
		},
		{
			name:       "config beats the sibling default",
			defaults:   repos.Defaults{WorktreeRoot: "/var/worktrees"},
			want:       func(string) string { return "/var/worktrees" },
			wantSource: repos.SourceConfig,
		},
		{
			name: "the sibling default is a .lf-worktrees beside the clone",
			want: func(clone string) string {
				return filepath.Join(filepath.Dir(clone), naming.DefaultWorktreeRootDirName, filepath.Base(clone))
			},
			wantSource: repos.SourceSiblingDefault,
		},
		{
			name:       "a relative --worktrees is stored absolute",
			opts:       repos.Options{WorktreeRoot: "wt/./here"},
			want:       func(string) string { return filepath.Join(cwd, "wt", "here") },
			wantSource: repos.SourceFlag,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFixture(t)
			got := mustResolve(t, repo, tt.opts, tt.defaults)

			if want := tt.want(repo.Dir); got.WorktreeRoot != want {
				t.Errorf("WorktreeRoot = %q, want %q", got.WorktreeRoot, want)
			}
			if !filepath.IsAbs(got.WorktreeRoot) {
				t.Errorf("WorktreeRoot = %q, want an absolute path", got.WorktreeRoot)
			}
			if got.WorktreeRootSource != tt.wantSource {
				t.Errorf("WorktreeRootSource = %q, want %q", got.WorktreeRootSource, tt.wantSource)
			}
		})
	}
}

// TestResolveFeaturePrefixOrder covers --prefix, then feature_prefix, then the
// package default, plus the rejection of a prefix git would not accept.
func TestResolveFeaturePrefixOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     repos.Options
		defaults repos.Defaults
		want     string
		wantErr  error
	}{
		{name: "--prefix wins", opts: repos.Options{FeaturePrefix: "bots/lf"}, defaults: repos.Defaults{FeaturePrefix: "cfg"}, want: "bots/lf"},
		{name: "config is next", defaults: repos.Defaults{FeaturePrefix: "cfg"}, want: "cfg"},
		{name: "the default is last", want: naming.DefaultFeaturePrefix},
		{name: "an illegal prefix is rejected", opts: repos.Options{FeaturePrefix: "lf/"}, wantErr: naming.ErrInvalidFeaturePrefix},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFixture(t)
			got, err := repos.Resolve(t.Context(), gitBinary(t), repo.Dir, tt.opts, tt.defaults)
			if tt.wantErr != nil {
				assertCheckFailure(t, err, tt.wantErr, repos.CheckFeaturePrefix, "")
				return
			}
			if err != nil {
				t.Fatalf("Resolve error = %v, want nil", err)
			}
			if got.FeaturePrefix != tt.want {
				t.Errorf("FeaturePrefix = %q, want %q", got.FeaturePrefix, tt.want)
			}
		})
	}
}

// TestResolveRejectionsNameTheFailedCheck covers AC-1.1, AC-1.2 and AC-1.4.
//
// Each case asserts the *named* check rather than only a non-nil error,
// because "invalid repository" is exactly the message AC-1.1 exists to
// prevent: it leaves the user with no idea whether to run `git init`,
// `git remote add` or `npx aiteam`.
func TestResolveRejectionsNameTheFailedCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		setup            func(t *testing.T) string
		wantCheck        string
		wantErr          error
		wantMessageNames string
	}{
		{
			name: "a path that does not exist",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "absent")
			},
			wantCheck: repos.CheckPath,
			wantErr:   fs.ErrNotExist,
		},
		{
			name: "a path that is a regular file",
			setup: func(t *testing.T) string {
				file := filepath.Join(t.TempDir(), "notadir")
				if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
					t.Fatalf("write %s: %v", file, err)
				}
				return file
			},
			wantCheck: repos.CheckDirectory,
			wantErr:   git.ErrNotDirectory,
		},
		{
			name:      "a directory that is not a git repository",
			setup:     func(t *testing.T) string { return t.TempDir() },
			wantCheck: repos.CheckGitRepository,
			wantErr:   git.ErrNotARepository,
		},
		{
			name: "a repository with no origin remote",
			setup: func(t *testing.T) string {
				repo := newFixture(t)
				repo.RemoveOrigin()
				return repo.Dir
			},
			wantCheck: repos.CheckOriginRemote,
			wantErr:   git.ErrRemoteNotFound,
		},
		{
			name: "a remote hosted somewhere other than GitHub",
			setup: func(t *testing.T) string {
				repo := newFixture(t)
				repo.SetOriginURL("git@gitlab.com:acme/acme-api.git")
				return repo.Dir
			},
			wantCheck:        repos.CheckSupportedForge,
			wantErr:          git.ErrUnsupportedForge,
			wantMessageNames: "only GitHub is supported",
		},
		{
			name: "a repository with no aiteam installation",
			setup: func(t *testing.T) string {
				repo := gittest.New(t)
				repo.SetOriginURL(testRemoteURL)
				return repo.Dir
			},
			wantCheck:        repos.CheckAiteam,
			wantErr:          repos.ErrAiteamMissing,
			wantMessageNames: "npx aiteam",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t)
			_, err := repos.Resolve(t.Context(), gitBinary(t), path, repos.Options{}, repos.Defaults{})
			assertCheckFailure(t, err, tt.wantErr, tt.wantCheck, tt.wantMessageNames)
		})
	}
}

// TestResolveRegistersWithoutATeam covers AC-1.5: a repository where aiteam is
// installed but declares no team is still registered, with an empty team.
func TestResolveRegistersWithoutATeam(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	if err := os.MkdirAll(filepath.Join(repo.Dir, ".aiteam"), 0o700); err != nil {
		t.Fatalf("create .aiteam: %v", err)
	}
	repo.SetOriginURL(testRemoteURL)

	got := mustResolve(t, repo, repos.Options{}, repos.Defaults{})
	if !got.Aiteam.Present {
		t.Fatalf("Aiteam.Present = false, want true")
	}
	if got.Aiteam.Team != "" {
		t.Errorf("Aiteam.Team = %q, want empty", got.Aiteam.Team)
	}
}

// TestResolveLeavesTheCloneUntouched is AC-1.7.
//
// The fixture is deliberately dirtied first — a staged addition, an unstaged
// modification and an untracked file. Against a clean checkout this assertion
// is trivially satisfiable and proves nothing: a bug that reset the index
// would still look like success.
func TestResolveLeavesTheCloneUntouched(t *testing.T) {
	t.Parallel()

	repo := newFixture(t)
	repo.Dirty()

	before := gittest.Capture(t, repo.Dir)
	if before.Status == "" {
		t.Fatalf("the fixture is clean; this assertion would prove nothing")
	}

	gittest.AssertUnchanged(t, repo.Dir, func() {
		mustResolve(t, repo, repos.Options{}, repos.Defaults{})
	})
}

// TestResolveIsDeterministic runs registration twice over the same clone and
// requires the same answer. It is the property `repo add`'s idempotency rests
// on: the upsert can only be safe if re-resolving produces the same row.
func TestResolveIsDeterministic(t *testing.T) {
	t.Parallel()

	repo := newFixture(t)
	first := mustResolve(t, repo, repos.Options{}, repos.Defaults{})
	second := mustResolve(t, repo, repos.Options{}, repos.Defaults{})

	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("Resolve is not deterministic (-first +second):\n%s", diff)
	}
}

// TestStoreRepoLeavesLastFetchAlone pins the one field registration must not
// set: a re-registration that stamped last_fetch_at would make litefactory
// believe it had fetched a repository it has never contacted.
func TestStoreRepoLeavesLastFetchAlone(t *testing.T) {
	t.Parallel()

	repo := newFixture(t)
	registration := mustResolve(t, repo, repos.Options{}, repos.Defaults{})

	row := registration.StoreRepo(time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC))
	if !row.LastFetchAt.IsZero() {
		t.Errorf("LastFetchAt = %v, want the zero time", row.LastFetchAt)
	}
	if row.AiteamVerifiedAt.IsZero() {
		t.Errorf("AiteamVerifiedAt is zero, want the verification time")
	}
	if row.Path != registration.Path || row.Owner != "acme" || row.BaseBranch != gittest.DefaultBranch {
		t.Errorf("StoreRepo did not carry the registration through: %+v", row)
	}
}

// assertCheckFailure requires err to be a *repos.CheckError naming check and
// wrapping want, and to mention needle when one is given.
func assertCheckFailure(t *testing.T, err error, want error, check, needle string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want %v", want)
	}
	var checkErr *repos.CheckError
	if !errors.As(err, &checkErr) {
		t.Fatalf("error = %v (%T), want a *repos.CheckError", err, err)
	}
	if checkErr.Check != check {
		t.Errorf("failed check = %q, want %q (message: %s)", checkErr.Check, check, err)
	}
	if !errors.Is(err, want) {
		t.Errorf("errors.Is(err, %v) = false; err = %v", want, err)
	}
	if needle != "" && !strings.Contains(err.Error(), needle) {
		t.Errorf("message %q does not mention %q", err.Error(), needle)
	}
}
