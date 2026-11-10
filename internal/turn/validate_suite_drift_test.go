package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

// driftRepo commits recipe into a real repository and returns a suite
// repository that checks the commit out in a scratch worktree.
func driftRepo(t *testing.T, recipe string) (GitValidateSuiteRepo, string) {
	t.Helper()
	repo := gittest.New(t)
	repo.WriteFile(".litefactory/bug-checks.yaml", recipe)
	commit := repo.Commit("add recipe")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	scratchRoot := config.NewPaths(filepath.Join(t.TempDir(), "home")).ValidateScratchDir()
	return GitValidateSuiteRepo{Clone: clone, ClonePath: repo.Dir, Remote: "origin", ScratchRoot: scratchRoot}, commit
}

// writesReport wraps a runner so that each command also writes a report file
// into the checked-out source tree, as `--junitxml=settings-groups.xml` does.
func writesReport(t *testing.T, inner ValidateSuiteRunner) ValidateSuiteRunner {
	return func(ctx context.Context, w string, c config.BugCheckCommand, s func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		if err := os.WriteFile(filepath.Join(w, "settings-groups.xml"), []byte("<testsuite/>"), 0o600); err != nil {
			t.Fatal(err)
		}
		return inner(ctx, w, c, s)
	}
}

// A suite that writes into the tested tree blocks, and its reason names the
// path it wrote so the owner and the reporter see a defect of the tested code.
func TestValidateSuiteDriftNamesChangedPaths(t *testing.T) {
	f := newSuiteFixture(t)
	repo, commit := driftRepo(t, suiteRecipe)
	f.b.Repo = repo
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: commit}, nil
	}
	f.b.Run = writesReport(t, f.b.Run)
	res, out, err := f.exec(31)
	if err != nil || res.Outcome != store.SuiteBlocked {
		t.Fatalf("got %q %+v %v", res.Outcome, out, err)
	}
	if !strings.Contains(out.Reason, "settings-groups.xml") {
		t.Fatalf("drift reason does not name the written path: %q", out.Reason)
	}
	t.Log(out.Reason)
}

func TestValidateCoverageDriftNamesChangedPaths(t *testing.T) {
	b, scratch := coverageGuardFixture(t)
	repo, commit := driftRepo(t, string(scratch.recipe))
	if commit == "" {
		t.Fatal("no commit")
	}
	b.Repo = repo
	request, err := b.DB.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "drift", SourceKey: "request", Policy: "full", Commit: commit})
	if err != nil {
		t.Fatal(err)
	}
	b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: commit, RequestID: request.ID, Lineage: "drift", Policy: "full", AssessmentVersion: coverage.Version}, nil
	}
	b.Run = writesReport(t, b.Run)
	res, out, err := guardExecute(t, b, 731)
	if err != nil || res.Outcome != "blocked" {
		t.Fatalf("got %q %+v %v", res.Outcome, out, err)
	}
	if !strings.Contains(out.Reason, "settings-groups.xml") {
		t.Fatalf("drift reason does not name the written path: %q", out.Reason)
	}
	t.Log(out.Reason)
}
