package teampackage

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/cloudvane/litefactory/internal/config"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchedProfileMutationRefusesBeforeConsumer(t *testing.T) {
	for _, action := range []string{"tamper", "missing"} {
		t.Run(action, func(t *testing.T) {
			_, server := newFixture(t, "1.0.0")
			m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
			entry := installFixture(t, m, server.URL+"/teams/teams.json", "1.0.0")
			name := filepath.Join(entry.Path, config.StackProfilePath)
			var err error
			if action == "missing" {
				err = os.Remove(name)
			} else {
				err = os.WriteFile(name, []byte(`{"version":1,"environment":[{"name":"API_KEY","value":"private-canary"}]}`), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			starts := 0
			err = m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
				return a.WithSelected(t.Context(), "", "example", "1.0.0", func(Installed, fs.FS) error { starts++; return nil })
			})
			if err == nil || starts != 0 {
				t.Fatalf("admission err=%v starts=%d", err, starts)
			}
			if strings.Contains(err.Error(), "private-canary") {
				t.Fatal("diagnostic exposed sensitive profile value")
			}
		})
	}
}

func TestEditableProfileCaptureUsesFreshLocalBytes(t *testing.T) {
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	installFixture(t, m, source, "1.0.0")
	local, err := m.Clone(t.Context(), source, "example", "1.0.0", "custom", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	for _, id := range []string{"first", "second"} {
		raw := []byte(`{"version":1,"guidance":[{"id":"` + id + `","skill":"research/skills/fixture/SKILL.md"}]}`)
		if err = os.WriteFile(filepath.Join(local.Path, config.StackProfilePath), raw, 0600); err != nil {
			t.Fatal(err)
		}
		err = m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
			return a.WithSelected(t.Context(), "local", "custom", "2.0.0", func(entry Installed, files fs.FS) error {
				if !entry.Editable || entry.Integrity {
					t.Fatal("local capture represented as immutable fetched release")
				}
				got, e := fs.ReadFile(files, config.StackProfilePath)
				if e != nil {
					return e
				}
				if string(got) != string(raw) {
					t.Fatal("stale profile bytes")
				}
				resolved, e := config.ResolveStackProfile(got, nil)
				if e != nil {
					return e
				}
				if previous == resolved.EffectiveDigest {
					t.Fatal("local edit did not refresh capture digest")
				}
				previous = resolved.EffectiveDigest
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnknownProfilelessReleaseRefusesWithoutGoFallback(t *testing.T) {
	fixture, server := newFixture(t, "1.0.0")
	fixture.mu.Lock()
	r := &fixture.catalogue.Releases[0]
	kept := r.Files[:0]
	for _, file := range r.Files {
		if file.Path != config.StackProfilePath {
			kept = append(kept, file)
		}
	}
	r.Files = kept
	r.Checksum = digest(r.Files)
	delete(fixture.files, "/teams/"+r.Download+"/"+config.StackProfilePath)
	fixture.mu.Unlock()
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	if _, _, err := m.Install(t.Context(), server.URL+"/teams/teams.json", "example", "1.0.0"); err == nil {
		t.Fatal("unknown profileless release used legacy Go fallback")
	}
}

func TestLocalGuidanceOverrideCaptureIncludesOptionalSkillAndRejectsSymlink(t *testing.T) {
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	installFixture(t, m, source, "1.0.0")
	local, err := m.Clone(t.Context(), source, "example", "1.0.0", "custom", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	profile := []byte(`{"version":1,"guidance":[{"id":"fixture","skill":"research/skills/fixture/SKILL.md"}],"override_fields":["guidance"]}`)
	if err = os.WriteFile(filepath.Join(local.Path, config.StackProfilePath), profile, 0600); err != nil {
		t.Fatal(err)
	}
	optional := filepath.Join(local.Path, "research/skills/optional/SKILL.md")
	if err = os.MkdirAll(filepath.Dir(optional), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("---\nname: optional\ndescription: New local stack guidance\n---\nAdvice.")
	if err = os.WriteFile(optional, raw, 0600); err != nil {
		t.Fatal(err)
	}
	err = m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
		return a.WithSelected(t.Context(), "local", "custom", "2.0.0", func(_ Installed, files fs.FS) error {
			got, e := fs.ReadFile(files, "research/skills/optional/SKILL.md")
			if e != nil {
				return e
			}
			if string(got) != string(raw) {
				t.Fatal("optional local override skill missing")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(optional); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("../fixture/SKILL.md", optional); err != nil {
		t.Fatal(err)
	}
	starts := 0
	err = m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
		return a.WithSelected(t.Context(), "local", "custom", "2.0.0", func(Installed, fs.FS) error { starts++; return nil })
	})
	if err == nil || starts != 0 {
		t.Fatal("symlink inventory admitted for override closure", err, starts)
	}
}

func TestFetchedGuidanceOverrideCapturesOptionalInventory(t *testing.T) {
	fixture, server := newFixture(t, "1.0.0")
	profile := []byte(`{"version":1,"guidance":[{"id":"fixture","skill":"research/skills/fixture/SKILL.md"}],"override_fields":["guidance"]}`)
	optional := []byte("---\nname: optional\ndescription: Optional inventory guidance\n---\nAdvice.")
	fixture.mu.Lock()
	r := &fixture.catalogue.Releases[0]
	for _, entry := range []struct {
		name string
		raw  []byte
	}{{config.StackProfilePath, profile}, {"research/skills/optional/SKILL.md", optional}} {
		sum := sha256.Sum256(entry.raw)
		updated := false
		for i := range r.Files {
			if r.Files[i].Path == entry.name {
				r.Files[i].SHA256 = hex.EncodeToString(sum[:])
				updated = true
			}
		}
		if !updated {
			r.Files = append(r.Files, File{Path: entry.name, SHA256: hex.EncodeToString(sum[:])})
		}
		fixture.files["/teams/"+r.Download+"/"+entry.name] = entry.raw
	}
	r.Checksum = digest(r.Files)
	fixture.mu.Unlock()
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	installFixture(t, m, source, "1.0.0")
	err := m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
		return a.WithSelected(t.Context(), source, "example", "1.0.0", func(entry Installed, files fs.FS) error {
			if !entry.Integrity || entry.Editable {
				t.Fatal("fetched integrity lost")
			}
			got, e := fs.ReadFile(files, "research/skills/optional/SKILL.md")
			if e != nil {
				return e
			}
			if string(got) != string(optional) {
				t.Fatal("optional inventory not captured")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}
