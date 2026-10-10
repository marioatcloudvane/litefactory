package git_test

import (
	"bytes"
	"github.com/cloudvane/litefactory/internal/gittest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStackOverridesUsesExactCommitAndPreservesDirtyFiles(t *testing.T) {
	repo := gittest.New(t)
	raw := "{\"version\":1}\n"
	repo.WriteFile(".litefactory/stack.overrides.json", raw)
	commit := repo.Commit("override")
	clone := openFixture(t, repo)
	repo.WriteFile(".litefactory/stack.overrides.json", "dirty canary")
	before := repo.Git("status", "--porcelain=v1")
	got, err := clone.ReadStackOverrides(t.Context(), commit)
	if err != nil || !bytes.Equal(got, []byte(raw)) {
		t.Fatal("did not read admitted blob", err)
	}
	if after := repo.Git("status", "--porcelain=v1"); after != before {
		t.Fatal("read changed repository state")
	}
	dirty, err := os.ReadFile(filepath.Join(repo.Dir, ".litefactory/stack.overrides.json"))
	if err != nil || string(dirty) != "dirty canary" {
		t.Fatal("dirty file rewritten")
	}
	if _, err = clone.ReadStackOverrides(t.Context(), "HEAD"); err == nil {
		t.Fatal("mutable revision accepted")
	}
}
func TestReadStackOverridesRejectsUnsafeOrOversizedCommitEntries(t *testing.T) {
	for _, kind := range []string{"symlink directory", "symlink file", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			repo := gittest.New(t)
			switch kind {
			case "symlink directory":
				if err := os.Symlink("outside", filepath.Join(repo.Dir, ".litefactory")); err != nil {
					t.Fatal(err)
				}
			case "symlink file":
				if err := os.Mkdir(filepath.Join(repo.Dir, ".litefactory"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("outside", filepath.Join(repo.Dir, ".litefactory/stack.overrides.json")); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				repo.WriteFile(".litefactory/stack.overrides.json", strings.Repeat("x", (1<<20)+1))
			}
			commit := repo.Commit("unsafe override")
			if _, err := openFixture(t, repo).ReadStackOverrides(t.Context(), commit); err == nil {
				t.Fatal("unsafe committed override accepted")
			}
		})
	}
}
func TestReadStackOverridesDistinguishesAbsentAndEmpty(t *testing.T) {
	repo := gittest.New(t)
	clone := openFixture(t, repo)
	absent := repo.Git("rev-parse", "HEAD")
	got, err := clone.ReadStackOverrides(t.Context(), absent)
	if err != nil || got != nil {
		t.Fatal("absence was not preserved", err)
	}
	repo.WriteFile(".litefactory/stack.overrides.json", "")
	empty := repo.Commit("empty override")
	got, err = clone.ReadStackOverrides(t.Context(), empty)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatal("empty blob treated as absent", err)
	}
}
