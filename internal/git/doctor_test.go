package git_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// TestObserveDoctorBranchHead pins the sync_branch before-state: a branch only
// behind its last local origin reference reports that remote head, while a
// branch with unpushed commits keeps its local head.
func TestObserveDoctorBranchHead(t *testing.T) {
	repo := gittest.New(t)
	const branch = "lf/648-case"
	repo.Git("checkout", "--quiet", "-b", branch)
	start := repo.Head()
	repo.WriteFile("story.txt", "story merge\n")
	remote := repo.Commit("story merge")
	repo.Git("push", "--quiet", "origin", branch)
	repo.Git("reset", "--quiet", "--hard", start)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	observed, err := binary.ObserveDoctor(t.Context(), repo.Dir, branch)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Head != start || observed.BranchHead != remote {
		t.Fatalf("behind-only observation %+v, want head %s branch head %s", observed, start, remote)
	}
	repo.WriteFile("local.txt", "unpushed\n")
	local := repo.Commit("unpushed local")
	observed, err = binary.ObserveDoctor(t.Context(), repo.Dir, branch)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Head != local || observed.BranchHead != local {
		t.Fatalf("unpushed observation %+v, want local head %s", observed, local)
	}
}
