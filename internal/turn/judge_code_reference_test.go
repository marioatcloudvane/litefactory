package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestJudgeCodeReferenceDistinguishesCommittedAndLocalBytes(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("clean.go", "committed clean")
	repo.WriteFile("dirty.go", "committed dirty")
	commit := repo.Commit("code reference fixture")
	repo.WriteFile("dirty.go", "local change")
	repo.WriteFile("new.go", "untracked change")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureJudgeEvidence(t.Context(), binary, repo.Dir, "pinned source", nil)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, e := range snapshot.Evidence {
		refs[e.ID] = e.Reference
	}
	if refs["code:clean.go"] != commit+":clean.go" {
		t.Fatalf("clean bytes lack committed ref: %s", refs["code:clean.go"])
	}
	for _, id := range []string{"code:dirty.go", "code:new.go"} {
		if !strings.HasPrefix(refs[id], "local-snapshot:"+snapshot.LocalDigest+":") {
			t.Fatalf("local bytes claim committed ref: %s", refs[id])
		}
	}
}
