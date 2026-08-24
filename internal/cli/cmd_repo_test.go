package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

// repoTestRemoteURL is the origin the fixtures are repointed at.
//
// It names a repository that does not exist. `repo add` resolves the base
// branch through this remote in nearly every test below, so a registration
// that reached for the network would fail here rather than pass quietly on a
// machine that happens to be online.
const repoTestRemoteURL = "git@github.com:acme/does-not-exist.git"

// newRepoHome points $LF_HOME at a temporary directory that has been through
// the same bootstrap `litefactory init` performs: a default config.yaml and a
// migrated database. Both commands refuse to invent either, so a test that
// skipped this would only ever exercise the "run init first" path.
func newRepoHome(t *testing.T) config.Paths {
	t.Helper()

	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	paths := config.NewPaths(home)
	if _, err := config.WriteDefault(paths); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	return paths
}

// newRepoFixture builds a registrable clone: an origin on GitHub, a cached
// origin/HEAD and an aiteam marker declaring a team.
func newRepoFixture(t *testing.T) *gittest.Repo {
	t.Helper()

	repo := gittest.New(t)
	repo.WriteFile("CLAUDE.md", "# acme-api\n\n<!-- aiteam:team=go-backend -->\n")
	repo.Commit("declare the aiteam team")
	repo.SetOriginURL(repoTestRemoteURL)
	return repo
}

// listRepos reads the repos table back, which is the only honest way to check
// that an upsert did what the output claims.
func listRepos(t *testing.T, paths config.Paths) []store.Repo {
	t.Helper()

	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	list, err := db.ListRepos(t.Context())
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	return list
}

