package teampackage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestCloneIndependentOriginAndLocalLifecycle(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	original := installFixture(t, m, source, "1.0.0")
	before := snapshot(t, filepath.Dir(original.Path))
	m.validate = func(context.Context, fs.FS, map[string]string) error { return errors.New("must not capture") }
	clone, err := m.Clone(t.Context(), source, "example", "1.0.0", "custom", "2.0.0+local")
	if err != nil {
		t.Fatal(err)
	}
	if !clone.Editable || clone.Identity.Source != "local" || !reflect.DeepEqual(relativeSnapshot(t, original.Path), relativeSnapshot(t, clone.Path)) {
		t.Fatal("copy differs")
	}
	if clone.Origin.Source != source || clone.Origin.Checksum != original.Identity.Digest || clone.Origin.Slug != "example" || clone.Origin.Version != "1.0.0" || clone.Origin.ClonedAt.IsZero() {
		t.Fatalf("origin=%+v", clone.Origin)
	}
	receiptPath := filepath.Join(filepath.Dir(clone.Path), "receipt.json")
	receiptBefore, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone.Path, "research/flow.dot"), []byte(`digraph { team="builtin"; }`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(clone.Path, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	inspected, err := m.Inspect(t.Context(), "local", "custom", "2.0.0+local")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspected.Origin, clone.Origin) || inspected.Identity.Digest == clone.Identity.Digest {
		t.Fatal("edited origin or digest")
	}
	second, err := m.Clone(t.Context(), "local", "custom", "2.0.0+local", "second", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if second.Origin.Source != "local" || second.Origin.Slug != "custom" || second.Origin.Checksum != inspected.Identity.Digest {
		t.Fatal("immediate origin lost")
	}
	if _, err := os.Stat(filepath.Join(second.Path, "empty")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(original.Path))) {
		t.Fatal("source changed")
	}
	if _, _, err := m.Update(t.Context(), "local", "custom"); err == nil {
		t.Fatal("updated local")
	}
	if removed, err := m.Remove(t.Context(), source, "example", "1.0.0"); !removed || err != nil {
		t.Fatal(err)
	}
	receiptAfter, err := os.ReadFile(receiptPath)
	if err != nil || string(receiptBefore) != string(receiptAfter) {
		t.Fatal("origin history changed")
	}
	all, err := m.List(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("list=%v %v", all, err)
	}
	if removed, err := m.Remove(t.Context(), "local", "custom", "2.0.0+local"); !removed || err != nil {
		t.Fatal(err)
	}
	if _, err := m.Inspect(t.Context(), "local", "second", "1.0.0"); err != nil {
		t.Fatal(err)
	}
}

func TestCloneRefusalsAndExactSelection(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing", "ambiguous", "bad name", "bad version", "fetched damage", "collision", "local config", "unsafe", "symlink", "special", "count", "file size", "total size"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			_, server := newFixture(t, "1.0.0")
			m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
			source := server.URL + "/teams/teams.json"
			original := installFixture(t, m, source, "1.0.0")
			slug, ver, newSlug, newVer := "example", "1.0.0", "custom", "1.0.0"
			want := ErrInvalidPackage
			switch scenario {
			case "missing":
				slug = "missing"
				want = ErrNotFound
			case "ambiguous":
				_, other := newFixture(t, "1.0.0")
				m.client.Transport = other.Client().Transport
				installFixture(t, m, other.URL+"/teams/teams.json", "1.0.0")
				if _, err := m.Clone(t.Context(), source, slug, ver, "exact", "1.0.0"); err != nil {
					t.Fatal(err)
				}
				source = ""
				want = nil
			case "bad name":
				newSlug = "../bad"
			case "bad version":
				newVer = "v1.0.0"
			case "fetched damage":
				if err := os.WriteFile(filepath.Join(original.Path, "research/flow.dot"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "collision":
				newSlug = "example"
				want = ErrConflict
			default:
				local, err := m.Clone(t.Context(), source, slug, ver, "editable", "1.0.0")
				if err != nil {
					t.Fatal(err)
				}
				source = "local"
				slug = "editable"
				target := filepath.Join(local.Path, "extra")
				switch scenario {
				case "local config":
					target = filepath.Join(local.Path, "team.config")
					if err := os.WriteFile(target, []byte("invalid: ../graph"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "unsafe":
					if err := os.WriteFile(filepath.Join(local.Path, "bad%name"), nil, 0o600); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(original.Path, target); err != nil {
						t.Fatal(err)
					}
				case "special":
					if err := syscall.Mkfifo(target, 0o600); err != nil {
						t.Fatal(err)
					}
				case "count":
					for i := 0; i < MaxFiles; i++ {
						if err := os.WriteFile(filepath.Join(local.Path, fmt.Sprintf("extra-%d", i)), nil, 0o600); err != nil {
							t.Fatal(err)
						}
					}
				case "file size":
					if err := os.WriteFile(target, make([]byte, teamsource.MaxSourceBytes+1), 0o600); err != nil {
						t.Fatal(err)
					}
				case "total size":
					for i := 0; i < 65; i++ {
						if err := os.WriteFile(filepath.Join(local.Path, fmt.Sprintf("extra-%d", i)), make([]byte, teamsource.MaxSourceBytes), 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			before := snapshot(t, filepath.Dir(original.Path))
			_, err := m.Clone(t.Context(), source, slug, ver, newSlug, newVer)
			if err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("refusal=%v want %v", err, want)
			}
			if scenario == "ambiguous" && !strings.Contains(err.Error(), "--source") {
				t.Fatal(err)
			}
			if scenario == "collision" && !strings.Contains(err.Error(), "example") {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(original.Path))) {
				t.Fatal("source changed")
			}
			if _, err := os.Lstat(m.destination("local", newSlug, newVer)); err == nil {
				t.Fatal("destination visible")
			}
		})
	}
}

func TestCloneFaultsAndCancellationCleanStaging(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"copy", "receipt", "publish", "cancel", "pre-cancel"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			_, server := newFixture(t, "1.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			original := installFixture(t, m, source, "1.0.0")
			before := snapshot(t, filepath.Dir(original.Path))
			fault := errors.New("injected clone failure")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			m.writeClone = func(name string, raw []byte) error {
				if phase == "copy" && filepath.Base(name) != "receipt.json" || phase == "receipt" && filepath.Base(name) == "receipt.json" {
					return fault
				}
				if phase == "cancel" {
					cancel()
				}
				return writeStagedFile(name, raw)
			}
			if phase == "publish" {
				m.rename = func(string, string) error { return fault }
			}
			if phase == "pre-cancel" {
				cancel()
			}
			_, err := m.Clone(ctx, source, "example", "1.0.0", "custom", "1.0.0")
			want := fault
			if phase == "cancel" || phase == "pre-cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want %v", err, want)
			}
			if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(original.Path))) {
				t.Fatal("source changed")
			}
			entries, err := m.List(t.Context())
			if err != nil || len(entries) != 1 {
				t.Fatalf("partial clone %v %v", entries, err)
			}
			stages, err := os.ReadDir(p.TeamsStagingDir())
			if err != nil || len(stages) != 0 {
				t.Fatalf("staging=%v %v", stages, err)
			}
		})
	}
}

func relativeSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for p, raw := range snapshot(t, root) {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		result[rel] = raw
	}
	return result
}
