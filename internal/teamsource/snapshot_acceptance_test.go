package teamsource

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSnapshotModeIsolationIgnoresUnselectedSources(t *testing.T) {
	t.Parallel()
	root := centralFixture(t)
	before, err := CaptureRoot(t.Context(), root, "go-team", "research")
	if err != nil {
		t.Fatal(err)
	}
	// A broken implementation dependency must not enter a research snapshot.
	if err := os.WriteFile(filepath.Join(root, "skills/go-constraints/SKILL.md"), []byte("invalid skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "agents/shared/implement-orchestrator.md")); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureRoot(t.Context(), root, "go-team", "research")
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest != after.Digest || !reflect.DeepEqual(before.Files, after.Files) {
		t.Fatal("unselected sources changed research content or digest")
	}
	for p := range after.Files {
		if strings.HasPrefix(p, "skills/") || strings.Contains(p, "implement") {
			t.Fatalf("unselected source included: %s", p)
		}
	}
}

func TestSnapshotRejectsTransitiveResourceEscape(t *testing.T) {
	for _, kind := range []string{"relative", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := centralFixture(t)
			outside := filepath.Join(t.TempDir(), "resource.md")
			if err := os.WriteFile(outside, []byte("external resource"), 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "relative" {
				skill := filepath.Join(root, "skills/go-constraints/SKILL.md")
				rel, err := filepath.Rel(filepath.Dir(skill), outside)
				if err != nil {
					t.Fatal(err)
				}
				body, err := os.ReadFile(skill)
				if err != nil {
					t.Fatal(err)
				}
				body = []byte(strings.Replace(string(body), "references/checklist.md", filepath.ToSlash(rel), 1))
				if err := os.WriteFile(skill, body, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				resource := filepath.Join(root, "skills/go-constraints/references/checklist.md")
				if err := os.Remove(resource); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, resource); err != nil {
					t.Fatal(err)
				}
			}
			if snapshot, err := CaptureRoot(t.Context(), root, "go-team", "implement"); err == nil {
				t.Fatalf("accepted escaped resource: %+v", snapshot)
			}
		})
	}
}