// TestRepoAddRegistersTheClone covers the happy path and AC-1.6: the row lands
// with the resolved base branch, the sibling worktree root and the team.
func TestRepoAddRegistersTheClone(t *testing.T) {
	paths := newRepoHome(t)
	repo := newRepoFixture(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	list := listRepos(t, paths)
	if len(list) != 1 {
		t.Fatalf("registered %d repositories, want 1", len(list))
	}
	got := list[0]
	wantRoot := filepath.Join(filepath.Dir(repo.Dir), ".lf-worktrees", filepath.Base(repo.Dir))
	switch {
	case got.Path != repo.Dir:
		t.Errorf("path = %q, want %q", got.Path, repo.Dir)
	case got.Forge != "github" || got.Slug() != "acme/does-not-exist":
		t.Errorf("remote = %s:%s, want github:acme/does-not-exist", got.Forge, got.Slug())
	case got.BaseBranch != gittest.DefaultBranch:
		t.Errorf("base branch = %q, want %q", got.BaseBranch, gittest.DefaultBranch)
	case got.WorktreeRoot != wantRoot:
		t.Errorf("worktree root = %q, want %q", got.WorktreeRoot, wantRoot)
	case got.AiteamTeam != "go-backend":
		t.Errorf("aiteam team = %q, want %q", got.AiteamTeam, "go-backend")
	case !got.LastFetchAt.IsZero():
		t.Errorf("last fetch = %v, want the zero time", got.LastFetchAt)
	}

	for _, want := range []string{"registered", "github:acme/does-not-exist", repo.Dir, gittest.DefaultBranch, "go-backend"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout does not mention %q:\n%s", want, stdout)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestRepoAddIsIdempotent is AC-1.8: a second `repo add` on the same path
// updates the mutable fields and creates no second row.
func TestRepoAddIsIdempotent(t *testing.T) {
	paths := newRepoHome(t)
	repo := newRepoFixture(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitOK {
		t.Fatalf("first add exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	first := listRepos(t, paths)
	if len(first) != 1 {
		t.Fatalf("after the first add there are %d rows, want 1", len(first))
	}

	// Change every mutable field the second time round, including the team.
	repo.WriteFile("CLAUDE.md", "# acme-api\n\n<!-- aiteam:team=swift-apple -->\n")
	repo.Commit("change the team")
	stdout.Reset()
	stderr.Reset()

	code := router.Run(t.Context(), []string{
		"repo", "add", repo.Dir,
		"--base", "develop", "--worktrees", "/srv/worktrees", "--prefix", "bots",
	})
	if code != ExitOK {
		t.Fatalf("second add exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	second := listRepos(t, paths)
	if len(second) != 1 {
		t.Fatalf("after the second add there are %d rows, want 1 (no duplicate)", len(second))
	}
	got := second[0]
	switch {
	case got.ID != first[0].ID:
		t.Errorf("row id changed from %d to %d; the upsert replaced rather than updated", first[0].ID, got.ID)
	case got.BaseBranch != "develop":
		t.Errorf("base branch = %q, want %q", got.BaseBranch, "develop")
	case got.WorktreeRoot != "/srv/worktrees":
		t.Errorf("worktree root = %q, want %q", got.WorktreeRoot, "/srv/worktrees")
	case got.FeaturePrefix != "bots":
		t.Errorf("feature prefix = %q, want %q", got.FeaturePrefix, "bots")
	case got.AiteamTeam != "swift-apple":
		t.Errorf("aiteam team = %q, want %q", got.AiteamTeam, "swift-apple")
	}
	if !strings.Contains(stdout.String(), "updated") {
		t.Errorf("stdout does not say the registration was updated:\n%s", stdout)
	}
}

// TestRepoAddWithoutAiteamIsRefused is AC-1.4: exit 3, the message names
// `npx aiteam`, and nothing is registered.
func TestRepoAddWithoutAiteamIsRefused(t *testing.T) {
	paths := newRepoHome(t)
	repo := gittest.New(t)
	repo.SetOriginURL(repoTestRemoteURL)
	router, _, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitPrecondition {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr.String(), "npx aiteam") {
		t.Errorf("stderr does not name `npx aiteam`:\n%s", stderr)
	}
	if list := listRepos(t, paths); len(list) != 0 {
		t.Errorf("registered %d repositories, want 0", len(list))
	}
}

// TestRepoAddWarnsWhenTheTeamIsUnknown is AC-1.5: an undetectable team is a
// warning and a null column, and the repository is registered anyway.
func TestRepoAddWarnsWhenTheTeamIsUnknown(t *testing.T) {
	paths := newRepoHome(t)
	repo := gittest.New(t)
	if err := os.MkdirAll(filepath.Join(repo.Dir, ".aiteam"), 0o700); err != nil {
		t.Fatalf("create .aiteam: %v", err)
	}
	repo.SetOriginURL(repoTestRemoteURL)
	router, _, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("stderr carries no warning:\n%s", stderr)
	}
	list := listRepos(t, paths)
	if len(list) != 1 {
		t.Fatalf("registered %d repositories, want 1 — an unknown team must not block registration", len(list))
	}
	if list[0].AiteamTeam != "" {
		t.Errorf("aiteam team = %q, want empty (SQL NULL)", list[0].AiteamTeam)
	}
}

// TestRepoAddRejections covers the failure paths that reach the user as an
// exit code and a message.
func TestRepoAddRejections(t *testing.T) {
	tests := []struct {
		name     string
		args     func(t *testing.T) []string
		wantCode int
		wantHas  []string
	}{
		{
			name: "a non-GitHub remote",
			args: func(t *testing.T) []string {
				repo := newRepoFixture(t)
				repo.SetOriginURL("git@gitlab.com:acme/acme-api.git")
				return []string{"repo", "add", repo.Dir}
			},
			wantCode: ExitPrecondition,
			wantHas:  []string{"supported forge", "only GitHub is supported"},
		},
		{
			name: "no origin remote",
			args: func(t *testing.T) []string {
				repo := newRepoFixture(t)
				repo.RemoveOrigin()
				return []string{"repo", "add", repo.Dir}
			},
			wantCode: ExitPrecondition,
			wantHas:  []string{"origin remote"},
		},
		{
			name: "a directory that is not a repository",
			args: func(t *testing.T) []string {
				return []string{"repo", "add", t.TempDir()}
			},
			wantCode: ExitPrecondition,
			wantHas:  []string{"git repository"},
		},
		{
			name: "a path that does not exist",
			args: func(t *testing.T) []string {
				return []string{"repo", "add", filepath.Join(t.TempDir(), "absent")}
			},
			wantCode: ExitPrecondition,
			wantHas:  []string{"path exists"},
		},
		{
			name: "no base branch anywhere",
			args: func(t *testing.T) []string {
				repo := newRepoFixture(t)
				repo.Git("remote", "set-head", "origin", "--delete")
				return []string{"repo", "add", repo.Dir}
			},
			wantCode: ExitPrecondition,
			wantHas:  []string{"base branch", "--base"},
		},
		{
			name: "a --base git would not accept",
			args: func(t *testing.T) []string {
				repo := newRepoFixture(t)
				return []string{"repo", "add", repo.Dir, "--base", "--upload-pack=touch"}
			},
			wantCode: ExitUsage,
			wantHas:  []string{"repo add <path>"},
		},
		{
			name: "a missing positional argument",
			args: func(*testing.T) []string {
				return []string{"repo", "add"}
			},
			wantCode: ExitUsage,
			wantHas:  []string{"missing argument", "<path>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := newRepoHome(t)
			args := tt.args(t)
			router, _, stderr := newTestRouter(t)

			if code := router.Run(t.Context(), args); code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, tt.wantCode, stderr)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr does not mention %q:\n%s", want, stderr)
				}
			}
			if list := listRepos(t, paths); len(list) != 0 {
				t.Errorf("registered %d repositories, want 0", len(list))
			}
		})
	}
}

// TestRepoAddRequiresInit checks the bootstrap precondition: neither repo
// command may manufacture a $LF_HOME that `init` has not created.
func TestRepoAddRequiresInit(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	repo := newRepoFixture(t)
	router, _, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitPrecondition {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr.String(), "litefactory init") {
		t.Errorf("stderr does not tell the user to run init:\n%s", stderr)
	}
}

// TestRepoAddLeavesTheCloneUntouched is AC-1.7, driven through the command
// rather than through internal/repos, so it also covers everything the command
// layer does around registration.
//
// The fixture is dirtied first on purpose: against a clean checkout this
// assertion is trivially satisfiable and would not notice a wiped index.
func TestRepoAddLeavesTheCloneUntouched(t *testing.T) {
	newRepoHome(t)
	repo := newRepoFixture(t)
	repo.Dirty()
	router, _, stderr := newTestRouter(t)

	if before := gittest.Capture(t, repo.Dir); before.Status == "" {
		t.Fatalf("the fixture is clean; this assertion would prove nothing")
	}
	gittest.AssertUnchanged(t, repo.Dir, func() {
		if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitOK {
			t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
		}
	})
}

// TestRepoListRendersEveryColumn is AC-1.9, including the one value that is
// not a timestamp: a repository that has never been fetched prints "never".
func TestRepoListRendersEveryColumn(t *testing.T) {
	newRepoHome(t)
	repo := newRepoFixture(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir}); code != ExitOK {
		t.Fatalf("repo add exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	stdout.Reset()

	if code := router.Run(t.Context(), []string{"repo", "list"}); code != ExitOK {
		t.Fatalf("repo list exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	wantRoot := filepath.Join(filepath.Dir(repo.Dir), ".lf-worktrees", filepath.Base(repo.Dir))
	for _, want := range []string{
		repo.Dir,
		"github:acme/does-not-exist",
		gittest.DefaultBranch,
		"go-backend",
		wantRoot,
		"never",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("repo list output does not contain %q:\n%s", want, stdout)
		}
	}
}

// TestRepoListWithNothingRegistered checks the empty case says what to do
// next rather than printing a bare header.
func TestRepoListWithNothingRegistered(t *testing.T) {
	newRepoHome(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"repo", "list"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "repo add") {
		t.Errorf("empty listing does not say how to add one:\n%s", stdout)
	}
}
