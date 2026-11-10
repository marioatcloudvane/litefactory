package teambuild

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Every shipped team builds from teams/source, and the same source always
// builds the same bytes and checksum.
func TestEveryTeamSourceBuildsDeterministically(t *testing.T) {
	for _, team := range teams {
		t.Run(team, func(t *testing.T) {
			files, release, err := Build(t.Context(), repoSource(), team)
			if err != nil {
				t.Fatal(err)
			}
			if release.Slug != team || release.Checksum == "" || len(release.Files) != len(files) {
				t.Fatalf("release %s: slug %q, %d inventory entries for %d files", team, release.Slug, len(release.Files), len(files))
			}
			for _, name := range []string{"team.config", "stack-profile.json"} {
				if _, ok := files[name]; !ok {
					t.Fatalf("%s missing", name)
				}
			}
			for _, w := range Workflows {
				if _, ok := files[w+"/"+w+".dot"]; !ok {
					t.Fatalf("workflow %s missing", w)
				}
			}
			again, second, err := Build(t.Context(), repoSource(), team)
			if err != nil {
				t.Fatal(err)
			}
			if second.Checksum != release.Checksum || !reflect.DeepEqual(again, files) {
				t.Fatal("a second build of the same source differs")
			}
			// The build does not depend on where the source folder is.
			_, moved, err := Build(t.Context(), copySource(t), team)
			if err != nil {
				t.Fatal(err)
			}
			if moved.Checksum != release.Checksum {
				t.Fatal("build depends on the source folder location")
			}
		})
	}
}

func TestReleaseSlugMustMatchTheFolder(t *testing.T) {
	dir := copySource(t)
	p := filepath.Join(dir, "go-development", "release.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var release map[string]any
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatal(err)
	}
	release["slug"] = "other-team"
	release["download"] = "other-team/v1.0.0"
	raw, err = json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	put(t, p, raw)
	if _, _, err := Build(t.Context(), dir, "go-development"); err == nil || !strings.Contains(err.Error(), `slug must be "go-development"`) {
		t.Fatalf("mismatched slug accepted: %v", err)
	}
	if _, _, err := Build(t.Context(), dir, "missing-team"); err == nil {
		t.Fatal("missing team folder accepted")
	}
	if _, _, err := Build(t.Context(), "teams/source", "go-development"); err == nil {
		t.Fatal("relative source folder accepted")
	}
}

func TestBaseIsNotATeam(t *testing.T) {
	_, _, err := Build(t.Context(), repoSource(), "base-team")
	if err == nil || !strings.Contains(err.Error(), "base-team is a base, not a team") {
		t.Fatalf("base built as team: %v", err)
	}
}

// A child that only sets the base variables and adds its own stack files
// builds the current base inventory plus those files; base metadata is not
// installed, and each shared skill lands in every workflow.
func TestBaseTeamChildBuildsCurrentBaseInventory(t *testing.T) {
	dir := filepath.Join(canonicalTemp(t), "teams", "source")
	base := snapshot(t, filepath.Join(repoSource(), "base-team"))
	for name, raw := range base {
		put(t, filepath.Join(dir, "base-team", filepath.FromSlash(name)), raw)
	}
	own := snapshot(t, filepath.Join(repoSource(), "go-development"))
	for name, raw := range own {
		if name == "release.json" {
			raw = bytes.ReplaceAll(raw, []byte(`"go-development`), []byte(`"go-child`))
		}
		put(t, filepath.Join(dir, "go-child", filepath.FromSlash(name)), raw)
	}
	files, _, err := Build(t.Context(), dir, "go-child")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"team.config": true}
	for name := range base {
		if shared, ok := strings.CutPrefix(name, teamsource.SharedSkillsDir+"/"); ok {
			// A shared skill is installed into every workflow, not as itself.
			for _, w := range Workflows {
				want[w+"/skills/"+shared] = true
			}
			continue
		}
		if name != "base.json" {
			want[name] = true
		}
	}
	for name := range own {
		if strings.Contains(name, "/") || name == "stack-profile.json" {
			want[name] = true
		}
	}
	if len(files) != len(want) {
		t.Fatalf("built %d files, want %d", len(files), len(want))
	}
	for name := range want {
		if _, ok := files[name]; !ok {
			t.Errorf("%s missing from the build", name)
		}
	}
}

func TestInvalidSourceIsRefused(t *testing.T) {
	const team = "go-development"
	rules := team + "/implement/skills/litefactory-rules/"
	for _, tt := range []struct {
		name, cause string
		change      func(*testing.T, string)
	}{
		{"missing resource", "litefactory.md", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, rules, "litefactory.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe resource", "unsafe", func(t *testing.T, dir string) {
			p := filepath.Join(dir, rules, "SKILL.md")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("  - litefactory.md"), []byte("  - ../litefactory.md")))
		}},
		{"symlink", "symlink", func(t *testing.T, dir string) {
			p := filepath.Join(dir, rules, "litefactory.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("SKILL.md", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe download", "download", func(t *testing.T, dir string) {
			p := filepath.Join(dir, team, "release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte(team+"/v"+sourceVersion(t, team)), []byte("../outside")))
		}},
		{"foreign graph identity", "identity", func(t *testing.T, dir string) {
			p := filepath.Join(dir, team, "research/research.dot")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte(team), []byte("foreign")))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := flatFixture(t, team)
			tt.change(t, dir)
			_, _, err := Build(t.Context(), dir, team)
			if err == nil || !strings.Contains(err.Error(), tt.cause) {
				t.Fatalf("expected refusal naming %s: %v", tt.cause, err)
			}
		})
	}
	t.Run("invalid version", func(t *testing.T) {
		dir := flatFixture(t, team)
		p := filepath.Join(dir, team, "release.json")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		put(t, p, bytes.ReplaceAll(b, []byte(`"version": "`+sourceVersion(t, team)+`"`), []byte(`"version": "invalid"`)))
		files, release, err := Build(t.Context(), dir, team)
		if err == nil {
			_, _, err = newManager(t).InstallBuilt(t.Context(), identity(dir), files, release, teampackage.BuildInfo{})
		}
		if !errors.Is(err, teampackage.ErrInvalidPackage) || !strings.Contains(err.Error(), "SemVer") {
			t.Fatalf("invalid version installed: %v", err)
		}
	})
}

func TestCanonicalMigrationAndGraphContracts(t *testing.T) {
	root := teamsRepoRoot()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".claude", ".codex"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("former host root %s remains: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(repoSource(), "go-development", "shared")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("shared guidance tree remains: %v", err)
	}
}

func teamsRepoRoot() string { return filepath.Dir(filepath.Dir(repoSource())) }
