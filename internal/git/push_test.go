package git_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// fakeGitScript is a git that delivers the push and gets the upstream wrong,
// which is precisely what the real git was measured doing under concurrency: at
// parallelism 4 on git 2.50.1, 240 concurrent "push --set-upstream" runs from
// one clone exited 0 every time and put all 240 branches on the remote, while
// 89 printed "error: could not lock config file .git/config: File exists" and
// 60 left no branch.<name>.remote entry behind. git loses the race for the
// config lock, skips the write and exits 0 anyway.
//
// Losing that race on demand is not something real git can be asked to do, so
// the effect is reproduced rather than the race: every command but push is
// handed straight to the real git, and a push runs for real with
// --set-upstream removed, so the objects land and the exit status is 0 while
// the upstream write never happens. MODE then decides what state that leaves:
//
//	lost        nothing further — the push's config write simply did not happen
//	unset       branch.<name>.* removed outright, the AC-PAR-3.4 case
//	elsewhere   branch.<name>.remote pointing at a remote nobody pushed to
//
// No other git behaviour is simulated.
//
// The first %s is the absolute path of the real git, the second is MODE.
const fakeGitScript = `#!/bin/sh
set -e
REAL='%s'
MODE='%s'

if [ "$1" != "push" ]; then
	exec "$REAL" "$@"
fi
shift

# The refspec is the last argument; remember it before the list is rewritten.
for last; do :; done

# Keep every argument except --set-upstream, in order.
count=$#
i=0
while [ "$i" -lt "$count" ]; do
	arg="$1"
	shift
	if [ "$arg" != "--set-upstream" ]; then
		set -- "$@" "$arg"
	fi
	i=$((i + 1))
done

"$REAL" push "$@"

branch="${last%%:*}"
branch="${branch#refs/heads/}"
case "$MODE" in
	unset)
		"$REAL" config --unset-all "branch.$branch.remote" || true
		"$REAL" config --unset-all "branch.$branch.merge" || true
		;;
	elsewhere)
		"$REAL" config "branch.$branch.remote" "somewhere-else"
		;;
esac
exit 0
`

// installFakeGit puts the fake ahead of the real git on PATH for the rest of
// the test. Build the fixture repository first: gittest execs "git" by name
// too, and its own setup push has nothing to do with what is under test here.
func installFakeGit(t *testing.T, mode string) {
	t.Helper()

	found, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find the real git: %v", err)
	}
	actual, err := filepath.Abs(found)
	if err != nil {
		t.Fatalf("resolve %s: %v", found, err)
	}
	if strings.Contains(actual, "'") {
		t.Fatalf("a single quote in %q would break the fake's shell quoting", actual)
	}

	dir := t.TempDir()
	script := fmt.Sprintf(fakeGitScript, actual, mode)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatalf("write the fake git: %v", err)
	}
	// Setenv forbids t.Parallel in every test that calls this.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// recordedUpstream reads back the two keys that together are a branch's
// upstream. --default keeps an unset key an ordinary answer rather than the
// exit 1 the fixture helper would turn into a fatal error.
func recordedUpstream(t *testing.T, repo *gittest.Repo, branch string) (remote, merge string) {
	t.Helper()
	remote = repo.Git("config", "--default", "", "--get", "branch."+branch+".remote")
	merge = repo.Git("config", "--default", "", "--get", "branch."+branch+".merge")
	return remote, merge
}

