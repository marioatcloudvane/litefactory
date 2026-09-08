package teamsource

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/team"
)

func centralFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	f := os.DirFS("../team/testdata/teams")
	err := fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, p), 0o700)
		}
		b, err := fs.ReadFile(f, p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, p), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func TestSnapshotEmbeddedModesAndClosure(t *testing.T) {
	root := centralFixture(t)
	for _, mode := range team.Modes() {
		s, err := CaptureRoot(t.Context(), root, "go-team", mode)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if mode == "spike" && (len(s.Agents) != 0 || len(s.Files) != 1) {
			t.Fatalf("root-only snapshot=%+v", s)
		}
		if mode == "implement" {
			for _, p := range []string{"skills/go-constraints/SKILL.md", "skills/go-constraints/references/checklist.md", "skills/workflow/SKILL.md"} {
				if len(s.Files[p]) == 0 {
					t.Fatalf("missing transitive resource %s", p)
				}
			}
			again, err := CaptureRoot(t.Context(), root, "go-team", mode)
			if err != nil {
				t.Fatal(err)
			}
			if s.Digest != again.Digest {
				t.Fatal("nondeterministic digest")
			}
			if err := os.WriteFile(filepath.Join(root, "skills/go-constraints/references/checklist.md"), []byte("changed resource"), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := CaptureRoot(t.Context(), root, "go-team", mode)
			if err != nil {
				t.Fatal(err)
			}
			if s.Digest == changed.Digest {
				t.Fatal("resource change omitted from digest")
			}
		}
	}
}
func TestSnapshotRejectsSources(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "escape", "oversized", "metadata", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			root := centralFixture(t)
			p := filepath.Join(root, "agents/shared/codebase-tour.md")
			switch kind {
			case "missing":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(p, 0o700); err != nil {
					t.Fatal(err)
				}
			case "escape":
				outside := filepath.Join(t.TempDir(), "outside.md")
				if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, p); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(p, []byte(strings.Repeat("a", int(MaxSourceBytes)+1)), 0o600); err != nil {
					t.Fatal(err)
				}
			case "metadata":
				if err := os.WriteFile(p, []byte("---\nname: wrong\ndescription: Wrong\ntier: balanced\n---\nBody"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "cycle":
				p = filepath.Join(root, "skills/workflow/SKILL.md")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				b = []byte(strings.Replace(string(b), "name: workflow", "name: workflow\nrequires: [../go-constraints/SKILL.md]", 1))
				if err := os.WriteFile(p, b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			mode := "research"
			if kind == "cycle" {
				mode = "implement"
			}
			if _, err := CaptureRoot(t.Context(), root, "go-team", mode); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
}
func TestSnapshotContainedAbsoluteAndSymlink(t *testing.T) {
	root := centralFixture(t)
	p := filepath.Join(root, "index.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "agents/shared/codebase-tour.md")
	alias := filepath.Join(root, "reader.md")
	if err := os.Symlink(original, alias); err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "agents/shared/codebase-tour.md", alias, 1))
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureRoot(t.Context(), root, "go-team", "research"); err != nil {
		t.Fatal(err)
	}
}
func TestCentralNoFallbackAndCatalogLock(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	if _, err := Resolve(t.Context(), paths); err == nil {
		t.Fatal("missing catalog silently fell back")
	}
	root := centralFixture(t)
	if err := team.WithCatalogLock(t.Context(), root, func() error {
		_, err := CaptureRoot(t.Context(), root, "go-team", "research")
		if err == nil {
			return errors.New("capture ignored management lock")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CaptureRoot(ctx, root, "go-team", "research"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
