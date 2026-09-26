package git_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestBugRepairSnapshotRetainsImmutableDiffAndRejectsDrift(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("lookup.go", "package lookup\n// old normalization\n")
	base := repo.Commit("baseline")
	baseTree := repo.Git("rev-parse", "HEAD^{tree}")
	repo.WriteFile("lookup.go", "package lookup\n// corrected normalization\n")
	repo.WriteFile("lookup_test.go", "package lookup\n// regression fixture\n")
	fixed := repo.Commit("repair fixture")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	branch, dir := task(t, worktreeRoot(repo), 492, "repair snapshot")
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	repo.GitIn(dir, "checkout", "--detach", fixed)
	snapshot, err := result.Worktree.ObserveBugRepair(t.Context(), base, baseTree, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Paths, []string{"lookup.go", "lookup_test.go"}) || snapshot.Commit != fixed || snapshot.BaseCommit != base || !strings.Contains(snapshot.Diff, "corrected normalization") {
		t.Fatalf("incomplete snapshot: %+v", snapshot)
	}
	digest := sha256.Sum256([]byte(snapshot.Diff))
	if snapshot.DiffDigest != hex.EncodeToString(digest[:]) {
		t.Fatal("diff digest differs")
	}
	replay, err := result.Worktree.ObserveBugRepair(t.Context(), base, baseTree, fixed)
	if err != nil || !reflect.DeepEqual(snapshot, replay) {
		t.Fatalf("snapshot changed: %+v %v", replay, err)
	}
	if _, err = result.Worktree.ObserveBugRepair(t.Context(), base, strings.Repeat("a", 40), fixed); err == nil {
		t.Fatal("wrong baseline tree accepted")
	}
	file := filepath.Join(dir, "lookup.go")
	if err = os.WriteFile(file, []byte("retained partial edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = result.Worktree.ObserveBugRepair(t.Context(), base, baseTree, fixed); err == nil {
		t.Fatal("dirty source certified immutable")
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "retained partial edits" {
		t.Fatalf("observation discarded partial work: %q %v", got, err)
	}
}
