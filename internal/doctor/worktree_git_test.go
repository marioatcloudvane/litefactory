package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorWorktreeHandlesUseVerifiedGitChangesWithoutIndexWrites(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("deleted.txt", "tracked original")
	repo.WriteFile("clean.txt", "clean unrelated source")
	repo.Commit("fixture tracked data")
	repo.Git("checkout", "-b", "lf/406-case")
	repo.WriteFile("changed.go", "package issue\n")
	repo.WriteFile("AGENTS.md", "untrusted authority")
	if err := os.Remove(filepath.Join(repo.Dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ref := naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "api", Issue: 406}
	evidence := store.DoctorEvidence{TaskRef: ref.String()}
	gittest.AssertUnchanged(t, repo.Dir, func() {
		observed, err := binary.ObserveDoctor(t.Context(), repo.Dir, "lf/406-case")
		if err != nil {
			t.Fatal(err)
		}
		(&Service{}).collectWorktreeFiles(t.Context(), ref, store.DoctorLocalSources{WorktreePath: repo.Dir, Branch: "lf/406-case"}, observed.Status, &evidence)
	})
	records := map[string]store.DoctorEvidenceRecord{}
	for _, r := range evidence.Records {
		records[r.ID] = r
	}
	if len(records) != 2 || records["changed.go"].State == "" || records["deleted.txt"].State != absentFile {
		t.Fatalf("wrong changed/deleted issue evidence: %+v", evidence)
	}
	if _, err = binary.ObserveDoctor(t.Context(), repo.Dir, "lf/407-other"); err == nil {
		t.Fatal("foreign branch accepted")
	}
}
