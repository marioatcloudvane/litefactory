package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestPushHeadToLostResponsePreservesPublicationUncertainty(t *testing.T) {
	t.Parallel()
	for _, descendant := range []bool{false, true} {
		t.Run(fmt.Sprintf("descendant=%t", descendant), func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			repo.Git("branch", "feature")
			repo.Git("push", "origin", "feature")
			expected := repo.Git("rev-parse", "HEAD")
			binary, err := Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			clone, err := binary.Open(t.Context(), repo.Dir)
			if err != nil {
				t.Fatal(err)
			}
			wt, err := clone.WorktreeAdd(t.Context(), filepath.Join(t.TempDir(), "retained"), WorktreeAddOptions{Branch: "feature"})
			if err != nil {
				t.Fatal(err)
			}
			repo.GitIn(wt.Path(), "commit", "--allow-empty", "-m", "sync merge")
			proposed := repo.GitIn(wt.Path(), "rev-parse", "HEAD")
			if strings.Contains(binary.Path(), "'") {
				t.Fatal("unsupported quoted git path")
			}
			wrapper := filepath.Join(t.TempDir(), "git")
			script := "#!/bin/sh\nREAL='" + binary.Path() + "'\nif [ \"$1\" != push ]; then exec \"$REAL\" \"$@\"; fi\n\"$REAL\" \"$@\" || exit $?\n"
			if descendant {
				script += "next=$(printf 'concurrent advance\\n' | \"$REAL\" -c user.name=Test -c user.email=test@example.invalid commit-tree HEAD^{tree} -p HEAD) || exit $?\n\"$REAL\" push origin \"$next:refs/heads/feature\" || exit $?\n"
			}
			script += "echo 'push response lost' >&2\nexit 1\n"
			if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			wt.runner.binary = wrapper
			err = wt.PushHeadTo(t.Context(), "origin", "feature", expected)
			var push *PushError
			if !errors.As(err, &push) || errors.Is(err, ErrIntegrationHeadMoved) {
				t.Fatalf("lost reply classified as no publication: %v", err)
			}
			remote := strings.Fields(repo.Git("ls-remote", "origin", "refs/heads/feature"))[0]
			if !descendant && remote != proposed {
				t.Fatal("push fixture did not publish proposed commit")
			}
			if ancestor := repo.Git("merge-base", proposed, remote); ancestor != proposed {
				t.Fatal("remote did not retain published commit")
			}
		})
	}
}