// TestPushFailsWhenGitDoesNotRecordTheUpstream is AC-PAR-3.4.
//
// git exiting 0 is not evidence that --set-upstream wrote anything, so the exit
// code is not what the check reads. A branch that reached the remote with no
// upstream behind it — or with the base branch's upstream still on it, which is
// what "worktree add -b <branch> origin/<base>" leaves and what the push is
// supposed to correct — is a failure litefactory has to report, because the
// alternative is signing its name to a run report calling the push clean while
// "git pull" in that worktree would pull main into the task branch.
//
// The error must also be honest about which half failed: the objects are on the
// remote — measured, in all 240 runs — so the message must not send the user
// looking for work that is already published.
func TestPushFailsWhenGitDoesNotRecordTheUpstream(t *testing.T) {
	tests := []struct {
		name string
		mode string
		// key is the configuration key the error must name, so each case is
		// pinned to the fault it is actually about.
		key string
	}{
		{name: "the upstream write was lost", mode: "lost", key: ".merge"},
		{name: "no upstream entry at all", mode: "unset", key: ".remote"},
		{name: "an upstream naming another remote", mode: "elsewhere", key: ".remote"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := gittest.New(t)
			installFakeGit(t, tc.mode)

			worktree, dir := newTaskWorktree(t, repo, 42, "Push without an upstream")
			repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")
			result, err := worktree.Commit(t.Context(), commitOptions("Wrote a file.", 1))
			if err != nil {
				t.Fatalf("Commit: %v", err)
			}

			err = worktree.Push(t.Context(), git.DefaultRemote)
			if err == nil {
				t.Fatal("Push reported success for a branch whose upstream git never recorded")
			}
			if !errors.Is(err, git.ErrUpstreamNotRecorded) {
				t.Fatalf("error = %v, want it to unwrap to ErrUpstreamNotRecorded", err)
			}
			if want := "branch." + worktree.Branch() + tc.key; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}

			var pushErr *git.PushError
			if !errors.As(err, &pushErr) {
				t.Fatalf("error %v is not a *git.PushError", err)
			}
			if !pushErr.Published {
				t.Error("PushError.Published = false, but the push itself succeeded and the branch reached the remote")
			}
			if pushErr.Worktree != dir {
				t.Errorf("PushError.Worktree = %q, want the local worktree %q", pushErr.Worktree, dir)
			}
			if pushErr.Branch != worktree.Branch() {
				t.Errorf("PushError.Branch = %q, want %q", pushErr.Branch, worktree.Branch())
			}
			if !strings.Contains(err.Error(), dir) {
				t.Errorf("error %q does not name the worktree holding the commit", err)
			}
			// The message for a push that never reached the remote would be a
			// lie here, and an expensive one: it would send the user looking
			// for a branch that is already published.
			undelivered := fmt.Sprintf("push %s to %s failed", worktree.Branch(), git.DefaultRemote)
			if strings.Contains(err.Error(), undelivered) {
				t.Errorf("error %q claims the push failed outright; the objects did reach the remote", err)
			}

			// The fake has to have produced the measured shape, or none of the
			// assertions above prove anything: the commit is on the origin, and
			// the branch's recorded upstream is not the one the push was told
			// to set.
			published := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+worktree.Branch())
			if published != result.SHA {
				t.Errorf("origin has %q on %s, want the new commit %q — the fake did not deliver the push",
					published, worktree.Branch(), result.SHA)
			}
			remote, merge := recordedUpstream(t, repo, worktree.Branch())
			if remote == git.DefaultRemote && merge == "refs/heads/"+worktree.Branch() {
				t.Errorf("branch.%s.{remote,merge} = %q, %q — the fake recorded a correct upstream, so it reproduced nothing",
					worktree.Branch(), remote, merge)
			}
		})
	}
}

// TestPushConfirmsTheUpstreamItAskedGitToSet is the other half of AC-PAR-3.4,
// and the more important one to keep green: a check that reported a failure for
// every push would satisfy the criterion and destroy the product. A real git
// that does record the upstream must still be a clean push — including the
// second time, because a second run pushes the same branch again.
func TestPushConfirmsTheUpstreamItAskedGitToSet(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 43, "Push with an upstream")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	if _, err := worktree.Commit(t.Context(), commitOptions("Wrote a file.", 1)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := worktree.Push(t.Context(), git.DefaultRemote); err != nil {
		t.Fatalf("Push: %v", err)
	}

	wantMerge := "refs/heads/" + worktree.Branch()
	if remote, merge := recordedUpstream(t, repo, worktree.Branch()); remote != git.DefaultRemote || merge != wantMerge {
		t.Fatalf("branch.%s.{remote,merge} = %q, %q, want %q, %q",
			worktree.Branch(), remote, merge, git.DefaultRemote, wantMerge)
	}

	// A second run pushes the same branch again with nothing new to send. The
	// verification must not turn that into a failure.
	if err := worktree.Push(t.Context(), git.DefaultRemote); err != nil {
		t.Fatalf("second Push: %v", err)
	}
	if remote, merge := recordedUpstream(t, repo, worktree.Branch()); remote != git.DefaultRemote || merge != wantMerge {
		t.Errorf("branch.%s.{remote,merge} = %q, %q after the second push, want %q, %q",
			worktree.Branch(), remote, merge, git.DefaultRemote, wantMerge)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Errorf("the run's file is gone after the push: %v", err)
	}
}
