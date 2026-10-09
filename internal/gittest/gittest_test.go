package gittest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestNewBuildsAWorkingFixture(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)

	if repo.Branch() != gittest.DefaultBranch {
		t.Errorf("fixture is on branch %q, want %q", repo.Branch(), gittest.DefaultBranch)
	}
	if repo.Head() == "" {
		t.Error("fixture has no commit")
	}
	if got := repo.Git("status", "--porcelain"); got != "" {
		t.Errorf("a new fixture is not clean: %q", got)
	}
	if got := repo.Git("remote", "get-url", "origin"); got != repo.Origin {
		t.Errorf("origin = %q, want %q", got, repo.Origin)
	}
	// A pushed upstream and a resolved origin/HEAD are what base-branch
	// resolution and worktree creation need from a fixture.
	if got := repo.Git("rev-parse", "--abbrev-ref", "origin/HEAD"); got != "origin/"+gittest.DefaultBranch {
		t.Errorf("origin/HEAD = %q, want origin/%s", got, gittest.DefaultBranch)
	}
	if got := repo.Git("rev-parse", "origin/"+gittest.DefaultBranch); got != repo.Head() {
		t.Errorf("origin/%s = %q, want %q", gittest.DefaultBranch, got, repo.Head())
	}

	// The fixture directory must already be canonical, so a test can compare
	// it directly with the path git reports.
	resolved, err := filepath.EvalSymlinks(repo.Dir)
	if err != nil {
		t.Fatalf("resolve fixture directory: %v", err)
	}
	if resolved != repo.Dir {
		t.Errorf("fixture directory %q is not symlink-resolved (%q)", repo.Dir, resolved)
	}
}

func TestFixtureIgnoresAmbientGitConfiguration(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)

	// The identity comes from the fixture environment, not from whatever the
	// developer has in ~/.gitconfig.
	if got := repo.Git("log", "-1", "--format=%an <%ae>"); !strings.Contains(got, "@litefactory.invalid") {
		t.Errorf("commit author = %q, want the fixture identity", got)
	}
	if got := repo.Git("config", "--default", "", "--get", "user.email"); got != "" {
		t.Errorf("fixture inherited user.email = %q from ambient configuration", got)
	}
}

func TestDirtyTouchesAllThreeStates(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.Dirty()

	status := repo.Git("status", "--porcelain")
	for _, want := range []string{"A  staged.txt", " M README.md", "?? untracked.txt"} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q does not contain %q", status, want)
		}
	}
}

// TestAssertUnchangedDetectsEachKindOfChange is the test of the test helper. A
// clone-untouched assertion that cannot fail is worse than no assertion at all,
// so each of the three things it watches is broken on purpose and the helper
// has to notice.
func TestAssertUnchangedDetectsEachKindOfChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*gittest.Repo)
		want   string
	}{
		{
			name:   "a new commit moves HEAD",
			change: func(r *gittest.Repo) { r.Git("commit", "--quiet", "--allow-empty", "-m", "sneaky") },
			want:   "git rev-parse HEAD",
		},
		{
			name:   "switching branch",
			change: func(r *gittest.Repo) { r.Git("switch", "--quiet", "--create", "elsewhere") },
			want:   "git rev-parse --abbrev-ref HEAD",
		},
		{
			name:   "staging a change",
			change: func(r *gittest.Repo) { r.Git("add", "--all") },
			want:   "git status --porcelain",
		},
		{
			name:   "discarding an uncommitted change",
			change: func(r *gittest.Repo) { r.Git("checkout", "--", "README.md") },
			want:   "git status --porcelain",
		},
		{
			name:   "writing a file",
			change: func(r *gittest.Repo) { r.WriteFile("intruder.txt", "written\n") },
			want:   "git status --porcelain",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			repo.Dirty()

			before := gittest.Capture(t, repo.Dir)
			tc.change(repo)
			after := gittest.Capture(t, repo.Dir)

			diff := before.Diff(after)
			if diff == "" {
				t.Fatalf("the snapshot did not notice %s", tc.name)
			}
			if !strings.Contains(diff, tc.want) {
				t.Errorf("diff\n%s\ndoes not mention %q", diff, tc.want)
			}
			if before == after {
				t.Error("snapshots compare equal despite the repository having changed")
			}
		})
	}
}

func TestAssertUnchangedPassesForReadOnlyWork(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.Dirty()

	gittest.AssertUnchanged(t, repo.Dir, func() {
		repo.Git("status", "--porcelain")
		repo.Git("log", "--oneline")
		repo.Git("fetch", "--prune", "origin")
	})
}

func TestSetOriginURLAndRemoveOrigin(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.SetOriginURL("git@github.com:acme/api.git")
	if got := repo.Git("remote", "get-url", "origin"); got != "git@github.com:acme/api.git" {
		t.Errorf("origin = %q, want the GitHub URL", got)
	}

	repo.RemoveOrigin()
	if got := repo.Git("remote"); got != "" {
		t.Errorf("remotes = %q, want none", got)
	}
}

func TestFixtureIsRemovedWithTheTest(t *testing.T) {
	t.Parallel()

	var dir string
	t.Run("inner", func(t *testing.T) {
		dir = gittest.New(t).Dir
	})
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("fixture %s outlived the test that created it", dir)
	}
}

func TestFixturesRemainIndependentAfterCleanup(t *testing.T) {
	var first string
	t.Run("first", func(t *testing.T) {
		r := gittest.New(t)
		first = r.Dir
		r.WriteFile("README.md", "first changed\n")
		r.Commit("change first")
		r.Git("push", "origin", "main")
	})
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first fixture still exists: %v", err)
	}
	a, b := gittest.New(t), gittest.New(t)
	initial := b.Head()
	a.WriteFile("README.md", "a changed\n")
	changed := a.Commit("change a")
	a.Git("push", "origin", "main")
	b.Git("fetch", "origin")
	if b.Head() != initial || b.Git("rev-parse", "origin/main") != initial {
		t.Fatal("fixtures share an origin or clone")
	}
	if b.Git("status", "--porcelain") != "" {
		t.Fatal("later fixture is dirty")
	}
	if b.Git("rev-parse", "--abbrev-ref", "@{upstream}") != "origin/main" {
		t.Fatal("upstream missing")
	}
	b.WriteFile("other.txt", "b changed\n")
	b.Commit("change b")
	b.Git("push", "origin", "main")
	a.Git("fetch", "origin")
	if a.Git("rev-parse", "origin/main") != changed {
		t.Fatal("second push changed first origin")
	}
	wt := filepath.Join(t.TempDir(), "worktree")
	a.Git("worktree", "add", "--detach", wt, "origin/main")
	if got := a.GitIn(wt, "rev-parse", "HEAD"); got != changed {
		t.Fatalf("worktree HEAD = %s", got)
	}
}
